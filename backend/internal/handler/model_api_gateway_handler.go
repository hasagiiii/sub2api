package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/fal"
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ModelAPIGatewayHandler handles the protocol-neutral asynchronous model API.
// It selects an eligible provider before applying media-specific validation:
// image tasks use FAL/Leonardo, while video tasks use FAL/AtlasCloud/APIZ.
//
// 路径形态：
//
//	POST /api/v1/model/{slug}                         -> submit
//	POST /api/v1/model/estimate_pricing               -> batch price estimate
//	GET  /api/v1/model/{slug}/requests/{id}           -> status or result
//	PUT  /api/v1/model/{slug}/requests/{id}/cancel    -> cancel
type ModelAPIGatewayHandler struct {
	gatewayService *service.GatewayService
	imagesService  *service.OpenAIGatewayService
	accountService *service.AccountService
	mediaService   *service.AsyncMediaService
	videoService   *service.AsyncVideoService
	settingService *service.SettingService
}

type modelAPIStatusResponse struct {
	fal.StatusResponse
	ActualCost float64                `json:"actual_cost"`
	Error      *modelAPIResponseError `json:"error,omitempty"`
}

type modelAPIResponseError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

const (
	modelAPIStatusProcessing = fal.StatusInQueue
	modelAPIStatusCompleted  = fal.StatusCompleted
	modelAPIStatusFailed     = fal.StatusFailed
)

const modelAPIUpstreamContextKey = "model_api_upstream"

// NewModelAPIGatewayHandler creates the shared asynchronous model facade.
func NewModelAPIGatewayHandler(
	gatewayService *service.GatewayService,
	imagesService *service.OpenAIGatewayService,
	accountService *service.AccountService,
	mediaService *service.AsyncMediaService,
	videoService *service.AsyncVideoService,
	settingService *service.SettingService,
) *ModelAPIGatewayHandler {
	return &ModelAPIGatewayHandler{
		gatewayService: gatewayService,
		imagesService:  imagesService,
		accountService: accountService,
		mediaService:   mediaService,
		videoService:   videoService,
		settingService: settingService,
	}
}

func (h *ModelAPIGatewayHandler) jsonError(c *gin.Context, status int, errType, message string) {
	response := gin.H{
		"error": gin.H{
			"type":    errType,
			"message": message,
		},
	}
	if c.Request != nil && c.Request.Method == http.MethodGet {
		logModelAPIClientResponse(c, modelAPIUpstreamFromContext(c), status, response)
	}
	c.JSON(status, response)
}

// Native is the /api/v1/model/*path entry point.
func (h *ModelAPIGatewayHandler) Native(c *gin.Context) {
	path := strings.Trim(c.Param("path"), "/")
	method := c.Request.Method

	switch {
	case method == http.MethodPost && path == "estimate_pricing":
		h.estimatePricingBatch(c)
	case method == http.MethodPost && strings.HasSuffix(path, "/estimate_pricing"):
		h.estimatePricing(c, path)
	case method == http.MethodPut && strings.HasSuffix(path, "/cancel"):
		h.nativeCancel(c, modelAPIRequestIDFromPath(path))
	case method == http.MethodGet && strings.Contains(path, "/requests/"):
		h.nativeResult(c, modelAPIRequestIDFromPath(path), false)
	case method == http.MethodPost:
		h.nativeSubmit(c, path)
	default:
		h.jsonError(c, http.StatusNotFound, "not_found_error", "Unsupported model endpoint")
	}
}

