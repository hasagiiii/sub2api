package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/bytedance"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

var ErrBytedanceAlreadyRunning = errors.New("image request has already started and cannot be canceled")

type bytedanceImageClient interface {
	Generate(context.Context, map[string]any) (map[string]any, error)
}

func (s *AsyncMediaService) newBytedanceClient(account *Account) (bytedanceImageClient, error) {
	if s.bytedanceClientFactory != nil {
		return s.bytedanceClientFactory(account)
	}
	proxy := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxy = account.Proxy.URL()
	}
	return bytedance.NewClient(account.GetCredential("base_url"), account.GetCredential("api_key"), proxy)
}

func (s *AsyncMediaService) submitBytedance(ctx context.Context, in *AsyncMediaSubmitInput) (*AsyncMediaTask, error) {
	repo, ok := s.taskRepo.(BytedanceExecutionRepository)
	if !ok {
		return nil, errors.New("bytedance execution repository unavailable")
	}
	model := in.Account.GetMappedModel(in.RequestedModel)
	payload, metadata, err := bytedance.NormalizeRequest(in.RawRequestBody, model)
	if err != nil {
		return nil, err
	}
	if in.InternalRequestID == "" {
		in.InternalRequestID = uuid.NewString()
	}
	if existing, lookupErr := s.taskRepo.GetByInternalRequestID(ctx, in.InternalRequestID); lookupErr != nil {
		return nil, lookupErr
	} else if existing != nil {
		if existing.APIKeyID != in.APIKeyID || existing.RequestedModel != in.RequestedModel {
			return nil, errors.New("request ID conflict")
		}
		e, eErr := repo.GetBytedance(ctx, existing.ID)
		if eErr != nil {
			return nil, eErr
		}
		if e == nil {
			return nil, errors.New("request ID conflict")
		}
		left, _ := json.Marshal(e.RequestPayload)
		right, _ := json.Marshal(payload)
		if !bytes.Equal(left, right) {
			return nil, errors.New("request ID payload conflict")
		}
		return existing, nil
	}
	if !in.RateMultiplierSet && in.RateMultiplier == 0 {
		in.RateMultiplier = 1
	}
	size, _ := payload["size"].(string)
	tier := NormalizeImageBillingTierOrDefault(size)
	count := 1
	if payload["layer_decomposition"] == true {
		count = 16
	}
	unitPrice, _, err := s.estimateCost(ctx, in.RequestedModel, model, in.GroupID, size, tier, "", 1, in.RateMultiplier)
	if err != nil {
		return nil, err
	}
	held := unitPrice * float64(count) * in.RateMultiplier
	billing := &BillingContext{ConsumerUserID: in.UserID, PayerUserID: in.UserID, BalanceSource: BalanceSourceSelf}
	if s.billingContextResolver != nil {
		billing, err = s.billingContextResolver.ResolveForAmount(ctx, in.UserID, held)
		if err != nil {
			return nil, err
		}
	}
	parameters := cloneAsyncMediaPayload(payload)
	for key, value := range metadata {
		parameters[key] = value
	}
	parameters["_provider"] = PlatformBytedance
	parameters["_account_rate_multiplier"] = in.Account.BillingRateMultiplier()
	deadline := time.Now().Add(s.failTimeout)
	base := strings.TrimRight(in.Account.GetCredential("base_url"), "/")
	if base == "" {
		base = domain.BytedanceBaseURL
	}
	task := &AsyncMediaTask{InternalRequestID: in.InternalRequestID, UpstreamRequestID: amStrPtr(uuid.NewString()),
		APIKeyID: in.APIKeyID, UserID: in.UserID, AccountID: amOptInt64(in.Account.ID), GroupID: in.GroupID, ChannelID: in.ChannelID,
		OrganizationID: billing.OrganizationID, PayerUserID: amInt64Ptr(billing.PayerUserID), BalanceSource: amStrPtr(billing.BalanceSource), AuthzGeneration: amInt64Ptr(billing.AuthzGeneration),
		Facade: AsyncMediaFacadeFal, RequestedModel: in.RequestedModel, UpstreamModel: amStrPtr(model), ImageSize: amStrPtr(size), SizeTier: amStrPtr(tier), NumImages: count,
		RequestParameters: parameters, Status: AsyncMediaStatusPending, HeldCost: held, RateMultiplier: in.RateMultiplier, FailDeadlineAt: &deadline,
		ClientIP: amStrPtr(in.ClientIP), UserAgent: amStrPtr(in.UserAgent), InboundEndpoint: amStrPtr(in.InboundEndpoint), UpstreamEndpoint: amStrPtr(base + "/images/generations"), statusCacheUpstream: PlatformBytedance}
	e := &BytedanceExecution{RequestPayload: payload, BillingType: in.BillingType, UnitPrice: unitPrice}
	if err = repo.CreateBytedance(ctx, task, e); err != nil {
		return nil, err
	}
	s.invalidateBytedanceBalance(ctx, task)
	s.cacheTaskStatus(ctx, task)
	if s.backgroundPolling {
		s.startBytedanceWorker(task, in.Account)
	}
	return task, nil
}

