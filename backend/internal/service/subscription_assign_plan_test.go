//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

// 管理员按套餐分配：分组集合与有效期都来自套餐，让分配出来的订阅与用户自己购买
// 的完全同构（一条订阅覆盖套餐全部分组，共享套餐的一份限额）。
func TestApplyPlanAssignmentDerivesGroupsAndValidityFromPlan(t *testing.T) {
	plan := &dbent.SubscriptionPlan{
		GroupID:      7,
		GroupIds:     []int64{7, 8, 9},
		ValidityDays: 3,
		ValidityUnit: "months",
	}

	input := &AssignSubscriptionInput{UserID: 1}
	require.NoError(t, applyPlanAssignment(input, plan))

	require.Equal(t, []int64{7, 8, 9}, input.GroupIDs)
	// 主分组取首个，展示链路（徽标/倍率）据此选定代表分组。
	require.Equal(t, int64(7), input.GroupID)
	require.Equal(t, psComputeValidityDays(3, "months"), input.ValidityDays)
}

// 管理员显式填了天数就以管理员为准：分配套餐常用于补偿，需要给出与售卖时长不同的
// 期限。
func TestApplyPlanAssignmentKeepsExplicitValidityDays(t *testing.T) {
	plan := &dbent.SubscriptionPlan{GroupID: 7, GroupIds: []int64{7}, ValidityDays: 30, ValidityUnit: "days"}

	input := &AssignSubscriptionInput{UserID: 1, ValidityDays: 7}
	require.NoError(t, applyPlanAssignment(input, plan))

	require.Equal(t, 7, input.ValidityDays)
}

// 存量套餐的 group_ids 还是空数组（迁移未回填或应用先上线），必须回退到单值主分组，
// 否则会把"未回填"当成"套餐没有分组"而拒绝分配。
func TestApplyPlanAssignmentFallsBackToPrimaryGroupColumn(t *testing.T) {
	plan := &dbent.SubscriptionPlan{GroupID: 5, ValidityDays: 30, ValidityUnit: "days"}

	input := &AssignSubscriptionInput{UserID: 1}
	require.NoError(t, applyPlanAssignment(input, plan))

	require.Equal(t, int64(5), input.GroupID)
	require.Equal(t, []int64{5}, input.GroupIDs)
}

func TestApplyPlanAssignmentRejectsPlanWithoutGroup(t *testing.T) {
	input := &AssignSubscriptionInput{UserID: 1}
	err := applyPlanAssignment(input, &dbent.SubscriptionPlan{ValidityDays: 30, ValidityUnit: "days"})
	require.ErrorIs(t, err, ErrSubscriptionPlanNoGroup)
}

func newPlanAssignService(subRepo UserSubscriptionRepository) *SubscriptionService {
	groupRepo := &subscriptionGroupRepoStub{
		group: &Group{ID: 1, SubscriptionType: SubscriptionTypeSubscription},
	}
	return NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)
}

// 按套餐分配必须按 (user, plan) 找复用目标。若按分组找，用户在该分组上已有的手动
// 订阅会被当成同一条而被续期，管理员想发的套餐反而没发出去。
func TestAssignPlanSubscriptionDoesNotReuseManualSubscriptionOnSameGroup(t *testing.T) {
	start := time.Now().Add(-time.Hour)
	subRepo := newSubscriptionUserSubRepoStub()
	subRepo.seed(&UserSubscription{
		ID:        10,
		UserID:    1001,
		GroupID:   1,
		GroupIDs:  []int64{1},
		StartsAt:  start,
		ExpiresAt: start.AddDate(0, 0, 30),
		Status:    SubscriptionStatusActive,
	})

	planID := int64(77)
	sub, err := newPlanAssignService(subRepo).AssignSubscription(context.Background(), &AssignSubscriptionInput{
		UserID:       1001,
		GroupID:      1,
		GroupIDs:     []int64{1, 2},
		PlanID:       &planID,
		ValidityDays: 30,
	})

	require.NoError(t, err)
	require.Equal(t, 1, subRepo.createCalls, "plan assignment must open its own quota pool")
	require.NotEqual(t, int64(10), sub.ID)
	require.Equal(t, &planID, sub.PlanID)
	require.Equal(t, []int64{1, 2}, sub.GroupIDs)
}

