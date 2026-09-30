//go:build unit

package service

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type inflightPayerCache struct {
	*memInflightCache
	balanceReads   atomic.Int64
	deductedUserID atomic.Int64
}

func (c *inflightPayerCache) GetUserBalance(ctx context.Context, userID int64) (float64, error) {
	c.balanceReads.Add(1)
	return c.memInflightCache.GetUserBalance(ctx, userID)
}

func (c *inflightPayerCache) DeductUserBalance(ctx context.Context, userID int64, amount float64) error {
	c.deductedUserID.Store(userID)
	return c.memInflightCache.DeductUserBalance(ctx, userID, amount)
}

func TestReserveInflight_PreservesEnterpriseBilling(t *testing.T) {
	orgID := int64(8)
	for _, tc := range []struct {
		name            string
		billing         *BillingContext
		wantReservation bool
	}{
		{"personal wallet", &BillingContext{ConsumerUserID: 20, PayerUserID: 20, BalanceSource: BalanceSourceSelf}, true},
		{"company wallet", &BillingContext{ConsumerUserID: 20, PayerUserID: 10, OrganizationID: &orgID, BalanceSource: BalanceSourceCompany}, false},
		{"IAM allocation", &BillingContext{ConsumerUserID: 20, PayerUserID: 20, OrganizationID: &orgID, BalanceSource: BalanceSourceAllocated}, false},
		{"owner with company fallback", &BillingContext{ConsumerUserID: 20, PayerUserID: 20, OrganizationID: &orgID, BalanceSource: BalanceSourceSelf}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := &inflightPayerCache{memInflightCache: newMemInflightCache(10)}
			cfg := &config.Config{}
			cfg.Billing.InflightReservation.Enabled = true
			svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
			svc.SetBillingContextResolver(&billingEligibilityResolverStub{result: tc.billing})
			t.Cleanup(svc.Stop)
			reservation, err := svc.ReserveInflight(context.Background(), &User{ID: 20}, nil, nil, 1)
			require.NoError(t, err)
			if tc.wantReservation {
				require.NotNil(t, reservation)
				require.Equal(t, int64(1), cache.balanceReads.Load())
				require.Equal(t, 1, cache.count())
				reservation.HandlerDone()
			} else {
				require.Nil(t, reservation)
				require.Zero(t, cache.balanceReads.Load(), "enterprise billing must not read a personal wallet")
				require.Zero(t, cache.count())
			}
		})
	}
}

func TestReserveInflight_EnterpriseSubscriptionSkipsPersonalReservation(t *testing.T) {
	cache := &inflightPayerCache{memInflightCache: newMemInflightCache(0)}
	cfg := &config.Config{}
	cfg.Billing.InflightReservation.Enabled = true
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(svc.Stop)
	subID := int64(9)
	ctx := WithBillingAPIKey(context.Background(), &APIKey{OrganizationSubscriptionID: &subID})
	reservation, err := svc.ReserveInflight(ctx, &User{ID: 20}, nil, nil, 1)
	require.NoError(t, err)
	require.Nil(t, reservation)
	require.Zero(t, cache.balanceReads.Load())
}

func TestSyncBalanceCacheAfterDeduction_InflightUsesActualPayer(t *testing.T) {
	cache := &inflightPayerCache{memInflightCache: newMemInflightCache(10)}
	cfg := &config.Config{}
	cfg.Billing.InflightReservation.Enabled = true
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(svc.Stop)
	syncBalanceCacheAfterDeduction(context.Background(), &postUsageBillingParams{
		User: &User{ID: 20}, Cost: &CostBreakdown{ActualCost: 2},
	}, &billingDeps{billingCacheService: svc}, &UsageBillingApplyResult{
		PayerUserID: 10, BalanceSource: BalanceSourceAllocated,
	})
	require.Equal(t, int64(10), cache.deductedUserID.Load())
	require.Equal(t, int32(1), cache.deducts.Load())
	require.InDelta(t, 8, cache.balance, 1e-9)
}