func (s *AsyncMediaService) startBytedanceWorker(task *AsyncMediaTask, account *Account) {
	if task == nil {
		return
	}
	s.bytedanceMu.Lock()
	if s.bytedanceStopped || s.bytedanceWorkers[task.ID] {
		s.bytedanceMu.Unlock()
		return
	}
	if s.bytedanceCtx == nil {
		s.bytedanceCtx, s.bytedanceCancel = context.WithCancel(context.Background())
		s.bytedanceWorkers = make(map[int64]bool)
	}
	s.bytedanceWorkers[task.ID] = true
	s.bytedanceWG.Add(1)
	ctx := s.bytedanceCtx
	s.bytedanceMu.Unlock()
	go func() {
		defer s.bytedanceWG.Done()
		defer func() { s.bytedanceMu.Lock(); delete(s.bytedanceWorkers, task.ID); s.bytedanceMu.Unlock() }()
		if err := s.runBytedance(ctx, task.ID, account); err != nil {
			logger.L().Warn("bytedance.execution_failed", zap.Int64("task_id", task.ID), zap.Error(err))
		}
	}()
}

func (s *AsyncMediaService) StopBytedanceWorkers() {
	s.bytedanceMu.Lock()
	s.bytedanceStopped = true
	if s.bytedanceCancel != nil {
		s.bytedanceCancel()
	}
	s.bytedanceMu.Unlock()
	s.bytedanceWG.Wait()
}

func (s *AsyncMediaService) runBytedance(ctx context.Context, id int64, account *Account) error {
	repo, ok := s.taskRepo.(BytedanceExecutionRepository)
	if !ok {
		return errors.New("bytedance repository unavailable")
	}
	task, err := s.taskRepo.GetByID(ctx, id)
	if err != nil || task == nil {
		return err
	}
	e, err := repo.GetBytedance(ctx, id)
	if err != nil || e == nil {
		return err
	}
	if task.IsTerminal() {
		return nil
	}
	if (e.State == "pending" || e.State == "running") && task.FailDeadlineAt != nil && time.Now().After(*task.FailDeadlineAt) {
		_, err = repo.RefundBytedance(ctx, id, "image execution deadline exceeded", false)
		s.refreshBytedanceStatus(ctx, task)
		s.recordTerminalMediaError(ctx, task, "image execution deadline exceeded")
		return err
	}
	if e.State == "pending" {
		if account == nil || account.Platform != PlatformBytedance || !account.IsSchedulable() {
			_, err = repo.RefundBytedance(ctx, id, "image account is unavailable", false)
			s.refreshBytedanceStatus(ctx, task)
			return err
		}
		claimed, claimErr := repo.ClaimBytedance(ctx, id)
		if claimErr != nil || !claimed {
			return claimErr
		}
		client, clientErr := s.newBytedanceClient(account)
		var result map[string]any
		if clientErr == nil {
			deadline := time.Now().Add(5 * time.Minute)
			if task.FailDeadlineAt != nil && task.FailDeadlineAt.Before(deadline) {
				deadline = *task.FailDeadlineAt
			}
			requestCtx, cancel := context.WithDeadline(ctx, deadline)
			result, clientErr = client.Generate(requestCtx, e.RequestPayload)
			cancel()
		}
		if clientErr != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(clientErr, bytedance.ErrOutcomeUnknown) {
				s.recordTerminalMediaError(ctx, task, clientErr.Error())
				return clientErr
			}
			_, refundErr := repo.RefundBytedance(ctx, id, clientErr.Error(), false)
			s.refreshBytedanceStatus(ctx, task)
			s.recordTerminalMediaError(ctx, task, clientErr.Error())
			return refundErr
		}
		if err = repo.SaveBytedanceResult(ctx, id, result); err != nil {
			return err
		}
		e.ResultPayload = result
		e.State = "result_ready"
		if s.deferred != nil {
			s.deferred.ScheduleLastUsedUpdate(account.ID)
		}
	}
	if e.State != "result_ready" {
		return nil
	}
	return s.settleBytedanceResult(ctx, task, e, repo)
}

