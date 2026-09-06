//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestBytedanceDurableTransactions(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	for _, count := range []int{0, 8, 16, 17} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			user := mustCreateUser(t, client, &service.User{Balance: 10})
			account := mustCreateAccount(t, client, &service.Account{Name: "seedream", Platform: service.PlatformBytedance, Type: service.AccountTypeAPIKey})
			key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: uuid.NewString()})
			t.Cleanup(func() {
				for _, statement := range []string{`DELETE FROM usage_logs WHERE user_id=$1`, `DELETE FROM async_media_tasks WHERE user_id=$1`, `DELETE FROM api_keys WHERE user_id=$1`, `DELETE FROM users WHERE id=$1`} {
					_, err := integrationDB.ExecContext(ctx, statement, user.ID)
					require.NoError(t, err)
				}
				_, err := integrationDB.ExecContext(ctx, `DELETE FROM accounts WHERE id=$1`, account.ID)
				require.NoError(t, err)
			})
			repo := &asyncMediaTaskRepository{sql: integrationDB, db: integrationDB}
			deadline := time.Now().Add(time.Hour)
			task := &service.AsyncMediaTask{InternalRequestID: uuid.NewString(), APIKeyID: key.ID, UserID: user.ID, AccountID: &account.ID, Facade: service.AsyncMediaFacadeFal, RequestedModel: domain.SeedreamModel, Status: service.AsyncMediaStatusPending, HeldCost: 1.6, RateMultiplier: 1, NumImages: 16, FailDeadlineAt: &deadline, RequestParameters: map[string]any{"_provider": "bytedance"}}
			e := &service.BytedanceExecution{RequestPayload: map[string]any{"prompt": "layers", "layer_decomposition": true}, UnitPrice: 0.1, BillingType: service.BillingTypeBalance}
			require.NoError(t, repo.CreateBytedance(ctx, task, e))
			var balance float64
			require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance FROM users WHERE id=$1`, user.ID).Scan(&balance))
			require.InDelta(t, 8.4, balance, 1e-9)
			var claims atomic.Int32
			var wg sync.WaitGroup
			errs := make(chan error, 8)
			for i := 0; i < 8; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					ok, err := repo.ClaimBytedance(ctx, task.ID)
					if err != nil {
						errs <- err
					}
					if ok {
						claims.Add(1)
					}
				}()
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				require.NoError(t, err)
			}
			require.Equal(t, int32(1), claims.Load())
			_, err := repo.RefundBytedance(ctx, task.ID, "cancel", true)
			require.ErrorIs(t, err, service.ErrBytedanceAlreadyRunning)
			result := map[string]any{"data": []any{map[string]any{"url": "https://example.com/background.jpg", "z_index": 0, "size": "2048x2048"}, map[string]any{"url": "https://example.com/layer.png", "z_index": 1, "bounding_box": map[string]any{"normalized": []int{100, 200, 300, 400}}}}, "usage": map[string]any{"generated_images": count, "output_tokens": 23107}}
			require.NoError(t, repo.SaveBytedanceResult(ctx, task.ID, result))
			e, err = repo.GetBytedance(ctx, task.ID)
			require.NoError(t, err)
			require.Equal(t, 0.1, e.UnitPrice)
			task.ResultPayload = e.ResultPayload
			task.ImageURLs = []string{"https://example.com/background.jpg", "https://example.com/layer.png"}
			errs = make(chan error, 8)
			for i := 0; i < 8; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, err := repo.SettleBytedance(ctx, task, count, float64(count)*e.UnitPrice, "")
					if err != nil {
						errs <- err
					}
				}()
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				require.NoError(t, err)
			}
			require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance FROM users WHERE id=$1`, user.ID).Scan(&balance))
			require.InDelta(t, 10-float64(count)*0.1, balance, 1e-9)
			var logs int
			var actualCount int
			require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*),MAX(image_count) FROM usage_logs WHERE task_id=$1`, task.ID).Scan(&logs, &actualCount))
			require.Equal(t, 1, logs)
			require.Equal(t, count, actualCount)
			saved, err := repo.GetByID(ctx, task.ID)
			require.NoError(t, err)
			require.Equal(t, service.AsyncMediaStatusSucceeded, saved.Status)
			raw, err := json.Marshal(saved.ResultPayload)
			require.NoError(t, err)
			require.Contains(t, string(raw), "bounding_box")
			require.Contains(t, string(raw), "23107")
		})
	}
}

func TestBytedancePresetMigration(t *testing.T) {
	var params []byte
	var resultField string
	require.NoError(t, integrationDB.QueryRow(`SELECT default_params,result_field FROM model_intros WHERE model_key=$1`, domain.SeedreamModel).Scan(&params, &resultField))
	require.Equal(t, "data[*].url", resultField)
	var preset map[string]any
	require.NoError(t, json.Unmarshal(params, &preset))
	require.Equal(t, "image-annotations", preset["image"].(map[string]any)["widget"])
	require.Equal(t, 10.0, preset["image"].(map[string]any)["maxItems"])
}
