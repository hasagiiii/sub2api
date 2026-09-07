//go:build unit

package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type geminiPlaygroundSelectionRepo struct {
	service.AccountRepository
	calls []string
}

type geminiPlaygroundHTTPUpstream struct {
	response []byte
	request  []byte
}

func (u *geminiPlaygroundHTTPUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.request, _ = io.ReadAll(req.Body)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"upstream-gemini-1"}},
		Body:       io.NopCloser(bytes.NewReader(u.response)),
	}, nil
}

func (u *geminiPlaygroundHTTPUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func (r *geminiPlaygroundSelectionRepo) ListSchedulableByGroupIDAndPlatforms(_ context.Context, _ int64, platforms []string) ([]service.Account, error) {
	r.calls = append(r.calls, platforms...)
	if len(platforms) == 1 && platforms[0] == service.PlatformAntigravity {
		return []service.Account{{
			ID: 12, Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth,
			Status: service.StatusActive, Schedulable: true,
		}}, nil
	}
	return nil, nil
}

const geminiPlaygroundTestPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="

func TestBuildGeminiPlaygroundRequestFromGenericPayload(t *testing.T) {
	request, err := buildGeminiPlaygroundRequest(context.Background(), "gemini-3.1-flash-image", service.PlatformGemini, service.AccountTypeAPIKey, map[string]any{
		"prompt":       "draw a lighthouse",
		"image_urls":   []any{"data:image/png;base64," + geminiPlaygroundTestPNG},
		"aspect_ratio": "16:9",
		"image_size":   "2k",
	}, nil)
	require.NoError(t, err)
	require.Equal(t, "2K", request.Input.Size)
	require.Equal(t, 1, request.Input.N)
	require.Equal(t, []string{"[inline image omitted]"}, request.RequestParameters["image_urls"])

	var payload map[string]any
	require.NoError(t, json.Unmarshal(request.Body, &payload))
	contents := payload["contents"].([]any)
	parts := contents[0].(map[string]any)["parts"].([]any)
	require.Equal(t, "draw a lighthouse", parts[0].(map[string]any)["text"])
	inline := parts[1].(map[string]any)["inlineData"].(map[string]any)
	require.Equal(t, "image/png", inline["mimeType"])
	require.Equal(t, geminiPlaygroundTestPNG, inline["data"])
	config := payload["generationConfig"].(map[string]any)
	require.Equal(t, []any{"TEXT", "IMAGE"}, config["responseModalities"])
	responseFormat := config["responseFormat"].(map[string]any)
	require.Equal(t, map[string]any{"aspectRatio": "16:9", "imageSize": "2K"}, responseFormat["image"])
	require.NotContains(t, config, "imageConfig")
}

func TestBuildGeminiPlaygroundRequestPreservesNativeContentsAndForcesImage(t *testing.T) {
	request, err := buildGeminiPlaygroundRequest(context.Background(), "gemini-3.1-flash-image", service.PlatformGemini, service.AccountTypeAPIKey, map[string]any{
		"contents": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": "native"}}}},
		"generationConfig": map[string]any{
			"responseModalities": []any{"TEXT"},
			"imageConfig":        map[string]any{"imageSize": "4K"},
		},
	}, nil)
	require.NoError(t, err)
	require.Equal(t, "4K", request.Input.Size)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(request.Body, &payload))
	config := payload["generationConfig"].(map[string]any)
	require.Equal(t, []any{"TEXT", "IMAGE"}, config["responseModalities"])
	require.Equal(t, "4K", config["responseFormat"].(map[string]any)["image"].(map[string]any)["imageSize"])
	require.NotContains(t, config, "imageConfig")
	require.NotNil(t, payload["contents"])
}

func TestBuildGemini25RequestOmitsUnsupportedImageSize(t *testing.T) {
	request, err := buildGeminiPlaygroundRequest(context.Background(), "gemini-2.5-flash-image", service.PlatformGemini, service.AccountTypeAPIKey, map[string]any{
		"prompt": "draw", "aspect_ratio": "16:9", "image_size": "4K",
	}, nil)
	require.NoError(t, err)
	require.Equal(t, "1K", request.Input.Size)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(request.Body, &payload))
	config := payload["generationConfig"].(map[string]any)
	imageConfig := config["responseFormat"].(map[string]any)["image"].(map[string]any)
	require.Equal(t, "16:9", imageConfig["aspectRatio"])
	require.NotContains(t, imageConfig, "imageSize")
	require.NotContains(t, config, "imageConfig")
	require.NotContains(t, request.RequestParameters, "image_size")
}

func TestBuildGeminiPlaygroundRequestUsesLegacyImageConfigForAntigravity(t *testing.T) {
	request, err := buildGeminiPlaygroundRequest(context.Background(), "gemini-3.1-flash-image", service.PlatformAntigravity, service.AccountTypeOAuth, map[string]any{
		"prompt": "draw", "aspect_ratio": "1:8", "image_size": "512",
	}, nil)
	require.NoError(t, err)
	require.Equal(t, "512", request.Input.Size)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(request.Body, &payload))
	config := payload["generationConfig"].(map[string]any)
	require.Equal(t, map[string]any{"aspectRatio": "1:8", "imageSize": "512"}, config["imageConfig"])
	require.NotContains(t, config, "responseFormat")
}

func TestBuildGeminiPlaygroundRequestEnforcesReferenceImageLimit(t *testing.T) {
	_, err := buildGeminiPlaygroundRequest(context.Background(), "gemini-2.5-flash-image", service.PlatformGemini, service.AccountTypeAPIKey, map[string]any{
		"prompt": "draw",
		"image_urls": []any{
			"data:image/png;base64," + geminiPlaygroundTestPNG,
			"data:image/png;base64," + geminiPlaygroundTestPNG,
			"data:image/png;base64," + geminiPlaygroundTestPNG,
			"data:image/png;base64," + geminiPlaygroundTestPNG,
		},
	}, nil)
	require.EqualError(t, err, "image_urls supports at most 3 reference images for gemini-2.5-flash-image")
}

