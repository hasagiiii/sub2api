//go:build unit

package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUsageBillingBatchCompanyBalanceAtomicity(t *testing.T) {
	for _, failSecond := range []bool{false, true} {
		name := "commit"
		if failSecond {
			name = "rollback"
		}
		t.Run(name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			repo := &usageBillingRepository{db: db}
			orgID := int64(9)
			cmds := []*service.UsageBillingCommand{
				{RequestID: "first", UserID: 1, APIKeyID: 2, OrganizationID: &orgID, PayerUserID: 3, BalanceSource: service.BalanceSourceCompany, BalanceCost: 2},
				{RequestID: "second", UserID: 1, APIKeyID: 2, OrganizationID: &orgID, PayerUserID: 3, BalanceSource: service.BalanceSourceCompany, BalanceCost: 4},
			}
			mock.ExpectBegin()
			mock.ExpectQuery("INSERT INTO usage_billing_dedup").WillReturnRows(sqlmock.NewRows([]string{"id", "request_id", "api_key_id"}).AddRow(10, "first", 2).AddRow(11, "second", 2))
			mock.ExpectQuery("FROM usage_billing_dedup_archive").WillReturnRows(sqlmock.NewRows([]string{"request_id", "api_key_id", "request_fingerprint"}))
			mock.ExpectQuery("UPDATE organizations").WithArgs(2.0, orgID).WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(8.0))
			second := mock.ExpectQuery("UPDATE organizations").WithArgs(4.0, orgID)
			if failSecond {
				second.WillReturnError(errors.New("billing failed"))
				mock.ExpectRollback()
			} else {
				second.WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(4.0))
				mock.ExpectCommit()
			}
			outcomes, err := repo.ApplyBatch(context.Background(), cmds)
			if failSecond {
				require.ErrorContains(t, err, "billing failed")
				require.Nil(t, outcomes)
			} else {
				require.NoError(t, err)
				require.Len(t, outcomes, 2)
				require.Equal(t, 8.0, *outcomes[0].Result.NewBalance)
				require.Equal(t, 4.0, *outcomes[1].Result.NewBalance)
				require.Equal(t, orgID, *outcomes[0].Result.OrganizationID)
				require.Equal(t, int64(3), outcomes[0].Result.PayerUserID)
				require.Equal(t, service.BalanceSourceCompany, outcomes[0].Result.BalanceSource)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
