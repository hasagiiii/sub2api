package service

import "context"

// sharedQuotaRepoDefaults 供测试桩嵌入，补齐共享额度池改动新增的三个仓储方法。
//
// 只覆盖【新增】的方法，不碰桩已自行实现的其余方法，避免嵌入带来的方法遮蔽。
// 所有方法都 panic 而不是返回零值：桩若意外走到未实现的路径，测试会立刻以明确
// 的原因失败，而不是拿到 nil 后在远处以难以定位的方式出错。需要其中某个方法的
// 桩自行定义同名方法即可覆盖（外层方法优先）。
type sharedQuotaRepoDefaults struct{}

func (sharedQuotaRepoDefaults) GetByUserIDAndPlanID(context.Context, int64, int64) (*UserSubscription, error) {
	panic("unimplemented: GetByUserIDAndPlanID")
}

func (sharedQuotaRepoDefaults) GetManualByUserIDAndGroupID(context.Context, int64, int64) (*UserSubscription, error) {
	panic("unimplemented: GetManualByUserIDAndGroupID")
}

func (sharedQuotaRepoDefaults) ReplaceCoveredGroups(context.Context, int64, []int64) error {
	panic("unimplemented: ReplaceCoveredGroups")
}
