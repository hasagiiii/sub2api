package repository

import (
	"context"
	"database/sql"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"testing"
	"time"
)

func cancellationRow(kind, status, note string, operator, reversal any) *sqlmock.Rows {
	columns := []string{"id", "event_type", "status", "source_type", "source_id", "account_id", "user_id", "plan_id", "platform", "group_id", "model", "category", "amount_usd", "original_amount", "original_currency", "fx_rate", "occurred_at", "note", "metadata", "operator_id", "reversal_of", "created_at"}
	now := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	return sqlmock.NewRows(columns).AddRow(9, kind, status, "manual", nil, nil, nil, nil, "", nil, "", "proxy", 5.0, nil, nil, nil, now, note, []byte(`{}`), operator, reversal, now)
}
func TestCancelExpenseRetainsRecordAndAudit(t *testing.T) {
	for _, status := range []string{"pending", "settled"} {
		t.Run(status, func(t *testing.T) {
			db, m, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()
			m.ExpectBegin()
			m.ExpectQuery("SELECT .* WHERE id=[$]1 FOR UPDATE").WithArgs(int64(9)).WillReturnRows(cancellationRow("expense", status, "original", nil, nil))
			m.ExpectQuery("SELECT EXISTS").WithArgs(int64(9)).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
			operator := int64(7)
			m.ExpectQuery("UPDATE cost_center_events SET status=[$]1,note=CASE.*operator_id=COALESCE.*RETURNING").WithArgs("cancelled", "mistake", operator, int64(9)).WillReturnRows(cancellationRow("expense", "cancelled", "original [mistake]", operator, nil))
			m.ExpectCommit()
			e, err := NewCostCenterRepository(db).UpdateEventStatus(context.Background(), 9, "cancelled", "mistake", &operator)
			if err != nil {
				t.Fatal(err)
			}
			if e.ID != 9 || e.Status != "cancelled" || e.Note != "original [mistake]" || e.OperatorID == nil || *e.OperatorID != operator {
				t.Fatalf("lost audit: %+v", e)
			}
			if err = m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestCostCenterMutationRejectsConflicts(t *testing.T) {
	for _, tc := range []struct {
		name, kind, status, target string
		reversed, reverse          bool
		reversal                   any
	}{
		{name: "cancelled cannot reopen", kind: "expense", status: "cancelled", target: "settled"},
		{name: "cancelled cannot pend", kind: "expense", status: "cancelled", target: "pending"},
		{name: "cancelled cannot cancel twice", kind: "expense", status: "cancelled", target: "cancelled"},
		{name: "income cannot cancel", kind: "income", status: "settled", target: "cancelled"},
		{name: "reversal cannot cancel", kind: "reversal", status: "settled", target: "cancelled", reversal: int64(8)},
		{name: "reversed cannot cancel", kind: "expense", status: "settled", target: "cancelled", reversed: true},
		{name: "cancelled cannot reverse", kind: "expense", status: "cancelled", reverse: true},
		{name: "pending cannot reverse", kind: "expense", status: "pending", reverse: true},
		{name: "reversal cannot reverse", kind: "reversal", status: "settled", reverse: true, reversal: int64(8)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, m, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()
			m.ExpectBegin()
			m.ExpectQuery("SELECT .* WHERE id=[$]1 FOR UPDATE").WithArgs(int64(9)).WillReturnRows(cancellationRow(tc.kind, tc.status, "", nil, tc.reversal))
			if tc.reversed {
				m.ExpectQuery("SELECT EXISTS").WithArgs(int64(9)).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
			}
			m.ExpectRollback()
			r := NewCostCenterRepository(db)
			if tc.reverse {
				_, err = r.ReverseEvent(context.Background(), 9, "mistake", nil)
			} else {
				_, err = r.UpdateEventStatus(context.Background(), 9, tc.target, "mistake", nil)
			}
			if !errors.Is(err, service.ErrCostCenterEventConflict) {
				t.Fatalf("expected conflict, got %v", err)
			}
			if err = m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestCancelExpenseRollsBackOnFailure(t *testing.T) {
	for _, stage := range []string{"missing", "lock", "reversal lookup", "update", "commit"} {
		t.Run(stage, func(t *testing.T) {
			db, m, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()
			failure := errors.New("database failure")
			want := failure
			m.ExpectBegin()
			q := m.ExpectQuery("SELECT .* FOR UPDATE").WithArgs(int64(9))
			switch stage {
			case "missing":
				q.WillReturnError(sql.ErrNoRows)
				want = service.ErrCostCenterEventNotFound
			case "lock":
				q.WillReturnError(failure)
			default:
				q.WillReturnRows(cancellationRow("expense", "settled", "", nil, nil))
				q = m.ExpectQuery("SELECT EXISTS").WithArgs(int64(9))
				if stage == "reversal lookup" {
					q.WillReturnError(failure)
				} else {
					q.WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
					q = m.ExpectQuery("UPDATE cost_center_events")
					if stage == "update" {
						q.WillReturnError(failure)
					} else {
						q.WillReturnRows(cancellationRow("expense", "cancelled", "mistake", nil, nil))
					}
				}
			}
			if stage == "commit" {
				m.ExpectCommit().WillReturnError(failure)
			} else {
				m.ExpectRollback()
			}
			_, err = NewCostCenterRepository(db).UpdateEventStatus(context.Background(), 9, "cancelled", "mistake", nil)
			if !errors.Is(err, want) {
				t.Fatalf("expected %v, got %v", want, err)
			}
			if err = m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestReverseEventKeepsIdempotencyKeyUnderLock(t *testing.T) {
	db, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	r := NewCostCenterRepository(db)
	when := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		m.ExpectBegin()
		m.ExpectQuery("SELECT .* WHERE id=[$]1 FOR UPDATE").WithArgs(int64(9)).WillReturnRows(cancellationRow("expense", "settled", "", nil, nil))
		m.ExpectQuery("INSERT INTO cost_center_events.*ON CONFLICT [(]idempotency_key[)].*DO UPDATE SET updated_at=NOW[(][)]").WithArgs("reversal", "settled", "reversal", "reversal:9", "reversal:9", nil, nil, nil, "", nil, "", "proxy", 5.0, nil, nil, nil, when, "mistake", sqlmock.AnyArg(), nil, int64(9)).WillReturnRows(cancellationRow("reversal", "settled", "mistake", nil, int64(9)))
		m.ExpectCommit()
		if _, err = r.ReverseEvent(context.Background(), 9, "mistake", nil); err != nil {
			t.Fatal(err)
		}
	}
	if err = m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
