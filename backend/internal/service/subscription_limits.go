package service

// SubscriptionLimits 是一条订阅额度池的三个窗口限额。
//
// 语义与 Group 的 *_limit_usd 一致：nil 或 <= 0 都表示"该窗口不限额"。注意这
// 与 user_platform_quotas 的 0 = 完全禁用相反，不要混用。
type SubscriptionLimits struct {
	DailyLimitUSD   *float64
	WeeklyLimitUSD  *float64
	MonthlyLimitUSD *float64
}

func positiveLimit(v *float64) bool { return v != nil && *v > 0 }

func (l SubscriptionLimits) HasDailyLimit() bool   { return positiveLimit(l.DailyLimitUSD) }
func (l SubscriptionLimits) HasWeeklyLimit() bool  { return positiveLimit(l.WeeklyLimitUSD) }
func (l SubscriptionLimits) HasMonthlyLimit() bool { return positiveLimit(l.MonthlyLimitUSD) }

// IsZero 报告该套餐是否一个窗口都没配限额。用于判断是否整体回退到分组限额。
func (l SubscriptionLimits) IsZero() bool {
	return !l.HasDailyLimit() && !l.HasWeeklyLimit() && !l.HasMonthlyLimit()
}

// GroupLimits 取出分组自身配置的限额。
func GroupLimits(group *Group) SubscriptionLimits {
	if group == nil {
		return SubscriptionLimits{}
	}
	return SubscriptionLimits{
		DailyLimitUSD:   group.DailyLimitUSD,
		WeeklyLimitUSD:  group.WeeklyLimitUSD,
		MonthlyLimitUSD: group.MonthlyLimitUSD,
	}
}

// ResolveSubscriptionLimits 决定一条订阅实际受哪组限额约束。
//
// 套餐限额优先，未设置的窗口逐个回退到分组限额。这样：
//   - 套餐设了日限额、没设周限额 → 日按套餐、周仍按分组，不会因为套餐只配了
//     一项就把分组上原有的另外两项保护一并丢掉；
//   - 套餐完全没配（以及后台手动分配、不属于任何套餐的订阅）→ 行为与改动前
//     完全一致。
//
// 关于回退窗口与多分组的关系：套餐限额是整个额度池共享的一份，与当前路由到哪
// 个分组无关；而回退到分组的那些窗口，取的是本次请求实际使用的分组的限额。这
// 是刻意的——那些窗口本就没有套餐级约束，沿用分组自身的保护比放任不管更安全。
func ResolveSubscriptionLimits(planLimits SubscriptionLimits, group *Group) SubscriptionLimits {
	groupLimits := GroupLimits(group)
	resolved := SubscriptionLimits{}

	if planLimits.HasDailyLimit() {
		resolved.DailyLimitUSD = planLimits.DailyLimitUSD
	} else {
		resolved.DailyLimitUSD = groupLimits.DailyLimitUSD
	}
	if planLimits.HasWeeklyLimit() {
		resolved.WeeklyLimitUSD = planLimits.WeeklyLimitUSD
	} else {
		resolved.WeeklyLimitUSD = groupLimits.WeeklyLimitUSD
	}
	if planLimits.HasMonthlyLimit() {
		resolved.MonthlyLimitUSD = planLimits.MonthlyLimitUSD
	} else {
		resolved.MonthlyLimitUSD = groupLimits.MonthlyLimitUSD
	}
	return resolved
}

// EffectiveLimits 返回这条订阅当前生效的限额。
//
// PlanLimits 由加载订阅时从来源套餐实时读入（不是购买时快照），因此管理员调整
// 套餐限额会立即对已售出的订阅生效。
func (s *UserSubscription) EffectiveLimits(group *Group) SubscriptionLimits {
	if s == nil {
		return GroupLimits(group)
	}
	return ResolveSubscriptionLimits(s.PlanLimits, group)
}
