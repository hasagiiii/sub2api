package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/fal"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const inlineImageWorkerLimit = 8

func (s *AsyncMediaService) SetInlineImageGenerator(fn InlineImageGenerator) { s.inlineGenerator = fn }

func (s *AsyncMediaService) SubmitOpenAIImageTask(ctx context.Context, in *AsyncMediaSubmitInput, payload map[string]any) (*AsyncMediaTask, error) {
	repo, ok := s.taskRepo.(InlineImageRepository)
	if !ok || s.inlineGenerator == nil {
		return nil, errors.New("background image executor unavailable")
	}
	if in == nil || in.Account == nil {
		return nil, errors.New("missing image account")
	}
	if in.InternalRequestID == "" {
		in.InternalRequestID = uuid.NewString()
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(raw)
	fingerprint := hex.EncodeToString(hash[:])
	lookup := func() (*AsyncMediaTask, error) {
		return s.LookupInlineImageTask(ctx, in.InternalRequestID, in.APIKeyID, in.RequestedModel, payload)
	}

	if existing, err := lookup(); err != nil || existing != nil {
		return existing, err
	}
	if !in.RateMultiplierSet && in.RateMultiplier == 0 {
		in.RateMultiplier = 1
	}
	count := max(1, in.Input.N)
	tier := NormalizeImageBillingTierOrDefault(in.Input.Size)
	quality := fal.MapQualityToFal(in.Input.Quality)
	upstream := in.Account.GetMappedModel(modelAPIImageAccountModel(in.Account, in.RequestedModel))
	_, held, err := s.estimateCost(ctx, in.RequestedModel, upstream, in.GroupID, in.Input.Size, tier, quality, count, in.RateMultiplier, len(in.Input.ImageURLs))
	if err != nil {
		return nil, err
	}
	payer := &BillingContext{ConsumerUserID: in.UserID, PayerUserID: in.UserID, BalanceSource: BalanceSourceSelf}
	if s.billingContextResolver != nil {
		payer, err = s.billingContextResolver.ResolveForAmount(ctx, in.UserID, held)
		if err != nil {
			return nil, err
		}
	}
	deadline := time.Now().Add(s.failTimeout)
	task := &AsyncMediaTask{ExecutionKind: InlineImageExecutionOpenAI, InternalRequestID: in.InternalRequestID, UpstreamRequestID: amStrPtr(in.InternalRequestID),
		AccountID: amOptInt64(in.AccountID), APIKeyID: in.APIKeyID, UserID: in.UserID, GroupID: in.GroupID, ChannelID: in.ChannelID,
		OrganizationID: payer.OrganizationID, PayerUserID: amInt64Ptr(payer.PayerUserID), BalanceSource: amStrPtr(payer.BalanceSource), AuthzGeneration: amInt64Ptr(payer.AuthzGeneration),
		Facade: AsyncMediaFacadeOpenAI, RequestedModel: in.RequestedModel, UpstreamModel: amStrPtr(upstream), ImageSize: amStrPtr(in.Input.Size), Quality: amStrPtr(quality), NumImages: count,
		RequestParameters: cloneAsyncMediaPayload(in.RequestParameters), Status: AsyncMediaStatusPending, HeldCost: held, RateMultiplier: in.RateMultiplier, SizeTier: amStrPtr(tier), FailDeadlineAt: &deadline,
		ClientIP: amStrPtr(in.ClientIP), UserAgent: amStrPtr(in.UserAgent), XApp: RequestAppFromContext(ctx), InboundEndpoint: amStrPtr(in.InboundEndpoint), UpstreamEndpoint: amStrPtr("/v1/images/generations"), statusCacheUpstream: PlatformOpenAI}
	if in.Input.IsEdit {
		task.UpstreamEndpoint = amStrPtr("/v1/images/edits")
	}
	journal := &InlineImageExecution{RequestPayload: cloneAsyncMediaPayload(payload), RequestHash: fingerprint, BillingType: in.BillingType}
	if err = repo.CreateInlineImage(ctx, task, journal); err != nil {
		if errors.Is(err, ErrInlineImageConflict) {
			return lookup()
		}
		return nil, err
	}
	s.invalidateBytedanceBalance(ctx, task)
	s.cacheTaskStatus(ctx, task)
	if s.backgroundPolling {
		s.startInlineImageWorker(task, in.Account)
	}
	logger.L().Info("image_task.accepted", zap.Int64("task_id", task.ID), zap.String("request_id", task.InternalRequestID), zap.Int("requested_images", count))
	return task, nil
}

func (s *AsyncMediaService) startInlineImageWorker(task *AsyncMediaTask, account *Account) {
	if task == nil || task.IsTerminal() || s.inlineGenerator == nil {
		return
	}
	s.inlineMu.Lock()
	if s.inlineStopped || s.inlineWorkers[task.ID] || len(s.inlineWorkers) >= inlineImageWorkerLimit {
		s.inlineMu.Unlock()
		return
	}
	if s.inlineCtx == nil {
		s.inlineCtx, s.inlineCancel = context.WithCancel(context.Background())
		s.inlineWorkers = map[int64]bool{}
	}
	s.inlineWorkers[task.ID] = true
	s.inlineWG.Add(1)
	parent := s.inlineCtx
	s.inlineMu.Unlock()
	go func() {
		defer s.inlineWG.Done()
		defer func() { s.inlineMu.Lock(); delete(s.inlineWorkers, task.ID); s.inlineMu.Unlock() }()
		defer func() {
			if p := recover(); p != nil {
				logger.L().Error("image_task.worker_panic", zap.Int64("task_id", task.ID), zap.Any("panic", p))
			}
		}()
		if err := s.runInlineImage(parent, task.ID, account); err != nil {
			logger.L().Warn("image_task.stage_retry_pending", zap.Int64("task_id", task.ID), zap.String("request_id", task.InternalRequestID), zap.Error(err))
		}
	}()
}
func (s *AsyncMediaService) StopInlineImageWorkers() {
	s.inlineMu.Lock()
	s.inlineStopped = true
	if s.inlineCancel != nil {
		s.inlineCancel()
	}
	s.inlineMu.Unlock()
	s.inlineWG.Wait()
}

func (s *AsyncMediaService) runInlineImage(parent context.Context, id int64, account *Account) error {
	repo, ok := s.taskRepo.(InlineImageRepository)
	if !ok {
		return errors.New("inline image repository unavailable")
	}
	processingTimeout := s.failTimeout
	if processingTimeout <= 0 {
		processingTimeout = defaultAsyncMediaFailTimeout
	}
	ctx, cancel := context.WithTimeout(parent, processingTimeout)
	defer cancel()
	task, err := s.taskRepo.GetByID(ctx, id)
	if err != nil || task == nil {
		return err
	}
	if task.IsTerminal() {
		return nil
	}
	e, err := repo.GetInlineImage(ctx, id)
	if err != nil || e == nil {
		return err
	}
	if (e.State == "generating" || e.State == "pending") && task.FailDeadlineAt != nil && time.Now().After(*task.FailDeadlineAt) {
		reason := "Generation outcome could not be confirmed; held funds require review. This task was not generated again."
		if e.State == "pending" {
			reason = "Image task expired before generation started."
		}
		changed, failErr := repo.FailInlineImage(ctx, id, reason, e.State == "pending", false)
		err = failErr
		if changed {
			s.recordInlineImageFailure(ctx, task, reason, e.State == "pending")
		}
		s.refreshInlineImage(ctx, id)
		return err
	}

	key := &APIKey{ID: task.APIKeyID, UserID: task.UserID, GroupID: task.GroupID}
	if e.State == "pending" {
		if account == nil {
			return errors.New("image account could not be loaded; execution remains queued")
		}
		if task.GroupID != nil && s.groupRepo != nil {
			key.Group, err = s.groupRepo.GetByID(ctx, *task.GroupID)
			if err != nil {
				return err
			}
		}
		if s.userRepo != nil {
			key.User, err = s.userRepo.GetByID(ctx, task.UserID)
			if err != nil {
				return err
			}
		}
	}
	token := uuid.NewString()
	claimed, err := repo.ClaimInlineImage(ctx, id, token)
	if err != nil || !claimed {
		return err
	}
	s.refreshInlineImage(ctx, id)
	defer func() {
		releaseCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		_ = repo.ReleaseInlineImage(releaseCtx, id, token)
	}()
	// Renew ownership while generating/storing. A lost lease cancels local work;
	// persisted generation is never claimed a second time, even after a restart.
	heartbeatDone := make(chan struct{})
	heartbeatExited := make(chan struct{})
	defer func() { close(heartbeatDone); cancel(); <-heartbeatExited }()
	go func() {
		defer close(heartbeatExited)
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatDone:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				heartbeatCtx, stop := context.WithTimeout(ctx, 5*time.Second)
				renewed, err := repo.RenewInlineImage(heartbeatCtx, id, token)
				stop()
				if err != nil || !renewed {
					cancel()
					return
				}
			}
		}
	}()
	if e.State == "pending" {
		var result *AsyncMediaInlineResult
		definite := false
		if account == nil || !account.IsSchedulable() {
			err = errors.New("image account is unavailable")
			definite = true
		} else {
			deadline := time.Now().Add(processingTimeout)
			if task.FailDeadlineAt != nil && task.FailDeadlineAt.Before(deadline) {
				deadline = *task.FailDeadlineAt
			}
			generationCtx, stop := context.WithDeadline(ctx, deadline)
			logger.L().Info("image_task.generation_started", zap.Int64("task_id", id), zap.String("request_id", task.InternalRequestID))
			result, err = s.inlineGenerator(generationCtx, task, account, key, e.RequestPayload)
			stop()
		}
		if err == nil && (result == nil || len(result.ImageURLs) == 0) {
			err = errors.New("upstream returned no images")
			definite = true
		}
		if err != nil {
			var upstream *OpenAIImagesUpstreamError
			if errors.Is(err, ErrQueuedImageNotStarted) || (errors.As(err, &upstream) && upstream.StatusCode >= 400 && upstream.StatusCode < 500) {
				definite = true
			}
			reason := err.Error()
			if !definite {
				reason = "Generation outcome could not be confirmed; held funds require review. " + reason
			}
			if saveErr := retryInlineImageSave(func(saveCtx context.Context) error {
				return repo.SaveInlineImageFailure(saveCtx, id, token, reason, definite)
			}); saveErr != nil {
				return saveErr
			}
			e.State = "failure_ready"
			e.FailureReason = reason
			e.FailureDefinitive = definite
		} else {
			// Persist raw output before storage; upload retries never invoke the provider again.
			if err = retryInlineImageSave(func(saveCtx context.Context) error {
				return repo.SaveInlineImageResult(saveCtx, id, token, result, false)
			}); err != nil {
				return err
			}
			e.State = "result_ready"
			e.Result = result
			logger.L().Info("image_task.generation_checkpointed", zap.Int64("task_id", id), zap.String("request_id", task.InternalRequestID), zap.String("upstream_request_id", result.RequestID), zap.Int("generated_images", len(result.ImageURLs)))
		}
	}
	if e.State == "failure_ready" {
		finishCtx, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		changed, failErr := repo.FailInlineImage(finishCtx, id, e.FailureReason, e.FailureDefinitive, false)
		err = failErr
		if changed {
			s.recordInlineImageFailure(finishCtx, task, e.FailureReason, e.FailureDefinitive)
		}
		s.refreshInlineImage(finishCtx, id)
		return err
	}
	if e.State == "result_ready" {
		if e.Result == nil {
			return errors.New("missing image result checkpoint")
		}
		if err = s.storeInlineImageResult(ctx, task, e.Result, func() error { return repo.SaveInlineImageResult(ctx, id, token, e.Result, false) }); err != nil {
			return err
		}
		if err = retryInlineImageSave(func(saveCtx context.Context) error {
			return repo.SaveInlineImageResult(saveCtx, id, token, e.Result, true)
		}); err != nil {
			return err
		}
		e.State = "stored"
	}
	if e.State != "stored" || e.Result == nil {
		return nil
	}
	task.ImageURLs = e.Result.ImageURLs
	task.CosURLs = e.Result.COSURLs
	task.ImageMetadata = e.Result.ImageMetadata
	task.ResultPayload = e.Result.ResultPayload
	finishCtx, done := context.WithTimeout(context.Background(), 30*time.Second)
	defer done()
	total, actual, err := s.estimateCost(finishCtx, task.RequestedModel, amDerefStr(task.UpstreamModel), task.GroupID, amDerefStr(task.ImageSize), amDerefStr(task.SizeTier), amDerefStr(task.Quality), len(task.ImageURLs), task.RateMultiplier, asyncInputImageCount(task))
	if err != nil {
		return fmt.Errorf("price completed images: %w", err)
	}
	actual = min(actual, task.HeldCost)
	usage := InlineImageTerminalUsage(task, e.BillingType, total, actual, BillingStatusCharged)
	metadata := ResolveImageBillingSize("", e.Result.ImageOutputSizes)
	usage.ImageOutputSize = metadata.OutputSize
	usage.ImageSizeBreakdown = metadata.Breakdown
	if account != nil {
		usage.AccountRateMultiplier = amFloat64Ptr(account.BillingRateMultiplier())
	}
	changed, err := repo.FinishInlineImage(finishCtx, task, usage)
	if err != nil {
		return err
	}
	s.refreshInlineImage(finishCtx, id)
	if changed {
		logger.L().Info("image_task.completed", zap.Int64("task_id", id), zap.String("request_id", task.InternalRequestID), zap.Int("generated_images", len(task.ImageURLs)))
		s.scheduleAccountLastUsed(account)
	}
	return nil
}