func (s *AsyncMediaService) settleBytedanceResult(ctx context.Context, task *AsyncMediaTask, e *BytedanceExecution, repo BytedanceExecutionRepository) error {
	intro := &ModelIntro{ResultField: "data[*].url"}
	if configured, err := s.lookupModelIntro(ctx, task.RequestedModel); err == nil && configured != nil {
		intro = configured
	}
	urls, _ := extractConfiguredImageURLs(e.ResultPayload, intro)
	if len(urls) == 0 {
		urls, _ = extractConfiguredImageURLs(e.ResultPayload, &ModelIntro{ResultField: "data[*].url"})
	}
	if len(urls) == 0 {
		// A persisted provider response is retained for investigation, even when no image can be billed.
		task.ResultPayload = cloneAsyncMediaPayload(e.ResultPayload)
		_, err := repo.RefundBytedance(ctx, task.ID, "upstream returned no images", false)
		s.refreshBytedanceStatus(ctx, task)
		return err
	}
	task.ImageURLs = urls
	task.ImageMetadata = extractConfiguredImageMetadata(e.ResultPayload, urls)
	data, _ := e.ResultPayload["data"].([]any)
	for i := range task.ImageMetadata {
		for _, raw := range data {
			entry, ok := raw.(map[string]any)
			if !ok || entry["url"] != task.ImageMetadata[i].URL {
				continue
			}
			if size, ok := entry["size"].(string); ok {
				parts := strings.Split(size, "x")
				if len(parts) == 2 {
					task.ImageMetadata[i].Width, _ = strconv.Atoi(parts[0])
					task.ImageMetadata[i].Height, _ = strconv.Atoi(parts[1])
				}
			}
			if format, ok := entry["output_format"].(string); ok {
				task.ImageMetadata[i].ContentType = "image/" + format
			}
		}
	}
	task.ResultPayload = cloneAsyncMediaPayload(e.ResultPayload)
	if s.cos != nil && s.cos.IsEnabled(ctx) {
		transferred, _, ok := s.cos.TransferImagesWithSizes(ctx, urls)
		if ok && len(transferred) == len(urls) {
			task.CosURLs = transferred
			mapping := map[string]string{}
			for i, u := range urls {
				mapping[u] = transferred[i]
			}
			replaceURLsInPayload(task.ResultPayload, mapping)
		}
	}
	count, countErr := bytedance.BillableImages(e.ResultPayload, e.RequestPayload["layer_decomposition"] == true)
	final := e.UnitPrice * float64(count) * task.RateMultiplier
	reason := ""
	if countErr != nil {
		count = -1
		final = task.HeldCost
		reason = countErr.Error()
	}
	_, err := repo.SettleBytedance(ctx, task, count, final, reason)
	if err != nil {
		_, err = repo.SettleBytedance(ctx, task, count, task.HeldCost, "image settlement failed; manual settlement required")
	}
	s.refreshBytedanceStatus(ctx, task)
	return err
}

func (s *AsyncMediaService) cancelBytedance(ctx context.Context, task *AsyncMediaTask) error {
	repo, ok := s.taskRepo.(BytedanceExecutionRepository)
	if !ok {
		return errors.New("bytedance repository unavailable")
	}
	_, err := repo.RefundBytedance(ctx, task.ID, "cancelled by client", true)
	if err == nil {
		s.refreshBytedanceStatus(ctx, task)
	}
	return err
}

func (s *AsyncMediaService) refreshBytedanceStatus(ctx context.Context, task *AsyncMediaTask) {
	current, err := s.taskRepo.GetByID(ctx, task.ID)
	if err == nil && current != nil {
		current.statusCacheUpstream = PlatformBytedance
		s.cacheTaskStatus(ctx, current)
	}
	s.invalidateBytedanceBalance(ctx, task)
}
func (s *AsyncMediaService) invalidateBytedanceBalance(ctx context.Context, task *AsyncMediaTask) {
	if s.balanceCache != nil {
		_ = s.balanceCache.InvalidateUserBalance(ctx, amDerefInt64(task.PayerUserID))
	}
}

