package middleware

import (
	"context"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/service"
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
			refreshed, maintenanceErr := subscriptionService.EnsureWindowMaintenance(ctx, subscription)
			if maintenanceErr != nil {
				return nil, maintenanceErr
			}
			subscription = refreshed
			_, validateErr = subscriptionService.ValidateAndCheckLimits(subscription, candidate.Group)
		}
		if validateErr != nil {
			candidate.Unavailable = validateErr
			continue
		}
		candidate.Subscription = subscription
	}
	state := service.NewAPIKeyRoutingState(apiKey, candidates)
	index, ok := state.FirstAvailable()
	if !ok {
		return nil, service.ErrNoAvailableAccounts
	}
	state.Activate(index)
	return state, nil
}
