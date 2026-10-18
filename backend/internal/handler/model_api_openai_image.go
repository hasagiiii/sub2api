package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/fal"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Translate only the unified facade's transport fields. Keep /edit as an
// operation while sending the canonical model name to the OpenAI image API.
func buildModelAPIOpenAIImageRequest(original *gin.Context, model string, payload map[string]any) (*gin.Context, []byte, *service.OpenAIImagesRequest, error) {
	if payload == nil {
		return nil, nil, nil, errors.New("request body must be a JSON object")
	}
	canonical, _ := service.ModelAPIOpenAIImageModel(model)
	native := cloneAnyMap(payload)
	native["model"] = canonical
	native["stream"] = false
	native["response_format"] = "b64_json"
	if _, exists := native["n"]; !exists {
		if count, exists := native["num_images"]; exists {
			native["n"] = count
		}
	}
	if _, exists := native["size"]; !exists {
		if size := fal.MapSizeFromFal(native["image_size"]); size != "" {
			native["size"] = size
		}
	}
	urls, err := stringSliceValue(native["image_urls"])
	if err != nil {
		return nil, nil, nil, err
	}
	if len(urls) > 0 {
		images := make([]any, 0, len(urls))
		for _, url := range urls {
			images = append(images, map[string]any{"image_url": url})
		}
		native["images"] = images
	}
	if mask := strings.TrimSpace(stringValue(native["mask_url"])); mask != "" {
		native["mask"] = map[string]any{"image_url": mask}
	}
	endpoint := "/v1/images/generations"
	if modelAPIImageAPI(model) == service.FalAPIEdit || native["images"] != nil || native["mask"] != nil {
		endpoint = "/v1/images/edits"
	}
	for _, key := range []string{"num_images", "image_size", "image_urls", "mask_url", "sync_mode"} {
		delete(native, key)
	}
	body, err := json.Marshal(native)
	if err != nil {
		return nil, nil, nil, err
	}
	internal, _ := gin.CreateTestContext(httptest.NewRecorder())
	internal.Keys = original.Copy().Keys
	internal.Request = httptest.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body)).WithContext(original.Request.Context())
	internal.Request.Header = original.Request.Header.Clone()
	internal.Request.Header.Set("Content-Type", "application/json")
	parsed, err := (&service.OpenAIGatewayService{}).ParseOpenAIImagesRequest(internal, body)
	if err != nil {
		return nil, nil, nil, err
	}
	if parsed.Prompt == "" {
		return nil, nil, nil, errors.New("prompt is required")
	}
	return internal, body, parsed, nil
}

