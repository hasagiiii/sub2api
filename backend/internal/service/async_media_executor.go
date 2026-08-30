package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/fal"
	"github.com/Wei-Shaw/sub2api/internal/pkg/leonardo"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// 异步媒体执行内核的默认时序参数。
const (
	defaultAsyncMediaPollInterval = 2 * time.Second
	// defaultAsyncMediaFailTimeout 是任务从创建到强制判失（退费兜底）的最长时间。
	// reconciler 在任务超过 fail_deadline_at 仍未出图时退费并置 expired。
	defaultAsyncMediaFailTimeout      = 30 * time.Minute
	AsyncMediaTaskStatusProcessingTTL = 30 * time.Second
	AsyncMediaTaskStatusTerminalTTL   = 7 * 24 * time.Hour
	// AsyncMediaTaskStatusTTL is kept as the terminal TTL for callers that need
	// the long-lived cache duration explicitly.
	AsyncMediaTaskStatusTTL = AsyncMediaTaskStatusTerminalTTL
)

// AsyncMediaService 异步媒体任务执行内核。
//
// 职责：
//   - 提交任务（构建 fal 请求 → 预扣费 → 落库 pending → submit → running）
//   - 轮询/取结果（running → succeeded，取 images）
//   - 失败判定（status 明确失败 或 到达 fail_deadline_at）与退费（幂等）
//   - 成功转存 COS 并在成功终态追加写 usage_log；失败终态写入 ops 错误记录
//
// 计费采用「预扣 + 结算退差」模型：
//   - 提交时按 (size_tier × quality × num_images) 预扣 heldCost
//   - 成功时按实际出图数量结算 finalCost；若 finalCost < heldCost 退还差额
//   - 失败/超时退还全部 heldCost
//
// 余额账本仅对 BillingTypeBalance 生效；订阅计费的额度核算沿用既有 usage_log 记录路径。
type AsyncMediaService struct {
	taskRepo               AsyncMediaTaskRepository
	userRepo               UserRepository
	groupRepo              GroupRepository
	billing                *BillingService
	resolver               *ModelPricingResolver
	cos                    *COSImageTransferService
	deferred               *DeferredService
	billingContextResolver *BillingContextResolver
	opsService             *OpsService
	modelIntroService      *ModelIntroService
	statusCache            AsyncMediaTaskStatusStore
	backgroundPolling      bool
	balanceCache           interface {
		InvalidateUserBalance(ctx context.Context, userID int64) error
	}

	pollInterval time.Duration
	failTimeout  time.Duration
}

func (s *AsyncMediaService) SetBalanceCache(cache interface {
	InvalidateUserBalance(ctx context.Context, userID int64) error
}) {
	if s != nil {
		s.balanceCache = cache
	}
}

func (s *AsyncMediaService) SetBillingContextResolver(resolver *BillingContextResolver) {
	if s != nil {
		s.billingContextResolver = resolver
	}
}

// SetOpsService 注入异步任务终态错误记录服务。图片任务已离开 HTTP
// 中间件生命周期，失败必须在终态显式写入 ops_error_logs。
func (s *AsyncMediaService) SetOpsService(ops *OpsService) {
	if s != nil {
		s.opsService = ops
	}
}

// SetModelIntroService injects the model output schema used to extract native
// provider result fields.
func (s *AsyncMediaService) SetModelIntroService(intros *ModelIntroService) {
	if s != nil {
		s.modelIntroService = intros
	}
}

func (s *AsyncMediaService) SetStatusCache(cache AsyncMediaTaskStatusStore) {
	if s != nil {
		s.statusCache = cache
	}
}

// SetBackgroundPollingEnabled enables the server-owned polling loop. It is
// enabled by production wiring; unit tests may leave it disabled and advance a
// task explicitly.
func (s *AsyncMediaService) SetBackgroundPollingEnabled(enabled bool) {
	if s != nil {
		s.backgroundPolling = enabled
	}
}

// NewAsyncMediaService 创建异步媒体执行内核。
//
// groupRepo 用于 estimateCost 回查分组逐模型定价和二维价格矩阵；可为 nil，
// 此时仅依赖 resolver 解析的渠道定价。
func NewAsyncMediaService(
	taskRepo AsyncMediaTaskRepository,
	userRepo UserRepository,
	groupRepo GroupRepository,
	billing *BillingService,
	resolver *ModelPricingResolver,
	cos *COSImageTransferService,
) *AsyncMediaService {
	return &AsyncMediaService{
		taskRepo:     taskRepo,
		userRepo:     userRepo,
		groupRepo:    groupRepo,
		billing:      billing,
		resolver:     resolver,
		cos:          cos,
		pollInterval: defaultAsyncMediaPollInterval,
		failTimeout:  defaultAsyncMediaFailTimeout,
	}
}

// SetPollInterval 配置轮询间隔（reconciler / 配置项）。
func (s *AsyncMediaService) SetPollInterval(d time.Duration) {
	if d > 0 {
		s.pollInterval = d
	}
}

// SetFailTimeout 配置任务强制判失（退费兜底）时间。
func (s *AsyncMediaService) SetFailTimeout(d time.Duration) {
	if d > 0 {
		s.failTimeout = d
	}
}

// SetDeferredService 注入延迟批量更新服务，用于在账号实际被使用时记录 last_used_at。
func (s *AsyncMediaService) SetDeferredService(d *DeferredService) {
	s.deferred = d
}

// FailTimeout 返回当前的失败兜底时间。
func (s *AsyncMediaService) FailTimeout() time.Duration { return s.failTimeout }

// AsyncMediaSubmitInput 提交异步媒体任务的入参。
type AsyncMediaSubmitInput struct {
	Account *Account
	User    *User

	APIKeyID  int64
	UserID    int64
	AccountID int64
	GroupID   *int64
	ChannelID *int64

	Facade            string // openai / fal
	InternalRequestID string // 网关侧生成的内部请求 ID（幂等键）
	RequestedModel    string // 客户端请求模型（映射前）

	Input fal.ImageGenInput // 协议无关的图片请求描述
	// RawRequestBody preserves the caller's native FAL JSON payload. It is used
	// for non-SeedVR native endpoints whose schema is not fal.Request.
	RawRequestBody []byte
	// UpscaleRequest is set for the native seedvr/upscale/image facade. SeedVR
	// uses a different fal payload/response shape than gpt-image requests.
	UpscaleRequest    *fal.UpscaleRequest
	RequestParameters map[string]any // sanitized non-binary client parameters for usage details

	BillingType       int8    // 0=balance / 1=subscription
	RateMultiplier    float64 // 下游图片计费倍率
	RateMultiplierSet bool    // true 时保留显式 0 倍率

	// 请求元信息（提交时持久化到任务表，供终态 usage_log 回填端点/IP/UA）。
	ClientIP        string // 客户端 IP
	UserAgent       string // 客户端 User-Agent
	InboundEndpoint string // 对外门面端点（客户端可见路径）
}

