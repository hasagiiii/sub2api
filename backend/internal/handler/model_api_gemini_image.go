package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/fal"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	_ "golang.org/x/image/webp"
)

const maxGeminiPlaygroundImageBytes = 32 << 20

type geminiPlaygroundRequest struct {
	Body              []byte
	Input             fal.ImageGenInput
	RequestParameters map[string]any
}

type geminiInlineImage struct {
	MIMEType string
	Data     []byte
}

func (h *ModelAPIGatewayHandler) selectGeminiImageAccount(
	ctx context.Context,
	groupID *int64,
	group *service.Group,
	model string,
) (*service.Account, error) {
	if h.geminiService == nil {
		return nil, service.ErrNoAvailableAccounts
	}
	if group == nil || group.Platform != service.PlatformComposite {
		return h.geminiService.SelectAccountForModel(ctx, groupID, "", model)
	}
	var lastErr error
	for _, platform := range []string{service.PlatformGemini, service.PlatformAntigravity} {
		routingGroup := *group
		routingGroup.Platform = platform
		routingGroup.Hydrated = true
		if routingGroup.Status == "" {
			routingGroup.Status = service.StatusActive
		}
		routed := context.WithValue(ctx, ctxkey.Group, &routingGroup)
		account, err := h.geminiService.SelectAccountForModel(routed, groupID, "", model)
		if err == nil && account != nil {
			return account, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = service.ErrNoAvailableAccounts
	}
	return nil, lastErr
}

func (h *ModelAPIGatewayHandler) nativeGeminiImageSubmit(
	c *gin.Context,
	apiKey *service.APIKey,
	subject middleware2.AuthSubject,
	reqLog *zap.Logger,
	model string,
	payload map[string]any,
	account *service.Account,
) {
	request, err := buildGeminiPlaygroundRequest(c.Request.Context(), model, account.Platform, account.Type, payload, h.cosService)
	if err != nil {
		h.jsonError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	billingType := service.BillingTypeBalance
	if subscription, _ := middleware2.GetSubscriptionFromContext(c); subscription != nil && apiKey.Group != nil && apiKey.Group.IsSubscriptionType() {
		billingType = service.BillingTypeSubscription
	}
	rateMultiplier := 1.0
	if h.imagesService != nil {
		rateMultiplier = h.imagesService.ResolveImageRateMultiplier(c.Request.Context(), subject.UserID, apiKey)
	}
	submitInput := &service.AsyncMediaSubmitInput{
		Account:           account,
		User:              apiKey.User,
		APIKeyID:          apiKey.ID,
		UserID:            subject.UserID,
		AccountID:         account.ID,
		GroupID:           apiKey.GroupID,
		Facade:            service.AsyncMediaFacadeGemini,
		InternalRequestID: modelAPIInternalRequestID(c),
		RequestedModel:    model,
		Input:             request.Input,
		RequestParameters: request.RequestParameters,
		BillingType:       billingType,
		RateMultiplier:    rateMultiplier,
		RateMultiplierSet: true,
		ClientIP:          c.ClientIP(),
		UserAgent:         c.GetHeader("User-Agent"),
		InboundEndpoint:   modelAPIInboundEndpoint(c),
	}
	task, err := h.mediaService.SubmitInline(c.Request.Context(), submitInput, func(ctx context.Context) (*service.AsyncMediaInlineResult, error) {
		return h.generateGeminiImages(ctx, c, account, model, request.Body)
	})
	if err != nil {
		reqLog.Warn("model_api.gemini_image_submit_failed", zap.String("platform", account.Platform), zap.Error(err))
		switch {
		case errors.Is(err, service.ErrInsufficientBalance):
			h.jsonError(c, http.StatusPaymentRequired, "insufficient_balance", "Insufficient balance to hold the estimated cost for this image task. Please recharge and try again.")
		case errors.Is(err, service.ErrAsyncMediaPricingMissing):
			h.jsonError(c, http.StatusServiceUnavailable, "pricing_unavailable", "Image model pricing is not configured for this group/channel; please contact the administrator")
		default:
			h.jsonError(c, http.StatusBadGateway, "api_error", publicImageSubmitFailure)
		}
		return
	}

	requestID := derefStringPtr(task.UpstreamRequestID)
	base := h.callbackBase(c, model, requestID)
	c.JSON(http.StatusOK, fal.SubmitResponse{
		RequestID: requestID, Status: modelAPIStatusProcessing,
		StatusURL: base, ResponseURL: base, CancelURL: base + "/cancel",
	})
}

func buildGeminiPlaygroundRequest(ctx context.Context, model, platform, accountType string, payload map[string]any, cos *service.COSImageTransferService) (*geminiPlaygroundRequest, error) {
	if payload == nil {
		return nil, errors.New("request body must be a JSON object")
	}
	imageSize := normalizeGeminiImageSize(stringValue(payload["image_size"]))
	imageSize = supportedGeminiImageSizeOrDefault(model, imageSize)
	requestParameters := map[string]any{"num_images": 1}
	if geminiModelSupportsImageSize(model) {
		requestParameters["image_size"] = imageSize
	}

	if _, native := payload["contents"]; native {
		nativePayload := cloneAnyMap(payload)
		ensureGeminiImageGenerationConfig(nativePayload)
		normalizeGeminiImageConfig(nativePayload, platform, accountType)
		if !geminiModelSupportsImageSize(model) {
			removeGeminiImageConfigValue(nativePayload, "imageSize")
		} else if nativeSize := geminiImageConfigValue(nativePayload, "imageSize"); nativeSize != "" {
			setGeminiImageConfigValue(nativePayload, platform, accountType, "imageSize", supportedGeminiImageSizeOrDefault(model, normalizeGeminiImageSize(nativeSize)))
		}
		body, err := json.Marshal(nativePayload)
		if err != nil {
			return nil, fmt.Errorf("encode Gemini request: %w", err)
		}
		requestParameters["native_contents"] = true
		if aspectRatio := geminiImageConfigValue(nativePayload, "aspectRatio"); aspectRatio != "" {
			requestParameters["aspect_ratio"] = aspectRatio
		}
		if nativeSize := geminiImageConfigValue(nativePayload, "imageSize"); nativeSize != "" {
			imageSize = strings.ToUpper(nativeSize)
			requestParameters["image_size"] = imageSize
		}
		return &geminiPlaygroundRequest{
			Body:              body,
			Input:             fal.ImageGenInput{Size: imageSize, N: 1},
			RequestParameters: requestParameters,
		}, nil
	}

	prompt := strings.TrimSpace(stringValue(payload["prompt"]))
	if prompt == "" {
		return nil, errors.New("prompt is required")
	}
	requestParameters["prompt"] = service.TruncateUsageRequestPrompt(prompt)
	parts := []any{map[string]any{"text": prompt}}
	imageURLs, err := stringSliceValue(payload["image_urls"])
	if err != nil {
		return nil, err
	}
	if maxImages := geminiMaxReferenceImages(model); len(imageURLs) > maxImages {
		return nil, fmt.Errorf("image_urls supports at most %d reference images for %s", maxImages, model)
	}
	if len(imageURLs) > 0 {
		requestParameters["image_urls"] = sanitizedGeminiImageURLs(imageURLs)
		for i, imageURL := range imageURLs {
			data, mimeType, downloadErr := loadGeminiReferenceImage(ctx, cos, imageURL)
			if downloadErr != nil {
				return nil, fmt.Errorf("image_urls[%d]: %w", i, downloadErr)
			}
			parts = append(parts, map[string]any{"inlineData": map[string]any{
				"mimeType": mimeType,
				"data":     base64.StdEncoding.EncodeToString(data),
			}})
		}
	}

	generationConfig := map[string]any{"responseModalities": []string{"TEXT", "IMAGE"}}
	imageConfig := map[string]any{}
	if aspectRatio := strings.TrimSpace(stringValue(payload["aspect_ratio"])); aspectRatio != "" {
		if !geminiStringOptionExists(geminiAspectRatioOptions(model), aspectRatio) {
			return nil, fmt.Errorf("unsupported aspect_ratio %q for %s", aspectRatio, model)
		}
		imageConfig["aspectRatio"] = aspectRatio
		requestParameters["aspect_ratio"] = aspectRatio
	}
	if imageSize != "" && geminiModelSupportsImageSize(model) {
		imageConfig["imageSize"] = imageSize
	}
	if len(imageConfig) > 0 {
		setGeminiImageConfig(generationConfig, imageConfig, platform, accountType)
	}
	nativePayload := map[string]any{
		"contents": []any{map[string]any{
			"role":  "user",
			"parts": parts,
		}},
		"generationConfig": generationConfig,
	}
	body, err := json.Marshal(nativePayload)
	if err != nil {
		return nil, fmt.Errorf("encode Gemini request: %w", err)
	}
	return &geminiPlaygroundRequest{
		Body:              body,
		Input:             fal.ImageGenInput{Size: imageSize, N: 1},
		RequestParameters: requestParameters,
	}, nil
}

func removeGeminiImageConfigValue(payload map[string]any, key string) {
	config, _ := payload["generationConfig"].(map[string]any)
	imageConfig, _ := config["imageConfig"].(map[string]any)
	if imageConfig != nil {
		delete(imageConfig, key)
	}
	responseFormat, _ := config["responseFormat"].(map[string]any)
	responseImage, _ := responseFormat["image"].(map[string]any)
	if responseImage != nil {
		delete(responseImage, key)
	}
}

func setGeminiImageConfigValue(payload map[string]any, platform, accountType, key, value string) {
	config, _ := payload["generationConfig"].(map[string]any)
	if config == nil {
		return
	}
	if geminiUsesLegacyImageConfig(platform, accountType) {
		imageConfig, _ := config["imageConfig"].(map[string]any)
		if imageConfig != nil {
			imageConfig[key] = value
		}
		return
	}
	responseFormat, _ := config["responseFormat"].(map[string]any)
	responseImage, _ := responseFormat["image"].(map[string]any)
	if responseImage != nil {
		responseImage[key] = value
	}
}

func geminiModelSupportsImageSize(model string) bool {
	normalized := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(model), "models/"))
	return !strings.HasPrefix(normalized, "gemini-2.5-flash-image")
}