// 反向：手动分配必须限定 plan_id IS NULL。否则管理员给某分组手动分配时会去延长一条
// 用户花钱买来的套餐订阅。
func TestManualAssignDoesNotReusePlanSubscriptionOnSameGroup(t *testing.T) {
	start := time.Now().Add(-time.Hour)
	planID := int64(77)
	subRepo := newSubscriptionUserSubRepoStub()
	subRepo.seed(&UserSubscription{
		ID:        11,
		UserID:    1002,
		GroupID:   1,
		GroupIDs:  []int64{1},
		PlanID:    &planID,
		StartsAt:  start,
		ExpiresAt: start.AddDate(0, 0, 30),
		Status:    SubscriptionStatusActive,
	})

	sub, err := newPlanAssignService(subRepo).AssignSubscription(context.Background(), &AssignSubscriptionInput{
		UserID:       1002,
		GroupID:      1,
		ValidityDays: 30,
	})

	require.NoError(t, err)
	require.Equal(t, 1, subRepo.createCalls)
	require.NotEqual(t, int64(11), sub.ID)
	require.Nil(t, sub.PlanID)
}

// 重复分配同一套餐落到同一条订阅上；套餐的分组构成在两次分配之间被改过时，续期要
// 把覆盖集合同步到最新，否则续了期却停留在旧的分组范围上。
func TestAssignPlanSubscriptionRenewsExpiredAndSyncsCoveredGroups(t *testing.T) {
	start := time.Now().AddDate(0, 0, -60)
	planID := int64(77)
	subRepo := newSubscriptionUserSubRepoStub()
	subRepo.seed(&UserSubscription{
		ID:        12,
		UserID:    1003,
		GroupID:   1,
		GroupIDs:  []int64{1},
		PlanID:    &planID,
		StartsAt:  start,
		ExpiresAt: start.AddDate(0, 0, 30),
		Status:    SubscriptionStatusExpired,
	})

	sub, err := newPlanAssignService(subRepo).AssignSubscription(context.Background(), &AssignSubscriptionInput{
		UserID:       1003,
		GroupID:      1,
		GroupIDs:     []int64{1, 2},
		PlanID:       &planID,
		ValidityDays: 30,
	})

	require.NoError(t, err)
	require.Equal(t, 0, subRepo.createCalls, "same plan must renew instead of opening a second pool")
	require.Equal(t, int64(12), sub.ID)
	require.Equal(t, []int64{1, 2}, sub.GroupIDs)
}

// 覆盖分组里只要有一个不是订阅型，这条订阅就会在那个分组上放出没有限额依据的流量，
// 因此必须逐个校验而不是只看主分组。
func TestAssignSubscriptionValidatesEveryCoveredGroup(t *testing.T) {
	groupRepo := &subscriptionGroupRepoStub{
		group: &Group{ID: 1, SubscriptionType: SubscriptionTypeStandard},
	}
	svc := NewSubscriptionService(groupRepo, newSubscriptionUserSubRepoStub(), nil, nil, nil)

	planID := int64(77)
	_, err := svc.AssignSubscription(context.Background(), &AssignSubscriptionInput{
		UserID:   1004,
		GroupID:  1,
		GroupIDs: []int64{1, 2},
		PlanID:   &planID,
	})

	require.ErrorIs(t, err, ErrGroupNotSubscriptionType)
}

func TestAssignSubscriptionRequiresAGroup(t *testing.T) {
	svc := newPlanAssignService(newSubscriptionUserSubRepoStub())

	_, err := svc.AssignSubscription(context.Background(), &AssignSubscriptionInput{UserID: 1005})

	require.ErrorIs(t, err, ErrSubscriptionGroupRequired)
}

// 批量按套餐分配时套餐只解析一次：套餐不可用要在动手之前整批失败，不能有的用户
// 分到了、有的没分到。
func TestBulkAssignPlanSubscriptionFailsBeforeAssigningAnyone(t *testing.T) {
	subRepo := newSubscriptionUserSubRepoStub()
	svc := newPlanAssignService(subRepo)

	planID := int64(404)
	_, err := svc.BulkAssignSubscription(context.Background(), &BulkAssignSubscriptionInput{
		UserIDs:      []int64{1, 2, 3},
		PlanID:       &planID,
		ValidityDays: 30,
	})

	require.ErrorIs(t, err, ErrSubscriptionPlanNotFound)
	require.Equal(t, 0, subRepo.createCalls)
}
