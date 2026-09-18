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

func subscriptionBoundKey() *APIKey {
	subID := int64(77)
	groupID := int64(10)
	return &APIKey{ID: 1, UserID: 5, GroupID: &groupID, UserSubscriptionID: &subID}
}

// 绑定订阅的 Key：候选分组来自订阅的覆盖集合，且每个候选都指向【同一条】订阅。
// 这是"消费扣哪个额度池"不再有歧义的根据。
func TestResolveCandidatesForSubscriptionBoundKeyUsesCoveredGroups(t *testing.T) {
	svc := &APIKeyService{
		groupRepo: &boundKeyGroupRepo{groups: map[int64]*Group{
			10: {ID: 10, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription},
			11: {ID: 11, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription},
			12: {ID: 12, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription},
		}},
		userSubRepo: &boundKeySubRepo{sub: activeSharedSubscription()},
	}

	candidates := svc.ResolveAPIKeyRoutingCandidates(context.Background(), subscriptionBoundKey())

	require.Len(t, candidates, 3)
	for i, want := range []int64{10, 11, 12} {
		require.NoError(t, candidates[i].Unavailable)
		require.NotNil(t, candidates[i].Group)
		require.Equal(t, want, candidates[i].Group.ID)
		require.NotNil(t, candidates[i].Subscription, "candidate %d must carry the bound pool", i)
		require.Equal(t, int64(77), candidates[i].Subscription.ID,
			"every covered group must share the same quota pool")
	}
}

// 订阅被撤销后这把 Key 失去额度池依据，必须整体不可用而不是静默回退到某个分组
// ——否则用户会在订阅已失效的情况下继续消费。
func TestResolveCandidatesForSubscriptionBoundKeyRejectsMissingSubscription(t *testing.T) {
	svc := &APIKeyService{
		groupRepo:   &boundKeyGroupRepo{groups: map[int64]*Group{10: {ID: 10, Status: StatusActive}}},
		userSubRepo: &boundKeySubRepo{err: ErrSubscriptionNotFound},
	}

	candidates := svc.ResolveAPIKeyRoutingCandidates(context.Background(), subscriptionBoundKey())

	require.Len(t, candidates, 1)
	require.ErrorIs(t, candidates[0].Unavailable, ErrSubscriptionNotFound)
	require.Nil(t, candidates[0].Group)
}

// 越权防线：Key 的持有者与订阅归属不一致时绝不能放行，否则一把 Key 就能消费他
// 人的额度池。
func TestResolveCandidatesForSubscriptionBoundKeyRejectsForeignSubscription(t *testing.T) {
	foreign := activeSharedSubscription()
	foreign.UserID = 999
	svc := &APIKeyService{
		groupRepo:   &boundKeyGroupRepo{groups: map[int64]*Group{10: {ID: 10, Status: StatusActive}}},
		userSubRepo: &boundKeySubRepo{sub: foreign},
	}

	candidates := svc.ResolveAPIKeyRoutingCandidates(context.Background(), subscriptionBoundKey())

	require.Len(t, candidates, 1)
	require.ErrorIs(t, candidates[0].Unavailable, ErrSubscriptionInvalid)
}

// 覆盖的分组里有失效的，只标记该候选不可用，其余仍可路由。
func TestResolveCandidatesForSubscriptionBoundKeySkipsInactiveGroup(t *testing.T) {
	svc := &APIKeyService{
		groupRepo: &boundKeyGroupRepo{groups: map[int64]*Group{
			10: {ID: 10, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription},
			11: {ID: 11, Status: "disabled", SubscriptionType: SubscriptionTypeSubscription},
			12: {ID: 12, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription},
		}},
		userSubRepo: &boundKeySubRepo{sub: activeSharedSubscription()},
	}

	candidates := svc.ResolveAPIKeyRoutingCandidates(context.Background(), subscriptionBoundKey())

	require.Len(t, candidates, 3)
	require.NoError(t, candidates[0].Unavailable)
	require.ErrorIs(t, candidates[1].Unavailable, ErrGroupNotFound)
	require.NoError(t, candidates[2].Unavailable)
}
