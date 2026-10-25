//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// dailyRollingResetRepo 记录 ResetDailyUsage 收到的新窗口起点。
type dailyRollingResetRepo struct {
	userSubRepoNoop

	resetCalled    bool
	newWindowStart time.Time
}

func (r *dailyRollingResetRepo) ResetDailyUsage(_ context.Context, _ int64, _ *time.Time, newWindowStart time.Time) error {
	r.resetCalled = true
	r.newWindowStart = newWindowStart
	return nil
}

// rollingPurchaseAt 是测试用的购买时刻：D1 09:38。
func rollingPurchaseAt() time.Time {
	return time.Date(2026, 8, 6, 9, 38, 0, 0, time.UTC)
}

func newRollingTestSub(startsAt, expiresAt, dailyWindowStart time.Time) *UserSubscription {
	start := dailyWindowStart
	return &UserSubscription{
		ID:               1,
		UserID:           10,
		GroupID:          20,
		StartsAt:         startsAt,
		ExpiresAt:        expiresAt,
		DailyUsageUSD:    43.34,
		DailyWindowStart: &start,
	}
}

func runRollingMaintenance(t *testing.T, sub *UserSubscription, now time.Time) *dailyRollingResetRepo {
	t.Helper()
	repo := &dailyRollingResetRepo{}
	svc := NewSubscriptionService(groupRepoNoop{}, repo, nil, nil, nil)
	svc.now = func() time.Time { return now }
	require.NoError(t, svc.CheckAndResetWindows(context.Background(), sub))
	return repo
}

// 反馈场景：D1 09:38 购买日卡，D2 09:10 续费 1 天。续费后不得在 09:10 重置，
// 必须等到 D2 09:38（购买时刻 +24h）。分别覆盖旧版 0 点锚点与新版购买时刻锚点。
func TestDailyRolling_RenewedDailyCardWaitsForPurchaseTime(t *testing.T) {
	purchase := rollingPurchaseAt()
	renewedExpiry := purchase.AddDate(0, 0, 2)
	anchors := map[string]time.Time{
		"legacy midnight anchor": time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC),
		"purchase time anchor":   purchase,
	}

	for name, anchor := range anchors {
		t.Run(name, func(t *testing.T) {
			before := runRollingMaintenance(t, newRollingTestSub(purchase, renewedExpiry, anchor), purchase.Add(23*time.Hour+32*time.Minute))
			require.False(t, before.resetCalled, "D2 09:10 不应重置，第一张日卡的额度要用满 24 小时")

			sub := newRollingTestSub(purchase, renewedExpiry, anchor)
			after := runRollingMaintenance(t, sub, purchase.Add(24*time.Hour))
			require.True(t, after.resetCalled, "D2 09:38 应重置")
			require.Equal(t, purchase.Add(24*time.Hour), after.newWindowStart)
			require.Zero(t, sub.DailyUsageUSD)
		})
	}
}

// 一次续多天：每天都在购买时刻刷新，最后一个窗口的重置时间展示为到期时间。
func TestDailyRolling_StackedRenewalRefreshesAtPurchaseTimeEveryDay(t *testing.T) {
	purchase := rollingPurchaseAt()
	expiresAt := purchase.AddDate(0, 0, 4)

	for day := 1; day <= 3; day++ {
		boundary := purchase.Add(time.Duration(day) * 24 * time.Hour)
		prev := boundary.Add(-24 * time.Hour)

		repo := runRollingMaintenance(t, newRollingTestSub(purchase, expiresAt, prev), boundary.Add(-time.Minute))
		require.False(t, repo.resetCalled, "第 %d 天购买时刻前不应重置", day)

		repo = runRollingMaintenance(t, newRollingTestSub(purchase, expiresAt, prev), boundary)
		require.True(t, repo.resetCalled, "第 %d 天购买时刻应重置", day)
		require.Equal(t, boundary, repo.newWindowStart)
	}

	last := newRollingTestSub(purchase, expiresAt, purchase.Add(3*24*time.Hour))
	require.Equal(t, expiresAt, *last.DailyResetTime())
	require.False(t, last.NeedsDailyResetAt(expiresAt.Add(time.Hour)), "到期后不再推进日窗口")
}

