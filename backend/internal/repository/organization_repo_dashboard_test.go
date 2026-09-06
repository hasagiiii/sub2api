package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOrganizationDashboardCountsOnlyIAMUsers(t *testing.T) {
	for _, reuse := range []bool{false, true} {
		name := "standalone"
		if reuse {
			name = "request_context"
		}
		t.Run(name, func(t *testing.T) {
			testOrganizationDashboardCountsOnlyIAMUsers(t, reuse)
		})
	}
}

func testOrganizationDashboardCountsOnlyIAMUsers(t *testing.T, reuse bool) {
	db, mock := newSQLMock(t)
	repo := &organizationRepository{db: db}
	effectiveAt := time.Now().UTC().Add(-24 * time.Hour)
	ctx := context.Background()

	mock.ExpectQuery("SELECT o.id, o.account_id").
		WithArgs(int64(99)).
		WillReturnRows(sqlmock.NewRows([]string{
			"organization_id", "account_id", "company_id", "owner_user_id", "name", "organization_status",
			"membership_id", "role", "membership_status", "authz_generation", "effective_at", "policy_names", "actions",
		}).AddRow(int64(7), "account-1", "company-1", int64(99), "Example", service.OrganizationStatusActive,
			int64(11), service.OrganizationRoleOwner, service.MembershipStatusActive, int64(1), effectiveAt, "{}", "{}"))
	if reuse {
		// The middleware performs the only context query in this request.
		org, err := repo.GetContextForUser(ctx, 99)
		require.NoError(t, err)
		ctx = service.WithOrganizationReadContext(ctx, 99, org)
	}
	mock.ExpectQuery("SELECT count\\(\\*\\), count\\(\\*\\) FILTER \\(WHERE created_at >= \\$2\\)").
		WithArgs(int64(7), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"total_users", "today_new_users", "total_iam_users", "active_iam_users"}).
			AddRow(int64(4), int64(0), int64(3), int64(2)))
	mock.ExpectQuery("FROM api_keys k JOIN organization_memberships").
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"total_api_keys", "active_api_keys"}).AddRow(int64(0), int64(0)))
	mock.ExpectQuery("SELECT count\\(\\*\\), COALESCE\\(sum\\(l\\.input_tokens\\),0\\)").
		WithArgs(int64(7), effectiveAt).
		WillReturnRows(sqlmock.NewRows([]string{"requests", "input", "output", "cache_creation", "cache_read", "cost", "actual_cost", "account_cost", "duration"}).
			AddRow(int64(0), int64(0), int64(0), int64(0), int64(0), float64(0), float64(0), float64(0), float64(0)))
	mock.ExpectQuery("SELECT count\\(\\*\\), count\\(DISTINCT l\\.user_id\\)").
		WithArgs(int64(7), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"requests", "active_users", "input", "output", "cache_creation", "cache_read", "cost", "actual_cost", "account_cost"}).
			AddRow(int64(0), int64(0), int64(0), int64(0), int64(0), int64(0), float64(0), float64(0), float64(0)))
	mock.ExpectQuery("SELECT count\\(\\*\\), COALESCE\\(sum\\(l\\.input_tokens\\+l\\.output_tokens").
		WithArgs(int64(7), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"requests", "tokens"}).AddRow(int64(0), int64(0)))

	stats, err := repo.OrganizationDashboard(ctx, 99)
	require.NoError(t, err)
	require.EqualValues(t, 4, stats.TotalUsers)
	require.EqualValues(t, 3, stats.TotalAccounts)
	require.EqualValues(t, 2, stats.NormalAccounts)
	require.Zero(t, stats.ErrorAccounts)
	require.Zero(t, stats.RateLimitAccounts)
	require.Zero(t, stats.OverloadAccounts)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestOrganizationDashboardReadContextStillChecksPermissions(t *testing.T) {
	for _, test := range []struct {
		name               string
		organizationStatus string
		membershipStatus   string
		role               string
	}{
		{"no_finance_permission", service.OrganizationStatusActive, service.MembershipStatusActive, "member"},
		{"archived_member", service.OrganizationStatusActive, "archived", service.OrganizationRoleOwner},
		{"inactive_organization", "disabled", service.MembershipStatusActive, service.OrganizationRoleOwner},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, mock := newSQLMock(t)
			repo := &organizationRepository{db: db}
			ctx := service.WithOrganizationReadContext(context.Background(), 99, &service.OrganizationContext{
				OrganizationID:     7,
				OrganizationStatus: test.organizationStatus,
				MembershipStatus:   test.membershipStatus,
				Role:               test.role,
			})
			_, err := repo.OrganizationDashboard(ctx, 99)
			require.ErrorIs(t, err, service.ErrOrganizationPermission)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestOrganizationDashboardDoesNotReuseAnotherUsersContext(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &organizationRepository{db: db}
	ctx := service.WithOrganizationReadContext(context.Background(), 99, &service.OrganizationContext{
		OrganizationID:     7,
		OrganizationStatus: service.OrganizationStatusActive,
		MembershipStatus:   service.MembershipStatusActive,
		Role:               service.OrganizationRoleOwner,
	})
	mock.ExpectQuery("SELECT o.id, o.account_id").WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"organization_id"}))
	_, err := repo.OrganizationDashboard(ctx, 42)
	require.ErrorIs(t, err, service.ErrOrganizationPermission)
	require.NoError(t, mock.ExpectationsWereMet())
}
