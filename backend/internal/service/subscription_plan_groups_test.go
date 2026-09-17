//go:build unit

package service

import (
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

func TestNormalizeGroupIDsDropsInvalidAndDeduplicatesPreservingOrder(t *testing.T) {
	t.Parallel()
	// 顺序必须保留：首个元素会写回单值主分组列，决定结账页/广场卡片展示哪个
	// 分组的平台与倍率，排序会打乱管理员的配置意图。
	require.Equal(t, []int64{9, 3, 5}, normalizeGroupIDs([]int64{9, 3, 9, 5, 3}))
	require.Nil(t, normalizeGroupIDs([]int64{0, -1}))
	require.Nil(t, normalizeGroupIDs(nil))
	require.Equal(t, []int64{4}, normalizeGroupIDs([]int64{0, 4, -2, 4}))
}

func TestPlanGroupIDsFallsBackToLegacySingleColumn(t *testing.T) {
	t.Parallel()
	// 迁移期：应用已上线但回填未跑，group_ids 为空而 group_id 有效。
	// 此时必须回退到单值列，否则套餐会被当成"没有任何分组"而静默不发放订阅。
	legacy := &dbent.SubscriptionPlan{GroupID: 12}
	require.Equal(t, []int64{12}, PlanGroupIDs(legacy))
	require.Equal(t, int64(12), PlanPrimaryGroupID(legacy))

	multi := &dbent.SubscriptionPlan{GroupID: 12, GroupIds: []int64{12, 13}}
	require.Equal(t, []int64{12, 13}, PlanGroupIDs(multi))
	require.Equal(t, int64(12), PlanPrimaryGroupID(multi))

	require.Nil(t, PlanGroupIDs(nil))
	require.Zero(t, PlanPrimaryGroupID(nil))
	require.Nil(t, PlanGroupIDs(&dbent.SubscriptionPlan{}))
}

func TestOrderSubscriptionGroupIDsFallsBackToLegacySingleColumn(t *testing.T) {
	t.Parallel()
	groupID := int64(7)
	legacy := &dbent.PaymentOrder{SubscriptionGroupID: &groupID}
	require.Equal(t, []int64{7}, OrderSubscriptionGroupIDs(legacy))

	multi := &dbent.PaymentOrder{SubscriptionGroupID: &groupID, SubscriptionGroupIds: []int64{7, 8}}
	require.Equal(t, []int64{7, 8}, OrderSubscriptionGroupIDs(multi))

	require.Nil(t, OrderSubscriptionGroupIDs(nil))
	require.Nil(t, OrderSubscriptionGroupIDs(&dbent.PaymentOrder{}))
}

func TestPaymentSubscriptionAssignedActionFitsAuditColumn(t *testing.T) {
	t.Parallel()
	// payment_audit_logs.action 限长 50；分组后缀后仍需可容纳 int64 全长。
	require.Equal(t, "SUBSCRIPTION_ASSIGNED:7", paymentSubscriptionAssignedAction(7))
	require.LessOrEqual(t, len(paymentSubscriptionAssignedAction(9223372036854775807)), 50)
}

func TestCreatePlanRequestResolvedGroupIDs(t *testing.T) {
	t.Parallel()
	// 新客户端传 group_ids。
	require.Equal(t, []int64{3, 4}, CreatePlanRequest{GroupIDs: []int64{3, 4, 3}}.ResolvedGroupIDs())
	// 旧客户端只传 group_id。
	require.Equal(t, []int64{5}, CreatePlanRequest{GroupID: 5}.ResolvedGroupIDs())
	// 两者都传时以 group_ids 为准。
	require.Equal(t, []int64{3}, CreatePlanRequest{GroupID: 5, GroupIDs: []int64{3}}.ResolvedGroupIDs())
	require.Nil(t, CreatePlanRequest{}.ResolvedGroupIDs())
}

func TestUpdatePlanRequestResolvedGroupIDsReportsWhetherTouched(t *testing.T) {
	t.Parallel()
	// 未触碰分组字段的补丁不得改动已有绑定。
	ids, touched := UpdatePlanRequest{}.ResolvedGroupIDs()
	require.False(t, touched)
	require.Nil(t, ids)

	groupID := int64(5)
	ids, touched = UpdatePlanRequest{GroupID: &groupID}.ResolvedGroupIDs()
	require.True(t, touched)
	require.Equal(t, []int64{5}, ids)

	ids, touched = UpdatePlanRequest{GroupIDs: &[]int64{8, 9, 8}}.ResolvedGroupIDs()
	require.True(t, touched)
	require.Equal(t, []int64{8, 9}, ids)

	// 显式传空数组表示"清空"，校验层会据此报 PLAN_GROUP_REQUIRED。
	ids, touched = UpdatePlanRequest{GroupIDs: &[]int64{}}.ResolvedGroupIDs()
	require.True(t, touched)
	require.Nil(t, ids)
	require.Error(t, validatePlanPatch(UpdatePlanRequest{GroupIDs: &[]int64{}}))
}

func TestMergeGroupModelNamesUnionsAndDeduplicates(t *testing.T) {
	t.Parallel()
	groupModels := map[int64][]string{
		7: {"claude-opus-4-6", "gpt-5.5"},
		8: {"GPT-5.5", "gemini-2.5-flash"},
	}
	// 大小写不敏感去重，首次出现的原始大小写胜出，最终按字典序排序。
	require.Equal(t,
		[]string{"claude-opus-4-6", "gemini-2.5-flash", "gpt-5.5"},
		mergeGroupModelNames([]int64{7, 8}, groupModels),
	)
	// 单分组走直通路径，保持既有口径。
	require.Equal(t, []string{"claude-opus-4-6", "gpt-5.5"}, mergeGroupModelNames([]int64{7}, groupModels))
}
