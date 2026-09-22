package repository

import (
	"database/sql"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestDedupeOrganizationFallbackCandidatesSkipsCurrentPlan(t *testing.T) {
	currentPlanID := int64(100)
	otherPlanID := int64(200)
	candidates := []service.OrganizationSubscription{
		{ID: 1, PlanID: &currentPlanID, GroupID: 11},
		{ID: 2, PlanID: &currentPlanID, GroupID: 12},
		{ID: 3, PlanID: &otherPlanID, GroupID: 21},
		{ID: 4, PlanID: &otherPlanID, GroupID: 22},
		{ID: 5, GroupID: 31},
		{ID: 6, GroupID: 32},
	}

	got := dedupeOrganizationFallbackCandidates(sql.NullInt64{Int64: currentPlanID, Valid: true}, candidates)

	require.Equal(t, []int64{3, 5, 6}, []int64{got[0].ID, got[1].ID, got[2].ID})
}
