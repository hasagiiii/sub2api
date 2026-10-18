//go:build unit

package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/tidwall/gjson"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestModelAPIOpenAIImageAliases(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, model := range []string{"gpt-image-2", "gpt-image-2.5-sunburst", "gpt-image-2.5-flare"} {
		for _, suffix := range []string{"", "/edit"} {
			slug := "openai/" + model + suffix
			t.Run(slug, func(t *testing.T) {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/models/"+slug, nil)
				payload := map[string]any{"prompt": "draw a lighthouse", "num_images": float64(2), "quality": "high", "image_size": map[string]any{"width": float64(1024), "height": float64(1536)}}
				if suffix != "" {
					payload["image_urls"] = []any{"https://example.test/reference.png"}
					payload["mask_url"] = "https://example.test/mask.png"
				}
				internal, body, parsed, err := buildModelAPIOpenAIImageRequest(c, slug, payload)
				require.NoError(t, err)
				require.Equal(t, model, parsed.Model)
				require.Equal(t, 2, parsed.N)
				require.Equal(t, "1024x1536", parsed.Size)
				require.Equal(t, suffix != "", parsed.IsEdits())
				require.Equal(t, parsed.Endpoint, internal.Request.URL.Path)
				var forwarded map[string]any
				require.NoError(t, json.Unmarshal(body, &forwarded))
				require.Equal(t, model, forwarded["model"])
				require.NotContains(t, forwarded, "num_images")
				require.NotContains(t, forwarded, "image_urls")
				require.NotContains(t, forwarded, "image_size")
				if suffix != "" {
					require.Equal(t, []string{"https://example.test/reference.png"}, parsed.InputImageURLs)
					require.Equal(t, "https://example.test/mask.png", parsed.MaskImageURL)
				}
				require.Contains(t, payload, "num_images", "do not mutate the caller payload")
			})
		}
	}
}

func TestModelAPIOpenAIEditRequiresReferenceImage(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/models/openai/gpt-image-2/edit", nil)
	_, _, _, err := buildModelAPIOpenAIImageRequest(c, "openai/gpt-image-2/edit", map[string]any{"prompt": "edit"})
	require.ErrorContains(t, err, "images[].image_url is required")
}

func TestModelAPIPluralCallbacks(t *testing.T) {
	for _, prefix := range []string{"/api/v1/model", "/api/v1/models"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "https://example.test"+prefix+"/openai/gpt-image-2/edit", nil)
		h := &ModelAPIGatewayHandler{}
		require.Equal(t, "https://example.test"+prefix+"/openai/gpt-image-2/edit/requests/task-1", h.callbackBase(c, "openai/gpt-image-2/edit", "task-1"))
	}
}

func TestModelAPIOpenAIImageModelRejectsOtherProviderAliases(t *testing.T) {
	for _, model := range []string{"other/gpt-image-2", "openai/gpt-image-2/unknown", "openai/gpt-5"} {
		actual, ok := service.ModelAPIOpenAIImageModel(model)
		require.False(t, ok)
		require.Equal(t, model, actual)
	}
}

type modelAPIOpenAIHTTP struct {
	path string
	body []byte
}

func (u *modelAPIOpenAIHTTP) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.path = req.URL.Path
	u.body, _ = io.ReadAll(req.Body)
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}, "X-Request-Id": []string{"openai-generated"}}, Request: req, Body: io.NopCloser(bytes.NewBufferString(`{"data":[{"b64_json":"` + geminiPlaygroundTestPNG + `"}]}`))}, nil
}
func (u *modelAPIOpenAIHTTP) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, id, concurrency)
}

