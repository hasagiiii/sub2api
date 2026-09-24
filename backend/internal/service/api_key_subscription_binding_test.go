package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type boundKeyGroupRepo struct {
	groupRepoNoop
	groups map[int64]*Group
}

func (r *boundKeyGroupRepo) GetByID(_ context.Context, id int64) (*Group, error) {
	if g, ok := r.groups[id]; ok {
		return g, nil
	}
	return nil, ErrGroupNotFound
}

type boundKeySubRepo struct {
	userSubRepoNoop
	sub        *UserSubscription
	err        error
	activeSubs []UserSubscription
}

func (r *boundKeySubRepo) GetByID(context.Context, int64) (*UserSubscription, error) {
	if r.err != nil {
		return nil, r.err
	}
	cp := *r.sub
	return &cp, nil
}

func (r *boundKeySubRepo) ListActiveByUserID(context.Context, int64) ([]UserSubscription, error) {
	return append([]UserSubscription(nil), r.activeSubs...), nil
}

func activeSharedSubscription() *UserSubscription {
	return &UserSubscription{
		ID:        77,
		UserID:    5,
		GroupID:   10,
		GroupIDs:  []int64{10, 11, 12},
		Status:    SubscriptionStatusActive,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
}

// pinnedPoolKey 是一把"分组 10 + 指定套餐 77"的 Key，并配了一个同平台回退分组。
func pinnedPoolKey() *APIKey {
	subID := int64(77)
	groupID := int64(10)
	return &APIKey{ID: 1, UserID: 5, GroupID: &groupID, FallbackGroupIDs: []int64{11}, UserSubscriptionID: &subID}
}

func pinnedPoolService(sub *UserSubscription, err error) *APIKeyService {
	return &APIKeyService{
		groupRepo: &boundKeyGroupRepo{groups: map[int64]*Group{
			10: {ID: 10, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription},
			11: {ID: 11, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription},
			12: {ID: 12, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription},
		}},
		userSubRepo: &boundKeySubRepo{sub: sub, err: err},
	}
}

// 手动分配的订阅仍然只提供额度池，候选保持 group_id + fallback。
func TestResolveCandidatesKeepsExplicitGroupsForManualSubscription(t *testing.T) {
	sub := activeSharedSubscription()
	sub.GroupIDs = []int64{sub.GroupID}
	svc := pinnedPoolService(sub, nil)

	candidates := svc.ResolveAPIKeyRoutingCandidates(context.Background(), pinnedPoolKey())

	// A manually assigned subscription is still an explicit quota pool. Its
	// routing remains the key's primary group plus configured fallback groups.
	require.Len(t, candidates, 2)
	require.Equal(t, int64(10), candidates[0].Group.ID)
	require.Equal(t, int64(11), candidates[1].Group.ID)
}

func TestResolveCandidatesExpandsLegacyMultiGroupSubscription(t *testing.T) {
	svc := pinnedPoolService(activeSharedSubscription(), nil)
	candidates := svc.ResolveAPIKeyRoutingCandidates(context.Background(), pinnedPoolKey())

	require.Len(t, candidates, 3)
	require.Equal(t, []int64{10, 11, 12}, []int64{
		candidates[0].Group.ID,
		candidates[1].Group.ID,
		candidates[2].Group.ID,
	})
}

func TestResolveCandidatesExpandsPlanCoverageForPinnedPlanKey(t *testing.T) {
	planID := int64(900)
	sub := activeSharedSubscription()
	sub.PlanID = &planID
	svc := pinnedPoolService(sub, nil)
	groupRepo, ok := svc.groupRepo.(*boundKeyGroupRepo)
	require.True(t, ok)
	groups := groupRepo.groups
	groups[10].Platform = PlatformOpenAI
	groups[11].Platform = PlatformDeepseek
	groups[12].Platform = PlatformAnthropic
	key := pinnedPoolKey()

	candidates := svc.ResolveAPIKeyRoutingCandidates(context.Background(), key)

	// The saved group remains preferred, but a plan key must also try every
	// covered group before returning that the requested model is unsupported.
	require.Len(t, candidates, 3)
	require.Equal(t, []int64{10, 11, 12}, []int64{
		candidates[0].Group.ID,
		candidates[1].Group.ID,
		candidates[2].Group.ID,
	})
	for _, candidate := range candidates {
		require.Nil(t, candidate.Unavailable)
	}
}

// 旧版套餐 Key 可能没有保存 group_id。认证时仍应从订阅的实际覆盖关系恢复
// 候选分组，而不是把空候选集转换成 NO_AVAILABLE_GROUP。
func TestResolveCandidatesRecoversLegacySubscriptionBoundKeyWithoutGroup(t *testing.T) {
	svc := pinnedPoolService(activeSharedSubscription(), nil)
	subscriptionID := int64(77)
	key := &APIKey{ID: 1, UserID: 5, UserSubscriptionID: &subscriptionID}

	candidates := svc.ResolveAPIKeyRoutingCandidates(context.Background(), key)

	require.Len(t, candidates, 3)
	require.Equal(t, []int64{10, 11, 12}, []int64{
		candidates[0].Group.ID,
		candidates[1].Group.ID,
		candidates[2].Group.ID,
	})
}

func TestResolveCandidatesKeepsAllPlatformsForAutoPlanKey(t *testing.T) {
	sub := activeSharedSubscription()
	sub.GroupIDs = []int64{10, 11}
	svc := &APIKeyService{
		groupRepo: &boundKeyGroupRepo{groups: map[int64]*Group{
			10: {ID: 10, Platform: PlatformOpenAI, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription},
			11: {ID: 11, Platform: PlatformAnthropic, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription},
		}},
		userSubRepo: &boundKeySubRepo{sub: sub},
	}
	subscriptionID := sub.ID
	candidates := svc.ResolveAPIKeyRoutingCandidates(context.Background(), &APIKey{
		UserID:             sub.UserID,
		UserSubscriptionID: &subscriptionID,
	})

	require.Len(t, candidates, 2)
	require.Nil(t, candidates[0].Unavailable)
	require.Nil(t, candidates[1].Unavailable)
}

func TestResolveCandidatesReportsMissingLegacySubscription(t *testing.T) {
	subscriptionID := int64(77)
	key := &APIKey{ID: 1, UserID: 5, UserSubscriptionID: &subscriptionID}
	svc := pinnedPoolService(nil, ErrSubscriptionNotFound)

	candidates := svc.ResolveAPIKeyRoutingCandidates(context.Background(), key)

	require.Len(t, candidates, 1)
	require.ErrorIs(t, candidates[0].Unavailable, ErrSubscriptionNotFound)
}

func TestResolveCandidatesRejectsForeignLegacySubscription(t *testing.T) {
	subscriptionID := int64(77)
	key := &APIKey{ID: 1, UserID: 5, UserSubscriptionID: &subscriptionID}
	foreign := activeSharedSubscription()
	foreign.UserID = 999
	svc := pinnedPoolService(foreign, nil)

	candidates := svc.ResolveAPIKeyRoutingCandidates(context.Background(), key)

	require.Len(t, candidates, 1)
	require.ErrorIs(t, candidates[0].Unavailable, ErrSubscriptionInvalid)
}

func TestResolveCandidatesReportsLegacySubscriptionWithoutGroups(t *testing.T) {
	subscriptionID := int64(77)
	key := &APIKey{ID: 1, UserID: 5, UserSubscriptionID: &subscriptionID}
	sub := activeSharedSubscription()
	sub.GroupID = 0
	sub.GroupIDs = nil
	svc := pinnedPoolService(sub, nil)

	candidates := svc.ResolveAPIKeyRoutingCandidates(context.Background(), key)

	require.Len(t, candidates, 1)
	require.ErrorIs(t, candidates[0].Unavailable, ErrSubscriptionNotFound)
}

func TestSelectPlanRouteGroupAllowsModelListingWithoutModel(t *testing.T) {
	sub := activeSharedSubscription()
	groups := map[int64]*Group{
		10: {ID: 10, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription, RateMultiplier: 1.2},
		11: {ID: 11, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription, RateMultiplier: 0.8},
		12: {ID: 12, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription, RateMultiplier: 1.0},
	}
	svc := &APIKeyService{
		groupRepo:   &boundKeyGroupRepo{groups: groups},
		userSubRepo: &boundKeySubRepo{sub: sub},
	}
	subscriptionID := sub.ID
	key := &APIKey{UserID: sub.UserID, UserSubscriptionID: &subscriptionID}

	group, err := svc.SelectPlanRouteGroup(context.Background(), key, "")

	require.NoError(t, err)
	require.NotNil(t, group)
	require.Equal(t, int64(11), group.ID)
}

// 指定的套餐覆盖该分组时，额度池就是它。
func TestPinnedSubscriptionForGroupReturnsPinnedPool(t *testing.T) {
	svc := pinnedPoolService(activeSharedSubscription(), nil)

	sub := svc.PinnedSubscriptionForGroup(context.Background(), pinnedPoolKey(), 11)

	require.NotNil(t, sub)
	require.Equal(t, int64(77), sub.ID)
}

// 回退落到套餐没覆盖的分组时不能扣这条套餐——那等于让它为没买的分组付费。
// 返回 nil 让调用方改为按分组反查，而不是整个请求失败：分组绑定本身仍然有效。
func TestPinnedSubscriptionForGroupIgnoresUncoveredGroup(t *testing.T) {
	sub := activeSharedSubscription()
	sub.GroupIDs = []int64{10}
	svc := pinnedPoolService(sub, nil)

	require.Nil(t, svc.PinnedSubscriptionForGroup(context.Background(), pinnedPoolKey(), 11))
}

// 越权防线：Key 的持有者与订阅归属不一致时绝不能扣那条订阅。
func TestPinnedSubscriptionForGroupRejectsForeignSubscription(t *testing.T) {
	foreign := activeSharedSubscription()
	foreign.UserID = 999
	svc := pinnedPoolService(foreign, nil)

	require.Nil(t, svc.PinnedSubscriptionForGroup(context.Background(), pinnedPoolKey(), 10))
}

// 指定的套餐已过期/被撤销时退回按分组反查，而不是让这把 Key 整体不可用：
// 用户可能还有另一条覆盖该分组的有效套餐。
func TestPinnedSubscriptionForGroupFallsBackWhenPinUnusable(t *testing.T) {
	require.Nil(t, pinnedPoolService(nil, ErrSubscriptionNotFound).
		PinnedSubscriptionForGroup(context.Background(), pinnedPoolKey(), 10))

	expired := activeSharedSubscription()
	expired.Status = SubscriptionStatusExpired
	require.Nil(t, pinnedPoolService(expired, nil).
		PinnedSubscriptionForGroup(context.Background(), pinnedPoolKey(), 10))
}

// 未指定套餐时不去查订阅：额度池由分组反查决定。
func TestPinnedSubscriptionForGroupNilWithoutPin(t *testing.T) {
	groupID := int64(10)
	key := &APIKey{ID: 1, UserID: 5, GroupID: &groupID}

	require.Nil(t, pinnedPoolService(activeSharedSubscription(), nil).
		PinnedSubscriptionForGroup(context.Background(), key, 10))
}

// 保存时就要拦住"指定的套餐不覆盖所选分组"：留到运行时只会静默回退到按分组反查，
// 用户以为自己选定了池子，实际并没有生效。
func TestValidatePinnedUserSubscriptionRejectsUncoveredGroup(t *testing.T) {
	sub := activeSharedSubscription()
	sub.GroupIDs = []int64{10, 11}
	svc := pinnedPoolService(sub, nil)

	groupID := int64(12)
	subID := int64(77)
	err := svc.validatePinnedUserSubscription(context.Background(), 5, &groupID, &subID)

	require.ErrorIs(t, err, ErrSubscriptionGroupMismatch)
}

func TestValidatePinnedUserSubscriptionAcceptsCoveredGroup(t *testing.T) {
	svc := pinnedPoolService(activeSharedSubscription(), nil)

	groupID := int64(11)
	subID := int64(77)
	require.NoError(t, svc.validatePinnedUserSubscription(context.Background(), 5, &groupID, &subID))
}

func TestValidatePinnedUserSubscriptionRefreshesPlanCoverage(t *testing.T) {
	sub := activeSharedSubscription()
	planID := int64(100)
	sub.PlanID = &planID
	sub.GroupIDs = []int64{10}
	refreshed := *sub
	refreshed.GroupIDs = []int64{10, 11, 12}
	svc := &APIKeyService{
		userSubRepo: &boundKeySubRepo{sub: sub, activeSubs: []UserSubscription{refreshed}},
	}

	groupID := int64(12)
	subID := int64(77)
	require.NoError(t, svc.validatePinnedUserSubscription(context.Background(), 5, &groupID, &subID))
}

func TestValidatePinnedUserSubscriptionRequiresGroup(t *testing.T) {
	svc := pinnedPoolService(activeSharedSubscription(), nil)

	subID := int64(77)
	err := svc.validatePinnedUserSubscription(context.Background(), 5, nil, &subID)

	require.ErrorIs(t, err, ErrGroupRequiredForSubscription)
}

// 不指定套餐是正常情况，不应因此报错，也不该去查订阅。
func TestValidatePinnedUserSubscriptionAllowsNoPin(t *testing.T) {
	svc := pinnedPoolService(nil, ErrSubscriptionNotFound)

	groupID := int64(10)
	require.NoError(t, svc.validatePinnedUserSubscription(context.Background(), 5, &groupID, nil))
}

func TestSelectCheapestGroupPrefersLowerRateAndKeepsOrderOnTie(t *testing.T) {
	expensive := &Group{ID: 1, Platform: "openai", Status: StatusActive, RateMultiplier: 2, SubscriptionType: SubscriptionTypeSubscription}
	cheap := &Group{ID: 2, Platform: PlatformOpenAI, Status: StatusActive, RateMultiplier: 0.5, SubscriptionType: SubscriptionTypeSubscription}
	selected := selectCheapestGroup([]*Group{expensive, cheap}, "gpt-4.1")
	require.Equal(t, int64(2), selected.ID)

	first := &Group{ID: 3, Status: StatusActive, RateMultiplier: 1}
	second := &Group{ID: 4, Status: StatusActive, RateMultiplier: 1}
	selected = selectCheapestGroup([]*Group{first, second}, "same-model")
	require.Equal(t, int64(3), selected.ID)
}

func TestSelectCheapestGroupDoesNotRouteKnownModelToAnotherPlatform(t *testing.T) {
	grok := &Group{ID: 1, Platform: PlatformGrok, Status: StatusActive, RateMultiplier: 0.1}
	openai := &Group{ID: 2, Platform: PlatformOpenAI, Status: StatusActive, RateMultiplier: 1}

	selected := selectCheapestGroup([]*Group{grok, openai}, "gpt-5.6-sol")

	require.Equal(t, int64(2), selected.ID)
}
