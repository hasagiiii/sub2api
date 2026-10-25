package service

import (
	"time"
)

const subscriptionDayDuration = 24 * time.Hour

type SubscriptionGroupLimit struct {
	GroupID         int64
	Name            string
	DailyLimitUSD   *float64
	WeeklyLimitUSD  *float64
	MonthlyLimitUSD *float64
}

type SubscriptionGroupUsage struct {
	GroupID            int64
	DailyWindowStart   *time.Time
	WeeklyWindowStart  *time.Time
	MonthlyWindowStart *time.Time
	DailyUsageUSD      float64
	WeeklyUsageUSD     float64
	MonthlyUsageUSD    float64
}

type UserSubscription struct {
	ID      int64
	UserID  int64
	GroupID int64
	// PlanID 是来源套餐；nil 表示后台手动分配、不属于任何套餐的订阅。
	PlanID *int64
	// PlanName 是来源套餐的展示名称；手动分配的订阅为空。
	PlanName string
	// GroupIDs 是这条订阅覆盖的全部分组（含 GroupID，且 GroupID 为首元素）。
	// 套餐级限额和套餐级累计由订阅行共享；套餐完全未配置限额时，各分组才按实际
	// 分组使用 GroupUsages 独立校验。空切片表示尚未加载，读取方应回退到 GroupID。
	GroupIDs []int64
	// GroupNames 与 GroupIDs 一一对应，用于向用户展示套餐覆盖的全部分组。
	GroupNames []string
	// GroupLimits 按 GroupIDs 顺序保存套餐内各分组的原始限额。
	GroupLimits []SubscriptionGroupLimit
	// GroupUsages 按分组保存独立用量。套餐模式当前使用订阅行计数器，无套餐限额模式
	// 使用这里的分组计数器；两层数据始终同时累计。
	GroupUsages map[int64]SubscriptionGroupUsage
	// PlanLimits 是来源套餐的限额，加载订阅时实时读入。只要任一窗口有套餐限额，
	// EffectiveLimits 对其他窗口返回不限额；只有完全未配置时才使用分组限额。
	PlanLimits SubscriptionLimits

	StartsAt  time.Time
	ExpiresAt time.Time
	Status    string

	DailyWindowStart   *time.Time
	WeeklyWindowStart  *time.Time
	MonthlyWindowStart *time.Time

	DailyUsageUSD   float64
	WeeklyUsageUSD  float64
	MonthlyUsageUSD float64

	AssignedBy *int64
	AssignedAt time.Time
	Notes      string

	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time

	User           *User
	Group          *Group
	AssignedByUser *User
}

func (s *UserSubscription) IsActive() bool {
	return s.Status == SubscriptionStatusActive && time.Now().Before(s.ExpiresAt)
}

func (s *UserSubscription) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

func (s *UserSubscription) DaysRemaining() int {
	return s.daysRemainingAt(time.Now())
}

func (s *UserSubscription) daysRemainingAt(now time.Time) int {
	remaining := s.ExpiresAt.Sub(now)
	if remaining <= 0 {
		return 0
	}

	days := int(remaining / subscriptionDayDuration)
	if remaining%subscriptionDayDuration != 0 {
		days++
	}
	return days
}

func (s *UserSubscription) IsWindowActivated() bool {
	return s.DailyWindowStart != nil || s.WeeklyWindowStart != nil || s.MonthlyWindowStart != nil
}

func (s *UserSubscription) HasOneTimeDailyQuota() bool {
	if s == nil || s.StartsAt.IsZero() || s.ExpiresAt.IsZero() {
		return false
	}
	return !s.ExpiresAt.After(s.StartsAt.AddDate(0, 0, 1))
}

func (s *UserSubscription) NeedsDailyReset() bool {
	return s.NeedsDailyResetAt(time.Now())
}

func (s *UserSubscription) NeedsDailyResetAt(now time.Time) bool {
	_, ok := s.automaticDailyWindowStartAt(now)
	return ok
}

func (s *UserSubscription) NeedsWeeklyReset() bool {
	return s.NeedsWeeklyResetAt(time.Now())
}

func (s *UserSubscription) NeedsWeeklyResetAt(now time.Time) bool {
	if s.WeeklyWindowStart == nil {
		return false
	}
	return !now.Before(s.WeeklyWindowStart.Add(7 * 24 * time.Hour))
}