func (h *ModelAPIGatewayHandler) nativeSubmit(c *gin.Context, model string) {
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.jsonError(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.jsonError(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	reqLog := logger.FromContext(c.Request.Context()).With(
		zap.String("component", "handler.model_api.native_submit"),
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.String("model", model),
	)

	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil || len(body) == 0 {
		h.jsonError(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		h.jsonError(c, http.StatusBadRequest, "invalid_request_error", "Invalid JSON body")
		return
	}
	if apiKey.Group != nil && apiKey.Group.Platform == service.PlatformComposite && strings.EqualFold(modelAPIImageAPI(model), service.FalAPIEdit) {
		if err := validateCompositeEditURLs(payload); err != nil {
			h.jsonError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
			return
		}
	}

	// Probe the asynchronous image pool first. Pricing mode and account model
	// mappings keep video-only endpoints out of this pool. Known image models
	// remain image requests even when every image account is disabled.
	knownImageModel := modelAPIIsKnownImageModel(model)
	if knownImageModel && !service.GroupAllowsImageGeneration(apiKey.Group) {
		h.jsonError(c, http.StatusForbidden, "permission_error", service.ImageGenerationPermissionMessage())
		return
	}
	// Explicit video operation slugs must bypass the image-account probe. Besides
	// producing misleading images-basic diagnostics, probing image accounts first
	// can advance request-scoped fallback routing past the group that owns the
	// matching AtlasCloud/APIZ account.
	if !knownImageModel && modelAPIIsExplicitVideoModel(model) {
		h.nativeVideoSubmitAfterFeatureGate(c, apiKey, subject, reqLog, model, payload)
		return
	}
	falAPI := modelAPIImageAPI(model)
	imageAccount, imageErr := h.gatewayService.SelectAsyncImageAccountInGroup(c.Request.Context(), apiKey.GroupID, "", model, nil, falAPI)
	if imageErr == nil && imageAccount != nil {
		h.nativeImageSubmit(c, apiKey, subject, reqLog, model, body, imageAccount)
		return
	}
	if knownImageModel {
		reqLog.Warn("model_api.no_available_image_account", zap.Error(imageErr))
		h.jsonError(c, http.StatusInternalServerError, "api_error", "Internal server error")
		return
	}
	// Image account selection is authoritative for every non-explicit-video
	// model. Do not reinterpret a failed image selection as a video request;
	// that used to expose misleading validation errors such as missing
	// resolution for native FAL image endpoints (e.g. imageutils/rembg).
	if errors.Is(imageErr, service.ErrNoAvailableAccounts) {
		reqLog.Warn("model_api.no_available_image_account", zap.Error(imageErr))
		h.jsonError(c, http.StatusInternalServerError, "api_error", "Internal server error")
		return
	}
	if imageErr != nil && !errors.Is(imageErr, service.ErrNoAvailableAccounts) {
		reqLog.Warn("model_api.image_account_selection_failed", zap.Error(imageErr))
		h.jsonError(c, http.StatusServiceUnavailable, "api_error", "failed to select image account")
		return
	}
	if !domain.IsVideoModelName(model) {
		h.jsonError(c, http.StatusNotFound, "not_found_error", "Unsupported model")
		return
	}
	h.nativeVideoSubmitAfterFeatureGate(c, apiKey, subject, reqLog, model, payload)
}

func (h *ModelAPIGatewayHandler) nativeVideoSubmitAfterFeatureGate(
	c *gin.Context,
	apiKey *service.APIKey,
	subject middleware2.AuthSubject,
	reqLog *zap.Logger,
	model string,
	payload map[string]any,
) {
	if h.settingService == nil || !h.settingService.IsVideoFeatureEnabled(c.Request.Context()) {
		h.jsonError(c, http.StatusForbidden, "feature_disabled", "Video feature is disabled")
		return
	}
	h.nativeVideoSubmit(c, apiKey, subject, reqLog, model, payload)
}

func (h *ModelAPIGatewayHandler) nativeImageSubmit(
	c *gin.Context,
	apiKey *service.APIKey,
	subject middleware2.AuthSubject,
	reqLog *zap.Logger,
	model string,
	body []byte,
	account *service.Account,
) {
	if !service.GroupAllowsImageGeneration(apiKey.Group) {
		h.jsonError(c, http.StatusForbidden, "permission_error", service.ImageGenerationPermissionMessage())
		return
	}
	var input fal.ImageGenInput
	var requestParameters map[string]any
	var upscaleRequest *fal.UpscaleRequest
	var rawRequestBody []byte
	if account.Platform == service.PlatformBytedance {
		var size string
		var count int
		var err error
		rawRequestBody, size, count, err = service.BytedanceRequestInput(body, account.GetMappedModel(model))
		if err != nil {
			h.jsonError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
			return
		}
		input = fal.ImageGenInput{Size: size, N: count}
	} else if service.IsSeedVRUpscaleModel(model) {
		var request fal.UpscaleRequest
		if err := json.Unmarshal(body, &request); err != nil {
			h.jsonError(c, http.StatusBadRequest, "invalid_request_error", "Invalid image request body")
			return
		}
		upscaleRequest = &request
		// SeedVR has no prompt/image_size fields. Keep a single image input so
		// the shared billing path can still estimate and settle the task.
		input = fal.ImageGenInput{Size: "auto", N: 1, OutputFormat: request.OutputFormat}
		requestParameters = map[string]any{
			"upscale_mode":   request.UpscaleMode,
			"upscale_factor": request.UpscaleFactor,
			"output_format":  request.OutputFormat,
		}
	} else {
		rawRequestBody = append([]byte(nil), body...)
		var request fal.Request
		if err := json.Unmarshal(body, &request); err != nil {
			h.jsonError(c, http.StatusBadRequest, "invalid_request_error", "Invalid image request body")
			return
		}
		input = fal.FalRequestToInput(&request)
		if strings.EqualFold(modelAPIImageAPI(model), service.FalAPIEdit) {
			input.IsEdit = true
		}
		requestParameters = map[string]any{
			"prompt":        service.TruncateUsageRequestPrompt(request.Prompt),
			"image_size":    request.ImageSize,
			"quality":       request.Quality,
			"num_images":    request.NumImages,
			"output_format": request.OutputFormat,
			"sync_mode":     request.SyncMode,
		}
		if len(request.ImageURLs) > 0 {
			requestParameters["image_urls"] = request.ImageURLs
		}
		if request.MaskURL != "" {
			requestParameters["mask_url"] = request.MaskURL
		}
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
		Facade:            service.AsyncMediaFacadeFal,
		InternalRequestID: modelAPIInternalRequestID(c),
		RequestedModel:    model,
		Input:             input,
		RawRequestBody:    rawRequestBody,
		UpscaleRequest:    upscaleRequest,
		RequestParameters: requestParameters,
		BillingType:       billingType,
		RateMultiplier:    rateMultiplier,
		RateMultiplierSet: true,
		ClientIP:          c.ClientIP(),
		UserAgent:         c.GetHeader("User-Agent"),
		InboundEndpoint:   modelAPIInboundEndpoint(c),
	}
	task, err := h.mediaService.SubmitAsync(c.Request.Context(), submitInput)
	if err != nil {
		reqLog.Warn("model_api.image_submit_failed", zap.String("platform", account.Platform), zap.Error(err))
		switch {
		case errors.Is(err, service.ErrInsufficientBalance):
			h.jsonError(c, http.StatusPaymentRequired, "insufficient_balance", "Insufficient balance to hold the estimated cost for this image task. Please recharge and try again.")
		case errors.Is(err, service.ErrAsyncMediaPricingMissing):
			h.jsonError(c, http.StatusServiceUnavailable, "pricing_unavailable", "Image model pricing is not configured for this group/channel; please contact the administrator")
		default:
			// Provider/network details stay in server logs and must not be exposed to clients.
			h.jsonError(c, http.StatusBadGateway, "api_error", publicImageSubmitFailure)
		}
		return
	}

	reqID := derefStringPtr(task.UpstreamRequestID)
	base := h.callbackBase(c, model, reqID)
	c.JSON(http.StatusOK, fal.SubmitResponse{
		RequestID:   reqID,
		Status:      modelAPIStatusProcessing,
		StatusURL:   base,
		ResponseURL: base,
		CancelURL:   base + "/cancel",
	})
}

func (h *ModelAPIGatewayHandler) nativeVideoSubmit(
	c *gin.Context,
	apiKey *service.APIKey,
	subject middleware2.AuthSubject,
	reqLog *zap.Logger,
	model string,
	payload map[string]any,
) {
	resolution, duration, aspectRatio := service.ExtractVideoBillingDims(payload)
	// duration 允许为 0：客户端传 duration="auto" 或缺失时按兜底秒数预扣，
	// 完成后按上游 result 里的实际时长重算 finalCost。
	if resolution == "" {
		h.jsonError(c, http.StatusBadRequest, "invalid_request_error", "Missing 'resolution' (e.g. 480p / 720p / 1080p)")
		return
	}

	// 选号：视频链路统一走 /api/v1/model 门面，在当前混合分组内按“该模型属于哪个平台”
	// 选出对应平台账号（fal / atlascloud / apiz / higgsfield），再转发到该账号。
	// slug 自带 api 段（如 .../text-to-video），api 传空串。
	account, err := h.gatewayService.SelectFalAccountInGroup(c.Request.Context(), apiKey.GroupID, "", model, nil, "")
	if err != nil || account == nil {
		reqLog.Warn("model_api.no_available_video_account", zap.Error(err))
		h.jsonError(c, http.StatusServiceUnavailable, "api_error", "no available video account")
		return
	}

	billingType := service.BillingTypeBalance
	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	if subscription != nil && apiKey.Group != nil && apiKey.Group.IsSubscriptionType() {
		billingType = service.BillingTypeSubscription
	}
	rateMultiplier := 1.0
	if account.RateMultiplier != nil && *account.RateMultiplier > 0 {
		rateMultiplier = *account.RateMultiplier
	}

	// 应用账号级 model_mapping：客户端传入的模型名（如 fal-ai/bytedance/...）
	// 会被映射到该账号对应的真实上游模型名（例如 apiz 账号会把 seedance 系列
	// 映射到 bytedance-seedance-1-0-pro-t2v 之类的上游 model 值）。
	// 未配置或未命中时，退回客户端原始模型名。
	upstreamModel := model
	if mappedModel, matched := account.ResolveMappedModel(model); matched {
		if mappedModel = strings.TrimSpace(mappedModel); mappedModel != "" {
			upstreamModel = mappedModel
		}
	}

	submitInput := &service.AsyncVideoSubmitInput{
		Account:           account,
		User:              apiKey.User,
		APIKeyID:          apiKey.ID,
		UserID:            subject.UserID,
		AccountID:         account.ID,
		GroupID:           apiKey.GroupID,
		Facade:            service.AsyncVideoFacadeFal,
		InternalRequestID: modelAPIInternalRequestID(c),
		RequestedModel:    model,
		UpstreamModel:     upstreamModel,
		RequestPayload:    payload,
		Resolution:        resolution,
		DurationSeconds:   duration,
		AspectRatio:       aspectRatio,
		BillingType:       billingType,
		RateMultiplier:    rateMultiplier,
		ClientIP:          c.ClientIP(),
		UserAgent:         c.GetHeader("User-Agent"),
		InboundEndpoint:   modelAPIInboundEndpoint(c),
	}

	task, err := h.videoService.SubmitAsync(c.Request.Context(), submitInput)
	if err != nil {
		reqLog.Warn("model_api.video_submit_failed", zap.Error(err))
		// 余额不足 → 402 Payment Required；防止并发攻击“借用未预扣的余额”烧钱。
		// 前端可根据 402 特化提示（例如"余额不足，请充值后再试"），而不再吞成
		// 通用的 Failed to submit video task 提示。
		if errors.Is(err, service.ErrInsufficientBalance) {
			h.jsonError(c, http.StatusPaymentRequired, "insufficient_balance", "Insufficient balance to hold the estimated cost for this video task. Please recharge and try again.")
			return
		}
		h.jsonError(c, http.StatusBadGateway, "api_error", publicVideoSubmitFailure)
		return
	}

	reqID := ""
	if task.UpstreamRequestID != nil {
		reqID = *task.UpstreamRequestID
	}
	base := h.callbackBase(c, model, reqID)
	c.JSON(http.StatusOK, fal.SubmitResponse{
		RequestID:   reqID,
		Status:      modelAPIStatusProcessing,
		StatusURL:   base,
		ResponseURL: base,
		CancelURL:   base + "/cancel",
	})
}

func (h *ModelAPIGatewayHandler) nativeResult(c *gin.Context, reqID string, statusRequest bool) {
	mediaTask, videoTask, account := h.loadTaskAndAccount(c, reqID)
	if mediaTask == nil && videoTask == nil {
		return
	}
	upstream := modelAPIUpstream(account)
	if mediaTask != nil && mediaTask.IsStatusCacheHit() {
		if cachedUpstream := strings.TrimSpace(mediaTask.StatusCacheUpstream()); cachedUpstream != "" {
			upstream = cachedUpstream
		}
	}
	c.Set(modelAPIUpstreamContextKey, upstream)
	if mediaTask != nil {
		h.writeMediaResult(c, reqID, mediaTask, upstream, statusRequest)
		return
	}
	if !videoTask.IsTerminal() {
		response := modelAPIStatusResponse{
			StatusResponse: fal.StatusResponse{Status: videoFalStatusFromTask(videoTask), RequestID: reqID},
			ActualCost:     videoTask.FinalCost,
		}
		logModelAPIClientResponse(c, upstream, http.StatusAccepted, response)
		c.JSON(http.StatusAccepted, response)
		return
	}
	if videoTask.Status != service.AsyncVideoStatusSucceeded {
		response := modelAPIStatusFailureResponse(reqID, videoTask.Status == service.AsyncVideoStatusExpired, isCanceledReason(videoTask.ErrorReason), true)
		logModelAPIClientResponse(c, upstream, http.StatusOK, response)
		c.JSON(http.StatusOK, response)
		return
	}
	// Preserve the upstream result payload while appending the authoritative
	// amount settled by Sub2API. Clone the map so the persisted task payload is
	// not mutated by response decoration.
	response := gin.H{"status": fal.StatusCompleted, "request_id": reqID, "data": modelAPIResultPayload(videoTask.ResultPayload, videoTask.FinalCost)}
	logModelAPIClientResponse(c, upstream, http.StatusOK, response)
	c.JSON(http.StatusOK, response)
}

func (h *ModelAPIGatewayHandler) nativeCancel(c *gin.Context, reqID string) {
	mediaTask, videoTask, account := h.loadTaskAndAccount(c, reqID)
	if mediaTask == nil && videoTask == nil {
		return
	}
	var err error
	if mediaTask != nil {
		err = h.mediaService.CancelTask(c.Request.Context(), mediaTask, account)
	} else {
		err = h.videoService.CancelTask(c.Request.Context(), videoTask, account)
	}
	if err != nil {
		if errors.Is(err, service.ErrBytedanceAlreadyRunning) {
			h.jsonError(c, http.StatusConflict, "conflict_error", err.Error())
			return
		}
		h.jsonError(c, http.StatusBadGateway, "api_error", "Failed to cancel task")
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": fal.StatusCanceled, "request_id": reqID})
}

func (h *ModelAPIGatewayHandler) loadTaskAndAccount(c *gin.Context, reqID string) (*service.AsyncMediaTask, *service.AsyncVideoTask, *service.Account) {
	if strings.TrimSpace(reqID) == "" {
		h.jsonError(c, http.StatusBadRequest, "invalid_request_error", "Missing request id")
		return nil, nil, nil
	}
	if h.mediaService == nil && h.videoService == nil {
		h.jsonError(c, http.StatusInternalServerError, "api_error", "Internal server error")
		return nil, nil, nil
	}
	var mediaTask *service.AsyncMediaTask
	if h.mediaService != nil {
		var mediaErr error
		mediaTask, mediaErr = h.mediaService.GetTaskByUpstreamID(c.Request.Context(), reqID)
		if c.Request.Method != http.MethodGet {
			mediaTask, mediaErr = h.mediaService.GetTaskForMutation(c.Request.Context(), reqID)
		}
		if mediaErr != nil {
			h.jsonError(c, http.StatusInternalServerError, "api_error", "Failed to load request")
			return nil, nil, nil
		}
	}
	var videoTask *service.AsyncVideoTask
	if mediaTask == nil && h.videoService != nil {
		var videoErr error
		videoTask, videoErr = h.videoService.GetTaskByUpstreamID(c.Request.Context(), reqID)
		if videoErr != nil {
			h.jsonError(c, http.StatusInternalServerError, "api_error", "Failed to load request")
			return nil, nil, nil
		}
	}
	if mediaTask == nil && videoTask == nil {
		h.jsonError(c, http.StatusNotFound, "not_found_error", "Request not found")
		return nil, nil, nil
	}
	taskAPIKeyID := int64(0)
	var accountID *int64
	if mediaTask != nil {
		taskAPIKeyID = mediaTask.APIKeyID
		accountID = mediaTask.AccountID
	} else {
		taskAPIKeyID = videoTask.APIKeyID
		accountID = videoTask.AccountID
	}
	if apiKey, ok := middleware2.GetAPIKeyFromContext(c); ok && apiKey.ID != taskAPIKeyID {
		h.jsonError(c, http.StatusNotFound, "not_found_error", "Request not found")
		return nil, nil, nil
	}
	var account *service.Account
	if mediaTask != nil && mediaTask.IsStatusCacheHit() {
		logger.FromContext(c.Request.Context()).Debug("model_api.status_cache_hit",
			zap.String("upstream_request_id", reqID),
			zap.String("upstream", mediaTask.StatusCacheUpstream()),
			zap.String("status", mediaTask.Status),
		)
		return mediaTask, videoTask, nil
	}
	if accountID != nil && h.accountService != nil {
		if acc, accErr := h.accountService.GetByID(c.Request.Context(), *accountID); accErr == nil {
			account = acc
		}
	}
	return mediaTask, videoTask, account
}

func (h *ModelAPIGatewayHandler) writeMediaResult(c *gin.Context, reqID string, task *service.AsyncMediaTask, upstream string, statusRequest bool) {
	if !task.IsTerminal() {
		response := modelAPIStatusResponse{
			StatusResponse: fal.StatusResponse{Status: imageStatusFromTask(task), RequestID: reqID},
			ActualCost:     task.FinalCost,
		}
		logModelAPIClientResponse(c, upstream, http.StatusAccepted, response)
		c.JSON(http.StatusAccepted, response)
		return
	}
	if task.Status != service.AsyncMediaStatusSucceeded {
		response := modelAPIStatusFailureResponse(reqID, task.Status == service.AsyncMediaStatusExpired, isCanceledReason(task.ErrorReason), false)
		logModelAPIClientResponse(c, upstream, http.StatusOK, response)
		c.JSON(http.StatusOK, response)
		return
	}
	response := gin.H{"status": modelAPIStatusCompleted, "request_id": reqID, "data": buildAsyncImageResultPayload(task)}
	logModelAPIClientResponse(c, upstream, http.StatusOK, response)
	c.JSON(http.StatusOK, response)
}

func logModelAPIClientResponse(c *gin.Context, upstream string, statusCode int, response any) {
	fields := []zap.Field{
		zap.String("upstream", upstream),
		zap.Int("status_code", statusCode),
	}
	if raw, err := json.Marshal(response); err != nil {
		fields = append(fields, zap.Error(err))
	} else {
		fields = append(fields, zap.ByteString("response_body", raw))
	}
	logger.FromContext(c.Request.Context()).Debug("model_api.client_response", fields...)
}

func modelAPIUpstream(account *service.Account) string {
	if account == nil || strings.TrimSpace(account.Platform) == "" {
		return "unknown"
	}
	return account.Platform
}

func modelAPIUpstreamFromContext(c *gin.Context) string {
	if c != nil {
		if upstream, ok := c.Get(modelAPIUpstreamContextKey); ok {
			if value, ok := upstream.(string); ok && strings.TrimSpace(value) != "" {
				return value
			}
		}
	}
	return "unknown"
}

func modelAPIResultPayload(payload map[string]any, actualCost float64) map[string]any {
	response := make(map[string]any, len(payload)+1)
	for key, value := range payload {
		response[key] = value
	}
	response["actual_cost"] = actualCost
	return response
}

func modelAPIStatusFailureResponse(reqID string, timeout, canceled, video bool) modelAPIStatusResponse {
	message := "Image generation failed; please check the error field."
	if video {
		message = "Video generation failed; please check the error field."
	}
	errorType := "api_error"
	status := modelAPIStatusFailed
	if canceled {
		status = fal.StatusCanceled
		errorType = "canceled"
		if video {
			message = "Video generation was canceled."
		} else {
			message = "Image generation was canceled."
		}
	}
	if timeout {
		status = modelAPIStatusFailed
		errorType = "timeout_error"
		if video {
			message = "Video generation timed out."
		} else {
			message = "Image generation timed out."
		}
	}
	return modelAPIStatusResponse{
		StatusResponse: fal.StatusResponse{Status: status, RequestID: reqID},
		Error:          &modelAPIResponseError{Type: errorType, Message: message},
	}
}

func (h *ModelAPIGatewayHandler) callbackBase(c *gin.Context, model, reqID string) string {
	scheme := "https"
	if c.Request.TLS == nil && c.GetHeader("X-Forwarded-Proto") != "https" {
		scheme = "http"
	}
	model = strings.Trim(model, "/")
	if model == "" {
		return scheme + "://" + c.Request.Host + "/api/v1/model/requests/" + reqID
	}
	return scheme + "://" + c.Request.Host + "/api/v1/model/" + model + "/requests/" + reqID
}

// ----- helpers -----

func modelAPIInternalRequestID(c *gin.Context) string {
	if v, _ := c.Request.Context().Value(ctxkey.ClientRequestID).(string); strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	if v := strings.TrimSpace(c.GetHeader("x-client-request-id")); v != "" {
		return v
	}
	return uuid.New().String()
}

func modelAPIInboundEndpoint(c *gin.Context) string {
	if p := c.FullPath(); p != "" {
		return p
	}
	if c.Request != nil && c.Request.URL != nil {
		return c.Request.URL.Path
	}
	return ""
}

func modelAPIRequestIDFromPath(path string) string {
	_, rest, ok := strings.Cut(path, "/requests/")
	if !ok {
		return ""
	}
	rest = strings.TrimSuffix(rest, "/cancel")
	rest = strings.TrimSuffix(rest, "/status")
	return strings.Trim(rest, "/")
}

func modelAPIImageAPI(model string) string {
	model = strings.ToLower(strings.Trim(strings.TrimSpace(model), "/"))
	if strings.HasSuffix(model, "/"+service.FalAPIEdit) {
		return service.FalAPIEdit
	}
	return ""
}

func modelAPIIsKnownImageModel(model string) bool {
	normalized := strings.ToLower(strings.Trim(strings.TrimSpace(model), "/"))
	normalized = strings.TrimPrefix(normalized, "fal-ai/")
	if normalized == domain.SeedreamModel {
		return true
	}
	// SeedVR upscale is an image-facade endpoint even though its multi-segment
	// slug also matches the generic video-model naming convention.
	if normalized == "seedvr/upscale/image" {
		return true
	}
	for _, segment := range strings.Split(normalized, "/") {
		if service.IsGPTImageGenerationModel(segment) {
			return true
		}
	}
	if _, ok := domain.DefaultFalModelMapping[normalized]; ok {
		return true
	}
	if _, ok := domain.DefaultLeonardoModelMapping[normalized]; ok {
		return true
	}
	return false
}

func modelAPIIsExplicitVideoModel(model string) bool {
	normalized := strings.ToLower(strings.Trim(strings.TrimSpace(model), "/"))
	for _, operation := range []string{
		"text-to-video",
		"image-to-video",
		"reference-to-video",
		"video-to-video",
	} {
		if strings.HasSuffix(normalized, "/"+operation) {
			return true
		}
	}
	return false
}

func imageStatusFromTask(task *service.AsyncMediaTask) string {
	switch task.Status {
	case service.AsyncMediaStatusSucceeded:
		return modelAPIStatusCompleted
	case service.AsyncMediaStatusRefunded:
		if isCanceledReason(task.ErrorReason) {
			return fal.StatusCanceled
		}
		return modelAPIStatusFailed
	case service.AsyncMediaStatusFailed, service.AsyncMediaStatusExpired:
		return modelAPIStatusFailed
	case service.AsyncMediaStatusPending:
		return fal.StatusInQueue
	default:
		return fal.StatusInProgress
	}
}

func videoFalStatusFromTask(task *service.AsyncVideoTask) string {
	switch task.Status {
	case service.AsyncVideoStatusSucceeded:
		return modelAPIStatusCompleted
	case service.AsyncVideoStatusRefunded:
		if isCanceledReason(task.ErrorReason) {
			return fal.StatusCanceled
		}
		return modelAPIStatusFailed
	case service.AsyncVideoStatusFailed, service.AsyncVideoStatusExpired, service.AsyncVideoStatusRefundFailed:
		return modelAPIStatusFailed
	case service.AsyncVideoStatusPending:
		return fal.StatusInQueue
	default:
		return fal.StatusInProgress
	}
}

func isCanceledReason(reason *string) bool {
	return reason != nil && strings.EqualFold(strings.TrimSpace(*reason), "cancelled by client")
}