func (h *ModelAPIGatewayHandler) nativeOpenAIImageSubmit(c *gin.Context, apiKey *service.APIKey, subject middleware2.AuthSubject, reqLog *zap.Logger, model string, payload map[string]any, account *service.Account) {
	if h.imagesService == nil || h.mediaService == nil {
		h.jsonError(c, http.StatusServiceUnavailable, "api_error", "OpenAI image generation is unavailable")
		return
	}
	_, _, parsed, err := buildModelAPIOpenAIImageRequest(c, model, payload)
	if err != nil {
		h.jsonError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	if !account.SupportsOpenAIImageCapability(parsed.RequiredCapability) {
		h.jsonError(c, http.StatusServiceUnavailable, "api_error", "No available OpenAI account supports these image options")
		return
	}
	billingType := service.BillingTypeBalance
	if subscription, _ := middleware2.GetSubscriptionFromContext(c); subscription != nil && apiKey.Group != nil && apiKey.Group.IsSubscriptionType() {
		billingType = service.BillingTypeSubscription
	}
	in := &service.AsyncMediaSubmitInput{
		Account: account, User: apiKey.User, APIKeyID: apiKey.ID, UserID: subject.UserID,
		AccountID: account.ID, GroupID: apiKey.GroupID, Facade: service.AsyncMediaFacadeOpenAI,
		InternalRequestID: modelAPIInternalRequestID(c), RequestedModel: model,
		Input:             fal.ImageGenInput{Prompt: parsed.Prompt, Size: parsed.Size, Quality: parsed.Quality, N: parsed.N, OutputFormat: parsed.OutputFormat, ImageURLs: parsed.InputImageURLs, MaskURL: parsed.MaskImageURL, IsEdit: parsed.IsEdits()},
		RequestParameters: map[string]any{"prompt": service.TruncateUsageRequestPrompt(parsed.Prompt), "num_images": parsed.N, "image_size": parsed.Size, "quality": parsed.Quality, "output_format": parsed.OutputFormat, "image_urls": sanitizedGeminiImageURLs(parsed.InputImageURLs)},
		BillingType:       billingType, RateMultiplier: h.imagesService.ResolveImageRateMultiplier(c.Request.Context(), subject.UserID, apiKey), RateMultiplierSet: true,
		ClientIP: c.ClientIP(), UserAgent: c.GetHeader("User-Agent"), InboundEndpoint: modelAPIInboundEndpoint(c),
	}
	task, err := h.mediaService.SubmitOpenAIImageTask(c.Request.Context(), in, payload)
	if err != nil {
		reqLog.Warn("model_api.openai_image_submit_failed", zap.Error(err))
		switch {
		case errors.Is(err, service.ErrInlineImageConflict):
			h.jsonError(c, http.StatusConflict, "conflict_error", err.Error())
		case errors.Is(err, service.ErrInsufficientBalance):
			h.jsonError(c, http.StatusPaymentRequired, "insufficient_balance", "Insufficient balance to hold the estimated cost for this image task. Please recharge and try again.")
		case errors.Is(err, service.ErrAsyncMediaPricingMissing):
			h.jsonError(c, http.StatusServiceUnavailable, "pricing_unavailable", "Image model pricing is not configured for this group/channel; please contact the administrator")
		default:
			h.jsonError(c, http.StatusBadGateway, "api_error", publicImageSubmitFailure)
		}
		return
	}
	requestID := task.InternalRequestID
	base := h.callbackBase(c, model, requestID)
	c.JSON(http.StatusOK, fal.SubmitResponse{RequestID: requestID, Status: modelAPIStatusProcessing, StatusURL: base, ResponseURL: base, CancelURL: base + "/cancel"})
}

func (h *ModelAPIGatewayHandler) generateModelAPIOpenAIImages(ctx context.Context, internal *gin.Context, account *service.Account, body []byte, parsed *service.OpenAIImagesRequest) (*service.AsyncMediaInlineResult, error) {
	recorder := httptest.NewRecorder()
	output, _ := gin.CreateTestContext(recorder)
	output.Keys = internal.Copy().Keys
	output.Request = internal.Request.Clone(ctx)
	forwarded, err := h.imagesService.ForwardQueuedImages(ctx, output, account, body, parsed)
	if err != nil {
		return nil, err
	}
	if recorder.Code < 200 || recorder.Code >= 300 {
		return nil, fmt.Errorf("OpenAI images returned HTTP %d", recorder.Code)
	}
	var response struct {
		fal.OpenAIImagesResponse
		RequestedImages *int            `json:"requested_images"`
		GeneratedImages *int            `json:"generated_images"`
		Errors          json.RawMessage `json:"errors"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		return nil, fmt.Errorf("decode OpenAI images: %w", err)
	}
	result := &service.AsyncMediaInlineResult{}
	if forwarded != nil {
		result.RequestID = forwarded.RequestID
	}
	for i, item := range response.Data {
		if item.B64JSON != "" {
			data, err := base64.StdEncoding.DecodeString(item.B64JSON)
			if err != nil {
				return nil, fmt.Errorf("decode OpenAI image %d: %w", i, err)
			}
			cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
			if err != nil {
				return nil, fmt.Errorf("invalid OpenAI image %d: %w", i, err)
			}
			item.ContentType = "image/" + format
			item.Width, item.Height, item.FileSize = cfg.Width, cfg.Height, int64(len(data))
			item.URL = "data:" + item.ContentType + ";base64," + item.B64JSON
		}
		if strings.TrimSpace(item.URL) == "" {
			continue
		}
		item.FileName = imageFileName(item.URL, i, item.ContentType)
		result.ImageURLs = append(result.ImageURLs, item.URL)
		size := ""
		if item.Width > 0 && item.Height > 0 {
			size = fmt.Sprintf("%dx%d", item.Width, item.Height)
		}
		result.ImageOutputSizes = append(result.ImageOutputSizes, size)
		result.ImageMetadata = append(result.ImageMetadata, service.ImageOutputMetadata{URL: item.URL, ContentType: item.ContentType, FileName: item.FileName, FileSize: item.FileSize, Width: item.Width, Height: item.Height})
		response.Data[i] = item
	}
	if len(result.ImageURLs) == 0 {
		return nil, errors.New("OpenAI returned no images")
	}
	if len(result.COSURLs) != len(result.ImageURLs) {
		result.COSURLs = nil
	}
	encoded, err := json.Marshal(fal.OpenAIResponseToFal(&response.OpenAIImagesResponse))
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(encoded, &result.ResultPayload); err != nil {
		return nil, err
	}
	if response.RequestedImages != nil {
		result.ResultPayload["requested_images"] = *response.RequestedImages
	}
	if response.GeneratedImages != nil {
		result.ResultPayload["generated_images"] = *response.GeneratedImages
	}
	if len(response.Errors) > 0 {
		result.ResultPayload["errors"] = response.Errors
	}
	return result, nil
}

// Rebuild request-local state from durable task data; never retain a live gin.Context.
func (h *ModelAPIGatewayHandler) generateQueuedOpenAIImages(ctx context.Context, task *service.AsyncMediaTask, account *service.Account, key *service.APIKey, payload map[string]any) (*service.AsyncMediaInlineResult, error) {
	original, _ := gin.CreateTestContext(httptest.NewRecorder())
	original.Request = httptest.NewRequest(http.MethodPost, "/", nil).WithContext(ctx)
	original.Request.Header.Set("User-Agent", derefStringPtr(task.UserAgent))
	original.Request.Header.Set("x-app", derefStringPtr(task.XApp))
	original.Set("api_key", key)
	internal, body, parsed, err := buildModelAPIOpenAIImageRequest(original, task.RequestedModel, payload)
	if err != nil {
		return nil, err
	}
	return h.generateModelAPIOpenAIImages(ctx, internal, account, body, parsed)
}
