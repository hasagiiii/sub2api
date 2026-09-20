package middleware

import (
	"context"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"go.uber.org/zap"
)

func prepareAPIKeyRoutingState(
	ctx context.Context,
	apiKeyService *service.APIKeyService,
	subscriptionService *service.SubscriptionService,
	apiKey *service.APIKey,
	skipBilling bool,
) (*service.APIKeyRoutingState, error) {
	if apiKey == nil {
		return nil, nil
	}
	// 指定了额度池的 Key 也必须建路由状态：没有它，订阅会退化成按 (用户, 分组)
	// 反查，在用户持有多个覆盖该分组的套餐时就会扣错池子——而这正是"指定套餐"
	// 要解决的问题。
	pinnedPool := apiKey.UserSubscriptionID != nil && *apiKey.UserSubscriptionID > 0
	if !pinnedPool && len(apiKey.FallbackGroupIDs) == 0 {
		return nil, nil
	}
	candidates := apiKeyService.ResolveAPIKeyRoutingCandidates(ctx, apiKey)
	rejectedSubscriptions := make(map[int]*service.UserSubscription)
	for index := range candidates {
		candidate := &candidates[index]
		// The primary enterprise group is checked against its organization
		// subscription by the auth middleware. It has no personal subscription
		// record to resolve here; only fallback candidates use this path.
		if index == 0 && apiKey.OrganizationSubscriptionID != nil {
			continue
		}
		if candidate.Unavailable != nil || candidate.Group == nil || skipBilling || !candidate.Group.IsSubscriptionType() {
			continue
		}
		if subscriptionService == nil {
			continue
		}
		// 优先用 Key 指定的额度池，但仅当它覆盖本候选分组时（回退可能落到套餐
		// 没覆盖的分组上，扣那条套餐等于让它为没买的分组付费）。
		subscription := apiKeyService.PinnedSubscriptionForGroup(ctx, apiKey, candidate.Group.ID)
		if subscription == nil {
			// 未指定或指定的池不覆盖该分组：由 (用户, 分组) 反查。
			resolved, err := subscriptionService.GetActiveSubscription(ctx, apiKey.UserID, candidate.Group.ID)
			if err != nil {
				if errors.Is(err, service.ErrSubscriptionNotFound) {
					candidate.Unavailable = err
					continue
				}
				return nil, err
			}
			subscription = resolved
		}
		needsMaintenance, validateErr := subscriptionService.ValidateAndCheckLimits(subscription, candidate.Group)
		if needsMaintenance {
			refreshed, maintenanceErr := subscriptionService.EnsureWindowMaintenanceForGroup(ctx, subscription, candidate.Group.ID)
			if maintenanceErr != nil {
				return nil, maintenanceErr
			}
			subscription = refreshed
			_, validateErr = subscriptionService.ValidateAndCheckLimits(subscription, candidate.Group)
		}
		if validateErr != nil {
			candidate.Unavailable = validateErr
			rejectedSubscriptions[index] = subscription
			continue
		}
		candidate.Subscription = subscription
	}
	state := service.NewAPIKeyRoutingState(apiKey, candidates)
	index, ok := state.FirstAvailable()
	if !ok {
		// Preserve the reason the candidates were rejected. In particular, a
		// subscription that has hit its limit must not be reported as though the
		// API key had no group at all; the auth middleware maps these errors to
		// the appropriate subscription/usage response.
		var firstErr error
		for index, candidate := range candidates {
			logAPIKeyRoutingRejection(ctx, apiKey, index, len(candidates), candidate, rejectedSubscriptions[index])
			if firstErr == nil && candidate.Unavailable != nil {
				firstErr = candidate.Unavailable
			}
		}
		if firstErr != nil {
			return nil, firstErr
		}
		return nil, service.ErrNoAvailableAccounts
	}
	state.Activate(index)
	return state, nil
}