// SubmitAsync 提交一个异步媒体任务：预扣费 → 落库 → 提交上游 → 置 running。
//
// 任一前置步骤失败将回滚已扣余额并返回错误；任务一旦成功提交即进入 running，
// 后续由 WaitForTerminal（伪同步）或 reconciler（兜底）推进到终态。
func (s *AsyncMediaService) SubmitAsync(ctx context.Context, in *AsyncMediaSubmitInput) (*AsyncMediaTask, error) {
	if in == nil {
		return nil, errors.New("nil async media submit input")
	}
	if in.Account == nil {
		return nil, errors.New("async media: account is required")
	}
	if strings.TrimSpace(in.InternalRequestID) == "" {
		// The Leonardo proxy requires an idempotency key for every submit. Keep
		// direct service callers safe as well as the HTTP facade, which normally
		// supplies the client request ID.
		in.InternalRequestID = uuid.NewString()
	}
	if !in.RateMultiplierSet && in.RateMultiplier == 0 {
		in.RateMultiplier = 1
	}

	upstreamModel := s.resolveUpstreamModel(in.Account, in.RequestedModel, in.Input.IsEdit)
	rawSize := in.Input.Size
	sizeTier := NormalizeImageBillingTierOrDefault(rawSize)
	quality := fal.MapQualityToFal(in.Input.Quality)
	numImages := in.Input.N
	if numImages <= 0 {
		numImages = 1
	}

	// 预估并预扣费用（按 num_images 的满额预扣）。
	_, heldCost, err := s.estimateCost(ctx, in.RequestedModel, upstreamModel, in.GroupID, rawSize, sizeTier, quality, numImages, in.RateMultiplier)
	if err != nil {
		return nil, fmt.Errorf("async media: estimate cost: %w", err)
	}
	billingContext := &BillingContext{ConsumerUserID: in.UserID, PayerUserID: in.UserID, BalanceSource: BalanceSourceSelf}
	if s.billingContextResolver != nil {
		billingContext, err = s.billingContextResolver.ResolveForAmount(ctx, in.UserID, heldCost)
		if err != nil {
			return nil, fmt.Errorf("async media: resolve payer: %w", err)
		}
	}
	if err := s.charge(ctx, in.BillingType, billingContext, heldCost); err != nil {
		return nil, fmt.Errorf("async media: pre-charge: %w", err)
	}

	failDeadline := time.Now().Add(s.failTimeout)
	task := &AsyncMediaTask{
		InternalRequestID: in.InternalRequestID,
		APIKeyID:          in.APIKeyID,
		UserID:            in.UserID,
		OrganizationID:    billingContext.OrganizationID,
		PayerUserID:       amInt64Ptr(billingContext.PayerUserID),
		BalanceSource:     amStrPtr(billingContext.BalanceSource),
		AuthzGeneration:   amInt64Ptr(billingContext.AuthzGeneration),
		AccountID:         amOptInt64(in.AccountID),
		GroupID:           in.GroupID,
		ChannelID:         in.ChannelID,
		Facade:            in.Facade,
		RequestedModel:    in.RequestedModel,
		UpstreamModel:     amStrPtr(upstreamModel),
		// ImageSize 存客户端原始 size（如 "1024x1024"），结算阶段据此命中分组二维矩阵；
		// 已归一化的计费档位（"2K" 等）单独存入 SizeTier 字段。
		ImageSize:         amStrPtr(rawSize),
		Quality:           amStrPtr(quality),
		NumImages:         numImages,
		RequestParameters: in.RequestParameters,
		Status:            AsyncMediaStatusPending,
		HeldCost:          heldCost,
		RateMultiplier:    in.RateMultiplier,
		SizeTier:          amStrPtr(sizeTier),
		FailDeadlineAt:    &failDeadline,
		ClientIP:          amStrPtr(in.ClientIP),
		UserAgent:         amStrPtr(in.UserAgent),
		InboundEndpoint:   amStrPtr(in.InboundEndpoint),
		UpstreamEndpoint:  amStrPtr(asyncImageUpstreamEndpoint(in.Account, upstreamModel)),
	}
	if err := s.taskRepo.Create(ctx, task); err != nil {
		// 落库失败：回滚预扣费，避免漏退。
		s.refund(ctx, in.BillingType, billingContext, heldCost)
		return nil, fmt.Errorf("async media: create task: %w", err)
	}

	requestID, statusURL, responseURL, err := s.submitUpstream(ctx, in, upstreamModel)
	if err != nil {
		s.markFailedAndRefund(ctx, task, in.BillingType, "submit: "+err.Error())
		return task, fmt.Errorf("async media: submit: %w", err)
	}
	if err := s.taskRepo.UpdateUpstreamRef(ctx, task.ID, requestID, statusURL, responseURL); err != nil {
		logger.L().Warn("async_media.update_upstream_ref_failed",
			zap.Int64("task_id", task.ID), zap.Error(err))
	}
	task.UpstreamRequestID = amStrPtr(requestID)
	task.StatusURL = amStrPtr(statusURL)
	task.ResponseURL = amStrPtr(responseURL)
	task.Status = AsyncMediaStatusRunning
	task.UpdatedAt = time.Now().UTC()
	task.statusCacheUpstream = in.Account.Platform
	s.cacheTaskStatus(ctx, task)
	if s.backgroundPolling {
		s.startBackgroundPolling(task, in.Account, in.BillingType)
	}

	// 账号已成功向上游提交任务，视为本次被使用：记录 last_used_at（延迟批量刷库）。
	if s.deferred != nil {
		s.deferred.ScheduleLastUsedUpdate(in.Account.ID)
	}
	return task, nil
}

// WaitForTerminal 伪同步阻塞等待任务终态，直到出图成功、明确失败或 ctx 超时。
//
// 关键约束：ctx 超时（伪同步等待超时）返回 ErrAsyncMediaPending，
// 但不退费、不终结任务——任务仍由 reconciler 兜底处理。
var ErrAsyncMediaPending = errors.New("async media task still pending")

// ErrAsyncMediaPricingMissing 表示上游模型未在渠道/分组中配置可用的定价。
//
// 触发条件：image / per-request 模式下，分层价（含 size_tier × quality）与
// 默认按次价均为 0，意味着该模型未配置任何有效定价。此时禁止提交任务，
// 防止账户被「免费刷图」。
var ErrAsyncMediaPricingMissing = errors.New("async media pricing not configured")

func (s *AsyncMediaService) WaitForTerminal(ctx context.Context, task *AsyncMediaTask, in *AsyncMediaSubmitInput) (*AsyncMediaTask, error) {
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()
	for {
		updated, done, err := s.pollOnce(ctx, task, in.Account, in.BillingType)
		if err != nil {
			return updated, err
		}
		if done {
			return updated, nil
		}
		select {
		case <-ctx.Done():
			// 伪同步超时：不退费、不终结，交由 reconciler 兜底。
			return task, ErrAsyncMediaPending
		case <-ticker.C:
		}
	}
}

// GetTaskByInternalID 按内部请求 ID 查询任务（不存在返回 nil,nil）。
func (s *AsyncMediaService) GetTaskByInternalID(ctx context.Context, internalRequestID string) (*AsyncMediaTask, error) {
	return s.taskRepo.GetByInternalRequestID(ctx, internalRequestID)
}

// GetTaskByID returns an image task by primary key.
func (s *AsyncMediaService) GetTaskByID(ctx context.Context, id int64) (*AsyncMediaTask, error) {
	return s.taskRepo.GetByID(ctx, id)
}

// ListByUserAndModel returns image tasks for the model playground history.
func (s *AsyncMediaService) ListByUserAndModel(ctx context.Context, userID int64, requestedModel string, offset, limit int) ([]*AsyncMediaTask, int64, error) {
	return s.taskRepo.ListByUserAndModel(ctx, userID, requestedModel, offset, limit)
}

// GetTaskByUpstreamID 按上游 request_id 查询任务（不存在返回 nil,nil）。
func (s *AsyncMediaService) GetTaskByUpstreamID(ctx context.Context, upstreamRequestID string) (*AsyncMediaTask, error) {
	upstreamRequestID = strings.TrimSpace(upstreamRequestID)
	if upstreamRequestID == "" {
		return nil, nil
	}
	if s != nil && s.statusCache != nil {
		cached, err := s.statusCache.GetAsyncMediaTaskStatus(ctx, upstreamRequestID)
		if err == nil && cached != nil {
			return cached.toTask(), nil
		}
		if err != nil && !errors.Is(err, ErrAsyncMediaTaskStatusNotFound) {
			logger.L().Warn("async_media.status_cache_get_failed",
				zap.String("upstream", "redis"),
				zap.String("request_id", upstreamRequestID),
				zap.Error(err),
			)
		}
	}
	task, err := s.taskRepo.GetByUpstreamRequestID(ctx, upstreamRequestID)
	if err == nil && task != nil {
		s.cacheTaskStatus(ctx, task)
	}
	return task, err
}

func (s *AsyncMediaService) cacheTaskStatus(ctx context.Context, task *AsyncMediaTask) {
	if s == nil || s.statusCache == nil || task == nil {
		return
	}
	status := asyncMediaTaskStatusFromTask(task)
	if status == nil {
		return
	}
	ttl := AsyncMediaTaskStatusCacheTTL(status.Status)
	if status.Status == AsyncMediaStatusPending || status.Status == AsyncMediaStatusRunning {
		ttl = s.pollInterval + 5*time.Second
		if ttl < 5*time.Second {
			ttl = 5 * time.Second
		}
	}
	if err := s.statusCache.SetAsyncMediaTaskStatus(ctx, status, ttl); err != nil {
		logger.L().Warn("async_media.status_cache_set_failed",
			zap.String("upstream", "redis"),
			zap.String("request_id", status.RequestID),
			zap.String("status", status.Status),
			zap.Duration("ttl", ttl),
			zap.Error(err),
		)
	}
}

func (s *AsyncMediaService) startBackgroundPolling(task *AsyncMediaTask, account *Account, billingType int8) {
	if s == nil || task == nil || account == nil || task.IsTerminal() {
		return
	}
	go s.backgroundPollLoop(context.Background(), task, account, billingType)
}

