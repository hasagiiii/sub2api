package service

import "testing"

func TestUserSubscriptionUsesPlanModeForEveryWindow(t *testing.T) {
	planDaily := 10.0
	groupDaily := 3.0
	groupWeekly := 4.0
	sub := &UserSubscription{
		PlanLimits: SubscriptionLimits{DailyLimitUSD: &planDaily},
		GroupUsages: map[int64]SubscriptionGroupUsage{
			2: {GroupID: 2, DailyUsageUSD: 9, WeeklyUsageUSD: 3.5},
		},
	}
	group := &Group{ID: 2, DailyLimitUSD: &groupDaily, WeeklyLimitUSD: &groupWeekly}

	if !sub.CheckDailyLimit(group, 1) {
		t.Fatal("package-level daily usage should be checked against the package limit")
	}
	if !sub.CheckWeeklyLimit(group, 100) {
		t.Fatal("an unconfigured weekly plan window should be unlimited")
	}
	if !sub.CheckMonthlyLimit(group, 0) {
		t.Fatal("an unlimited monthly window should remain unlimited")
	}
}

func TestUserSubscriptionGroupUsageFallbackPreservesLegacySnapshots(t *testing.T) {
	limit := 10.0
	sub := &UserSubscription{DailyUsageUSD: 9, PlanLimits: SubscriptionLimits{}}
	group := &Group{ID: 2, DailyLimitUSD: &limit}

	if !sub.CheckDailyLimit(group, 1) {
		t.Fatal("legacy snapshots without group rows should use the package counter")
	}
}

func TestUserSubscriptionWithoutPlanLimitsUsesIndependentGroupUsage(t *testing.T) {
	dailyLimit := 3.0
	weeklyLimit := 4.0
	monthlyLimit := 5.0
	sub := &UserSubscription{
		PlanLimits: SubscriptionLimits{},
		GroupUsages: map[int64]SubscriptionGroupUsage{
			2: {GroupID: 2, DailyUsageUSD: 2.5, WeeklyUsageUSD: 3.5, MonthlyUsageUSD: 4.5},
		},
	}
	group := &Group{
		ID:              2,
		DailyLimitUSD:   &dailyLimit,
		WeeklyLimitUSD:  &weeklyLimit,
		MonthlyLimitUSD: &monthlyLimit,
	}

	if !sub.UsesIndependentGroupUsage() {
		t.Fatal("a subscription without package limits should use group counters")
	}
	if sub.CheckDailyLimit(group, 0.6) {
		t.Fatal("daily usage should be checked against the selected group's counter")
	}
	if sub.CheckWeeklyLimit(group, 0.6) {
		t.Fatal("weekly usage should be checked against the selected group's counter")
	}
	if sub.CheckMonthlyLimit(group, 0.6) {
		t.Fatal("monthly usage should be checked against the selected group's counter")
	}
}
