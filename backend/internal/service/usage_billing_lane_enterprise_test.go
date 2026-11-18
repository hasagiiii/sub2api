//go:build unit

package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

type enterpriseLaneContextRepo struct {
	*laneTestRepo
	keys []*APIKey
}

func (r *enterpriseLaneContextRepo) Apply(ctx context.Context, cmd *UsageBillingCommand) (*UsageBillingApplyResult, error) {
	key, _ := ctx.Value(billingAPIKeyContextKey{}).(*APIKey)
	r.keys = append(r.keys, key)
	return r.laneTestRepo.Apply(ctx, cmd)
}
func TestUsageBillingLanePreservesEnterpriseRequestContexts(t *testing.T) {
	repo := &enterpriseLaneContextRepo{laneTestRepo: newLaneTestRepo()}
	lane := NewUsageBillingLaneCombiner(repo)
	defer lane.Stop()
	org := int64(5)
	keys := []*APIKey{{ID: 11}, {ID: 12}}
	for i, key := range keys {
		ctx := WithBillingAPIKey(context.Background(), key)
		result, err := lane.Apply(ctx, &UsageBillingCommand{RequestID: []string{"org-key-a", "org-key-b"}[i], UserID: 1, APIKeyID: key.ID, OrganizationID: &org, BalanceSource: BalanceSourceCompany, BalanceCost: 1})
		require.NoError(t, err)
		require.True(t, result.Applied)
	}
	require.Equal(t, keys, repo.keys)
	require.Empty(t, repo.batchCalls)
	require.Zero(t, lane.Stats().SingleApplies, "key-scoped commands must bypass the shared-context lane")
}
func TestUsageBillingLaneBypassesEnterpriseCommandsWithoutKeyContext(t *testing.T) {
	id := int64(5)
	cases := []UsageBillingCommand{{OrganizationID: &id}, {OrganizationSubscriptionID: &id}, {SubscriptionGroupID: &id}, {AuthzGeneration: 2}, {PayerUserID: 2}, {BalanceSource: BalanceSourceAllocated}}
	for _, cmd := range cases {
		repo := newLaneTestRepo()
		lane := NewUsageBillingLaneCombiner(repo)
		cmd.UserID = 1
		cmd.RequestID = "enterprise"
		_, err := lane.Apply(context.Background(), &cmd)
		require.NoError(t, err)
		require.Zero(t, lane.Stats().SingleApplies)
		require.Len(t, repo.applyCalls, 1)
		lane.Stop()
	}
}