func geminiImageSizeOptions(model string) []string {
	normalized := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(model), "models/"))
	switch {
	case strings.HasPrefix(normalized, "gemini-2.5-flash-image"):
		return nil
	case strings.HasPrefix(normalized, "gemini-3.1-flash-lite-image"):
		return []string{"512", "1K", "2K"}
	case strings.HasPrefix(normalized, "gemini-3.1-flash-image"):
		return []string{"512", "1K", "2K", "4K"}
	default:
		return []string{"1K", "2K", "4K"}
	}
}

func normalizeGeminiImageSize(value string) string {
	normalized := strings.ToUpper(strings.TrimSpace(value))
	if normalized == "0.5K" {
		return "512"
	}
	return normalized
}

func supportedGeminiImageSizeOrDefault(model, requested string) string {
	options := geminiImageSizeOptions(model)
	if len(options) == 0 {
		return "1K"
	}
	if requested == "" {
		return "1K"
	}
	for _, option := range options {
		if requested == option {
			return requested
		}
	}
	return "1K"
}

func geminiMaxReferenceImages(model string) int {
	normalized := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(model), "models/"))
	if strings.HasPrefix(normalized, "gemini-2.5-flash-image") {
		return 3
	}
	return 14
}

func geminiAspectRatioOptions(model string) []string {
	options := []string{"1:1", "2:3", "3:2", "3:4", "4:3", "4:5", "5:4", "9:16", "16:9", "21:9"}
	normalized := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(model), "models/"))
	if !strings.HasPrefix(normalized, "gemini-2.5-flash-image") {
		options = append(options, "1:4", "1:8", "4:1", "8:1")
	}
	return options
}

