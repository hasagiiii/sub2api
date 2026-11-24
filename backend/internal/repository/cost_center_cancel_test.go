package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func cancellationRow(kind, status, note string, operator, reversal any) *sqlmock.Rows {
	columns := []string{"id", "event_type", "status", "source_type", "source_id", "account_id", "user_id", "plan_id", "platform", "group_id", "model", "category", "amount_usd", "original_amount", "original_currency", "fx_rate", "occurred_at", "note", "metadata", "operator_id", "reversal_of", "created_at"}
	now := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	return sqlmock.NewRows(columns).AddRow(9, kind, status, "manual", nil, nil, nil, nil, "", nil, "", "proxy", 5.0, nil, nil, nil, now, note, []byte(`{}`), operator, reversal, now)
}

func TestCancelEventRetainsRecordAndAudit(t *testing.T) {
	for _, kind := range []string{"expense", "income", "reversal"} {
		for _, status := range []string{"pending", "settled"} {
			t.Run(kind+"/"+status, func(t *testing.T) {
				db, m, err := sqlmock.New()
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = db.Close() }()
				m.ExpectBegin()
				m.ExpectExec("SELECT pg_advisory_xact_lock").WillReturnResult(sqlmock.NewResult(0, 1))
				m.ExpectQuery("SELECT status,event_type .* FOR UPDATE").WithArgs(int64(9)).WillReturnRows(sqlmock.NewRows([]string{"status", "event_type"}).AddRow(status, kind))
				operator := int64(7)
				m.ExpectExec("WITH RECURSIVE linked").WithArgs("mistake", operator, int64(9)).WillReturnResult(sqlmock.NewResult(0, 1))
				var reversal any
				if kind == "reversal" {
					reversal = int64(8)
				}
				m.ExpectQuery("UPDATE cost_center_events SET status=[$]1,note=CASE.*operator_id=COALESCE.*RETURNING").WithArgs("cancelled", "mistake", operator, int64(9)).WillReturnRows(cancellationRow(kind, "cancelled", "original [mistake]", operator, reversal))
				m.ExpectCommit()
				e, err := NewCostCenterRepository(db).UpdateEventStatus(context.Background(), 9, "cancelled", "mistake", &operator)
				if err != nil {
					t.Fatal(err)
				}
				if e.ID != 9 || e.Status != "cancelled" || e.EventType != kind || e.Note != "original [mistake]" || e.OperatorID == nil || *e.OperatorID != operator || e.AmountUSD != 5 {
					t.Fatalf("lost record or audit: %+v", e)
				}
				if kind == "reversal" && (e.ReversalOf == nil || *e.ReversalOf != 8) {
					t.Fatalf("lost historical relationship: %+v", e)
				}
				if err = m.ExpectationsWereMet(); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestCancelEventRollsBackOnFailure(t *testing.T) {
	for _, stage := range []string{"begin", "serialization", "missing", "lock", "linked", "update", "commit"} {
		t.Run(stage, func(t *testing.T) {
			db, m, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()
			failure := errors.New("database failure")
			want := failure
			begin := m.ExpectBegin()
			if stage == "begin" {
				begin.WillReturnError(failure)
			} else {
				lock := m.ExpectExec("SELECT pg_advisory_xact_lock")
				if stage == "serialization" {
					lock.WillReturnError(failure)
				} else {
					lock.WillReturnResult(sqlmock.NewResult(0, 1))
					q := m.ExpectQuery("SELECT status,event_type .* FOR UPDATE").WithArgs(int64(9))
					switch stage {
					case "missing":
						q.WillReturnError(sql.ErrNoRows)
						want = service.ErrCostCenterEventNotFound
					case "lock":
						q.WillReturnError(failure)
					default:
						q.WillReturnRows(sqlmock.NewRows([]string{"status", "event_type"}).AddRow("settled", "expense"))
						linked := m.ExpectExec("WITH RECURSIVE linked").WithArgs("mistake", nil, int64(9))
						if stage == "linked" {
							linked.WillReturnError(failure)
						} else {
							linked.WillReturnResult(sqlmock.NewResult(0, 1))
							q = m.ExpectQuery("UPDATE cost_center_events")
							if stage == "update" {
								q.WillReturnError(failure)
							} else {
								q.WillReturnRows(cancellationRow("expense", "cancelled", "mistake", nil, nil))
							}
						}
					}
				}
				if stage == "commit" {
					m.ExpectCommit().WillReturnError(failure)
				} else {
					m.ExpectRollback()
				}
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
