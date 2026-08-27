//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/fal"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type modelAPINoAccountRepo struct {
	service.AccountRepository
	listCalls *int
}

func (r *modelAPINoAccountRepo) ListSchedulableByGroupIDAndPlatform(context.Context, int64, string) ([]service.Account, error) {
	if r.listCalls != nil {
		(*r.listCalls)++
	}
	return nil, nil
}

type modelAPIDisabledSettingRepo struct {
	service.SettingRepository
}

func (r *modelAPIDisabledSettingRepo) GetValue(context.Context, string) (string, error) {
	return "false", nil
}

func newModelAPITestGatewayService() *service.GatewayService {
	return newModelAPITestGatewayServiceWithRepo(&modelAPINoAccountRepo{})
}

func newModelAPITestGatewayServiceWithRepo(repo service.AccountRepository) *service.GatewayService {
	return service.NewGatewayService(
		repo, nil, nil, nil, nil, nil, nil, nil, &config.Config{}, nil, nil,
		&service.BillingService{},
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
}

func performModelAPISubmit(t *testing.T, handler *ModelAPIGatewayHandler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/v1/model/*path", func(c *gin.Context) {
		groupID := int64(9)
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
			ID:      17,
			GroupID: &groupID,
			Group: &service.Group{
				ID:                   groupID,
				Platform:             service.PlatformComposite,
				AllowImageGeneration: true,
			},
		})
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 23})
		handler.Native(c)
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestModelAPIGatewayKnownImageNeverFallsThroughToVideoValidation(t *testing.T) {
	handler := NewModelAPIGatewayHandler(newModelAPITestGatewayService(), nil, nil, nil, nil, nil)

	recorder := performModelAPISubmit(t, handler, "/api/v1/model/openai/gpt-image-2", `{"prompt":"studio photo"}`)

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), "no available image account")
	require.NotContains(t, recorder.Body.String(), "Missing 'resolution'")
}

func TestModelAPIGatewayCompositeEditRejectsNonURLImageParameter(t *testing.T) {
	handler := NewModelAPIGatewayHandler(newModelAPITestGatewayService(), nil, nil, nil, nil, nil)

	recorder := performModelAPISubmit(t, handler, "/api/v1/model/gpt-image-2/edit", `{"image_urls":["https://example.test/reference.png","data:image/png;base64,aW1hZ2U="]}`)

	require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
	require.JSONEq(t, `{
		"error": {
			"type": "invalid_request_error",
			"message": "invalid parameter 'image_urls[1]': must be a valid HTTP or HTTPS URL"
		}
	}`, recorder.Body.String())
}

func TestModelAPIGatewayVideoFeatureGateOnlyAppliesAfterMediaRouting(t *testing.T) {
	settingService := service.NewSettingService(&modelAPIDisabledSettingRepo{}, &config.Config{})
	handler := NewModelAPIGatewayHandler(newModelAPITestGatewayService(), nil, nil, nil, nil, settingService)

	recorder := performModelAPISubmit(t, handler, "/api/v1/model/bytedance/seedance-2.5/image-to-video", `{"resolution":"720p"}`)

	require.Equal(t, http.StatusForbidden, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), `"type":"feature_disabled"`)
}

func TestModelAPIGatewayExplicitVideoSkipsImageAccountProbe(t *testing.T) {
	listCalls := 0
	repo := &modelAPINoAccountRepo{listCalls: &listCalls}
	settingService := service.NewSettingService(&modelAPIDisabledSettingRepo{}, &config.Config{})
	handler := NewModelAPIGatewayHandler(newModelAPITestGatewayServiceWithRepo(repo), nil, nil, nil, nil, settingService)

	recorder := performModelAPISubmit(t, handler, "/api/v1/model/bytedance/seedance-2.5/text-to-video", `{"resolution":"720p"}`)

	require.Equal(t, http.StatusForbidden, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), `"type":"feature_disabled"`)
	require.Zero(t, listCalls, "explicit video requests must not query the image account pool")
}

func TestMediaFalStatusFromTaskMapsTerminalFailureToFailed(t *testing.T) {
	for _, status := range []string{
		service.AsyncMediaStatusFailed,
		service.AsyncMediaStatusRefunded,
		service.AsyncMediaStatusExpired,
	} {
		t.Run(status, func(t *testing.T) {
			require.Equal(t, fal.StatusFailed, imageStatusFromTask(&service.AsyncMediaTask{Status: status}))
		})
	}
}

func TestVideoFalStatusFromTaskMapsTerminalFailureToFailed(t *testing.T) {
	for _, status := range []string{
		service.AsyncVideoStatusFailed,
		service.AsyncVideoStatusRefunded,
		service.AsyncVideoStatusExpired,
		service.AsyncVideoStatusRefundFailed,
	} {
		t.Run(status, func(t *testing.T) {
			require.Equal(t, fal.StatusFailed, videoFalStatusFromTask(&service.AsyncVideoTask{Status: status}))
		})
	}
}

func TestModelAPIResultPayloadAddsAuthoritativeActualCostWithoutMutation(t *testing.T) {
	original := map[string]any{
		"video":       map[string]any{"url": "https://cdn.example.test/video.mp4"},
		"seed":        float64(42),
		"actual_cost": float64(999),
	}

	result := modelAPIResultPayload(original, 1.25)

	require.Equal(t, 1.25, result["actual_cost"])
	require.Equal(t, float64(999), original["actual_cost"])
	require.Equal(t, original["video"], result["video"])
}

func TestModelAPIStatusResponseIncludesActualCost(t *testing.T) {
	raw, err := json.Marshal(modelAPIStatusResponse{
		StatusResponse: fal.StatusResponse{Status: fal.StatusCompleted, RequestID: "request-1"},
		ActualCost:     1.25,
	})

	require.NoError(t, err)
	require.JSONEq(t, `{"status":"COMPLETED","request_id":"request-1","actual_cost":1.25}`, string(raw))
}

func TestWriteAsyncImageResultIncludesActualCost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	task := &service.AsyncMediaTask{
		FinalCost: 0.75,
		ImageURLs: []string{"https://cdn.example.test/image.png"},
	}

	writeAsyncImageResult(ctx, task)

	require.Equal(t, http.StatusOK, recorder.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, 0.75, body["actual_cost"])
	require.Len(t, body["images"], 1)
}