func geminiStringOptionExists(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func sanitizedGeminiImageURLs(urls []string) []string {
	out := make([]string, len(urls))
	for i, value := range urls {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), "data:image/") {
			out[i] = "[inline image omitted]"
		} else {
			out[i] = value
		}
	}
	return out
}

func ensureGeminiImageGenerationConfig(payload map[string]any) {
	config, _ := payload["generationConfig"].(map[string]any)
	if config == nil {
		config = map[string]any{}
		payload["generationConfig"] = config
	}
	hasImage := false
	switch modalities := config["responseModalities"].(type) {
	case []any:
		for _, modality := range modalities {
			if strings.EqualFold(strings.TrimSpace(stringValue(modality)), "IMAGE") {
				hasImage = true
				break
			}
		}
	case []string:
		for _, modality := range modalities {
			if strings.EqualFold(strings.TrimSpace(modality), "IMAGE") {
				hasImage = true
				break
			}
		}
	}
	if !hasImage {
		config["responseModalities"] = []string{"TEXT", "IMAGE"}
	}
}

func geminiImageConfigValue(payload map[string]any, key string) string {
	config, _ := payload["generationConfig"].(map[string]any)
	imageConfig, _ := config["imageConfig"].(map[string]any)
	if value := strings.TrimSpace(stringValue(imageConfig[key])); value != "" {
		return value
	}
	responseFormat, _ := config["responseFormat"].(map[string]any)
	responseImage, _ := responseFormat["image"].(map[string]any)
	return strings.TrimSpace(stringValue(responseImage[key]))
}

