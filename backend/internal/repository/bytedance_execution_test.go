package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestBytedanceSettlementAdjustsDifferenceAtomically(t *testing.T) {
	for _, count := range []int{0, 8, 16, 17} {
		t.Run(string(rune('A'+count)), func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()
			repo := &asyncMediaTaskRepository{sql: db, db: db}
			held := 1.6
			cost := float64(count) * 0.1
			mock.ExpectBegin()
			mock.ExpectQuery("SELECT state,billing_type,unit_price").WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows([]string{"state", "billing_type", "unit_price"}).AddRow("result_ready", 0, 0.1))
			if count != 16 {
				mock.ExpectExec("UPDATE users SET balance").WithArgs(cost-held, int64(1)).WillReturnResult(sqlmock.NewResult(0, 1))
			}
			mock.ExpectExec("UPDATE async_media_tasks").WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec("UPDATE bytedance_image_executions").WithArgs(int64(7), "settled", count, nil).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec("INSERT INTO usage_logs").WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectCommit()
			updated, err := repo.SettleBytedance(context.Background(), &service.AsyncMediaTask{ID: 7, UserID: 1, HeldCost: held, RateMultiplier: 1, RequestParameters: map[string]any{}}, count, cost, "")
			require.NoError(t, err)
			require.True(t, updated)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestBytedanceSettlementInsufficientBalanceRollsBack(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	repo := &asyncMediaTaskRepository{sql: db, db: db}
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT state,billing_type,unit_price").WillReturnRows(sqlmock.NewRows([]string{"state", "billing_type", "unit_price"}).AddRow("result_ready", 0, 0.1))
	mock.ExpectExec("UPDATE users SET balance").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()
	_, err = repo.SettleBytedance(context.Background(), &service.AsyncMediaTask{ID: 1, UserID: 1, HeldCost: 1.6}, 17, 1.7, "")
	require.ErrorIs(t, err, service.ErrInsufficientBalance)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBytedanceTerminalSettlementDoesNotChargeTwice(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	repo := &asyncMediaTaskRepository{sql: db, db: db}
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT state,billing_type,unit_price").WillReturnRows(sqlmock.NewRows([]string{"state", "billing_type", "unit_price"}).AddRow("settled", 0, 0.1))
	mock.ExpectRollback()
	updated, err := repo.SettleBytedance(context.Background(), &service.AsyncMediaTask{ID: 1}, 17, 1.7, "")
	require.NoError(t, err)
	require.False(t, updated)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBytedanceCancellationCannotRefundAnExecutingRequest(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	repo := &asyncMediaTaskRepository{sql: db, db: db}
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT state,billing_type").WillReturnRows(sqlmock.NewRows([]string{"state", "billing_type"}).AddRow("running", 0))
	mock.ExpectRollback()
	_, err = repo.RefundBytedance(context.Background(), 1, "cancel", true)
	require.ErrorIs(t, err, service.ErrBytedanceAlreadyRunning)
	require.NoError(t, mock.ExpectationsWereMet())
}