func TestExtractGeminiInlineImages(t *testing.T) {
	body := []byte(`{"candidates":[{"content":{"parts":[{"text":"done"},{"inlineData":{"mimeType":"image/png","data":"` + geminiPlaygroundTestPNG + `"}}]}}]}`)
	images, text, err := extractGeminiInlineImages(body)
	require.NoError(t, err)
	require.Equal(t, "done", text)
	require.Len(t, images, 1)
	require.Equal(t, "image/png", images[0].MIMEType)
	want, err := base64.StdEncoding.DecodeString(geminiPlaygroundTestPNG)
	require.NoError(t, err)
	require.Equal(t, want, images[0].Data)
}

func TestGenerateGeminiImagesUsesNativeForwarderAndNormalizesResult(t *testing.T) {
	upstream := &geminiPlaygroundHTTPUpstream{response: []byte(`{"candidates":[{"content":{"parts":[{"text":"created"},{"inlineData":{"mimeType":"image/png","data":"` + geminiPlaygroundTestPNG + `"}}]}}]}`)}
	geminiService := service.NewGeminiMessagesCompatService(nil, nil, nil, nil, nil, nil, upstream, nil, &config.Config{})
	handler := &ModelAPIGatewayHandler{geminiService: geminiService}
	account := &service.Account{
		ID: 7, Platform: service.PlatformGemini, Type: service.AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "test-key"},
	}
	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"draw"}]}],"generationConfig":{"responseModalities":["TEXT","IMAGE"]}}`)
	gin.SetMode(gin.TestMode)
	original, _ := gin.CreateTestContext(httptest.NewRecorder())
	original.Request = httptest.NewRequest(http.MethodPost, "/api/v1/model/gemini-3.1-flash-image", bytes.NewReader(body))

	result, err := handler.generateGeminiImages(context.Background(), original, account, "gemini-3.1-flash-image", body)

	require.NoError(t, err)
	require.Equal(t, "upstream-gemini-1", result.RequestID)
	require.Len(t, result.ImageURLs, 1)
	require.Contains(t, result.ImageURLs[0], "data:image/png;base64,")
	require.Empty(t, result.COSURLs)
	require.Equal(t, []string{"1x1"}, result.ImageOutputSizes)
	require.Equal(t, "created", result.ResultPayload["text"])
	require.NotContains(t, string(mustGeminiJSON(result.ResultPayload)), "inlineData")
	require.JSONEq(t, string(body), string(upstream.request))
}

func TestGeminiImageModelsUseImageRoutingAndDefaultIntro(t *testing.T) {
	for _, model := range []string{"gemini-2.5-flash-image", "models/gemini-3.1-flash-image-preview"} {
		require.True(t, modelAPIIsKnownImageModel(model))
		require.True(t, isImagePlaygroundModel(model))
	}
	intro := defaultGeminiImageIntro("gemini-3.1-flash-image")
	require.Equal(t, "image", intro.ResultType)
	require.Equal(t, "images[0].url", intro.ResultField)
	require.Contains(t, intro.DefaultParams, "prompt")
	require.Contains(t, intro.DefaultParams, "image_urls")
	require.Contains(t, intro.DefaultParams, "image_size")
	require.NotContains(t, defaultGeminiImageIntro("gemini-2.5-flash-image").DefaultParams, "image_size")
	require.Equal(t, []string{"512", "1K", "2K"}, defaultGeminiImageIntro("gemini-3.1-flash-lite-image").DefaultParams["image_size"].(map[string]any)["options"])

	item := toMediaTaskItem(&service.AsyncMediaTask{RequestParameters: map[string]any{"prompt": "again"}})
	require.Equal(t, map[string]any{"prompt": "again"}, item.RequestPayload)
}

func TestVideoModelSlugsForGeminiAccountsOnlyExposeImageModels(t *testing.T) {
	gemini := &service.Account{Platform: service.PlatformGemini}
	require.Equal(t, []string{"gemini-2.5-flash-image", "gemini-3.1-flash-image", "gemini-3.1-flash-lite-image"}, videoModelSlugsForAccount(gemini))

	antigravity := &service.Account{Platform: service.PlatformAntigravity, Credentials: map[string]any{
		"model_mapping": map[string]any{
			"gemini-2.5-flash":       "gemini-2.5-flash",
			"gemini-2.5-flash-image": "gemini-2.5-flash-image",
		},
	}}
	require.Equal(t, []string{"gemini-2.5-flash-image"}, videoModelSlugsForAccount(antigravity))
}

func TestSelectGeminiImageAccountFallsBackAcrossCompositePlatforms(t *testing.T) {
	repo := &geminiPlaygroundSelectionRepo{}
	geminiService := service.NewGeminiMessagesCompatService(repo, nil, nil, nil, nil, nil, nil, nil, &config.Config{})
	handler := &ModelAPIGatewayHandler{geminiService: geminiService}
	groupID := int64(5)

	account, err := handler.selectGeminiImageAccount(context.Background(), &groupID, &service.Group{
		ID: groupID, Platform: service.PlatformComposite,
	}, "gemini-2.5-flash-image")

	require.NoError(t, err)
	require.Equal(t, int64(12), account.ID)
	require.Equal(t, []string{service.PlatformGemini, service.PlatformAntigravity, service.PlatformAntigravity}, repo.calls)
}

func mustGeminiJSON(value any) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return encoded
}