func normalizeGeminiImageConfig(payload map[string]any, platform, accountType string) {
	config, _ := payload["generationConfig"].(map[string]any)
	if config == nil {
		return
	}
	imageConfig := map[string]any{}
	for _, key := range []string{"aspectRatio", "imageSize"} {
		if value := geminiImageConfigValue(payload, key); value != "" {
			if key == "imageSize" {
				value = normalizeGeminiImageSize(value)
			}
			imageConfig[key] = value
		}
	}
	if len(imageConfig) > 0 {
		setGeminiImageConfig(config, imageConfig, platform, accountType)
	}
	if geminiUsesLegacyImageConfig(platform, accountType) {
		removeGeminiResponseImageConfig(config)
	} else {
		delete(config, "imageConfig")
	}
}

func setGeminiImageConfig(generationConfig, imageConfig map[string]any, platform, accountType string) {
	if geminiUsesLegacyImageConfig(platform, accountType) {
		generationConfig["imageConfig"] = imageConfig
		return
	}
	responseFormat, _ := generationConfig["responseFormat"].(map[string]any)
	if responseFormat == nil {
		responseFormat = map[string]any{}
		generationConfig["responseFormat"] = responseFormat
	}
	responseFormat["image"] = imageConfig
}

func geminiUsesLegacyImageConfig(platform, accountType string) bool {
	return (platform == service.PlatformAntigravity && accountType != service.AccountTypeAPIKey) ||
		accountType == service.AccountTypeOAuth || accountType == service.AccountTypeServiceAccount
}

func removeGeminiResponseImageConfig(generationConfig map[string]any) {
	responseFormat, _ := generationConfig["responseFormat"].(map[string]any)
	if responseFormat == nil {
		return
	}
	delete(responseFormat, "image")
	if len(responseFormat) == 0 {
		delete(generationConfig, "responseFormat")
	}
}

func loadGeminiReferenceImage(ctx context.Context, cos *service.COSImageTransferService, rawURL string) ([]byte, string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if strings.HasPrefix(strings.ToLower(rawURL), "data:image/") {
		data, mimeType, err := decodeGeminiDataURL(rawURL)
		return data, mimeType, err
	}
	if cos == nil {
		return nil, "", errors.New("reference image downloader is unavailable")
	}
	data, contentType, err := cos.DownloadUntrustedToBytes(ctx, rawURL, maxGeminiPlaygroundImageBytes)
	if err != nil {
		return nil, "", err
	}
	contentType = normalizeGeminiImageMIME(contentType, data)
	if contentType == "" {
		return nil, "", errors.New("URL did not return a supported image")
	}
	return data, contentType, nil
}