func (s *UserSubscription) NeedsMonthlyReset() bool {
	return s.NeedsMonthlyResetAt(time.Now())
}

func (s *UserSubscription) NeedsMonthlyResetAt(now time.Time) bool {
	if s.MonthlyWindowStart == nil {
		return false
	}
	return !now.Before(s.MonthlyWindowStart.Add(30 * 24 * time.Hour))
}

func (s *UserSubscription) canAutomaticallyResetDailyAt(now time.Time) bool {
	_, ok := s.automaticDailyWindowStartAt(now)
	return ok
}

// dailyWindowIndexAt 返回 t 所在日窗口的序号。日窗口从 StartsAt（购买/重新开通时刻）
// 起每 24 小时滚动一次，刷新时刻固定为购买时刻，与激活、手动重置写入的锚点无关，
// 因此不会漂移。早于 StartsAt 的时刻（如旧版写入的 0 点锚点）归入第 0 个窗口。
// 序号不越过到期时间所在的窗口，避免到期后的不完整周期再发放一次额度。
func (s *UserSubscription) dailyWindowIndexAt(t time.Time) int64 {
	if !t.After(s.StartsAt) {
		return 0
	}
	index := int64(t.Sub(s.StartsAt) / subscriptionDayDuration)
	if s.ExpiresAt.After(s.StartsAt) {
		last := int64((s.ExpiresAt.Sub(s.StartsAt) - 1) / subscriptionDayDuration)
		if index > last {
			index = last
		}
	}
	return index
}

// dailyWindowStartAt 返回 now 所在日窗口的起点（StartsAt + N*24h）。
// 激活、手动重置、自动推进写入的日窗口锚点都取此值，保证展示层按“起点+24h”
// 计算的重置时间与实际刷新时刻一致。
func (s *UserSubscription) dailyWindowStartAt(now time.Time) time.Time {
	if s.StartsAt.IsZero() {
		return now
	}
	return s.StartsAt.Add(time.Duration(s.dailyWindowIndexAt(now)) * subscriptionDayDuration)
}

// automaticDailyWindowStartAt 计算订阅级日窗口是否需要推进及新的窗口起点。
func (s *UserSubscription) automaticDailyWindowStartAt(now time.Time) (time.Time, bool) {
	return s.automaticDailyWindowStartFrom(s.DailyWindowStart, now)
}

// automaticDailyWindowStartFrom 按“从购买时刻起每 24 小时滚动”计算日窗口推进：
// previous 所在窗口早于 now 所在窗口时，推进到 now 所在窗口的起点。
// 日卡（期限不超过 1 天）为一次性额度，不自动刷新；续费延长期限后按购买时刻
// 继续滚动，已付费的当天额度用满 24 小时才刷新。
func (s *UserSubscription) automaticDailyWindowStartFrom(previous *time.Time, now time.Time) (time.Time, bool) {
	if previous == nil {
		return time.Time{}, false
	}
	if s.HasOneTimeDailyQuota() {
		return time.Time{}, false
	}
	if s.StartsAt.IsZero() {
		next := previous.Add(subscriptionDayDuration)
		if now.Before(next) {
			return time.Time{}, false
		}
		return previous.Add(now.Sub(*previous) / subscriptionDayDuration * subscriptionDayDuration), true
	}
	if s.dailyWindowIndexAt(now) <= s.dailyWindowIndexAt(*previous) {
		return time.Time{}, false
	}
	return s.dailyWindowStartAt(now), true
}

// canonicalDailyWindowStart 把任意日窗口锚点（含旧版 0 点锚点）换算为其所在
// 日窗口的起点，仅用于展示，保证前端按“起点+24h”算出的重置时间准确。
func (s *UserSubscription) canonicalDailyWindowStart(previous *time.Time) *time.Time {
	if previous == nil || s.StartsAt.IsZero() {
		return previous
	}
	t := s.StartsAt.Add(time.Duration(s.dailyWindowIndexAt(*previous)) * subscriptionDayDuration)
	return &t
}

func (s *UserSubscription) canAutomaticallyResetWeeklyAt(now time.Time) bool {
	_, ok := s.automaticWindowStartAt(s.WeeklyWindowStart, 7*24*time.Hour, now)
	return ok
}