func retryInlineImageSave(save func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for {
		attempt, stop := context.WithTimeout(ctx, 5*time.Second)
		err := save(attempt)
		stop()
		if err == nil {
			return nil
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return err
		case <-timer.C:
		}
	}
}
func (s *AsyncMediaService) refreshInlineImage(ctx context.Context, id int64) {
	if task, err := s.taskRepo.GetByID(ctx, id); err == nil && task != nil {
		s.invalidateBytedanceBalance(ctx, task)
		s.cacheTaskStatus(ctx, task)
	}
}

func InlineImageTerminalUsage(task *AsyncMediaTask, billing int8, total, actual float64, status string) *TerminalUsageLogInput {
	return &TerminalUsageLogInput{UserID: task.UserID, APIKeyID: task.APIKeyID, AccountID: amDerefInt64(task.AccountID), RequestID: task.InternalRequestID,
		OrganizationID: task.OrganizationID, PayerUserID: task.PayerUserID, BalanceSource: task.BalanceSource, AuthzGeneration: task.AuthzGeneration,
		Model: amDerefStr(task.UpstreamModel), RequestedModel: task.RequestedModel, UpstreamModel: amDerefStr(task.UpstreamModel), GroupID: task.GroupID, ChannelID: task.ChannelID,
		TotalCost: total, ActualCost: actual, RateMultiplier: task.RateMultiplier, BillingType: billing, RequestType: int16(RequestTypeSync), ImageCount: len(task.ImageURLs),
		ImageSize: asyncMediaUsageLogImageSize(task), ImageInputSize: amDerefStr(task.ImageSize), ImageQuality: amDerefStr(task.Quality), BillingTier: amDerefStr(task.SizeTier), RequestParameters: task.RequestParameters,
		TaskID: task.ID, ImageURLs: task.ImageURLs, CosURLs: task.CosURLs, BillingStatus: status, ClientIP: amDerefStr(task.ClientIP), UserAgent: amDerefStr(task.UserAgent), XApp: amDerefStr(task.XApp),
		InboundEndpoint: amDerefStr(task.InboundEndpoint), UpstreamEndpoint: amDerefStr(task.UpstreamEndpoint), DurationMs: asyncMediaDurationMs(task)}
}