func decodeGeminiDataURL(value string) ([]byte, string, error) {
	header, encoded, ok := strings.Cut(value, ",")
	if !ok || !strings.HasSuffix(strings.ToLower(header), ";base64") {
		return nil, "", errors.New("invalid image data URL")
	}
	mimeType := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.ToLower(header), "data:"), ";base64"))
	if !strings.HasPrefix(mimeType, "image/") {
		return nil, "", errors.New("invalid image data URL MIME type")
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return nil, "", errors.New("invalid image data URL base64")
	}
	if len(data) == 0 || len(data) > maxGeminiPlaygroundImageBytes {
		return nil, "", errors.New("image data URL is empty or too large")
	}
	mimeType = normalizeGeminiImageMIME(mimeType, data)
	if mimeType == "" {
		return nil, "", errors.New("unsupported image data URL MIME type")
	}
	return data, mimeType, nil
}

func normalizeGeminiImageMIME(hint string, data []byte) string {
	hint = strings.ToLower(strings.TrimSpace(strings.Split(hint, ";")[0]))
	switch hint {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return hint
	}
	detected := strings.ToLower(strings.TrimSpace(strings.Split(http.DetectContentType(data), ";")[0]))
	switch detected {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return detected
	default:
		return ""
	}
}

func (h *ModelAPIGatewayHandler) generateGeminiImages(
	ctx context.Context,
	original *gin.Context,
	account *service.Account,
	model string,
	body []byte,
) (*service.AsyncMediaInlineResult, error) {
	recorder := httptest.NewRecorder()
	internal, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodPost, "/v1beta/models/"+model+":generateContent", bytes.NewReader(body)).WithContext(ctx)
	request.Header = original.Request.Header.Clone()
	request.Header.Set("Content-Type", "application/json")
	internal.Request = request

	var forwardResult *service.ForwardResult
	var err error
	if account.Platform == service.PlatformAntigravity && account.Type != service.AccountTypeAPIKey {
		forwardResult, err = h.geminiService.GetAntigravityGatewayService().ForwardGemini(
			ctx, internal, account, model, "generateContent", false, body, false,
		)
	} else {
		forwardResult, err = h.geminiService.ForwardNative(ctx, internal, account, model, "generateContent", false, body)
	}
	if err != nil {
		return nil, err
	}
	if recorder.Code < http.StatusOK || recorder.Code >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("gemini upstream returned HTTP %d", recorder.Code)
	}

	images, text, err := extractGeminiInlineImages(recorder.Body.Bytes())
	if err != nil {
		return nil, err
	}
	urls := make([]string, 0, len(images))
	cosURLs := make([]string, 0, len(images))
	sizes := make([]string, 0, len(images))
	metadata := make([]service.ImageOutputMetadata, 0, len(images))
	resultImages := make([]any, 0, len(images))
	for i, inline := range images {
		cfg, _, decodeErr := image.DecodeConfig(bytes.NewReader(inline.Data))
		if decodeErr != nil || cfg.Width <= 0 || cfg.Height <= 0 {
			return nil, fmt.Errorf("gemini returned invalid image data at index %d", i)
		}
		publicURL := "data:" + inline.MIMEType + ";base64," + base64.StdEncoding.EncodeToString(inline.Data)
		if h.cosService != nil {
			uploadedURL, uploadErr := h.cosService.UploadImageBytes(ctx, inline.Data, inline.MIMEType, fmt.Sprintf("gemini-%d%s", i+1, geminiImageExtension(inline.MIMEType)))
			if uploadErr != nil {
				return nil, fmt.Errorf("store Gemini image %d: %w", i, uploadErr)
			}
			if uploadedURL != "" {
				publicURL = uploadedURL
				cosURLs = append(cosURLs, uploadedURL)
			}
		}
		fileName := fmt.Sprintf("image-%d%s", i+1, geminiImageExtension(inline.MIMEType))
		meta := service.ImageOutputMetadata{
			URL: publicURL, ContentType: inline.MIMEType, FileName: fileName,
			FileSize: int64(len(inline.Data)), Width: cfg.Width, Height: cfg.Height,
		}
		urls = append(urls, publicURL)
		sizes = append(sizes, fmt.Sprintf("%dx%d", cfg.Width, cfg.Height))
		metadata = append(metadata, meta)
		resultImages = append(resultImages, map[string]any{
			"url": publicURL, "content_type": meta.ContentType, "file_name": meta.FileName,
			"file_size": meta.FileSize, "width": meta.Width, "height": meta.Height,
		})
	}
	if len(cosURLs) != len(urls) {
		cosURLs = nil
	}
	payload := map[string]any{"images": resultImages}
	if strings.TrimSpace(text) != "" {
		payload["text"] = text
	}
	requestID := strings.TrimSpace(recorder.Header().Get("x-request-id"))
	if forwardResult != nil && strings.TrimSpace(forwardResult.RequestID) != "" {
		requestID = strings.TrimSpace(forwardResult.RequestID)
	}
	return &service.AsyncMediaInlineResult{
		RequestID: requestID, ImageURLs: urls, COSURLs: cosURLs,
		ImageOutputSizes: sizes, ImageMetadata: metadata, ResultPayload: payload,
	}, nil
}