func logAPIKeyRoutingRejection(ctx context.Context, apiKey *service.APIKey, index, count int, candidate service.APIKeyRoutingCandidate, sub *service.UserSubscription) {
	reason := candidate.Unavailable
	if reason == nil {
		reason = service.ErrNoAvailableAccounts
	}
	status, code, _ := apiKeyRoutingErrorResponse(reason)
	fields := []zap.Field{
		zap.String("component", "middleware.api_key_routing"),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Int64("user_id", apiKey.UserID),
		zap.Int("candidate_index", index),
		zap.Int("candidate_count", count),
		zap.Int("response_status", status),
		zap.String("reason_code", code),
		zap.Error(reason),
	}
	if apiKey.UserSubscriptionID != nil {
		fields = append(fields, zap.Int64("pinned_subscription_id", *apiKey.UserSubscriptionID))
	}
	if candidate.Group != nil {
		fields = append(fields,
			zap.Int64("group_id", candidate.Group.ID),
			zap.String("group_status", candidate.Group.Status),
		)
	}
	if sub != nil {
		limits := sub.EffectiveLimits(candidate.Group)
		fields = append(fields,
			zap.Int64("subscription_id", sub.ID),
			zap.String("subscription_status", sub.Status),
			zap.Time("subscription_expires_at", sub.ExpiresAt),
			zap.Any("daily_window_start", sub.DailyWindowStart),
			zap.Any("weekly_window_start", sub.WeeklyWindowStart),
			zap.Any("monthly_window_start", sub.MonthlyWindowStart),
			zap.Float64("daily_usage_usd", sub.DailyUsageUSD),
			zap.Float64("weekly_usage_usd", sub.WeeklyUsageUSD),
			zap.Float64("monthly_usage_usd", sub.MonthlyUsageUSD),
			zap.Any("daily_limit_usd", limits.DailyLimitUSD),
			zap.Any("weekly_limit_usd", limits.WeeklyLimitUSD),
			zap.Any("monthly_limit_usd", limits.MonthlyLimitUSD),
			zap.Any("plan_daily_limit_usd", sub.PlanLimits.DailyLimitUSD),
			zap.Any("plan_weekly_limit_usd", sub.PlanLimits.WeeklyLimitUSD),
			zap.Any("plan_monthly_limit_usd", sub.PlanLimits.MonthlyLimitUSD),
		)
		if sub.PlanID != nil {
			fields = append(fields, zap.Int64("plan_id", *sub.PlanID))
		}
	}
	logger.FromContext(ctx).Warn("api_key.routing_precheck_rejected", fields...)
}

// apiKeyRoutingErrorResponse translates a request-wide routing precheck failure
// into the public auth error. Candidate billing failures are intentionally
// kept distinct from a missing/disabled group so clients can tell whether they
// need a different key, a renewed subscription, or a quota reset.
func apiKeyRoutingErrorResponse(err error) (status int, code, message string) {
	if errors.Is(err, service.ErrDailyLimitExceeded) ||
		errors.Is(err, service.ErrWeeklyLimitExceeded) ||
		errors.Is(err, service.ErrMonthlyLimitExceeded) {
		return 429, "USAGE_LIMIT_EXCEEDED", err.Error()
	}
	if errors.Is(err, service.ErrSubscriptionNotFound) {
		return 403, "SUBSCRIPTION_NOT_FOUND", err.Error()
	}
	if errors.Is(err, service.ErrSubscriptionExpired) ||
		errors.Is(err, service.ErrSubscriptionSuspended) ||
		errors.Is(err, service.ErrSubscriptionInvalid) {
		return 403, "SUBSCRIPTION_INVALID", err.Error()
	}
	if errors.Is(err, service.ErrGroupNotFound) ||
		errors.Is(err, service.ErrGroupNotAllowed) ||
		errors.Is(err, service.ErrNoAvailableAccounts) {
		return 403, "NO_AVAILABLE_GROUP", "No available API key group"
	}
	return 500, "INTERNAL_ERROR", "Failed to resolve API key group"
}
