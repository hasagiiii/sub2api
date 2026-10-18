//go:build integration

package repository

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func inlineExecutionFixture(t *testing.T) (*asyncMediaTaskRepository, *service.AsyncMediaTask, *service.InlineImageExecution) {
	t.Helper()
	client := testEntClient(t)
	user := mustCreateUser(t, client, &service.User{Email: uuid.NewString() + "@inline.test", Balance: 10})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-" + uuid.NewString()})
	account := mustCreateAccount(t, client, &service.Account{Name: "inline-" + uuid.NewString()})
	t.Cleanup(func() {
		ctx := context.Background()
		for _, statement := range []string{
			`DELETE FROM usage_logs WHERE user_id=$1`,
			`DELETE FROM async_media_tasks WHERE user_id=$1`,
			`DELETE FROM api_keys WHERE user_id=$1`,
			`DELETE FROM users WHERE id=$1`,
		} {
			_, err := integrationDB.ExecContext(ctx, statement, user.ID)
			require.NoError(t, err)
		}
		_, err := integrationDB.ExecContext(ctx, `DELETE FROM accounts WHERE id=$1`, account.ID)
		require.NoError(t, err)
	})
	id := uuid.NewString()
	deadline := time.Now().Add(time.Hour)
	task := &service.AsyncMediaTask{ExecutionKind: service.InlineImageExecutionOpenAI, InternalRequestID: id, UpstreamRequestID: &id, UserID: user.ID, APIKeyID: key.ID, AccountID: &account.ID, RequestedModel: "openai/gpt-image-2", UpstreamModel: asyncVideoStringPtr("gpt-image-2"), Status: service.AsyncMediaStatusPending, HeldCost: 2, RateMultiplier: 1, NumImages: 2, FailDeadlineAt: &deadline}
	e := &service.InlineImageExecution{RequestPayload: map[string]any{"prompt": "full request"}, RequestHash: "hash", BillingType: service.BillingTypeBalance}
	return &asyncMediaTaskRepository{sql: integrationDB, db: integrationDB}, task, e
}
func inlineBalance(t *testing.T, user int64) float64 {
	t.Helper()
	var value float64
	require.NoError(t, integrationDB.QueryRow(`SELECT balance FROM users WHERE id=$1`, user).Scan(&value))
	return value
}

