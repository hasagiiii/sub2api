package repository

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestCostCenterWhereUsesInclusiveStartExclusiveEnd(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	accountID := int64(7)
	where, args := costCenterWhere(service.CostCenterReportFilter{Start: start, End: end, AccountID: &accountID, SourceType: "subscription"})
	if where != "cost_center_events.occurred_at >= $1 AND cost_center_events.occurred_at < $2 AND cost_center_events.source_type <> 'upstream' AND NOT EXISTS (SELECT 1 FROM cost_center_events upstream_event WHERE upstream_event.id=cost_center_events.reversal_of AND upstream_event.source_type='upstream') AND cost_center_events.account_id=$3 AND cost_center_events.source_type=$4" {
		t.Fatalf("unexpected report predicate: %s", where)
	}
	if len(args) != 4 || args[0] != start || args[1] != end || args[2] != accountID || args[3] != "subscription" {
		t.Fatalf("unexpected report args: %#v", args)
	}
}

func TestCreateEventPersistsIdempotentSettledEvent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	repo := NewCostCenterRepository(db)
	occurred := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	key := "expense-plan:11:2026-01-02"
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO cost_center_events")).
		WithArgs("expense", "pending", "recurring", "expense-plan:11:2026-01-02", &key, nil, nil, nil, "", nil, "", "proxy", 10.0, nil, nil, nil, occurred, "", sqlmock.AnyArg(), nil, nil).
		WillReturnRows(sqlmock.NewRows([]string{"id", "event_type", "status", "source_type", "source_id", "account_id", "user_id", "plan_id", "platform", "group_id", "model", "category", "amount_usd", "original_amount", "original_currency", "fx_rate", "occurred_at", "note", "metadata", "operator_id", "reversal_of", "created_at"}).AddRow(3, "expense", "pending", "recurring", "expense-plan:11:2026-01-02", nil, nil, nil, "", nil, "", "proxy", 10.0, nil, nil, nil, occurred, "", []byte(`{}`), nil, nil, occurred))
	_, err = repo.CreateEvent(context.Background(), &service.CreateCostCenterEventInput{EventType: "expense", Status: "pending", SourceType: "recurring", SourceID: &key, IdempotencyKey: &key, Category: "proxy", AmountUSD: 10, OccurredAt: &occurred})
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestListCostCenterEventsExcludesUpstreamAndHydratesNames(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	repo := NewCostCenterRepository(db)
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	accountID, userID, operatorID := int64(7), int64(8), int64(9)
	wherePattern := regexp.QuoteMeta("cost_center_events.source_type <> 'upstream'")
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM cost_center_events WHERE .*"+wherePattern).
		WithArgs(start, end).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	columns := []string{"id", "event_type", "status", "source_type", "source_id", "account_id", "user_id", "plan_id", "platform", "group_id", "model", "category", "amount_usd", "original_amount", "original_currency", "fx_rate", "occurred_at", "note", "metadata", "operator_id", "reversal_of", "created_at", "account_name", "user_name", "operator_name"}
	mock.ExpectQuery("SELECT cost_center_events.id.*LEFT JOIN accounts.*LEFT JOIN users u.*LEFT JOIN users op.*"+wherePattern).
		WithArgs(start, end, 50, 0).
		WillReturnRows(sqlmock.NewRows(columns).AddRow(1, "expense", "settled", "account", nil, accountID, userID, nil, "openai", nil, "gpt", "account_expense", 12.0, nil, nil, nil, start, "renewal", []byte(`{}`), operatorID, nil, start, "primary-account", "consumer@example.com", "admin@example.com"))

	events, total, err := repo.ListEvents(context.Background(), service.CostCenterReportFilter{Start: start, End: end}, 1, 50)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(events) != 1 {
		t.Fatalf("unexpected page: total=%d events=%+v", total, events)
	}
	if events[0].AccountName != "primary-account" || events[0].UserName != "consumer@example.com" || events[0].OperatorName != "admin@example.com" {
		t.Fatalf("names were not hydrated: %+v", events[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSummarizeIncludesSettledRebateAmountNetOfReversals(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	repo := NewCostCenterRepository(db)
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)

	mock.ExpectQuery("SELECT .*category='rebate'.*FROM cost_center_events WHERE").
		WithArgs(start, end).
		WillReturnRows(sqlmock.NewRows([]string{"cash", "realized", "promo", "expenses", "rebate", "pending", "unknown"}).
			AddRow(100.0, 80.0, 5.0, 30.0, 12.5, 4.0, 0.0))
	mock.ExpectQuery("SELECT .*FROM cost_center_subscription_entitlements WHERE").
		WithArgs(start, end).
		WillReturnRows(sqlmock.NewRows([]string{"deferred", "expired"}).AddRow(20.0, 3.0))

	summary, err := repo.Summarize(context.Background(), service.CostCenterReportFilter{Start: start, End: end})
	if err != nil {
		t.Fatal(err)
	}
	if summary.RebateAmount != 12.5 {
		t.Fatalf("unexpected rebate amount: %v", summary.RebateAmount)
	}
	if summary.CashProfit != 70 || summary.OperatingProfit != 50 {
		t.Fatalf("rebate breakdown must not be subtracted twice: %+v", summary)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCostCenterCancellationTransaction(t *testing.T) {
	for _, tc := range []struct {
		name, kind, current    string
		linked                 int64
		failLinked, failTarget bool
	}{
		{name: "settled original with reversal", kind: "expense", current: "settled", linked: 1},
		{name: "pending original", kind: "expense", current: "pending"},
		{name: "historical reversal", kind: "reversal", current: "settled"},
		{name: "repeat cancellation", kind: "expense", current: "cancelled"},
		{name: "linked cancellation failure", kind: "expense", current: "settled", failLinked: true},
		{name: "target failure rolls back linked cancellation", kind: "expense", current: "settled", linked: 1, failTarget: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()
			mock.ExpectBegin()
			mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock(200, 1)")).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectQuery(regexp.QuoteMeta("SELECT status,event_type FROM cost_center_events WHERE id=$1 FOR UPDATE")).WithArgs(int64(9)).WillReturnRows(sqlmock.NewRows([]string{"status", "event_type"}).AddRow(tc.current, tc.kind))
			linked := mock.ExpectExec(`WITH RECURSIVE linked.*WHERE reversal_of=\$3.*UNION.*JOIN linked.*UPDATE cost_center_events.*WHERE id IN .*status <> 'cancelled'`).WithArgs("withdraw", int64(42), int64(9))
			failure := errors.New("write failed")
			if tc.failLinked {
				linked.WillReturnError(failure)
			} else {
				linked.WillReturnResult(sqlmock.NewResult(0, tc.linked))
				target := mock.ExpectQuery("UPDATE cost_center_events SET status=\\$1.*WHERE id=\\$4 RETURNING").WithArgs("cancelled", "withdraw", int64(42), int64(9))
				if tc.failTarget {
					target.WillReturnError(failure)
				} else {
					now := time.Now()
					var original any
					if tc.kind == "reversal" {
						original = int64(8)
					}
					columns := []string{"id", "event_type", "status", "source_type", "source_id", "account_id", "user_id", "plan_id", "platform", "group_id", "model", "category", "amount_usd", "original_amount", "original_currency", "fx_rate", "occurred_at", "note", "metadata", "operator_id", "reversal_of", "created_at"}
					target.WillReturnRows(sqlmock.NewRows(columns).AddRow(9, tc.kind, "cancelled", "manual", nil, nil, nil, nil, "", nil, "", "proxy", 5.0, nil, nil, nil, now, "withdraw", []byte(`{}`), 42, original, now))
				}
			}
			if tc.failLinked || tc.failTarget {
				mock.ExpectRollback()
			} else {
				mock.ExpectCommit()
			}
			operator := int64(42)
			event, err := NewCostCenterRepository(db).UpdateEventStatus(context.Background(), 9, "cancelled", "withdraw", &operator)
			if tc.failLinked || tc.failTarget {
				if !errors.Is(err, failure) {
					t.Fatalf("expected write failure, got %v", err)
				}
			} else if err != nil || event.Status != "cancelled" {
				t.Fatalf("event=%+v err=%v", event, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCostCenterRejectsUnsafeStatusTransitions(t *testing.T) {
	for _, tc := range []struct{ current, kind, target string }{
		{"cancelled", "expense", "settled"}, {"cancelled", "reversal", "settled"},
		{"settled", "reversal", "pending"}, {"settled", "expense", "pending"},
	} {
		t.Run(tc.current+tc.kind+tc.target, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()
			mock.ExpectBegin()
			mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock(200, 1)")).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectQuery("SELECT status,event_type.*FOR UPDATE").WithArgs(int64(9)).WillReturnRows(sqlmock.NewRows([]string{"status", "event_type"}).AddRow(tc.current, tc.kind))
			mock.ExpectRollback()
			_, err = NewCostCenterRepository(db).UpdateEventStatus(context.Background(), 9, tc.target, "reason", nil)
			if !errors.Is(err, service.ErrInvalidCostCenterStatus) {
				t.Fatalf("unexpected error %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCostCenterRepositoryRejectsReversalCreation(t *testing.T) {
	original := int64(1)
	for _, in := range []service.CreateCostCenterEventInput{
		{EventType: "reversal"}, {EventType: "expense", SourceType: "reversal"}, {EventType: "expense", ReversalOf: &original},
	} {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		_, err = NewCostCenterRepository(db).CreateEvent(context.Background(), &in)
		if !errors.Is(err, service.ErrCostCenterReversalDisabled) {
			t.Fatalf("unexpected error %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
		_ = db.Close()
	}
}
