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
	sub *UserSubscription
	err error
}

func (r *boundKeySubRepo) GetByID(context.Context, int64) (*UserSubscription, error) {
	if r.err != nil {
		return nil, r.err
	}
	cp := *r.sub
	return &cp, nil
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

// 指定了扣费套餐也不改变路由：候选仍是 group_id + fallback。
//
// 套餐里的两个分组可能提供相同模型，只有调用方知道该用哪一个；把候选换成套餐的
// 覆盖集合会让这个选择权丢失。
func TestResolveCandidatesIgnoresPinnedSubscription(t *testing.T) {
	svc := pinnedPoolService(activeSharedSubscription(), nil)

	candidates := svc.ResolveAPIKeyRoutingCandidates(context.Background(), pinnedPoolKey())

	// 套餐覆盖 10/11/12，但候选只有 Key 自己配的 10（主）与 11（回退）。
	require.Len(t, candidates, 2)
	require.Equal(t, int64(10), candidates[0].Group.ID)
	require.Equal(t, int64(11), candidates[1].Group.ID)
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