func (s *UserSubscription) canAutomaticallyResetMonthlyAt(now time.Time) bool {
	_, ok := s.automaticWindowStartAt(s.MonthlyWindowStart, 30*24*time.Hour, now)
	return ok
}

// windowResetAnchor 返回周/月窗口实际推进所依据的锚点。
// 早期订阅把首个窗口初始化在开通日零点；只有这个初始值是无歧义的，之后出现的
// 零点锚点可能来自手动重置，必须保持权威。
// 自动推进（automaticWindowStartAt）与对外展示的重置时间（WeeklyResetTime/
// MonthlyResetTime）必须共用这一修正，否则仪表盘显示的重置时间会早于窗口实际
// 滚动的时间。
// 日窗口按购买时刻滚动（automaticDailyWindowStartFrom），不走这里。
func (s *UserSubscription) windowResetAnchor(previous time.Time) time.Time {
	legacyAnchor := startOfDay(s.StartsAt)
	if legacyAnchor.Before(s.StartsAt) && previous.Equal(legacyAnchor) {
		return s.StartsAt
	}
	return previous
}

// automaticWindowStartAt 计算周/月窗口（期限对齐滚动窗口）的当前窗口起点。
// 窗口从锚点按整数个 period 步进，且不越过订阅到期时间，避免最后一个不完整
// 周期重复发放额度（issue #5051）。日窗口不走此函数，见 automaticDailyWindowStartFrom。
func (s *UserSubscription) automaticWindowStartAt(previous *time.Time, period time.Duration, now time.Time) (time.Time, bool) {
	if previous == nil {
		return time.Time{}, false
	}

	anchor := s.windowResetAnchor(*previous)
	next := anchor.Add(period)
	if now.Before(next) || !next.Before(s.ExpiresAt) {
		return time.Time{}, false
	}

	periods := now.Sub(anchor) / period
	lastPeriodBeforeExpiry := (s.ExpiresAt.Sub(anchor) - 1) / period
	if periods > lastPeriodBeforeExpiry {
		periods = lastPeriodBeforeExpiry
	}
	return anchor.Add(periods * period), true
}

func (s *UserSubscription) DailyResetTime() *time.Time {
	if s.DailyWindowStart == nil {
		return nil
	}
	if s.HasOneTimeDailyQuota() {
		t := s.ExpiresAt
		return &t
	}
	if s.StartsAt.IsZero() {
		t := s.DailyWindowStart.Add(subscriptionDayDuration)
		return &t
	}
	// 日窗口从购买时刻起每 24 小时滚动：下次刷新为当前窗口起点 + 24h，且不晚于到期时间。
	t := s.StartsAt.Add(time.Duration(s.dailyWindowIndexAt(*s.DailyWindowStart)+1) * subscriptionDayDuration)
	if s.ExpiresAt.After(s.StartsAt) && t.After(s.ExpiresAt) {
		t = s.ExpiresAt
	}
	return &t
}

func (s *UserSubscription) WeeklyResetTime() *time.Time {
	if s.WeeklyWindowStart == nil {
		return nil
	}
	t := s.windowResetAnchor(*s.WeeklyWindowStart).Add(7 * 24 * time.Hour)
	return &t
}

func (s *UserSubscription) MonthlyResetTime() *time.Time {
	if s.MonthlyWindowStart == nil {
		return nil
	}
	t := s.windowResetAnchor(*s.MonthlyWindowStart).Add(30 * 24 * time.Hour)
	return &t
}

// CheckDailyLimit 等方法按整条订阅的限额模式选择计数器：套餐配置了任一窗口限额
// 时，所有窗口都使用订阅级累计值；套餐完全没有限额时，才使用本次实际路由分组
// 的独立累计值。
func (s *UserSubscription) CheckDailyLimit(group *Group, additionalCost float64) bool {
	limits := s.EffectiveLimits(group)
	if !limits.HasDailyLimit() {
		return true
	}
	usage := s.DailyUsageUSD
	if !s.UsesPlanLimits() {
		usage = s.GroupUsage(group).DailyUsageUSD
	}
	return usage+additionalCost <= *limits.DailyLimitUSD
}