func (s *AsyncMediaService) CompleteBytedanceManualBilling(ctx context.Context, id int64, finalCost float64) error {
	if finalCost < 0 || math.IsNaN(finalCost) || math.IsInf(finalCost, 0) {
		return errors.New("invalid final cost")
	}
	repo, ok := s.taskRepo.(BytedanceExecutionRepository)
	if !ok {
		return errors.New("bytedance repository unavailable")
	}
	task, err := s.taskRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if task == nil {
		return fmt.Errorf("image task not found")
	}
	e, err := repo.GetBytedance(ctx, id)
	if err != nil {
		return err
	}
	if e == nil || e.State != "billing_failed" {
		return errors.New("image task is not awaiting manual settlement")
	}
	count := -1
	if e.BillableImages != nil {
		count = *e.BillableImages
	}
	_, err = repo.SettleBytedance(ctx, task, count, finalCost, "")
	if err == nil {
		s.refreshBytedanceStatus(ctx, task)
	}
	return err
}

// BytedanceRequestInput extracts the dimensions needed by the common estimate facade.
func BytedanceRequestInput(raw []byte, model string) ([]byte, string, int, error) {
	p, meta, err := bytedance.NormalizeRequest(raw, model)
	if err != nil {
		return nil, "", 0, err
	}
	for key, v := range meta {
		p[key] = v
	}
	encoded, err := json.Marshal(p)
	count := 1
	if p["layer_decomposition"] == true {
		count = 16
	}
	size, _ := p["size"].(string)
	return encoded, size, count, err
}

func BytedanceTerminalUsageInput(task *AsyncMediaTask, count int, finalCost, unitPrice float64, billingType int8, status string) *TerminalUsageLogInput {
	knownCount := count >= 0
	if count < 0 {
		count = 0
	}
	parameters := cloneAsyncMediaPayload(task.RequestParameters)
	if parameters == nil {
		parameters = map[string]any{}
	}
	if usage, ok := task.ResultPayload["usage"]; ok {
		parameters["usage"] = usage
	}
	parameters["billable_images"] = count
	if !knownCount {
		parameters["billable_images"] = nil
	}
	parameters["platform"] = PlatformBytedance
	accountRate := 1.0
	if n, ok := parameters["_account_rate_multiplier"].(float64); ok {
		accountRate = n
	}
	delete(parameters, "_account_rate_multiplier")
	delete(parameters, "_provider")
	total := unitPrice * float64(count)
	if status == BillingStatusFailed {
		total = asyncMediaBaseCost(task.HeldCost, task.RateMultiplier)
	}
	return &TerminalUsageLogInput{UserID: task.UserID, APIKeyID: task.APIKeyID, AccountID: amDerefInt64(task.AccountID), RequestID: task.InternalRequestID,
		OrganizationID: task.OrganizationID, PayerUserID: task.PayerUserID, BalanceSource: task.BalanceSource, AuthzGeneration: task.AuthzGeneration,
		Model: amDerefStr(task.UpstreamModel), RequestedModel: task.RequestedModel, UpstreamModel: amDerefStr(task.UpstreamModel), GroupID: task.GroupID, ChannelID: task.ChannelID,
		TotalCost: total, ActualCost: finalCost, RateMultiplier: task.RateMultiplier, AccountRateMultiplier: &accountRate, BillingType: billingType, RequestType: int16(RequestTypeSync),
		ImageCount: count, ImageSize: asyncMediaUsageLogImageSize(task), ImageInputSize: amDerefStr(task.ImageSize), BillingTier: amDerefStr(task.SizeTier), RequestParameters: parameters,
		TaskID: task.ID, ImageURLs: task.ImageURLs, CosURLs: task.CosURLs, BillingStatus: status, ClientIP: amDerefStr(task.ClientIP), UserAgent: amDerefStr(task.UserAgent), InboundEndpoint: amDerefStr(task.InboundEndpoint), UpstreamEndpoint: amDerefStr(task.UpstreamEndpoint), DurationMs: asyncMediaDurationMs(task)}
}
