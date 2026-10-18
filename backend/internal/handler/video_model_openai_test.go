//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type openAIPlaygroundUserRepo struct{ service.UserRepository }

func (openAIPlaygroundUserRepo) GetByID(context.Context, int64) (*service.User, error) {
	return &service.User{ID: 7}, nil
}

type openAIPlaygroundGroupRepo struct{ service.GroupRepository }

func (openAIPlaygroundGroupRepo) ListActive(context.Context) ([]service.Group, error) {
	return []service.Group{{ID: 9, Platform: service.PlatformComposite, Status: service.StatusActive, AllowImageGeneration: true}}, nil
}

type openAIPlaygroundSubscriptionRepo struct {
	service.UserSubscriptionRepository
}

func (openAIPlaygroundSubscriptionRepo) ListActiveByUserID(context.Context, int64) ([]service.UserSubscription, error) {
	return nil, nil
}

type openAIPlaygroundAccountRepo struct{ service.AccountRepository }

func (openAIPlaygroundAccountRepo) ListByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	if platform != service.PlatformOpenAI {
		return nil, nil
	}
	return []service.Account{
		{ID: 1, Platform: platform, Type: service.AccountTypeAPIKey, Status: service.StatusActive, GroupIDs: []int64{9}, Credentials: map[string]any{"model_mapping": map[string]any{"openai/gpt-image-2": "openai/gpt-image-2", "gpt-6": "gpt-6"}}},
		{ID: 2, Platform: platform, Type: service.AccountTypeAPIKey, Status: service.StatusActive, GroupIDs: []int64{99}, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-image-2.5-flare": "gpt-image-2.5-flare"}}},
		{ID: 3, Platform: platform, Type: service.AccountTypeAPIKey, Status: service.StatusDisabled, GroupIDs: []int64{9}, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-image-2.5-sunburst": "gpt-image-2.5-sunburst"}}},
	}, nil
}

func TestVideoModelsListIncludesAccessibleOpenAIImagesWithoutVideoFlag(t *testing.T) {
	gin.SetMode(gin.TestMode)
	keys := service.NewAPIKeyService(nil, openAIPlaygroundUserRepo{}, openAIPlaygroundGroupRepo{}, openAIPlaygroundSubscriptionRepo{}, nil, nil, &config.Config{})
	introService, mock := newPlaygroundIntroService(t)
	for _, slug := range []string{"gpt-image-2", "openai/gpt-image-2", "openai/gpt-image-2/edit"} {
		expectPlaygroundIntro(t, mock, slug, &service.ModelIntro{
			ModelKey: slug, SchedulingEnabled: true, ResultType: "image", ResultField: "images[0].url",
			DefaultParams: map[string]any{"prompt": map[string]any{"value": "", "required": true}},
		})
	}
	h := NewVideoModelHandler(keys, openAIPlaygroundAccountRepo{}, nil, introService, nil, nil)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/user/video-models", nil)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
	h.List(c)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var response struct {
		Data struct {
			Items []videoModelItem `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Len(t, response.Data.Items, 3)
	bySlug := map[string]videoModelItem{}
	for _, item := range response.Data.Items {
		bySlug[item.Slug] = item
	}
	for _, slug := range []string{"gpt-image-2", "openai/gpt-image-2", "openai/gpt-image-2/edit"} {
		item, ok := bySlug[slug]
		require.True(t, ok, slug)
		require.True(t, item.Available)
		require.Equal(t, "/api/v1/model/"+slug, item.SubmitPath)
		require.NotNil(t, item.Intro)
		require.Equal(t, "image", item.Intro.ResultType)
		require.Contains(t, item.Intro.DefaultParams, "prompt")
	}
}
