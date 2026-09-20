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
// 只要套餐配置了任意一个正限额，整条订阅就进入套餐限额模式：所有窗口都从
// 套餐配置读取，未配置的窗口表示不限额，不再回退到分组限额。只有套餐完全没有
// 配置任何限额（以及后台手动分配、不属于任何套餐的订阅）时，才使用当前路由
// 分组的独立限额。
func ResolveSubscriptionLimits(planLimits SubscriptionLimits, group *Group) SubscriptionLimits {
	if !planLimits.IsZero() {
		return planLimits
	}
	return GroupLimits(group)
}

// EffectiveLimits 返回这条订阅当前生效的限额。
//
// PlanLimits 由加载订阅时从来源套餐实时读入（不是购买时快照），因此管理员调整
// 套餐限额会立即对已售出的订阅生效。套餐只要配置任一窗口，整条订阅即使用套餐
// 模式；未配置的其他窗口为不限额。
func (s *UserSubscription) EffectiveLimits(group *Group) SubscriptionLimits {
	if s == nil {
		return GroupLimits(group)
	}
	return ResolveSubscriptionLimits(s.PlanLimits, group)
}
