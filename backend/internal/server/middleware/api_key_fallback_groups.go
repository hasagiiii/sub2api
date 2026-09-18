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
	// 绑定订阅的 Key 一定要走路由状态：它的可路由分组来自订阅的覆盖集合，没有
	// 路由状态就无法展开，请求会退化到单个 group_id。
	subscriptionBound := apiKey.UserSubscriptionID != nil && *apiKey.UserSubscriptionID > 0
	if !subscriptionBound && len(apiKey.FallbackGroupIDs) == 0 {
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
		subscription := candidate.Subscription
		if subscription == nil {
			// 按分组绑定的 Key：由 (用户, 分组) 反查订阅。
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
		// 绑定订阅的 Key 已在解析候选时带上了订阅本体（所有候选共用同一条），
		// 这里直接沿用，避免按分组反查时命中另一个覆盖同分组的额度池。
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
