package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func expectOrganizationReadContext(mock sqlmock.Sqlmock, userID int64, effectiveAt time.Time) {
	mock.ExpectQuery("SELECT o.id, o.account_id").WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{
			"organization_id", "account_id", "company_id", "owner_user_id", "name", "organization_status",
			"membership_id", "role", "membership_status", "authz_generation", "effective_at", "policy_names", "actions",
		}).AddRow(int64(7), "account-1", "company-1", userID, "Example", service.OrganizationStatusActive,
			int64(11), service.OrganizationRoleOwner, service.MembershipStatusActive, int64(1), effectiveAt, "{}", "{}"))
}

func TestOrganizationStatisticsReuseRequestContext(t *testing.T) {
	for _, endpoint := range []string{"charts", "users-trend", "spending-ranking"} {
		t.Run(endpoint, func(t *testing.T) {
			db, mock := newSQLMock(t)
			repo := &organizationRepository{db: db}
			effectiveAt := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
			expectOrganizationReadContext(mock, 99, effectiveAt)
			// Simulate the middleware lookup, then pass its snapshot downstream.
			org, err := repo.GetContextForUser(context.Background(), 99)
			require.NoError(t, err)
			ctx := service.WithOrganizationReadContext(context.Background(), 99, org)
			switch endpoint {
			case "charts":
				expectOrganizationChartQueries(mock, effectiveAt)
				_, err = repo.UsageCharts(ctx, 99, service.OrganizationUsageFilter{})
			case "users-trend":
				mock.ExpectQuery("WITH top_users AS").WithArgs(int64(7), effectiveAt, 12).
					WillReturnRows(sqlmock.NewRows([]string{"date"}))
				_, err = repo.OrganizationUsersTrend(ctx, 99, service.OrganizationUsageFilter{}, 12)
			case "spending-ranking":
				mock.ExpectQuery("WITH user_spend AS").WithArgs(int64(7), effectiveAt, 12).
					WillReturnRows(sqlmock.NewRows([]string{"user_id"}))
				_, err = repo.OrganizationSpendingRanking(ctx, 99, service.OrganizationUsageFilter{}, 12)
			}
			require.NoError(t, err)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func expectOrganizationChartQueries(mock sqlmock.Sqlmock, effectiveAt time.Time) {
	for _, query := range []string{
		"SELECT TO_CHAR",
		"SELECT COALESCE\\(l.requested_model",
		"SELECT COALESCE\\(l.group_id",
		"SELECT COALESCE\\(NULLIF\\(l.inbound_endpoint",
	} {
		mock.ExpectQuery(query).WithArgs(int64(7), effectiveAt).
			WillReturnRows(sqlmock.NewRows([]string{"bucket"}))
	}
}

func TestOrganizationChartsStandaloneLoadsContextOnce(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &organizationRepository{db: db}
	effectiveAt := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	expectOrganizationReadContext(mock, 99, effectiveAt)
	expectOrganizationChartQueries(mock, effectiveAt)
	_, err := repo.UsageCharts(context.Background(), 99, service.OrganizationUsageFilter{})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestOrganizationUsageScopeReadContextIsolation(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &organizationRepository{db: db}
	effectiveAt := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	ctx := service.WithOrganizationReadContext(context.Background(), 99, &service.OrganizationContext{
		OrganizationID:     999,
		OrganizationStatus: service.OrganizationStatusActive,
		MembershipStatus:   service.MembershipStatusActive,
		Role:               service.OrganizationRoleOwner,
	})
	// A different user and a new request must each reload from the database.
	for _, request := range []struct {
		ctx    context.Context
		userID int64
	}{{ctx, 42}, {context.Background(), 99}} {
		expectOrganizationReadContext(mock, request.userID, effectiveAt)
		_, args, err := repo.organizationUsageScope(request.ctx, request.userID, service.OrganizationUsageFilter{})
		require.NoError(t, err)
		require.Equal(t, []any{int64(7), effectiveAt}, args)
	}
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestOrganizationUsageScopeReadContextStillChecksPermissions(t *testing.T) {
	for _, test := range []struct {
		name       string
		membership string
		actions    []string
		allowed    bool
	}{
		{"no_finance_permission", service.MembershipStatusActive, nil, false},
		{"inactive_member", "archived", []string{service.ActionFinanceBalanceRead}, false},
		{"finance_reader", service.MembershipStatusActive, []string{service.ActionFinanceBalanceRead}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, mock := newSQLMock(t)
			repo := &organizationRepository{db: db}
			ctx := service.WithOrganizationReadContext(context.Background(), 99, &service.OrganizationContext{
				OrganizationStatus: service.OrganizationStatusActive,
				MembershipStatus:   test.membership,
				Role:               "member",
				Actions:            test.actions,
			})
			_, _, err := repo.organizationUsageScope(ctx, 99, service.OrganizationUsageFilter{})
			if test.allowed {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, service.ErrOrganizationPermission)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
