//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCostCenterCancellationAccounting(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	repo := NewCostCenterRepository(integrationDB)
	for _, scenario := range []string{"original", "reversal", "pending", "chain", "concurrent"} {
		t.Run(scenario, func(t *testing.T) {
			category := fmt.Sprintf("withdraw-%s-%d", scenario, time.Now().UnixNano())
			t.Cleanup(func() {
				_, err := integrationDB.ExecContext(context.Background(), `DELETE FROM cost_center_events WHERE category=$1`, category)
				require.NoError(t, err)
			})
			when := time.Now().UTC()
			insert := func(kind, status string, parent *int64) int64 {
				var id int64
				// Historical fixtures must use SQL: application creation rejects reversals.
				err := integrationDB.QueryRowContext(ctx, `INSERT INTO cost_center_events(event_type,status,source_type,category,amount_usd,occurred_at,reversal_of) VALUES($1,$2,'manual',$3,10,$4,$5) RETURNING id`, kind, status, category, when, parent).Scan(&id)
				require.NoError(t, err)
				return id
			}
			status := "settled"
			if scenario == "pending" {
				status = "pending"
			}
			original := insert("expense", status, nil)
			ids := []int64{original}
			var reversal int64
			if scenario != "pending" {
				reversal = insert("reversal", "settled", &original)
				ids = append(ids, reversal)
			}
			if scenario == "chain" {
				ids = append(ids, insert("reversal", "settled", &reversal))
			}
			filter := service.CostCenterReportFilter{Start: when.Add(-time.Hour), End: when.Add(time.Hour), Category: category}
			if scenario == "original" || scenario == "reversal" {
				before, err := repo.Summarize(ctx, filter)
				require.NoError(t, err)
				require.Zero(t, before.SettledExpenses)
			}
			if scenario == "concurrent" {
				start := make(chan struct{})
				results := make(chan error, 2)
				for _, id := range []int64{original, reversal} {
					go func(id int64) {
						<-start
						_, err := repo.UpdateEventStatus(ctx, id, "cancelled", "withdraw", nil)
						results <- err
					}(id)
				}
				close(start)
				require.NoError(t, <-results)
				require.NoError(t, <-results)
			} else {
				target := original
				if scenario == "reversal" {
					target = reversal
				}
				_, err := repo.UpdateEventStatus(ctx, target, "cancelled", "withdraw", nil)
				require.NoError(t, err)
			}
			after, err := repo.Summarize(ctx, filter)
			require.NoError(t, err)
			expected := 0.0
			if scenario == "reversal" {
				expected = 10
			}
			require.Equal(t, expected, after.SettledExpenses)
			require.Zero(t, after.PendingForecast)
			for _, id := range ids {
				var actual string
				require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT status FROM cost_center_events WHERE id=$1`, id).Scan(&actual))
				expectedStatus := "cancelled"
				if scenario == "reversal" && id == original {
					expectedStatus = "settled"
				}
				require.Equal(t, expectedStatus, actual)
			}
		})
	}
}