func extractGeminiInlineImages(body []byte) ([]geminiInlineImage, string, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, "", errors.New("gemini returned invalid JSON")
	}
	if wrapped, ok := payload["response"].(map[string]any); ok {
		payload = wrapped
	}
	candidates, _ := payload["candidates"].([]any)
	var images []geminiInlineImage
	var texts []string
	for _, candidateValue := range candidates {
		candidate, _ := candidateValue.(map[string]any)
		content, _ := candidate["content"].(map[string]any)
		parts, _ := content["parts"].([]any)
		for _, partValue := range parts {
			part, _ := partValue.(map[string]any)
			if text := strings.TrimSpace(stringValue(part["text"])); text != "" {
				texts = append(texts, text)
			}
			inline, _ := part["inlineData"].(map[string]any)
			if inline == nil {
				inline, _ = part["inline_data"].(map[string]any)
			}
			if inline == nil {
				continue
			}
			mimeType := stringValue(inline["mimeType"])
			if mimeType == "" {
				mimeType = stringValue(inline["mime_type"])
			}
			encoded := strings.TrimSpace(stringValue(inline["data"]))
			if encoded == "" {
				continue
			}
			data, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil || len(data) == 0 || len(data) > maxGeminiPlaygroundImageBytes {
				return nil, "", errors.New("gemini returned invalid image base64")
			}
			mimeType = normalizeGeminiImageMIME(mimeType, data)
			if mimeType == "" {
				continue
			}
			images = append(images, geminiInlineImage{MIMEType: mimeType, Data: data})
		}
	}
	if len(images) == 0 {
		return nil, strings.Join(texts, "\n"), errors.New("gemini returned no images")
	}
	return images, strings.Join(texts, "\n"), nil
}

func geminiImageExtension(mimeType string) string {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ".png"
	}
}

func stringValue(value any) string {
	valueString, _ := value.(string)
	return valueString
}

func stringSliceValue(value any) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	values, ok := value.([]any)
	if !ok {
		if typed, typedOK := value.([]string); typedOK {
			return append([]string(nil), typed...), nil
		}
		return nil, errors.New("image_urls must be an array of URLs")
	}
	out := make([]string, 0, len(values))
	for _, item := range values {
		urlValue := strings.TrimSpace(stringValue(item))
		if urlValue == "" {
			return nil, errors.New("image_urls must contain non-empty URL strings")
		}
		out = append(out, urlValue)
	}
	return out, nil
}

func cloneAnyMap(value map[string]any) map[string]any {
	encoded, _ := json.Marshal(value)
	var cloned map[string]any
	_ = json.Unmarshal(encoded, &cloned)
	return cloned
}
