//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestQuoteRechargeBonusRespectsValidityWindow(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	before := now.Add(-time.Hour)
	after := now.Add(time.Hour)
	tiers := []RechargeBonusTier{{MinAmount: 100, BonusPercent: 10}}

	cases := []struct {
		name      string
		from      *time.Time
		until     *time.Time
		wantBonus float64
	}{
		{name: "unbounded", wantBonus: 10},
		{name: "inside window", from: &before, until: &after, wantBonus: 10},
		{name: "not started", from: &after, wantBonus: 0},
		{name: "ended", until: &before, wantBonus: 0},
		{name: "until is exclusive", until: &now, wantBonus: 0},
		{name: "from is inclusive", from: &now, wantBonus: 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &PaymentConfig{
				BalanceRechargeMultiplier: 1,
				RechargeBonusTiers:        tiers,
				RechargeBonusValidFrom:    tc.from,
				RechargeBonusValidUntil:   tc.until,
			}
			quote := quoteRechargeBonusAt(cfg, 100, "CNY", now)
			require.InDelta(t, tc.wantBonus, quote.Bonus, 1e-9)
			require.Equal(t, tc.wantBonus > 0, cfg.RechargeBonusActiveAt(now))
			if tc.wantBonus == 0 {
				require.InDelta(t, 100, quote.Credited, 1e-9)
				require.InDelta(t, 100, quote.PayBase, 1e-9)
			}
		})
	}
}

func TestRechargeBonusActiveAtRequiresPositiveTier(t *testing.T) {
	now := time.Now()
	require.False(t, (&PaymentConfig{}).RechargeBonusActiveAt(now))
	require.False(t, (&PaymentConfig{RechargeBonusTiers: []RechargeBonusTier{{MinAmount: 10, BonusPercent: 0}}}).RechargeBonusActiveAt(now))
	require.True(t, (&PaymentConfig{RechargeBonusTiers: []RechargeBonusTier{{MinAmount: 10, BonusPercent: 5}}}).RechargeBonusActiveAt(now))
}

func TestRechargeBonusVersionTracksConfig(t *testing.T) {
	until := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	base := &PaymentConfig{RechargeBonusTiers: []RechargeBonusTier{{MinAmount: 100, BonusPercent: 10}}}
	v1 := base.RechargeBonusVersion()
	require.NotEmpty(t, v1)
	require.Equal(t, v1, (&PaymentConfig{RechargeBonusTiers: []RechargeBonusTier{{MinAmount: 100, BonusPercent: 10}}, RechargeBonusMode: RechargeBonusModeBonus}).RechargeBonusVersion())

	withUntil := *base
	withUntil.RechargeBonusValidUntil = &until
	require.NotEqual(t, v1, withUntil.RechargeBonusVersion())

	discount := *base
	discount.RechargeBonusMode = RechargeBonusModeDiscount
	require.NotEqual(t, v1, discount.RechargeBonusVersion())
}

func TestUpdatePaymentConfigRechargeBonusWindow(t *testing.T) {
	ctx := context.Background()

	t.Run("persists normalized RFC3339 UTC and parses back", func(t *testing.T) {
		repo := &paymentConfigSettingRepoStub{values: map[string]string{}}
		svc := &PaymentConfigService{settingRepo: repo}
		from := "2026-10-01T08:00:00+08:00"
		until := "2026-10-31T00:00:00Z"
		require.NoError(t, svc.UpdatePaymentConfig(ctx, UpdatePaymentConfigRequest{
			RechargeBonusValidFrom:  &from,
			RechargeBonusValidUntil: &until,
		}))
		require.Equal(t, "2026-10-01T00:00:00Z", repo.updates[SettingRechargeBonusValidFrom])
		require.Equal(t, "2026-10-31T00:00:00Z", repo.updates[SettingRechargeBonusValidUntil])

		cfg, err := svc.GetPaymentConfig(ctx)
		require.NoError(t, err)
		require.NotNil(t, cfg.RechargeBonusValidFrom)
		require.True(t, cfg.RechargeBonusValidFrom.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)))
		require.NotNil(t, cfg.RechargeBonusValidUntil)
	})

	t.Run("empty string clears and omitted side is untouched", func(t *testing.T) {
		repo := &paymentConfigSettingRepoStub{values: map[string]string{
			SettingRechargeBonusValidFrom:  "2026-10-01T00:00:00Z",
			SettingRechargeBonusValidUntil: "2026-10-31T00:00:00Z",
		}}
		svc := &PaymentConfigService{settingRepo: repo}
		empty := ""
		require.NoError(t, svc.UpdatePaymentConfig(ctx, UpdatePaymentConfigRequest{RechargeBonusValidUntil: &empty}))
		value, ok := repo.updates[SettingRechargeBonusValidUntil]
		require.True(t, ok)
		require.Equal(t, "", value)
		_, touched := repo.updates[SettingRechargeBonusValidFrom]
		require.False(t, touched)
	})

	t.Run("rejects invalid timestamp", func(t *testing.T) {
		repo := &paymentConfigSettingRepoStub{values: map[string]string{}}
		svc := &PaymentConfigService{settingRepo: repo}
		bad := "2026-10-01 00:00"
		require.Error(t, svc.UpdatePaymentConfig(ctx, UpdatePaymentConfigRequest{RechargeBonusValidFrom: &bad}))
		require.Nil(t, repo.updates)
	})

	t.Run("rejects from not before until, including against stored value", func(t *testing.T) {
		repo := &paymentConfigSettingRepoStub{values: map[string]string{
			SettingRechargeBonusValidUntil: "2026-10-31T00:00:00Z",
		}}
		svc := &PaymentConfigService{settingRepo: repo}
		from := "2026-11-01T00:00:00Z"
		require.Error(t, svc.UpdatePaymentConfig(ctx, UpdatePaymentConfigRequest{RechargeBonusValidFrom: &from}))
		require.Nil(t, repo.updates)
	})
}

func TestParsePaymentConfigIgnoresInvalidWindow(t *testing.T) {
	svc := &PaymentConfigService{}
	cfg := svc.parsePaymentConfig(map[string]string{
		SettingRechargeBonusValidFrom:  "garbage",
		SettingRechargeBonusValidUntil: "",
	})
	require.Nil(t, cfg.RechargeBonusValidFrom)
	require.Nil(t, cfg.RechargeBonusValidUntil)
}