// LookupInlineImageTask runs before scheduling so retries can find an accepted task
// even if the original upstream account is currently unavailable.
func (s *AsyncMediaService) LookupInlineImageTask(ctx context.Context, requestID string, keyID int64, model string, payload map[string]any) (*AsyncMediaTask, error) {
	if s == nil || s.taskRepo == nil {
		return nil, errors.New("image task repository unavailable")
	}
	if requestID == "" {
		return nil, nil
	}
	task, err := s.taskRepo.GetByInternalRequestID(ctx, requestID)
	if err != nil || task == nil {
		return nil, err
	}
	if task.ExecutionKind != InlineImageExecutionOpenAI || task.APIKeyID != keyID || task.RequestedModel != model {
		return nil, ErrInlineImageConflict
	}
	repo, ok := s.taskRepo.(InlineImageRepository)
	if !ok {
		return nil, errors.New("inline image repository unavailable")
	}
	e, err := repo.GetInlineImage(ctx, task.ID)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(raw)
	if e == nil || e.RequestHash != hex.EncodeToString(hash[:]) {
		return nil, ErrInlineImageConflict
	}
	return task, nil
}

func (s *AsyncMediaService) InlineImageStage(ctx context.Context, id int64) string {
	if repo, ok := s.taskRepo.(InlineImageRepository); ok {
		if state, err := repo.GetInlineImageState(ctx, id); err == nil {
			switch state {
			case "pending":
				return "queued"
			case "generating":
				return "generating"
			case "result_ready":
				return "storing"
			case "stored", "failure_ready":
				return "finalizing"
			}
		}
	}
	return ""
}

func (s *AsyncMediaService) recordInlineImageFailure(ctx context.Context, task *AsyncMediaTask, reason string, definitive bool) {
	event := "image_task.outcome_unknown"
	if definitive {
		event = "image_task.failed_and_refunded"
	}
	logger.L().Warn(event, zap.Int64("task_id", task.ID), zap.String("request_id", task.InternalRequestID), zap.String("reason", reason))
	s.recordTerminalMediaError(ctx, task, reason)
}
