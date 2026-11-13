package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCreateIAMMemberUsesOrganizationLimit(t *testing.T) {
	for _, tc := range []struct {
		name         string
		count, limit int
		allowed      bool
	}{
		{"reported_22_of_50", 22, 50, true},
		{"last_slot", 49, 50, true},
		{"full", 50, 50, false},
		{"over_limit", 51, 50, false},
		{"smaller_custom_limit", 5, 5, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newSQLMock(t)
			repo := &organizationRepository{db: db}
			mock.ExpectBegin()
			mock.ExpectQuery("SELECT o.id FROM organizations.*FOR UPDATE OF o").WithArgs(int64(1)).
				WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(7))
			mock.ExpectQuery("SELECT COALESCE.*o.member_limit FROM organizations").WithArgs(int64(7)).
				WillReturnRows(sqlmock.NewRows([]string{"company_id", "member_limit"}).AddRow("c123456789012345", tc.limit))
			mock.ExpectQuery("SELECT count.*organization_memberships.*status<>'archived'").WithArgs(int64(7)).
				WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(tc.count))
			if tc.allowed {
				mock.ExpectQuery("SELECT EXISTS").WithArgs(int64(7), "new-member").
					WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
				mock.ExpectExec("SAVEPOINT iam_account_id_retry").WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectQuery("INSERT INTO users").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(99))
				mock.ExpectExec("RELEASE SAVEPOINT iam_account_id_retry").WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectExec("INSERT INTO organization_memberships").WithArgs(int64(7), int64(99)).WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectExec("INSERT INTO organization_audit_events").WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectCommit()
			} else {
				mock.ExpectRollback()
			}
			member, err := repo.CreateIAMMember(context.Background(), 1, &service.User{LoginName: "new-member", PasswordHash: "hash"})
			if tc.allowed {
				require.NoError(t, err)
				require.Equal(t, int64(99), member.UserID)
			} else {
				require.ErrorIs(t, err, service.ErrIAMMemberLimit)
				require.Nil(t, member)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