func (s *UserSubscription) CheckWeeklyLimit(group *Group, additionalCost float64) bool {
	limits := s.EffectiveLimits(group)
	if !limits.HasWeeklyLimit() {
		return true
	}
	usage := s.WeeklyUsageUSD
	if !s.UsesPlanLimits() {
		usage = s.GroupUsage(group).WeeklyUsageUSD
	}
	return usage+additionalCost <= *limits.WeeklyLimitUSD
}

func (s *UserSubscription) CheckMonthlyLimit(group *Group, additionalCost float64) bool {
	limits := s.EffectiveLimits(group)
	if !limits.HasMonthlyLimit() {
		return true
	}
	usage := s.MonthlyUsageUSD
	if !s.UsesPlanLimits() {
		usage = s.GroupUsage(group).MonthlyUsageUSD
	}
	return usage+additionalCost <= *limits.MonthlyLimitUSD
}

// UsesIndependentGroupUsage is true for subscriptions whose package has no
// package-level limit. Such a package still records both package and group
// usage, but group limits are enforced against the per-group counters.
func (s *UserSubscription) UsesIndependentGroupUsage() bool {
	return s != nil && !s.UsesPlanLimits()
}

// UsesPlanLimits reports whether any positive package limit is configured. The
// result is intentionally subscription-wide: configuring one window switches
// all limit checks to package counters, while other package windows remain
// unlimited until explicitly configured.
func (s *UserSubscription) UsesPlanLimits() bool {
	return s != nil && !s.PlanLimits.IsZero()
}

func (s *UserSubscription) UsesGroupDailyUsage() bool {
	return s != nil && s.GroupUsages != nil && s.UsesIndependentGroupUsage()
}

func (s *UserSubscription) UsesGroupWeeklyUsage() bool {
	return s != nil && s.GroupUsages != nil && s.UsesIndependentGroupUsage()
}

func (s *UserSubscription) UsesGroupMonthlyUsage() bool {
	return s != nil && s.GroupUsages != nil && s.UsesIndependentGroupUsage()
}

func (s *UserSubscription) GroupUsage(group *Group) SubscriptionGroupUsage {
	if s == nil {
		return SubscriptionGroupUsage{}
	}
	groupID := s.GroupID
	if group != nil && group.ID > 0 {
		groupID = group.ID
	}
	if usage, ok := s.GroupUsages[groupID]; ok {
		return usage
	}
	return SubscriptionGroupUsage{
		GroupID:            groupID,
		DailyWindowStart:   s.DailyWindowStart,
		WeeklyWindowStart:  s.WeeklyWindowStart,
		MonthlyWindowStart: s.MonthlyWindowStart,
		DailyUsageUSD:      s.DailyUsageUSD,
		WeeklyUsageUSD:     s.WeeklyUsageUSD,
		MonthlyUsageUSD:    s.MonthlyUsageUSD,
	}
}

func (s *UserSubscription) GroupWindowActivated(group *Group) bool {
	usage := s.GroupUsage(group)
	return usage.DailyWindowStart != nil || usage.WeeklyWindowStart != nil || usage.MonthlyWindowStart != nil
}

// CoveredGroupIDs 返回这条订阅覆盖的全部分组，主分组在首位。
// GroupIDs 未加载时回退到单个主分组，保证调用方永远拿得到非空集合。
func (s *UserSubscription) CoveredGroupIDs() []int64 {
	if s == nil {
		return nil
	}
	if len(s.GroupIDs) == 0 {
		if s.GroupID <= 0 {
			return nil
		}
		return []int64{s.GroupID}
	}
	return s.GroupIDs
}

// CoversGroup 报告这条订阅的额度池是否覆盖指定分组。
// GroupIDs 为空表示未加载覆盖集合，此时回退比较主分组，保证存量调用不受影响。
func (s *UserSubscription) CoversGroup(groupID int64) bool {
	if s == nil || groupID <= 0 {
		return false
	}
	if len(s.GroupIDs) == 0 {
		return s.GroupID == groupID
	}
	for _, id := range s.GroupIDs {
		if id == groupID {
			return true
		}
	}
	return false
}

func (s *UserSubscription) CheckAllLimits(group *Group, additionalCost float64) (daily, weekly, monthly bool) {
	daily = s.CheckDailyLimit(group, additionalCost)
	weekly = s.CheckWeeklyLimit(group, additionalCost)
	monthly = s.CheckMonthlyLimit(group, additionalCost)
	return
}