// 手动重置写入的非边界锚点不改变刷新节奏：仍在下一个购买时刻刷新，而非重置时刻 +24h。
func TestDailyRolling_ManualResetAnchorDoesNotShiftCadence(t *testing.T) {
	purchase := rollingPurchaseAt()
	expiresAt := purchase.AddDate(0, 0, 30)
	manualResetAt := purchase.Add(2*24*time.Hour + 7*time.Hour + 11*time.Minute) // D3 16:49

	sub := newRollingTestSub(purchase, expiresAt, manualResetAt)
	nextBoundary := purchase.Add(3 * 24 * time.Hour) // D4 09:38

	require.False(t, sub.NeedsDailyResetAt(nextBoundary.Add(-time.Second)))
	require.True(t, sub.NeedsDailyResetAt(nextBoundary))
	require.Equal(t, nextBoundary, *sub.DailyResetTime())
}

// 存量多日订阅的 0 点锚点自愈：在购买时刻对应的边界推进到滚动窗口起点。
func TestDailyRolling_LegacyMidnightAnchorHealsToPurchaseCadence(t *testing.T) {
	purchase := rollingPurchaseAt()
	expiresAt := purchase.AddDate(0, 0, 30)
	legacyAnchor := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)

	repo := runRollingMaintenance(t, newRollingTestSub(purchase, expiresAt, legacyAnchor), time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC))
	require.False(t, repo.resetCalled, "当天购买时刻前不应重置")

	repo = runRollingMaintenance(t, newRollingTestSub(purchase, expiresAt, legacyAnchor), time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC))
	require.True(t, repo.resetCalled)
	require.Equal(t, time.Date(2026, 8, 10, 9, 38, 0, 0, time.UTC), repo.newWindowStart)
}

// 激活时日窗口锚定当前滚动窗口起点。
func TestDailyRolling_ActivationAnchorsAtCurrentWindowStart(t *testing.T) {
	repo := &activateWindowUserSubRepo{}
	svc := NewSubscriptionService(groupRepoNoop{}, repo, nil, nil, nil)
	purchase := rollingPurchaseAt()
	activatedAt := time.Date(2026, 8, 8, 7, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return activatedAt }
	sub := &UserSubscription{ID: 1, StartsAt: purchase, ExpiresAt: purchase.AddDate(0, 0, 30)}

	require.NoError(t, svc.CheckAndActivateWindow(context.Background(), sub))

	require.Equal(t, time.Date(2026, 8, 7, 9, 38, 0, 0, time.UTC), repo.dailyStart)
	require.Equal(t, activatedAt, repo.periodicStart)
}

// 列表展示：未过期时锚点换算为当前窗口起点；越过购买时刻边界后清零。
func TestDailyRolling_NormalizeExpiredWindows(t *testing.T) {
	purchase := rollingPurchaseAt()
	expiresAt := purchase.AddDate(0, 0, 2)
	legacyAnchor := time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC)

	subs := []UserSubscription{*newRollingTestSub(purchase, expiresAt, legacyAnchor)}
	normalizeExpiredWindowsAt(subs, purchase.Add(23*time.Hour))
	require.Equal(t, 43.34, subs[0].DailyUsageUSD)
	require.Equal(t, purchase, *subs[0].DailyWindowStart, "展示锚点应为购买时刻，前端起点+24h 即实际刷新时刻")

	subs = []UserSubscription{*newRollingTestSub(purchase, expiresAt, legacyAnchor)}
	normalizeExpiredWindowsAt(subs, purchase.Add(24*time.Hour))
	require.Zero(t, subs[0].DailyUsageUSD)
	require.Nil(t, subs[0].DailyWindowStart)
}

// 分组级日窗口与订阅级使用同一滚动规则。
func TestDailyRolling_GroupWindowFollowsSameCadence(t *testing.T) {
	purchase := rollingPurchaseAt()
	sub := &UserSubscription{StartsAt: purchase, ExpiresAt: purchase.AddDate(0, 0, 2)}
	groupAnchor := time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC)

	require.False(t, automaticGroupDailyWindowExpired(sub, &groupAnchor, purchase.Add(23*time.Hour+32*time.Minute)))
	windowStart, ok := automaticGroupDailyWindowStart(sub, &groupAnchor, purchase.Add(24*time.Hour))
	require.True(t, ok)
	require.Equal(t, purchase.Add(24*time.Hour), windowStart)
}

// 未续费的日卡仍是一次性额度：到期前不刷新，重置时间展示为到期时间。
func TestDailyRolling_OneTimeDailyCardUnchanged(t *testing.T) {
	purchase := rollingPurchaseAt()
	sub := newRollingTestSub(purchase, purchase.AddDate(0, 0, 1), purchase)

	repo := runRollingMaintenance(t, sub, purchase.Add(23*time.Hour))
	require.False(t, repo.resetCalled)
	require.Equal(t, purchase.AddDate(0, 0, 1), *sub.DailyResetTime())
}
