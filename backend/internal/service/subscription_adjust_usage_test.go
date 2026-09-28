//go:build unit

package service

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type adjustUsageRepoStub struct {
	userSubRepoNoop
	sub            *UserSubscription
	called         bool
	calledSubID    int64
	calledGroupID  int64
	calledAmount   float64
	incrementErr   error
	activateCalled bool
}

func (r *adjustUsageRepoStub) GetByID(_ context.Context, id int64) (*UserSubscription, error) {
	if r.sub == nil || r.sub.ID != id {
		return nil, ErrSubscriptionNotFound
	}
	return r.sub, nil
}

func (r *adjustUsageRepoStub) IncrementUsageForGroup(_ context.Context, subscriptionID, groupID int64, amount float64) error {
	r.called = true
	r.calledSubID = subscriptionID
	r.calledGroupID = groupID
	r.calledAmount = amount
	if r.incrementErr != nil {
		return r.incrementErr
	}
	r.sub.DailyUsageUSD += amount
	r.sub.WeeklyUsageUSD += amount
	r.sub.MonthlyUsageUSD += amount
	usage := r.sub.GroupUsages[groupID]
	usage.GroupID = groupID
	usage.DailyUsageUSD += amount
	usage.WeeklyUsageUSD += amount
	usage.MonthlyUsageUSD += amount
	r.sub.GroupUsages[groupID] = usage
	return nil
}

func (r *adjustUsageRepoStub) ActivateGroupWindows(context.Context, int64, int64, time.Time, time.Time) error {
	r.activateCalled = true
	return nil
}

func (r *adjustUsageRepoStub) ActivateWindows(context.Context, int64, time.Time, time.Time) error {
	r.activateCalled = true
	return nil
}

func (r *adjustUsageRepoStub) ResetGroupUsageWindows(context.Context, int64, int64, bool, bool, bool, time.Time, time.Time) error {
	return nil
}

func (r *adjustUsageRepoStub) ResetGroupDailyUsage(context.Context, int64, int64, *time.Time, time.Time) error {
	return nil
}

func (r *adjustUsageRepoStub) ResetGroupWeeklyUsage(context.Context, int64, int64, *time.Time, time.Time) error {
	return nil
}

func (r *adjustUsageRepoStub) ResetGroupMonthlyUsage(context.Context, int64, int64, *time.Time, time.Time) error {
	return nil
}

func newAdjustUsageService(repo *adjustUsageRepoStub) *SubscriptionService {
	return NewSubscriptionService(groupRepoNoop{}, repo, nil, nil, nil)
}

func TestAdminAdjustUsageIncrementsPackageAndSelectedGroup(t *testing.T) {
	planID := int64(301)
	repo := &adjustUsageRepoStub{
		sub: &UserSubscription{
			ID:              11,
			UserID:          22,
			GroupID:         10,
			GroupIDs:        []int64{10, 20},
			PlanID:          &planID,
			GroupUsages:     map[int64]SubscriptionGroupUsage{10: {GroupID: 10}},
			DailyUsageUSD:   1,
			WeeklyUsageUSD:  2,
			MonthlyUsageUSD: 3,
		},
	}

	updated, err := newAdjustUsageService(repo).AdminAdjustUsage(context.Background(), 11, 20, 4.25)

	require.NoError(t, err)
	require.True(t, repo.activateCalled)
	require.True(t, repo.called)
	require.Equal(t, int64(11), repo.calledSubID)
	require.Equal(t, int64(20), repo.calledGroupID)
	require.Equal(t, 4.25, repo.calledAmount)
	require.Equal(t, 5.25, updated.DailyUsageUSD)
	require.Equal(t, 6.25, updated.WeeklyUsageUSD)
	require.Equal(t, 7.25, updated.MonthlyUsageUSD)
	require.Equal(t, 4.25, updated.GroupUsages[20].DailyUsageUSD)
	require.Equal(t, 4.25, updated.GroupUsages[20].WeeklyUsageUSD)
	require.Equal(t, 4.25, updated.GroupUsages[20].MonthlyUsageUSD)
}

func TestAdminAdjustUsageRejectsUncoveredGroup(t *testing.T) {
	planID := int64(302)
	repo := &adjustUsageRepoStub{
		sub: &UserSubscription{ID: 12, GroupIDs: []int64{10}, PlanID: &planID},
	}

	_, err := newAdjustUsageService(repo).AdminAdjustUsage(context.Background(), 12, 20, 1)

	require.ErrorIs(t, err, ErrSubscriptionGroupNotCovered)
	require.False(t, repo.called)
}

func TestAdminAdjustUsageRequiresPlanAndPositiveFiniteAmount(t *testing.T) {
	repo := &adjustUsageRepoStub{sub: &UserSubscription{ID: 13, GroupIDs: []int64{10}}}
	svc := newAdjustUsageService(repo)

	_, err := svc.AdminAdjustUsage(context.Background(), 13, 10, 1)
	require.ErrorIs(t, err, ErrSubscriptionUsagePlanOnly)
	require.False(t, repo.called)

	planID := int64(303)
	repo.sub.PlanID = &planID
	for _, amount := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		_, err = svc.AdminAdjustUsage(context.Background(), 13, 10, amount)
		require.ErrorIs(t, err, ErrSubscriptionUsageAmount)
	}
	require.False(t, repo.called)
}
