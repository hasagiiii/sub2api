package service

import "testing"

func groupWithLimits(daily, weekly, monthly *float64) *Group {
	return &Group{
		DailyLimitUSD:   daily,
		WeeklyLimitUSD:  weekly,
		MonthlyLimitUSD: monthly,
	}
}

func TestResolveSubscriptionLimitsPrefersPlanPerWindow(t *testing.T) {
	group := groupWithLimits(ptrFloat(5), ptrFloat(50), ptrFloat(200))
	plan := SubscriptionLimits{DailyLimitUSD: ptrFloat(9)}

	resolved := ResolveSubscriptionLimits(plan, group)

	// 套餐设了日限额 -> 用套餐的
	if resolved.DailyLimitUSD == nil || *resolved.DailyLimitUSD != 9 {
		t.Fatalf("daily should come from the plan, got %v", resolved.DailyLimitUSD)
	}
	// 套餐没设周/月 -> 逐窗口回退到分组，而不是因为"套餐配过限额"就把分组原有
	// 的另外两项保护一并丢掉
	if resolved.WeeklyLimitUSD == nil || *resolved.WeeklyLimitUSD != 50 {
		t.Fatalf("weekly should fall back to the group, got %v", resolved.WeeklyLimitUSD)
	}
	if resolved.MonthlyLimitUSD == nil || *resolved.MonthlyLimitUSD != 200 {
		t.Fatalf("monthly should fall back to the group, got %v", resolved.MonthlyLimitUSD)
	}
}

func TestResolveSubscriptionLimitsFallsBackEntirelyWhenPlanHasNone(t *testing.T) {
	// 后台手动分配的订阅、以及未配限额的套餐，行为必须与改动前完全一致。
	group := groupWithLimits(ptrFloat(5), nil, ptrFloat(200))

	resolved := ResolveSubscriptionLimits(SubscriptionLimits{}, group)

	if resolved.DailyLimitUSD == nil || *resolved.DailyLimitUSD != 5 {
		t.Fatalf("daily should fall back to the group, got %v", resolved.DailyLimitUSD)
	}
	if resolved.WeeklyLimitUSD != nil {
		t.Fatalf("weekly should stay unlimited, got %v", *resolved.WeeklyLimitUSD)
	}
	if resolved.MonthlyLimitUSD == nil || *resolved.MonthlyLimitUSD != 200 {
		t.Fatalf("monthly should fall back to the group, got %v", resolved.MonthlyLimitUSD)
	}
}

func TestResolveSubscriptionLimitsTreatsNonPositivePlanLimitAsUnset(t *testing.T) {
	// 与 Group.HasDailyLimit 一致：<=0 表示未限额，不能被当成"限额为 0 即禁用"。
	group := groupWithLimits(ptrFloat(5), nil, nil)

	resolved := ResolveSubscriptionLimits(SubscriptionLimits{DailyLimitUSD: ptrFloat(0)}, group)

	if resolved.DailyLimitUSD == nil || *resolved.DailyLimitUSD != 5 {
		t.Fatalf("a zero plan limit must fall back to the group, got %v", resolved.DailyLimitUSD)
	}
}

// 这是本次改动要解决的核心问题：一份套餐额度由多个分组共享，而不是每个分组各
// 给一份。用量累加在订阅上，因此在任一分组消费都会推高同一个计数器。
func TestSharedPlanQuotaIsNotMultipliedAcrossCoveredGroups(t *testing.T) {
	planDaily := ptrFloat(10)
	// 两个分组各自的日限额都比套餐高，若判定误用分组限额就会放行
	groupA := groupWithLimits(ptrFloat(100), nil, nil)
	groupB := groupWithLimits(ptrFloat(100), nil, nil)

	sub := &UserSubscription{
		ID:         7,
		GroupID:    1,
		GroupIDs:   []int64{1, 2},
		PlanLimits: SubscriptionLimits{DailyLimitUSD: planDaily},
		// 已在分组 A 花掉 9.5，池子里只剩 0.5
		DailyUsageUSD: 9.5,
	}

	if !sub.CheckDailyLimit(groupA, 0.4) {
		t.Fatal("spending within the remaining shared quota should be allowed")
	}
	// 关键断言：切到分组 B 并不会获得一份新的额度
	if sub.CheckDailyLimit(groupB, 0.9) {
		t.Fatal("switching to another covered group must not grant a fresh quota pool")
	}
}

func TestSubscriptionCoversGroup(t *testing.T) {
	multi := &UserSubscription{GroupID: 1, GroupIDs: []int64{1, 2, 3}}
	for _, id := range []int64{1, 2, 3} {
		if !multi.CoversGroup(id) {
			t.Fatalf("group %d should be covered", id)
		}
	}
	if multi.CoversGroup(4) {
		t.Fatal("group 4 must not be covered")
	}

	// GroupIDs 未加载时回退比较主分组，保证存量调用路径不受影响
	legacy := &UserSubscription{GroupID: 42}
	if !legacy.CoversGroup(42) {
		t.Fatal("legacy subscription should cover its primary group")
	}
	if legacy.CoversGroup(43) {
		t.Fatal("legacy subscription should not cover other groups")
	}

	var nilSub *UserSubscription
	if nilSub.CoversGroup(1) {
		t.Fatal("nil subscription covers nothing")
	}
}

func TestCoveredGroupIDsAlwaysYieldsThePrimaryGroup(t *testing.T) {
	// 缓存失效要遍历覆盖的分组，这里返回空会导致 L1 索引残留旧订阅。
	legacy := &UserSubscription{GroupID: 9}
	got := legacy.CoveredGroupIDs()
	if len(got) != 1 || got[0] != 9 {
		t.Fatalf("expected the primary group as the sole entry, got %v", got)
	}

	multi := &UserSubscription{GroupID: 9, GroupIDs: []int64{9, 10}}
	if len(multi.CoveredGroupIDs()) != 2 {
		t.Fatalf("expected both covered groups, got %v", multi.CoveredGroupIDs())
	}
}