func TestInlineImageAtomicReservationAndSettlement(t *testing.T) {
	ctx := context.Background()
	repo, task, e := inlineExecutionFixture(t)
	var successes atomic.Int32
	var wg sync.WaitGroup
	var id atomic.Int64
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			copyTask := *task
			copyE := *e
			err := repo.CreateInlineImage(ctx, &copyTask, &copyE)
			if err == nil {
				successes.Add(1)
				id.Store(copyTask.ID)
			} else {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.ErrorIs(t, err, service.ErrInlineImageConflict)
	}
	require.Equal(t, int32(1), successes.Load())
	require.InDelta(t, 8, inlineBalance(t, task.UserID), 1e-8)
	task.ID = id.Load()
	token := "owner"
	claimed, err := repo.ClaimInlineImage(ctx, task.ID, token)
	require.NoError(t, err)
	require.True(t, claimed)
	claimed, err = repo.ClaimInlineImage(ctx, task.ID, "other")
	require.NoError(t, err)
	require.False(t, claimed)
	_, err = repo.FailInlineImage(ctx, task.ID, "cancel", true, true)
	require.ErrorIs(t, err, service.ErrInlineImageRunning)
	result := &service.AsyncMediaInlineResult{ImageURLs: []string{"https://img.test/1", "https://img.test/2"}, ResultPayload: map[string]any{"images": []any{map[string]any{"url": "https://img.test/1"}, map[string]any{"url": "https://img.test/2"}}}}
	require.NoError(t, repo.SaveInlineImageResult(ctx, task.ID, token, result, true))
	task.ImageURLs = result.ImageURLs
	task.ResultPayload = result.ResultPayload
	usage := service.InlineImageTerminalUsage(task, service.BillingTypeBalance, 1, 1, service.BillingStatusCharged)
	// A failure writing usage must roll back both success state and balance adjustment.
	usage.BillingStatus = strings.Repeat("x", 1000)
	_, err = repo.FinishInlineImage(ctx, task, usage)
	require.Error(t, err)
	require.InDelta(t, 8, inlineBalance(t, task.UserID), 1e-8)
	unfinished, err := repo.GetByID(ctx, task.ID)
	require.NoError(t, err)
	require.Equal(t, service.AsyncMediaStatusRunning, unfinished.Status)
	usage.BillingStatus = service.BillingStatusCharged
	errs = make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := repo.FinishInlineImage(ctx, task, usage)
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
	require.InDelta(t, 9, inlineBalance(t, task.UserID), 1e-8)
	stored, err := repo.GetByID(ctx, task.ID)
	require.NoError(t, err)
	require.Equal(t, service.AsyncMediaStatusSucceeded, stored.Status)
	require.Len(t, stored.ImageURLs, 2)
	var count int
	require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*) FROM usage_logs WHERE task_id=$1`, task.ID).Scan(&count))
	require.Equal(t, 1, count)
}

func TestInlineImageCheckpointRecoveryAndUnknownOutcome(t *testing.T) {
	ctx := context.Background()
	t.Run("upload checkpoints survive and exclude concurrent owners", func(t *testing.T) {
		repo, task, e := inlineExecutionFixture(t)
		require.NoError(t, repo.CreateInlineImage(ctx, task, e))
		_, err := repo.ClaimInlineImage(ctx, task.ID, "a")
		require.NoError(t, err)
		result := &service.AsyncMediaInlineResult{ImageURLs: []string{"data:image/png;base64,YQ==", "data:image/png;base64,Yg=="}, COSURLs: []string{"https://img.test/1", ""}}
		require.NoError(t, repo.SaveInlineImageResult(ctx, task.ID, "a", result, false))
		require.NoError(t, repo.ReleaseInlineImage(ctx, task.ID, "a"))
		claimed, err := repo.ClaimInlineImage(ctx, task.ID, "b")
		require.NoError(t, err)
		require.True(t, claimed)
		recovered, err := repo.GetInlineImage(ctx, task.ID)
		require.NoError(t, err)
		require.Equal(t, "result_ready", recovered.State)
		require.Equal(t, result, recovered.Result)
		require.Error(t, repo.SaveInlineImageResult(ctx, task.ID, "a", result, true))
		failed, err := repo.FailInlineImage(ctx, task.ID, "deadline", true, false)
		require.NoError(t, err)
		require.False(t, failed)
	})
	t.Run("lost generation is never claimed again or automatically refunded", func(t *testing.T) {
		repo, task, e := inlineExecutionFixture(t)
		require.NoError(t, repo.CreateInlineImage(ctx, task, e))
		_, err := repo.ClaimInlineImage(ctx, task.ID, "a")
		require.NoError(t, err)
		require.NoError(t, repo.ReleaseInlineImage(ctx, task.ID, "a"))
		claimed, err := repo.ClaimInlineImage(ctx, task.ID, "b")
		require.NoError(t, err)
		require.False(t, claimed)
		changed, err := repo.FailInlineImage(ctx, task.ID, "unknown outcome", false, false)
		require.NoError(t, err)
		require.True(t, changed)
		require.InDelta(t, 8, inlineBalance(t, task.UserID), 1e-8)
		saved, err := repo.GetByID(ctx, task.ID)
		require.NoError(t, err)
		require.Equal(t, "generation_outcome_unknown", *saved.ErrorCode)
		require.True(t, saved.IsTerminal())
	})
	t.Run("known failure refunds only once", func(t *testing.T) {
		repo, task, e := inlineExecutionFixture(t)
		require.NoError(t, repo.CreateInlineImage(ctx, task, e))
		for range 2 {
			_, err := repo.FailInlineImage(ctx, task.ID, "cancelled by client", true, true)
			if err != nil {
				require.ErrorIs(t, err, service.ErrInlineImageRunning)
			}
		}
		require.InDelta(t, 10, inlineBalance(t, task.UserID), 1e-8)
	})
}
