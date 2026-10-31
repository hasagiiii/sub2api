//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// Keep preference-specific observations out of the shared organization stub.
type companyPreferenceRepo struct {
	*organizationRepoStub
	balanceReads int
}

func (r *companyPreferenceRepo) GetOrganizationBalance(ctx context.Context, id int64) (float64, error) {
	r.balanceReads++
	return r.organizationRepoStub.GetOrganizationBalance(ctx, id)
}

func newCompanyPreferenceRepo(balance float64) *companyPreferenceRepo {
	id := int64(8)
	return &companyPreferenceRepo{organizationRepoStub: &organizationRepoStub{
		organizationBalance: balance,
		resolved:            &BillingContext{ConsumerUserID: 12, PayerUserID: 12, OrganizationID: &id, BalanceSource: BalanceSourceSelf},
	}}
}

func TestCompanyBalancePreferenceResolverAndAuth(t *testing.T) {
	lookupErr := errors.New("balance lookup unavailable")
	for _, mode := range []string{"preflight", "settlement", "auth"} {
		for _, tc := range []struct {
			name    string
			balance float64
			want    string
			err     error
		}{
			{"funded", 1, BalanceSourceCompany, nil},
			{"empty", 0, BalanceSourceSelf, nil},
			{"overdrawn", -1, BalanceSourceSelf, nil},
			{"below reserve", 0.005, BalanceSourceCompany, nil},
			{"at reserve", 0.01, BalanceSourceCompany, nil},
			{"lookup error", 1, "", lookupErr},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				repo := newCompanyPreferenceRepo(tc.balance)
				repo.organizationBalanceErr = tc.err
				key := &APIKey{UserID: 12, PreferCompanyBalance: true}
				ctx := WithBillingAPIKey(context.Background(), key)
				resolver := NewBillingContextResolver(repo)
				var got *BillingContext
				var err error
				switch mode {
				case "auth":
					svc := &APIKeyService{organizationRepo: repo}
					got = svc.ResolveBillingContextForAPIKey(ctx, key)
					require.Zero(t, repo.spendCheckCalls, "auth must not repeat spend-limit checks")
				case "settlement":
					got, err = resolver.ResolveForSettlement(ctx, 12, 2)
					require.Zero(t, repo.spendCheckCalls)
					require.Equal(t, 2.0, repo.requiredAmount, "allocation resolution still needs the actual charge")
				default:
					got, err = resolver.ResolveForAmount(ctx, 12, 0.01)
				}
				require.Equal(t, 1, repo.balanceReads)
				if tc.err != nil {
					require.Nil(t, got, "balance lookup errors cannot silently select self")
					if mode != "auth" {
						require.ErrorIs(t, err, tc.err)
					}
					return
				}
				require.NoError(t, err)
				want := tc.want
				require.Equal(t, want, got.BalanceSource)
				require.Equal(t, int64(12), got.PayerUserID)
				if mode == "preflight" && want == BalanceSourceCompany {
					require.Equal(t, 1, repo.spendCheckCalls)
					require.Equal(t, 0.01, repo.spendCheckAmount)
				}
			})
		}
	}
}

func TestCompanyBalancePreferencePreservesOtherSources(t *testing.T) {
	for _, mode := range []string{"preflight", "settlement", "auth"} {
		for _, scenario := range []string{"allocated", "preference disabled", "no organization"} {
			t.Run(mode+"/"+scenario, func(t *testing.T) {
				repo := newCompanyPreferenceRepo(100)
				repo.organizationBalanceErr = errors.New("must not query")
				key := &APIKey{UserID: 12, PreferCompanyBalance: true}
				switch scenario {
				case "allocated":
					repo.resolved.BalanceSource = BalanceSourceAllocated
				case "preference disabled":
					key.PreferCompanyBalance = false
				case "no organization":
					repo.resolved.OrganizationID = nil
				}
				before := *repo.resolved
				resolver := NewBillingContextResolver(repo)
				ctx := WithBillingAPIKey(context.Background(), key)
				var got *BillingContext
				var err error
				switch mode {
				case "auth":
					got = (&APIKeyService{organizationRepo: repo}).ResolveBillingContextForAPIKey(ctx, key)
				case "settlement":
					got, err = resolver.ResolveForSettlement(ctx, 12, 1)
				default:
					got, err = resolver.ResolveForAmount(ctx, 12, 1)
				}
				require.NoError(t, err)
				require.Equal(t, before, *got)
				require.Zero(t, repo.balanceReads)
			})
		}
	}
}

func TestCompanyBalancePreferenceRealResolverPreflight(t *testing.T) {
	for _, tc := range []struct {
		name              string
		company, personal float64
		lookupErr         error
		wantErr           bool
		wantPersonal      bool
	}{
		{"empty company uses funded personal", 0, 1, nil, false, true},
		{"negative company uses funded personal", -1, 1, nil, false, true},
		{"both empty reject", 0, 0, nil, true, true},
		{"personal below reserve rejects", 0, 0.005, nil, true, true},
		{"positive company below reserve rejects without switching wallet", 0.005, 1, nil, true, false},
		{"company at reserve does not need personal", 0.01, 0, nil, false, false},
		{"company read error cannot use personal", 1, 1, errors.New("db down"), true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newCompanyPreferenceRepo(tc.company)
			repo.organizationBalanceErr = tc.lookupErr
			cache := &balanceEligibilityCacheStub{balance: tc.personal}
			cfg := &config.Config{}
			cfg.Billing.MinimumBalanceReserve = 0.01
			svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
			svc.SetBillingContextResolver(NewBillingContextResolver(repo))
			t.Cleanup(svc.Stop)
			key := &APIKey{UserID: 12, PreferCompanyBalance: true}
			err := svc.CheckBillingEligibility(context.Background(), &User{ID: 12}, key, nil, nil, "")
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if tc.wantPersonal {
				require.Equal(t, int64(12), cache.lastUserID.Load())
			} else {
				require.Zero(t, cache.lastUserID.Load())
			}
		})
	}
}