func (s *AsyncMediaService) backgroundPollLoop(ctx context.Context, task *AsyncMediaTask, account *Account, billingType int8) {
	if task == nil || account == nil {
		return
	}
	for {
		if task.IsTerminal() || ctx.Err() != nil {
			return
		}
		token := uuid.NewString()
		locked := true
		if lockStore, ok := s.statusCache.(AsyncMediaTaskLockStore); ok {
			var err error
			locked, err = lockStore.TryAcquireAsyncMediaTaskLock(ctx, amDerefStr(task.UpstreamRequestID), token, s.pollInterval+5*time.Second)
			if err != nil {
				locked = false
			}
		}
		if locked {
			updated, done, err := s.pollOnce(ctx, task, account, billingType)
			if lockStore, ok := s.statusCache.(AsyncMediaTaskLockStore); ok {
				_ = lockStore.ReleaseAsyncMediaTaskLock(ctx, amDerefStr(task.UpstreamRequestID), token)
			}
			if err != nil {
				logger.L().Warn("async_media.background_poll_failed", zap.Int64("task_id", task.ID), zap.Error(err))
			}
			if updated != nil {
				task = updated
			}
			if done {
				return
			}
		}
		timer := time.NewTimer(s.pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (s *AsyncMediaService) pollOnceWithLock(ctx context.Context, task *AsyncMediaTask, account *Account, billingType int8) (*AsyncMediaTask, bool, error) {
	if s == nil || task == nil {
		return task, false, errors.New("async media poll: invalid task")
	}
	lockStore, ok := s.statusCache.(AsyncMediaTaskLockStore)
	if !ok {
		return s.pollOnce(ctx, task, account, billingType)
	}
	token := uuid.NewString()
	locked, err := lockStore.TryAcquireAsyncMediaTaskLock(ctx, amDerefStr(task.UpstreamRequestID), token, s.pollInterval+5*time.Second)
	if err != nil || !locked {
		return task, false, err
	}
	defer func() { _ = lockStore.ReleaseAsyncMediaTaskLock(ctx, amDerefStr(task.UpstreamRequestID), token) }()
	return s.pollOnce(ctx, task, account, billingType)
}

func AsyncMediaTaskStatusCacheTTL(status string) time.Duration {
	switch status {
	case AsyncMediaStatusPending, AsyncMediaStatusRunning:
		return AsyncMediaTaskStatusProcessingTTL
	default:
		return AsyncMediaTaskStatusTerminalTTL
	}
}

// AdvanceTask 推进单个任务一轮轮询，并在终态时结算/退费（供原生门面 status/result 触发）。
// 返回 (最新任务, 是否终态, error)。任务已终态时直接返回。
func (s *AsyncMediaService) AdvanceTask(ctx context.Context, task *AsyncMediaTask, account *Account) (*AsyncMediaTask, bool, error) {
	if task == nil {
		return nil, false, errors.New("async media advance: nil task")
	}
	if task.IsTerminal() {
		return task, true, nil
	}
	if account == nil {
		return task, false, errors.New("async media advance: account is nil")
	}
	return s.pollOnce(ctx, task, account, BillingTypeBalance)
}

// CancelTask 取消一个在飞任务并退费（幂等）。
func (s *AsyncMediaService) CancelTask(ctx context.Context, task *AsyncMediaTask, account *Account) error {
	if task == nil {
		return errors.New("async media cancel: nil task")
	}
	if task.IsTerminal() {
		return nil
	}
	if account != nil {
		task.statusCacheUpstream = account.Platform
	}
	if account != nil && account.Platform == PlatformFal && task.UpstreamRequestID != nil {
		if client, err := s.newClient(account); err == nil {
			cancelURL := client.BuildCancelURL(amDerefStr(task.UpstreamModel), *task.UpstreamRequestID)
			if cancelErr := client.Cancel(ctx, cancelURL); cancelErr != nil {
				logger.L().Warn("async_media.cancel_upstream_failed",
					zap.Int64("task_id", task.ID), zap.Error(cancelErr))
			}
		}
	}
	s.markFailedAndRefund(ctx, task, BillingTypeBalance, "cancelled by client")
	return nil
}

// ReconcileTask 供 reconciler 调用：推进单个未终结任务到终态。
// 到达 fail_deadline_at 仍未出图则强制退费置 expired。
func (s *AsyncMediaService) ReconcileTask(ctx context.Context, task *AsyncMediaTask, account *Account) error {
	if task == nil {
		return nil
	}
	if task.IsTerminal() {
		return nil
	}
	billingType := BillingTypeBalance // 兜底退费按余额账本（订阅额度由 usage_log 路径核算）

	if task.FailDeadlineAt != nil && time.Now().After(*task.FailDeadlineAt) {
		s.markFailedAndRefund(ctx, task, billingType, "fail deadline exceeded")
		return nil
	}
	if account == nil {
		return errors.New("async media reconcile: account is nil")
	}
	_, _, err := s.pollOnceWithLock(ctx, task, account, billingType)
	return err
}

// pollOnce 执行一轮状态查询并在终态时结算/退费。
// 返回 (最新任务, 是否终态, error)。
func (s *AsyncMediaService) pollOnce(ctx context.Context, task *AsyncMediaTask, account *Account, billingType int8) (*AsyncMediaTask, bool, error) {
	if task == nil {
		return nil, false, errors.New("async media poll: task is nil")
	}
	// A reconciler can observe the row during the short pending window between
	// task creation and the upstream submit response. There is no upstream URL
	// to call yet; leave the task pending and let the next reconcile retry.
	if task.UpstreamRequestID == nil || strings.TrimSpace(*task.UpstreamRequestID) == "" ||
		task.ResponseURL == nil || strings.TrimSpace(*task.ResponseURL) == "" {
		logger.FromContext(ctx).Debug("async_media.upstream_poll_skipped",
			zap.String("reason", "upstream_not_submitted"),
			zap.Int64("task_id", task.ID),
			zap.String("status", task.Status),
		)
		return task, false, nil
	}
	if task != nil && account != nil {
		task.statusCacheUpstream = account.Platform
	}
	responseURL := ""
	if task.ResponseURL != nil {
		responseURL = *task.ResponseURL
	}
	task.lastRunAt = time.Now().UTC()
	task.UpdatedAt = task.lastRunAt
	// The Redis heartbeat is deliberately independent of database updated_at.
	s.cacheTaskStatus(ctx, task)
	if account != nil && account.Platform == PlatformLeonardo {
		return s.pollLeonardoOnce(ctx, task, account, billingType)
	}
	client, err := s.newClient(account)
	if err != nil {
		return task, false, fmt.Errorf("async media poll: build client: %w", err)
	}
	statusURL := ""
	if task.StatusURL != nil {
		statusURL = *task.StatusURL
	}
	logger.FromContext(ctx).Debug("async_media.upstream_request",
		zap.String("operation", "status"),
		zap.String("method", http.MethodGet),
		zap.String("url", statusURL),
		zap.String("upstream_model", amDerefStr(task.UpstreamModel)),
		zap.String("upstream_request_id", amDerefStr(task.UpstreamRequestID)),
		zap.String("request_body", ""),
	)
	st, err := client.Status(ctx, statusURL)
	if err != nil {
		logger.FromContext(ctx).Debug("async_media.upstream_response",
			zap.String("operation", "status"),
			zap.String("url", statusURL),
			zap.String("upstream_request_id", amDerefStr(task.UpstreamRequestID)),
			zap.Error(err),
		)
		if IsSeedVRUpscaleModel(task.RequestedModel) {
			logger.FromContext(ctx).Debug("fal.image.upscale_status_response",
				zap.Int64("task_id", task.ID), zap.String("request_id", amDerefStr(task.UpstreamRequestID)),
				zap.String("url", statusURL), zap.Error(err))
		}
		var apiErr *fal.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode >= 400 && apiErr.StatusCode < 500 {
			// 明确的客户端错误视为失败（任务不可恢复）。
			s.markFailedAndRefund(ctx, task, billingType, fmt.Sprintf("status %d: %s", apiErr.StatusCode, apiErr.Body))
			return task, true, nil
		}
		// 网络/5xx：暂不终结，等待下一轮或 reconciler。
		return task, false, nil
	}
	if raw, marshalErr := json.Marshal(st); marshalErr == nil {
		logger.FromContext(ctx).Debug("async_media.upstream_response",
			zap.String("operation", "status"),
			zap.String("url", statusURL),
			zap.String("upstream_request_id", amDerefStr(task.UpstreamRequestID)),
			zap.String("response_body", truncateAsyncUpstreamBody(raw)),
		)
	}
	if IsSeedVRUpscaleModel(task.RequestedModel) {
		logger.FromContext(ctx).Debug("fal.image.upscale_status_response",
			zap.Int64("task_id", task.ID), zap.String("request_id", amDerefStr(task.UpstreamRequestID)),
			zap.String("url", statusURL), zap.String("status", st.Status), zap.String("response_url", st.ResponseURL))
	}

	if !st.IsTerminal() {
		return task, false, nil
	}

	// 终态：取结果。
	responseURL = st.ResponseURL
	upstreamModel := amDerefStr(task.UpstreamModel)
	if responseURL == "" && task.ResponseURL != nil {
		responseURL = *task.ResponseURL
	}
	var result *fal.Response
	var rawResult map[string]any
	if IsSeedVRUpscaleModel(task.RequestedModel) {
		upscaleResult, upscaleErr := client.UpscaleResult(ctx, responseURL)
		if upscaleErr != nil {
			err = upscaleErr
		} else if upscaleResult != nil {
			logger.FromContext(ctx).Debug("fal.image.upscale_result_response",
				zap.Int64("task_id", task.ID), zap.String("request_id", amDerefStr(task.UpstreamRequestID)),
				zap.String("url", responseURL), zap.Any("image", upscaleResult.Image))
			result = &fal.Response{Images: []fal.Image{{
				URL:         upscaleResult.Image.URL,
				ContentType: upscaleResult.Image.ContentType,
				Width:       upscaleResult.Image.Width,
				Height:      upscaleResult.Image.Height,
			}}}
		}
	} else {
		logger.FromContext(ctx).Debug("async_media.upstream_request",
			zap.String("operation", "result"),
			zap.String("method", http.MethodGet),
			zap.String("url", responseURL),
			zap.String("upstream_model", upstreamModel),
			zap.String("upstream_request_id", amDerefStr(task.UpstreamRequestID)),
			zap.String("request_body", ""),
		)
		rawResult, err = client.ResultRaw(ctx, responseURL)
		if err != nil {
			logger.FromContext(ctx).Debug("async_media.upstream_response",
				zap.String("operation", "result"), zap.String("url", responseURL),
				zap.String("upstream_request_id", amDerefStr(task.UpstreamRequestID)), zap.Error(err))
		} else if raw, marshalErr := json.Marshal(rawResult); marshalErr == nil {
			logger.FromContext(ctx).Debug("async_media.upstream_response",
				zap.String("operation", "result"), zap.String("url", responseURL),
				zap.String("upstream_request_id", amDerefStr(task.UpstreamRequestID)),
				zap.String("response_body", truncateAsyncUpstreamBody(raw)))
		}
		if encoded, marshalErr := json.Marshal(rawResult); marshalErr == nil {
			result = &fal.Response{}
			_ = json.Unmarshal(encoded, result)
		}
	}
	if err != nil {
		var apiErr *fal.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode >= 400 && apiErr.StatusCode < 500 {
			s.markFailedAndRefund(ctx, task, billingType, fmt.Sprintf("result %d: %s", apiErr.StatusCode, apiErr.Body))
			return task, true, nil
		}
		return task, false, nil
	}

	var imageURLs []string
	var imageOutputSizes []string
	var imageMetadata []ImageOutputMetadata
	var matchedFields []string
	if len(rawResult) > 0 && s.modelIntroService != nil {
		intro, introErr := s.lookupModelIntro(ctx, task.RequestedModel)
		if introErr == nil && intro != nil {
			imageURLs, matchedFields = extractConfiguredImageURLs(rawResult, intro)
			imageMetadata = extractConfiguredImageMetadata(rawResult, imageURLs)
			imageOutputSizes = imageOutputSizesFromMetadata(imageMetadata)
			logger.FromContext(ctx).Debug("async_media.result_field_extraction",
				zap.String("requested_model", task.RequestedModel),
				zap.String("configured_result_field", intro.ResultField),
				zap.Strings("extracted_fields", matchedFields),
				zap.Int("extracted_url_count", len(imageURLs)),
			)
		} else if introErr != nil {
			logger.FromContext(ctx).Debug("async_media.result_field_schema_lookup_failed",
				zap.String("requested_model", task.RequestedModel), zap.Error(introErr))
		}
	}
	if len(imageURLs) == 0 {
		imageURLs, imageOutputSizes = extractFalImageResult(result)
		imageMetadata = extractFalImageMetadata(result)
	}
	if len(imageURLs) == 0 {
		s.markFailedAndRefund(ctx, task, billingType, "upstream returned no images")
		return task, true, nil
	}

	task.ImageMetadata = imageMetadata
	s.markSucceeded(ctx, task, account.BillingRateMultiplier(), billingType, imageURLs, imageOutputSizes, rawResult)
	return task, true, nil
}

func (s *AsyncMediaService) lookupModelIntro(ctx context.Context, model string) (*ModelIntro, error) {
	if s == nil || s.modelIntroService == nil {
		return nil, errors.New("model intro service is unavailable")
	}
	requested := strings.Trim(strings.TrimSpace(model), "/")
	candidates := []string{requested}
	normalized := strings.TrimPrefix(requested, "fal-ai/")
	if normalized != "" && normalized != requested {
		candidates = append(candidates, normalized)
	} else if normalized != "" {
		candidates = append(candidates, "fal-ai/"+normalized)
	}
	var lastErr error
	for _, candidate := range candidates {
		intro, err := s.modelIntroService.Get(ctx, candidate)
		if err == nil && intro != nil {
			return intro, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// markSucceeded 成功结算：转存 COS、结算 finalCost（退差）、置 succeeded、终态写 usage_log。
func (s *AsyncMediaService) markSucceeded(ctx context.Context, task *AsyncMediaTask, accountRateMultiplier float64, billingType int8, imageURLs, imageOutputSizes []string, resultPayload map[string]any) {
	// COS 转存成功时复用已下载图片解析尺寸；未启用 COS 时，才从原图文件头补测。
	var cosURLs []string
	// TransferImagesWithSizes already retries each source URL up to the configured
	// limit. Once that transfer attempt has run, do not probe the same URLs again
	// just to discover dimensions: a failed transfer must fall back to the
	// original upstream URLs without another download.
	transferAttempted := false
	imageOutputSizes = mergeImageOutputSizes(imageOutputSizes, nil, len(imageURLs))
	if s.cos != nil {
		if s.cos.IsEnabled(ctx) {
			transferAttempted = true
			transferred, transferredSizes, ok := s.cos.TransferImagesWithSizes(ctx, imageURLs)
			imageOutputSizes = mergeImageOutputSizes(imageOutputSizes, transferredSizes, len(imageURLs))
			if ok {
				cosURLs = transferred
			}
		}
		if !transferAttempted && hasMissingImageOutputSize(imageOutputSizes) {
			detectedSizes := s.cos.DetectImageSizes(ctx, imageURLs)
			imageOutputSizes = mergeImageOutputSizes(imageOutputSizes, detectedSizes, len(imageURLs))
		}
	}
	if resultPayload != nil {
		task.ResultPayload = cloneAsyncMediaPayload(resultPayload)
		if len(cosURLs) == len(imageURLs) && len(cosURLs) > 0 {
			mapping := make(map[string]string, len(imageURLs))
			for i := range imageURLs {
				if strings.TrimSpace(imageURLs[i]) != "" && strings.TrimSpace(cosURLs[i]) != "" {
					mapping[imageURLs[i]] = cosURLs[i]
				}
			}
			if len(mapping) > 0 {
				task.ResultPayload = replaceURLsInPayload(task.ResultPayload, mapping).(map[string]any)
			}
		}
	}
	logger.L().Info("async_media.image_output_sizes_resolved",
		zap.Int64("task_id", task.ID),
		zap.String("request_id", task.InternalRequestID),
		zap.Strings("image_output_sizes", imageOutputSizes))

	// 结算 finalCost：按实际出图数量重算。
	upstreamModel := amDerefStr(task.UpstreamModel)
	rawSize := amDerefStr(task.ImageSize)
	sizeTier := amDerefStr(task.SizeTier)
	quality := amDerefStr(task.Quality)
	finalTotalCost, finalCost, err := s.estimateCost(ctx, task.RequestedModel, upstreamModel, task.GroupID, rawSize, sizeTier, quality, len(imageURLs), task.RateMultiplier)
	if err != nil {
		// 结算失败时按预扣额结算，避免误退。
		finalCost = task.HeldCost
		finalTotalCost = asyncMediaBaseCost(task.HeldCost, task.RateMultiplier)
	}
	if finalCost > task.HeldCost {
		finalCost = task.HeldCost // 预扣为上限，不超额扣费
		finalTotalCost = asyncMediaBaseCost(finalCost, task.RateMultiplier)
	}

	updated, err := s.taskRepo.MarkSucceeded(ctx, task.ID, imageURLs, cosURLs, task.ImageMetadata, task.ResultPayload, finalCost)
	if err != nil {
		logger.L().Error("async_media.mark_succeeded_failed", zap.Int64("task_id", task.ID), zap.Error(err))
		return
	}
	if !updated {
		// 可能已被另一条轮询路径成功终结。回读任务并刷新当前对象，避免外层把旧的 running task 当失败处理。
		current, getErr := s.taskRepo.GetByID(ctx, task.ID)
		if getErr != nil {
			logger.L().Warn("async_media.mark_succeeded_reload_failed", zap.Int64("task_id", task.ID), zap.Error(getErr))
			return
		}
		if current == nil || current.Status != AsyncMediaStatusSucceeded {
			status := ""
			if current != nil {
				status = current.Status
			}
			logger.L().Info("async_media.mark_succeeded_skipped_terminal", zap.Int64("task_id", task.ID), zap.String("current_status", status))
			return
		}
		upstream := task.statusCacheUpstream
		*task = *current
		task.statusCacheUpstream = upstream
		repairTotalCost := finalTotalCost
		repairCost := task.FinalCost
		if repairCost <= 0 {
			repairCost = finalCost
			if task.RateMultiplier > 0 {
				repairTotalCost = asyncMediaBaseCost(repairCost, task.RateMultiplier)
			}
		}
		repairImages := task.ImageURLs
		if len(repairImages) == 0 {
			repairImages = imageURLs
		}
		repairCos := task.CosURLs
		if len(repairCos) == 0 {
			repairCos = cosURLs
		}
		logger.L().Info("async_media.mark_succeeded_already_terminal", zap.Int64("task_id", task.ID), zap.String("request_id", task.InternalRequestID))
		s.cacheTaskStatus(ctx, task)
		s.writeTerminalUsageLog(ctx, task, billingType, repairTotalCost, repairCost, amFloat64Ptr(accountRateMultiplier), BillingStatusCharged, repairImages, repairCos, imageOutputSizes)
		return
	}
	task.Status = AsyncMediaStatusSucceeded
	task.ImageURLs = imageURLs
	task.CosURLs = cosURLs
	task.FinalCost = finalCost
	task.UpdatedAt = time.Now().UTC()
	s.cacheTaskStatus(ctx, task)

	// 退还预扣与结算的差额。
	if refundDelta := task.HeldCost - finalCost; refundDelta > 0 {
		s.refund(ctx, billingType, asyncMediaBillingContext(task), refundDelta)
	}

	s.writeTerminalUsageLog(ctx, task, billingType, finalTotalCost, finalCost, amFloat64Ptr(accountRateMultiplier), BillingStatusCharged, imageURLs, cosURLs, imageOutputSizes)
}

// markFailedAndRefund 失败终态：退还全部预扣、置 refunded/expired，并写入错误记录。
func (s *AsyncMediaService) markFailedAndRefund(ctx context.Context, task *AsyncMediaTask, billingType int8, reason string) {
	status := AsyncMediaStatusRefunded
	if task.FailDeadlineAt != nil && time.Now().After(*task.FailDeadlineAt) {
		status = AsyncMediaStatusExpired
	}
	updated, err := s.taskRepo.MarkRefunded(ctx, task.ID, status, reason)
	if err != nil {
		logger.L().Error("async_media.mark_refunded_failed", zap.Int64("task_id", task.ID), zap.Error(err))
		return
	}
	if !updated {
		// 已被其它路径终结（幂等）：不重复退费。
		return
	}
	task.Status = status
	task.ErrorReason = amStrPtr(reason)
	task.UpdatedAt = time.Now().UTC()
	s.cacheTaskStatus(ctx, task)
	if task.HeldCost > 0 {
		s.refund(ctx, billingType, asyncMediaBillingContext(task), task.HeldCost)
	}
	s.recordTerminalMediaError(ctx, task, reason)
}

func (s *AsyncMediaService) recordTerminalMediaError(ctx context.Context, task *AsyncMediaTask, reason string) {
	if s == nil || s.opsService == nil || task == nil || strings.EqualFold(strings.TrimSpace(reason), "cancelled by client") {
		return
	}
	statusCode := upstreamStatusCodeFromMessage(reason)
	if statusCode == 0 {
		statusCode = http.StatusBadGateway
	}
	userID, apiKeyID := task.UserID, task.APIKeyID
	entry := &OpsInsertErrorLogInput{
		RequestID: task.InternalRequestID, ClientRequestID: task.InternalRequestID,
		UserID: &userID, APIKeyID: &apiKeyID, GroupID: task.GroupID,
		Platform: task.Facade, Model: task.RequestedModel,
		RequestedModel: task.RequestedModel, UpstreamModel: amDerefStr(task.UpstreamModel),
		InboundEndpoint: amDerefStr(task.InboundEndpoint), UpstreamEndpoint: amDerefStr(task.UpstreamEndpoint),
		UserAgent: amDerefStr(task.UserAgent), ErrorPhase: "upstream", ErrorType: "upstream_error",
		Severity: "error", StatusCode: statusCode, ErrorMessage: reason, ErrorBody: reason,
		ErrorSource: "upstream", ErrorOwner: "provider", CreatedAt: time.Now(),
	}
	if task.AccountID != nil {
		accountID := *task.AccountID
		entry.AccountID = &accountID
	}
	if task.ClientIP != nil {
		clientIP := *task.ClientIP
		entry.ClientIP = &clientIP
	}
	if statusCode > 0 {
		entry.UpstreamStatusCode = &statusCode
	}
	upstreamMessage := reason
	entry.UpstreamErrorMessage = &upstreamMessage
	if err := s.opsService.RecordError(ctx, entry); err != nil && !errors.Is(err, ErrOpsDisabled) {
		logger.L().Warn("async_media.error_log_failed", zap.Int64("task_id", task.ID), zap.Error(err))
	}
}

// writeTerminalUsageLog 终态追加写一条 usage_log。
func (s *AsyncMediaService) writeTerminalUsageLog(
	ctx context.Context,
	task *AsyncMediaTask,
	billingType int8,
	totalCost float64,
	actualCost float64,
	accountRateMultiplier *float64,
	billingStatus string,
	imageURLs, cosURLs, imageOutputSizes []string,
) {
	outputMetadata := ResolveImageBillingSize("", imageOutputSizes)
	imageCount := task.NumImages
	if billingStatus == BillingStatusCharged && len(imageURLs) > 0 {
		imageCount = len(imageURLs)
	}
	in := &TerminalUsageLogInput{
		UserID:                task.UserID,
		APIKeyID:              task.APIKeyID,
		AccountID:             amDerefInt64(task.AccountID),
		RequestID:             task.InternalRequestID,
		OrganizationID:        task.OrganizationID,
		PayerUserID:           task.PayerUserID,
		BalanceSource:         task.BalanceSource,
		AuthzGeneration:       task.AuthzGeneration,
		Model:                 amDerefStr(task.UpstreamModel),
		RequestedModel:        task.RequestedModel,
		UpstreamModel:         amDerefStr(task.UpstreamModel),
		GroupID:               task.GroupID,
		ChannelID:             task.ChannelID,
		TotalCost:             totalCost,
		ActualCost:            actualCost,
		RateMultiplier:        task.RateMultiplier,
		AccountRateMultiplier: accountRateMultiplier,
		BillingType:           billingType,
		RequestType:           int16(RequestTypeSync),
		ImageCount:            imageCount,
		ImageSize:             asyncMediaUsageLogImageSize(task),
		ImageInputSize:        amDerefStr(task.ImageSize),
		ImageQuality:          amDerefStr(task.Quality),
		ImageOutputSize:       outputMetadata.OutputSize,
		ImageSizeSource:       asyncMediaUsageLogImageSizeSource(task),
		ImageSizeBreakdown:    outputMetadata.Breakdown,
		RequestParameters:     task.RequestParameters,
		BillingTier:           asyncMediaUsageLogImageSize(task),
		TaskID:                task.ID,
		ImageURLs:             imageURLs,
		CosURLs:               cosURLs,
		BillingStatus:         billingStatus,

		ClientIP:         amDerefStr(task.ClientIP),
		UserAgent:        amDerefStr(task.UserAgent),
		InboundEndpoint:  amDerefStr(task.InboundEndpoint),
		UpstreamEndpoint: amDerefStr(task.UpstreamEndpoint),
		DurationMs:       asyncMediaDurationMs(task),
	}
	inserted, err := s.taskRepo.InsertTerminalUsageLog(ctx, in)
	if err != nil {
		logger.L().Warn("async_media.terminal_usage_log_failed",
			zap.Int64("task_id", task.ID),
			zap.String("request_id", task.InternalRequestID),
			zap.String("billing_status", billingStatus),
			zap.Float64("total_cost", totalCost),
			zap.Float64("actual_cost", actualCost),
			zap.String("image_input_size", in.ImageInputSize),
			zap.String("image_output_size", in.ImageOutputSize),
			zap.Error(err))
		return
	}
	if inserted {
		logger.L().Info("async_media.terminal_usage_log_written",
			zap.Int64("task_id", task.ID),
			zap.String("request_id", task.InternalRequestID),
			zap.String("billing_status", billingStatus),
			zap.Float64("total_cost", totalCost),
			zap.Float64("actual_cost", actualCost),
			zap.String("image_input_size", in.ImageInputSize),
			zap.String("image_output_size", in.ImageOutputSize),
			zap.Int("image_count", len(imageURLs)))
	} else {
		logger.L().Warn("async_media.terminal_usage_log_conflict_or_skipped",
			zap.Int64("task_id", task.ID),
			zap.String("request_id", task.InternalRequestID),
			zap.String("billing_status", billingStatus),
			zap.Float64("total_cost", totalCost),
			zap.Float64("actual_cost", actualCost),
			zap.String("image_input_size", in.ImageInputSize),
			zap.String("image_output_size", in.ImageOutputSize),
			zap.Int("image_count", len(imageURLs)))
	}
	if billingStatus == BillingStatusCharged && s.billingContextResolver != nil {
		billing := &BillingContext{ConsumerUserID: task.UserID, OrganizationID: task.OrganizationID, PayerUserID: asyncMediaPayerID(task), BalanceSource: amDerefStr(task.BalanceSource)}
		if err := s.billingContextResolver.RecordSpendLimitAlert(ctx, billing); err != nil {
			logger.L().Warn("async_media.spend_limit_alert_failed", zap.Int64("task_id", task.ID), zap.Error(err))
		}
	}
}

func asyncMediaUsageLogImageSize(task *AsyncMediaTask) string {
	if task == nil {
		return ImageBillingSize2K
	}
	if tier, ok := ClassifyImageBillingTier(amDerefStr(task.SizeTier)); ok {
		return tier
	}
	return NormalizeImageBillingTierOrDefault(amDerefStr(task.ImageSize))
}

func asyncMediaUsageLogImageSizeSource(task *AsyncMediaTask) string {
	if task != nil {
		if _, ok := ClassifyImageBillingTier(amDerefStr(task.ImageSize)); ok {
			return ImageSizeSourceInput
		}
	}
	return ImageSizeSourceDefault
}

func asyncMediaPayerID(task *AsyncMediaTask) int64 {
	if task != nil && task.PayerUserID != nil && *task.PayerUserID > 0 {
		return *task.PayerUserID
	}
	if task != nil {
		return task.UserID
	}
	return 0
}

func asyncMediaBillingContext(task *AsyncMediaTask) *BillingContext {
	if task == nil {
		return nil
	}
	return &BillingContext{
		ConsumerUserID:  task.UserID,
		OrganizationID:  task.OrganizationID,
		PayerUserID:     asyncMediaPayerID(task),
		BalanceSource:   amDerefStr(task.BalanceSource),
		AuthzGeneration: amDerefInt64(task.AuthzGeneration),
	}
}

func amInt64Ptr(value int64) *int64       { return &value }
func amFloat64Ptr(value float64) *float64 { return &value }

func asyncMediaBaseCost(actualCost, rateMultiplier float64) float64 {
	if rateMultiplier > 0 {
		return actualCost / rateMultiplier
	}
	return actualCost
}

// estimateCost 通过统一计费入口估算 (size × quality × count) 的实际费用。
//
// 计费优先级（与 OpenAI 网关 calculateOpenAIImageCost 保持一致）：
//  1. 分组/渠道逐模型定价且模式为 PerRequest/Image
//     → 走 CalculateCostUnified（按 size_tier 命中分层价或默认按次价）。
//  2. 否则回退分组二维价格矩阵（image_pricing_matrix），按原始分辨率 + quality
//     命中后调用 CalculateImageCostWithQuality；旧三档（Price1K/2K/4K）作为兼容兜底。
//
// 强校验：两条路径均未拿到正向费用时返回 ErrAsyncMediaPricingMissing，
// 调用方据此拒绝提交任务，避免账户被「0 费用刷图」。
func (s *AsyncMediaService) estimateCost(
	ctx context.Context,
	requestedModel, upstreamModel string, groupID *int64,
	rawSize, sizeTier, quality string, count int, rateMultiplier float64,
) (float64, float64, error) {
	if s.billing == nil {
		return 0, 0, fmt.Errorf("%w: billing service not initialized", ErrAsyncMediaPricingMissing)
	}
	if s.resolver == nil {
		return 0, 0, fmt.Errorf("%w: pricing resolver not initialized", ErrAsyncMediaPricingMissing)
	}
	if count <= 0 {
		count = 1
	}

	group, groupErr := s.loadPricingGroup(ctx, groupID)
	pricingModel, resolved := s.resolveConfiguredImagePricing(
		ctx, requestedModel, upstreamModel, groupID, group,
	)

	// 路径 1：分组/渠道逐模型定价，公开请求模型优先，上游映射模型兼容兜底。
	if resolved != nil {
		breakdown, err := s.billing.CalculateCostUnified(CostInput{
			Ctx:            ctx,
			Model:          pricingModel,
			GroupID:        groupID,
			Group:          group,
			RequestCount:   count,
			SizeTier:       imageBillingSizeOrTier(rawSize),
			Quality:        quality,
			RateMultiplier: rateMultiplier,
			Resolver:       s.resolver,
			Resolved:       resolved,
		})
		if err != nil && !errors.Is(err, ErrModelPricingUnavailable) {
			return 0, 0, err
		}
		if err == nil && breakdown != nil && (rateMultiplier <= 0 || breakdown.ActualCost > 0) {
			return breakdown.TotalCost, breakdown.ActualCost, nil
		}
		// 逐模型定价计算未拿到正向费用：继续走分组兜底（与 OpenAI 图片路径一致）。
	}

	// 路径 2：分组二维价格矩阵兜底。
	if groupErr != nil {
		return 0, 0, groupErr
	}
	groupCfg := buildAsyncMediaGroupImagePriceConfig(group, rawSize, quality)
	fallbackModel := strings.TrimSpace(upstreamModel)
	if fallbackModel == "" {
		fallbackModel = strings.TrimSpace(requestedModel)
	}
	if groupCfg == nil {
		return 0, 0, fmt.Errorf("%w: model=%s group=%v no channel pricing and group not loadable",
			ErrAsyncMediaPricingMissing, fallbackModel, groupID)
	}
	breakdown, err := s.billing.CalculateImageCostWithQualityValidated(fallbackModel, imageBillingSizeOrTier(rawSize), quality, count, groupCfg, rateMultiplier)
	if err != nil {
		return 0, 0, err
	}
	if breakdown == nil {
		return 0, 0, fmt.Errorf("%w: model=%s empty group breakdown", ErrAsyncMediaPricingMissing, fallbackModel)
	}
	if rateMultiplier > 0 && breakdown.ActualCost <= 0 {
		return 0, 0, fmt.Errorf("%w: model=%s size=%s quality=%s group fallback zero cost",
			ErrAsyncMediaPricingMissing, fallbackModel, sizeTier, quality)
	}
	return breakdown.TotalCost, breakdown.ActualCost, nil
}

func (s *AsyncMediaService) loadPricingGroup(ctx context.Context, groupID *int64) (*Group, error) {
	if s.groupRepo == nil || groupID == nil || *groupID == 0 {
		return nil, nil
	}
	group, err := s.groupRepo.GetByID(ctx, *groupID)
	if err != nil {
		return nil, fmt.Errorf("%w: load group %d: %v", ErrAsyncMediaPricingMissing, *groupID, err)
	}
	return group, nil
}

func (s *AsyncMediaService) resolveConfiguredImagePricing(
	ctx context.Context,
	requestedModel, upstreamModel string,
	groupID *int64,
	group *Group,
) (string, *ResolvedPricing) {
	type candidate struct {
		model    string
		resolved *ResolvedPricing
	}
	var channelCandidate *candidate
	for _, model := range imagePricingModelCandidates(requestedModel, upstreamModel) {
		resolved := s.resolver.Resolve(ctx, PricingInput{Model: model, GroupID: groupID, Group: group})
		if !isConfiguredImagePricing(resolved) {
			continue
		}
		if resolved.Source == PricingSourceGroup {
			return model, resolved
		}
		if channelCandidate == nil && resolved.Source == PricingSourceChannel {
			channelCandidate = &candidate{model: model, resolved: resolved}
		}
	}
	if channelCandidate != nil {
		return channelCandidate.model, channelCandidate.resolved
	}
	return "", nil
}

func buildAsyncMediaGroupImagePriceConfig(group *Group, rawSize, quality string) *ImagePriceConfig {
	if group == nil {
		return nil
	}
	rawW, rawH, _ := parseImageBillingDimensions(rawSize)
	return group.BuildImagePriceConfig(rawW, rawH, quality)
}

// charge 预扣费用（仅 BillingTypeBalance 走余额账本）。
func (s *AsyncMediaService) charge(ctx context.Context, billingType int8, billing *BillingContext, amount float64) error {
	if amount <= 0 || billingType != BillingTypeBalance || s.userRepo == nil {
		return nil
	}
	if billing == nil {
		return ErrUserNotFound
	}
	if billing.UsesCompanyBalance() {
		if s.billingContextResolver == nil {
			return fmt.Errorf("organization balance resolver is unavailable")
		}
		_, err := s.billingContextResolver.DeductOrganizationBalance(ctx, billing, amount)
		return err
	}
	if err := s.userRepo.DeductBalance(ctx, billing.PayerUserID, amount); err != nil {
		return err
	}
	if s.balanceCache != nil {
		if err := s.balanceCache.InvalidateUserBalance(ctx, billing.PayerUserID); err != nil {
			logger.L().Warn("async_media.balance_cache_invalidate_failed", zap.Int64("payer_user_id", billing.PayerUserID), zap.Error(err))
		}
	}
	return nil
}

// refund 退还费用（仅 BillingTypeBalance 走余额账本）。
func (s *AsyncMediaService) refund(ctx context.Context, billingType int8, billing *BillingContext, amount float64) {
	if amount <= 0 || billingType != BillingTypeBalance || s.userRepo == nil {
		return
	}
	if billing == nil {
		return
	}
	if billing.UsesCompanyBalance() {
		if s.billingContextResolver == nil {
			logger.L().Error("async_media.refund_failed", zap.Error(errors.New("organization balance resolver is unavailable")))
			return
		}
		if _, err := s.billingContextResolver.CreditOrganizationBalance(ctx, billing, amount); err != nil {
			logger.L().Error("async_media.refund_failed", zap.Int64("organization_id", *billing.OrganizationID), zap.Float64("amount", amount), zap.Error(err))
		}
		return
	}
	if err := s.userRepo.UpdateBalance(ctx, billing.PayerUserID, amount); err != nil {
		logger.L().Error("async_media.refund_failed",
			zap.Int64("user_id", billing.PayerUserID), zap.Float64("amount", amount), zap.Error(err))
		return
	}
	if s.balanceCache != nil {
		if err := s.balanceCache.InvalidateUserBalance(ctx, billing.PayerUserID); err != nil {
			logger.L().Warn("async_media.balance_cache_invalidate_failed", zap.Int64("payer_user_id", billing.PayerUserID), zap.Error(err))
		}
	}
}

// resolveUpstreamModel 解析客户端模型到 fal 上游 slug。
// 账号/渠道自定义映射优先，缺失时按是否为编辑请求选择内置默认 slug。
func (s *AsyncMediaService) resolveUpstreamModel(account *Account, requestedModel string, isEdit bool) string {
	if account != nil && account.Platform == PlatformLeonardo {
		if mapped := strings.TrimSpace(account.GetModelMapping()[requestedModel]); mapped != "" {
			return mapped
		}
		return "gpt-image-2"
	}
	return resolveFalUpstreamModel(account, requestedModel, isEdit)
}

func (s *AsyncMediaService) submitUpstream(ctx context.Context, in *AsyncMediaSubmitInput, upstreamModel string) (string, string, string, error) {
	if in.Account.Platform == PlatformLeonardo {
		client, err := s.newLeonardoClient(in.Account)
		if err != nil {
			return "", "", "", err
		}
		request := leonardo.BuildSubmitRequest(upstreamModel, in.Input, in.Account.LeonardoEstimatedCreditCost())
		idempotencyKey := "sub2api-" + strings.TrimSpace(in.InternalRequestID)
		logger.FromContext(ctx).Debug("leonardo.image.submit_parameters",
			zap.String("upstream", "leonardo"),
			zap.Int64("account_id", in.Account.ID),
			zap.Int64("group_id", derefGroupID(in.GroupID)),
			zap.Int64("api_key_id", in.APIKeyID),
			zap.String("internal_request_id", in.InternalRequestID),
			zap.String("requested_model", in.RequestedModel),
			zap.String("upstream_model", request.Model),
			zap.String("provider", request.Provider),
			zap.String("task_type", request.TaskType),
			zap.String("mode", request.Mode),
			zap.String("prompt", truncateLeonardoDebugPrompt(request.Input.Prompt)),
			zap.String("quality", request.Input.Quality),
			zap.Int("width", request.Input.Width),
			zap.Int("height", request.Input.Height),
			zap.Strings("reference_image_urls", request.Input.ReferenceImageURLs),
			zap.Float64("estimated_credit_cost", request.EstimatedCreditCost),
			zap.String("idempotency_key", idempotencyKey),
		)
		task, err := client.Submit(ctx, request, idempotencyKey, in.InternalRequestID)
		if err != nil {
			return "", "", "", err
		}
		taskURL := client.BuildTaskURL(task.TaskUUID)
		return task.TaskUUID, taskURL, taskURL, nil
	}
	client, err := s.newClient(in.Account)
	if err != nil {
		return "", "", "", err
	}
	if in.UpscaleRequest != nil {
		request := *in.UpscaleRequest
		logger.FromContext(ctx).Debug("fal.image.upscale_submit_parameters",
			zap.Int64("account_id", in.Account.ID),
			zap.Int64("group_id", derefGroupID(in.GroupID)),
			zap.Int64("api_key_id", in.APIKeyID),
			zap.String("requested_model", in.RequestedModel),
			zap.String("upstream_model", upstreamModel),
			zap.String("image_url", truncateFalDebugValue(request.ImageURL)),
			zap.String("upscale_mode", request.UpscaleMode),
			zap.Int("upscale_factor", request.UpscaleFactor),
			zap.String("output_format", request.OutputFormat),
		)
		response, submitErr := client.SubmitUpscale(ctx, upstreamModel, &request)
		if submitErr != nil {
			logger.FromContext(ctx).Debug("fal.image.upscale_submit_response",
				zap.String("upstream_model", upstreamModel), zap.Error(submitErr))
			return "", "", "", submitErr
		}
		logger.FromContext(ctx).Debug("fal.image.upscale_submit_response",
			zap.String("upstream_model", upstreamModel),
			zap.String("request_id", response.RequestID),
			zap.String("status_url", response.StatusURL),
			zap.String("response_url", response.ResponseURL),
		)
		statusURL := response.StatusURL
		if statusURL == "" {
			statusURL = client.BuildStatusURL(upstreamModel, response.RequestID)
		}
		responseURL := response.ResponseURL
		if responseURL == "" {
			responseURL = client.BuildResponseURL(upstreamModel, response.RequestID)
		}
		return response.RequestID, statusURL, responseURL, nil
	}
	var response *fal.SubmitResponse
	if len(in.RawRequestBody) > 0 {
		logger.FromContext(ctx).Debug("async_media.upstream_request",
			zap.String("operation", "submit"),
			zap.String("method", http.MethodPost),
			zap.String("url", strings.TrimRight(in.Account.FalQueueBaseURL(), "/")+"/"+strings.TrimLeft(upstreamModel, "/")),
			zap.String("upstream_model", upstreamModel),
			zap.String("request_body", truncateAsyncUpstreamBody(in.RawRequestBody)),
		)
		response, err = client.SubmitRaw(ctx, upstreamModel, in.RawRequestBody)
	} else {
		request := fal.BuildRequest(in.Input)
		if raw, marshalErr := json.Marshal(request); marshalErr == nil {
			logger.FromContext(ctx).Debug("async_media.upstream_request",
				zap.String("operation", "submit"),
				zap.String("method", http.MethodPost),
				zap.String("url", strings.TrimRight(in.Account.FalQueueBaseURL(), "/")+"/"+strings.TrimLeft(upstreamModel, "/")),
				zap.String("upstream_model", upstreamModel),
				zap.String("request_body", truncateAsyncUpstreamBody(raw)),
			)
		}
		response, err = client.Submit(ctx, upstreamModel, request)
	}
	if err != nil {
		logger.FromContext(ctx).Debug("async_media.upstream_response",
			zap.String("operation", "submit"), zap.String("upstream_model", upstreamModel), zap.Error(err))
		return "", "", "", err
	}
	if raw, marshalErr := json.Marshal(response); marshalErr == nil {
		logger.FromContext(ctx).Debug("async_media.upstream_response",
			zap.String("operation", "submit"), zap.String("upstream_model", upstreamModel),
			zap.String("response_body", truncateAsyncUpstreamBody(raw)))
	}
	statusURL := response.StatusURL
	if statusURL == "" {
		statusURL = client.BuildStatusURL(upstreamModel, response.RequestID)
	}
	responseURL := response.ResponseURL
	if responseURL == "" {
		responseURL = client.BuildResponseURL(upstreamModel, response.RequestID)
	}
	return response.RequestID, statusURL, responseURL, nil
}

func (s *AsyncMediaService) pollLeonardoOnce(ctx context.Context, task *AsyncMediaTask, account *Account, billingType int8) (*AsyncMediaTask, bool, error) {
	client, err := s.newLeonardoClient(account)
	if err != nil {
		return task, false, fmt.Errorf("async media poll: build leonardo client: %w", err)
	}
	requestID := amDerefStr(task.UpstreamRequestID)
	statusURL := client.BuildTaskURL(requestID)
	statusLog := logger.FromContext(ctx)
	statusLog.Debug("leonardo.image.status_parameters",
		zap.String("upstream", "leonardo"),
		zap.Int64("account_id", account.ID),
		zap.Int64("task_id", task.ID),
		zap.String("request_id", task.InternalRequestID),
		zap.String("upstream_request_id", requestID),
		zap.String("url", statusURL),
	)
	upstreamTask, err := client.GetTask(ctx, requestID, task.InternalRequestID)
	if err != nil {
		responseFields := []zap.Field{
			zap.String("upstream", "leonardo"),
			zap.Int64("account_id", account.ID),
			zap.Int64("task_id", task.ID),
			zap.String("request_id", task.InternalRequestID),
			zap.String("upstream_request_id", requestID),
			zap.String("url", statusURL),
			zap.String("error", err.Error()),
		}
		var apiErr *leonardo.APIError
		if errors.As(err, &apiErr) {
			responseFields = append(responseFields,
				zap.Int("status_code", apiErr.StatusCode),
				zap.String("response_body", apiErr.Body),
			)
		}
		statusLog.Debug("leonardo.image.status_response", responseFields...)
		if apiErr != nil && apiErr.StatusCode >= 400 && apiErr.StatusCode < 500 {
			s.markFailedAndRefund(ctx, task, billingType, fmt.Sprintf("status %d: %s", apiErr.StatusCode, apiErr.Body))
			return task, true, nil
		}
		return task, false, nil
	}
	status := ""
	if upstreamTask != nil {
		status = upstreamTask.Status
	}
	statusLog.Debug("leonardo.image.status_response",
		zap.String("upstream", "leonardo"),
		zap.Int64("account_id", account.ID),
		zap.Int64("task_id", task.ID),
		zap.String("request_id", task.InternalRequestID),
		zap.String("upstream_request_id", requestID),
		zap.String("url", statusURL),
		zap.String("status", status),
		zap.Any("response", upstreamTask),
	)
	if upstreamTask.IsFailed() {
		s.markFailedAndRefund(ctx, task, billingType, upstreamTask.FailureMessage())
		return task, true, nil
	}
	if !upstreamTask.IsCompleted() {
		return task, false, nil
	}
	imageURLs, imageOutputSizes, imageMetadata := extractLeonardoImageResult(upstreamTask)
	if len(imageURLs) == 0 {
		s.markFailedAndRefund(ctx, task, billingType, "upstream returned no images")
		return task, true, nil
	}
	task.ImageMetadata = imageMetadata
	s.markSucceeded(ctx, task, account.BillingRateMultiplier(), billingType, imageURLs, imageOutputSizes, nil)
	return task, true, nil
}

// resolveFalUpstreamModel 解析客户端模型到 fal 上游 slug（账号/渠道自定义映射优先，
// 缺失时按是否为编辑请求选择内置默认 slug）。抽成包级函数供异步执行与选号阶段共用，
// 确保「选号时的定价预判」与「提交时的实际计费」使用同一上游模型。
func resolveFalUpstreamModel(account *Account, requestedModel string, isEdit bool) string {
	var mapping map[string]string
	if account != nil {
		mapping = account.GetModelMapping()
	}
	if IsSeedVRUpscaleModel(requestedModel) {
		for _, endpoint := range mapping {
			if mapped := strings.TrimSpace(endpoint); IsSeedVRUpscaleModel(mapped) {
				return mapped
			}
		}
		// Never let a stale alias (for example seedvr -> gpt-image-2) change
		// the protocol used by this native facade.
		return defaultFalUpscaleEndpoint
	}
	mapped := strings.TrimSpace(mapping[requestedModel])
	if mapped == "" {
		// Account mappings may use the prefixed/unprefixed form of a native FAL
		// endpoint. Match normalized paths before applying any image default.
		reqPath := normalizeFalModelPath(requestedModel)
		for key, endpoint := range mapping {
			if normalizeFalModelPath(key) == reqPath || normalizeFalModelPath(endpoint) == reqPath {
				if candidate := strings.TrimSpace(endpoint); candidate != "" {
					mapped = candidate
					break
				}
			}
		}
	}
	if !isEdit {
		if mapped != "" {
			return mapped
		}
		return domain.FalSlugTextToImage
	}

	// 直接映射本身已经是 edit endpoint 时原样使用。
	if _, api := falEndpointModelAPI(mapped); strings.EqualFold(api, FalAPIEdit) {
		return mapped
	}

	// /images/edits 可能仍以基础模型名请求；从同一账号映射中寻找同模型的 edit endpoint。
	targetModel, _ := falEndpointModelAPI(mapped)
	if targetModel == "" {
		targetModel, _ = falEndpointModelAPI(requestedModel)
	}
	bestKey := ""
	editEndpoint := ""
	for key, endpoint := range mapping {
		model, api := falEndpointModelAPI(endpoint)
		if !strings.EqualFold(api, FalAPIEdit) || !strings.EqualFold(model, targetModel) {
			continue
		}
		if bestKey == "" || key < bestKey {
			bestKey = key
			editEndpoint = strings.TrimSpace(endpoint)
		}
	}
	if editEndpoint != "" {
		return editEndpoint
	}

	// 自定义映射只有基础 endpoint 时，保持其 organization/model 并切换到 edit API。
	if mapped != "" {
		if _, api := falEndpointModelAPI(mapped); api == "" {
			return strings.TrimRight(mapped, "/") + "/" + FalAPIEdit
		}
	}
	return domain.FalSlugImageEdit
}

func normalizeFalModelPath(model string) string {
	normalized := strings.ToLower(strings.Trim(strings.TrimSpace(model), "/"))
	return strings.TrimPrefix(normalized, "fal-ai/")
}

// IsSeedVRUpscaleModel reports whether a model path is the native SeedVR
// image upscale facade, accepting both prefixed and unprefixed forms.
func IsSeedVRUpscaleModel(model string) bool {
	normalized := strings.ToLower(strings.Trim(strings.TrimSpace(model), "/"))
	normalized = strings.TrimPrefix(normalized, "fal-ai/")
	return normalized == "seedvr/upscale/image"
}

func truncateFalDebugValue(value string) string {
	value = strings.TrimSpace(value)
	const maxLen = 256
	if len(value) > maxLen {
		return value[:maxLen] + "..."
	}
	return value
}

const asyncUpstreamLogBodyLimit = 4 << 10

// truncateAsyncUpstreamBody keeps async provider diagnostics useful without
// allowing prompts, data URIs, or provider payloads to flood the logs.
func truncateAsyncUpstreamBody(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var value any
	if json.Unmarshal(raw, &value) == nil {
		truncateAsyncUpstreamJSON(&value)
		if encoded, err := json.Marshal(value); err == nil {
			raw = encoded
		}
	}
	if len(raw) <= asyncUpstreamLogBodyLimit {
		return string(raw)
	}
	return string(raw[:asyncUpstreamLogBodyLimit]) + "...(truncated)"
}

func truncateAsyncUpstreamJSON(value *any) {
	switch current := (*value).(type) {
	case map[string]any:
		for key, child := range current {
			truncateAsyncUpstreamJSON(&child)
			current[key] = child
		}
	case []any:
		for i := range current {
			truncateAsyncUpstreamJSON(&current[i])
		}
	case string:
		trimmed := strings.TrimSpace(current)
		if strings.HasPrefix(strings.ToLower(trimmed), "data:") {
			*value = fmt.Sprintf("[redacted data URI, bytes=%d]", len(current))
			return
		}
		if len(current) > 1024 {
			*value = current[:1024] + "...(truncated)"
		}
	}
}

// newClient 基于账号凭证与代理构建 fal 客户端。
func (s *AsyncMediaService) newClient(account *Account) (*fal.Client, error) {
	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	return fal.NewClient(fal.Config{
		APIKey:       account.FalAPIKey(),
		QueueBaseURL: account.FalQueueBaseURL(),
		SyncBaseURL:  account.FalSyncBaseURL(),
		ProxyURL:     proxyURL,
	})
}

func (s *AsyncMediaService) newLeonardoClient(account *Account) (*leonardo.Client, error) {
	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	return leonardo.NewClient(leonardo.Config{
		APIKey:   account.LeonardoAPIKey(),
		BaseURL:  account.LeonardoBaseURL(),
		ProxyURL: proxyURL,
	})
}

// ----- helpers -----

func mergeImageOutputSizes(current, fallback []string, count int) []string {
	if count < 0 {
		count = 0
	}
	out := make([]string, count)
	copy(out, current)
	for i := 0; i < count && i < len(fallback); i++ {
		if strings.TrimSpace(out[i]) == "" {
			out[i] = strings.TrimSpace(fallback[i])
		}
	}
	return out
}

func hasMissingImageOutputSize(sizes []string) bool {
	for _, size := range sizes {
		if strings.TrimSpace(size) == "" {
			return true
		}
	}
	return false
}
func amStrPtr(s string) *string {
	if s == "" {
		return nil
	}
	v := s
	return &v
}

// asyncMediaDurationMs 估算任务从提交到终结的耗时（毫秒）。
// 优先使用 finished_at - created_at；终态尚未回填 finished_at 时回退为「距创建时刻」。
func asyncMediaDurationMs(task *AsyncMediaTask) int64 {
	if task == nil || task.CreatedAt.IsZero() {
		return 0
	}
	end := time.Now()
	if task.FinishedAt != nil && !task.FinishedAt.IsZero() {
		end = *task.FinishedAt
	}
	d := end.Sub(task.CreatedAt).Milliseconds()
	if d < 0 {
		return 0
	}
	return d
}

// falUpstreamEndpoint 由上游 fal 模型 slug 推导对外展示的上游端点路径。
func falUpstreamEndpoint(upstreamModel string) string {
	slug := strings.TrimSpace(upstreamModel)
	if slug == "" {
		return ""
	}
	if !strings.HasPrefix(slug, "/") {
		slug = "/" + slug
	}
	return slug
}

func asyncImageUpstreamEndpoint(account *Account, upstreamModel string) string {
	if account != nil && account.Platform == PlatformLeonardo {
		return strings.TrimRight(account.LeonardoBaseURL(), "/") + "/v1/tasks"
	}
	return falUpstreamEndpoint(upstreamModel)
}

func amDerefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func amOptInt64(v int64) *int64 {
	if v == 0 {
		return nil
	}
	return &v
}

func amDerefInt64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

const leonardoDebugPromptLimit = 2000

func truncateLeonardoDebugPrompt(prompt string) string {
	runes := []rune(prompt)
	if len(runes) <= leonardoDebugPromptLimit {
		return prompt
	}
	return string(runes[:leonardoDebugPromptLimit]) + "...(truncated)"
}