func TestModelAPIOpenAIForwardCanonicalImages(t *testing.T) {
	for _, model := range []string{"gpt-image-2", "gpt-image-2.5-sunburst", "gpt-image-2.5-flare"} {
		for _, suffix := range []string{"", "/edit"} {
			t.Run(model+suffix, func(t *testing.T) {
				cfg := &config.Config{RunMode: config.RunModeSimple}
				account := service.Account{ID: 901, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "test", "model_mapping": map[string]any{model: model}}}
				repo := &openAIWSFailoverHandlerAccountRepoStub{accounts: []service.Account{account}}
				upstream := &modelAPIOpenAIHTTP{}
				images := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil, nil)
				h := &ModelAPIGatewayHandler{imagesService: images}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/models/openai/"+model+suffix, nil)
				payload := map[string]any{"prompt": "draw"}
				endpoint := "/v1/images/generations"
				if suffix != "" {
					payload["image_urls"] = []any{"https://example.test/reference.png"}
					endpoint = "/v1/images/edits"
				}
				internal, body, parsed, err := buildModelAPIOpenAIImageRequest(c, "openai/"+model+suffix, payload)
				require.NoError(t, err)
				result, err := h.generateModelAPIOpenAIImages(context.Background(), internal, &account, body, parsed)
				require.NoError(t, err)
				require.Equal(t, endpoint, upstream.path)
				require.Equal(t, model, gjson.GetBytes(upstream.body, "model").String())
				require.Len(t, result.ImageURLs, 1)
				require.Equal(t, "data:image/png;base64,"+geminiPlaygroundTestPNG, result.ImageURLs[0])
				require.Equal(t, "openai-generated", result.RequestID)
				require.Equal(t, 1, result.ImageMetadata[0].Width)
				require.Equal(t, "1x1", result.ImageOutputSizes[0])
				require.Contains(t, result.ResultPayload, "images")
			})
		}
	}
}

type modelAPIOpenAIBatchHTTP struct {
	calls        atomic.Int32
	invalidCount atomic.Bool
}

func (u *modelAPIOpenAIBatchHTTP) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	count := u.calls.Add(1)
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	if gjson.GetBytes(body, "n").Int() > 1 {
		u.invalidCount.Store(true)
	}
	status := http.StatusOK
	response := `{"data":[{"b64_json":"` + geminiPlaygroundTestPNG + `"}]}`
	if count == 2 {
		status = http.StatusBadRequest
		response = `{"error":{"type":"invalid_request_error","message":"image rejected"}}`
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}, "X-Request-Id": {"batch-generated"}}, Request: req, Body: io.NopCloser(bytes.NewBufferString(response))}, nil
}
func (u *modelAPIOpenAIBatchHTTP) DoWithTLS(req *http.Request, p string, id int64, n int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, p, id, n)
}

func TestModelAPIOpenAIBatchPreservesPartialFailureMetadata(t *testing.T) {
	cfg := &config.Config{RunMode: config.RunModeSimple}
	account := service.Account{ID: 901, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"access_token": "test", "chatgpt_account_id": "test-account"}}
	repo := &openAIWSFailoverHandlerAccountRepoStub{accounts: []service.Account{account}}
	upstream := &modelAPIOpenAIBatchHTTP{}
	images := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	h := &ModelAPIGatewayHandler{imagesService: images}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/models/openai/gpt-image-2", nil)
	internal, body, parsed, err := buildModelAPIOpenAIImageRequest(c, "openai/gpt-image-2", map[string]any{"prompt": "draw", "num_images": 3})
	require.NoError(t, err)
	result, err := h.generateModelAPIOpenAIImages(context.Background(), internal, &account, body, parsed)
	require.NoError(t, err)
	require.Equal(t, int32(3), upstream.calls.Load())
	require.False(t, upstream.invalidCount.Load())
	require.Len(t, result.ImageURLs, 2)
	require.Equal(t, 3, result.ResultPayload["requested_images"])
	require.Equal(t, 2, result.ResultPayload["generated_images"])
	raw, err := json.Marshal(result.ResultPayload)
	require.NoError(t, err)
	require.Len(t, gjson.GetBytes(raw, "errors").Array(), 1)
}
