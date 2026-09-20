package dto

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUserSubscriptionFromService_MapsPlanAndGroupLimits(t *testing.T) {
	planID := int64(7)
	planDaily := 12.5
	groupDaily := 4.25
	groupMonthly := 90.0

	out := UserSubscriptionFromService(&service.UserSubscription{
		ID:       1,
		PlanID:   &planID,
		PlanName: "图像套餐",
		PlanLimits: service.SubscriptionLimits{
			DailyLimitUSD: &planDaily,
		},
		GroupIDs:   []int64{10, 20},
		GroupNames: []string{"主分组", "备用分组"},
		GroupLimits: []service.SubscriptionGroupLimit{
			{GroupID: 10, Name: "主分组", DailyLimitUSD: &groupDaily},
			{GroupID: 20, Name: "备用分组", MonthlyLimitUSD: &groupMonthly},
		},
	})

	require.NotNil(t, out)
	require.Equal(t, "图像套餐", out.PlanName)
	require.Equal(t, &planDaily, out.PlanDailyLimitUSD)
	require.Equal(t, []int64{10, 20}, out.GroupIDs)
	require.Equal(t, []string{"主分组", "备用分组"}, out.GroupNames)
	require.Len(t, out.GroupLimits, 2)
	require.Equal(t, int64(10), out.GroupLimits[0].GroupID)
	require.Equal(t, &groupDaily, out.GroupLimits[0].DailyLimitUSD)
	require.Equal(t, int64(20), out.GroupLimits[1].GroupID)
	require.Equal(t, &groupMonthly, out.GroupLimits[1].MonthlyLimitUSD)
}
