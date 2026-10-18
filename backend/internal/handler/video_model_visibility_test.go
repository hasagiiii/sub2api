//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

var playgroundIntroColumns = []string{
	"model_key", "title", "description", "description_en", "cover_url", "default_params",
	"sort_order", "scheduling_enabled", "output_fields", "result_field", "result_type", "created_at", "updated_at",
}

func newPlaygroundIntroService(t *testing.T) (*service.ModelIntroService, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	mock.MatchExpectationsInOrder(false)
	t.Cleanup(func() { require.NoError(t, mock.ExpectationsWereMet()); _ = db.Close() })
	return service.NewModelIntroService(db), mock
}

func expectPlaygroundIntro(t *testing.T, mock sqlmock.Sqlmock, slug string, intro *service.ModelIntro) {
	t.Helper()
	rows := sqlmock.NewRows(playgroundIntroColumns)
	if intro != nil {
		params, err := json.Marshal(intro.DefaultParams)
		require.NoError(t, err)
		outputs, err := json.Marshal(intro.OutputFields)
		require.NoError(t, err)
		at := time.Unix(0, 0).UTC()
		rows.AddRow(slug, intro.Title, intro.Description, intro.DescriptionEn, intro.CoverURL, params, intro.SortOrder, intro.SchedulingEnabled, outputs, intro.ResultField, intro.ResultType, at, at)
	}
	mock.ExpectQuery("SELECT model_key").WithArgs(slug).WillReturnRows(rows)
}

type playgroundVisibilityAccountRepo struct {
	service.AccountRepository
	accounts []service.Account
}

func (r playgroundVisibilityAccountRepo) ListByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	var result []service.Account
	for _, account := range r.accounts {
		if account.Platform == platform {
			result = append(result, account)
		}
	}
	return result, nil
}

func listPlaygroundForVisibility(t *testing.T, accounts service.AccountRepository, intros *service.ModelIntroService) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	keys := service.NewAPIKeyService(nil, openAIPlaygroundUserRepo{}, openAIPlaygroundGroupRepo{}, openAIPlaygroundSubscriptionRepo{}, nil, nil, &config.Config{})
	h := NewVideoModelHandler(keys, accounts, nil, intros, nil, nil)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/user/video-models", nil)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
	h.List(c)
	return rec
}

func playgroundVisibilityItems(t *testing.T, rec *httptest.ResponseRecorder) []videoModelItem {
	t.Helper()
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var response struct {
		Data struct {
			Items []videoModelItem `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	return response.Data.Items
}

func TestPlaygroundCatalogRequiresEnabledIntroForEveryExactAlias(t *testing.T) {
	intros, mock := newPlaygroundIntroService(t)
	account := service.Account{ID: 1, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, GroupIDs: []int64{9}}
	for _, slug := range service.ModelAPIOpenAIImageSlugs(&account) {
		var intro *service.ModelIntro
		switch slug {
		case "gpt-image-2":
			intro = &service.ModelIntro{ModelKey: slug, SchedulingEnabled: true, Title: "Configured model", ResultType: "image"}
		case "openai/gpt-image-2.5-flare":
			intro = &service.ModelIntro{ModelKey: slug, SchedulingEnabled: false}
		}
		expectPlaygroundIntro(t, mock, slug, intro)
	}
	duplicateAccount := account
	duplicateAccount.ID = 2
	// A single lookup per exact slug, including absent and disabled entries,
	// even when multiple accessible accounts support the same model.
	items := playgroundVisibilityItems(t, listPlaygroundForVisibility(t, playgroundVisibilityAccountRepo{accounts: []service.Account{account, duplicateAccount}}, intros))
	require.Len(t, items, 1)
	require.Equal(t, "gpt-image-2", items[0].Slug)
	require.Equal(t, "Configured model", items[0].Intro.Title)
	// No introduction for prefixed/edit aliases or sunburst: none may be invented.
}

func TestPlaygroundCatalogRequiresIntroductionsAcrossProviders(t *testing.T) {
	for _, platform := range []string{service.PlatformGemini, service.PlatformAntigravity, service.PlatformFal, service.PlatformBytedance, service.PlatformAtlasCloud, service.PlatformApiz, service.PlatformHiggsfield} {
		t.Run(platform, func(t *testing.T) {
			intros, mock := newPlaygroundIntroService(t)
			model := "bytedance/seedance-2.5/text-to-video"
			if platform == service.PlatformGemini || platform == service.PlatformAntigravity {
				model = "gemini-3.1-flash-image"
			}
			account := service.Account{ID: 1, Platform: platform, Type: service.AccountTypeAPIKey, Status: service.StatusActive, GroupIDs: []int64{9}, Extra: map[string]any{"video_models_enabled": true}, Credentials: map[string]any{"model_mapping": map[string]any{model: model}}}
			slugs := videoModelSlugsForAccount(&account)
			require.NotEmpty(t, slugs)
			for _, slug := range slugs {
				expectPlaygroundIntro(t, mock, slug, nil)
			}
			require.Empty(t, playgroundVisibilityItems(t, listPlaygroundForVisibility(t, playgroundVisibilityAccountRepo{accounts: []service.Account{account}}, intros)))
		})
	}
}

func TestPlaygroundCatalogPreservesConfiguredFormWithoutGeneratedDefaults(t *testing.T) {
	intros, mock := newPlaygroundIntroService(t)
	configured := &service.ModelIntro{ModelKey: "openai/gpt-image-2", Title: "Admin title", SchedulingEnabled: true, DefaultParams: map[string]any{"prompt": map[string]any{"value": "configured prompt"}}, ResultType: "image", ResultField: "images[0].url"}
	expectPlaygroundIntro(t, mock, "gpt-image-2", nil)
	expectPlaygroundIntro(t, mock, "openai/gpt-image-2", configured)
	expectPlaygroundIntro(t, mock, "openai/gpt-image-2/edit", nil)
	items := playgroundVisibilityItems(t, listPlaygroundForVisibility(t, openAIPlaygroundAccountRepo{}, intros))
	require.Len(t, items, 1)
	require.Equal(t, configured.DefaultParams, items[0].Intro.DefaultParams)
	require.Equal(t, configured.ResultType, items[0].Intro.ResultType)
	require.Equal(t, configured.ResultField, items[0].Intro.ResultField)
	require.Empty(t, items[0].Intro.OutputFields)
}

func TestPlaygroundCatalogDoesNotFallBackWhenIntroductionsUnavailable(t *testing.T) {
	t.Run("dependency missing", func(t *testing.T) {
		rec := listPlaygroundForVisibility(t, openAIPlaygroundAccountRepo{}, nil)
		require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	})
	t.Run("database read fails", func(t *testing.T) {
		intros, mock := newPlaygroundIntroService(t)
		mock.ExpectQuery("SELECT model_key").WithArgs("gpt-image-2").WillReturnError(errors.New("database unavailable"))
		rec := listPlaygroundForVisibility(t, openAIPlaygroundAccountRepo{}, intros)
		require.Equal(t, http.StatusInternalServerError, rec.Code)
		require.NotContains(t, rec.Body.String(), `"items"`)
		require.NotContains(t, rec.Body.String(), "database unavailable")
	})
}
