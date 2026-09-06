//go:build unit

package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/bytedance"
	"github.com/stretchr/testify/require"
)

type seedreamTestRepo struct {
	*fakeTaskRepo
	lock           sync.Mutex
	execution      *BytedanceExecution
	count          int
	final          float64
	reason         string
	failSettlement bool
}

func (r *seedreamTestRepo) CreateBytedance(ctx context.Context, task *AsyncMediaTask, execution *BytedanceExecution) error {
	r.lock.Lock()
	defer r.lock.Unlock()
	if err := r.Create(ctx, task); err != nil {
		return err
	}
	cp := *execution
	cp.State = "pending"
	cp.TaskID = task.ID
	r.execution = &cp
	return nil
}
func (r *seedreamTestRepo) GetBytedance(context.Context, int64) (*BytedanceExecution, error) {
	r.lock.Lock()
	defer r.lock.Unlock()
	cp := *r.execution
	return &cp, nil
}
func (r *seedreamTestRepo) ClaimBytedance(context.Context, int64) (bool, error) {
	r.lock.Lock()
	defer r.lock.Unlock()
	if r.execution.State != "pending" {
		return false, nil
	}
	r.execution.State = "running"
	return true, nil
}
func (r *seedreamTestRepo) SaveBytedanceResult(_ context.Context, _ int64, result map[string]any) error {
	r.lock.Lock()
	defer r.lock.Unlock()
	r.execution.ResultPayload = result
	r.execution.State = "result_ready"
	return nil
}
func (r *seedreamTestRepo) SettleBytedance(_ context.Context, _ *AsyncMediaTask, count int, cost float64, reason string) (bool, error) {
	r.lock.Lock()
	defer r.lock.Unlock()
	if r.failSettlement && reason == "" {
		return false, ErrInsufficientBalance
	}
	r.count = count
	r.final = cost
	r.reason = reason
	r.execution.State = "settled"
	if reason != "" {
		r.execution.State = "billing_failed"
	}
	return true, nil
}
func (r *seedreamTestRepo) RefundBytedance(_ context.Context, _ int64, reason string, cancel bool) (bool, error) {
	r.lock.Lock()
	defer r.lock.Unlock()
	if cancel && r.execution.State != "pending" {
		return false, ErrBytedanceAlreadyRunning
	}
	r.execution.State = "refunded"
	r.reason = reason
	return true, nil
}

type seedreamTestClient func(context.Context, map[string]any) (map[string]any, error)

func (f seedreamTestClient) Generate(ctx context.Context, p map[string]any) (map[string]any, error) {
	return f(ctx, p)
}
func seedreamFixture(t *testing.T) (*AsyncMediaService, *seedreamTestRepo, *Account, *AsyncMediaSubmitInput) {
	t.Helper()
	repo := &seedreamTestRepo{fakeTaskRepo: newFakeTaskRepo()}
	resolver := newImageBillingResolver(t, 1, domain.SeedreamModel, 0.1)
	svc := NewAsyncMediaService(repo, nil, nil, newTestBillingService(), resolver, nil)
	svc.backgroundPolling = false
	account := &Account{ID: 10, Platform: PlatformBytedance, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "test"}}
	in := newSubmitInput(account, 1, 1)
	in.RequestedModel = domain.SeedreamModel
	in.RawRequestBody = []byte(`{"prompt":"separate layers","image":"https://example.com/reference.png","layer_decomposition":true}`)
	return svc, repo, account, in
}
func TestBytedanceSubmissionIsDurableBeforeBackgroundCall(t *testing.T) {
	svc, repo, account, in := seedreamFixture(t)
	started := make(chan struct{})
	release := make(chan struct{})
	svc.bytedanceClientFactory = func(*Account) (bytedanceImageClient, error) {
		return seedreamTestClient(func(ctx context.Context, p map[string]any) (map[string]any, error) {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return map[string]any{"data": []any{map[string]any{"url": "https://example.com/out.png", "z_index": 0.0}}, "usage": map[string]any{"generated_images": 0.0}}, nil
		}), nil
	}
	svc.backgroundPolling = true
	defer svc.StopBytedanceWorkers()
	task, err := svc.SubmitAsync(context.Background(), in)
	require.NoError(t, err)
	require.NotZero(t, task.ID)
	require.InDelta(t, 1.6, task.HeldCost, 1e-9)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	stored, err := repo.GetByID(context.Background(), task.ID)
	require.NoError(t, err)
	require.NotNil(t, stored)
	require.ErrorIs(t, svc.CancelTask(context.Background(), stored, account), ErrBytedanceAlreadyRunning)
	close(release)
}
func TestBytedanceRecoveryAndSettlement(t *testing.T) {
	for _, tt := range []struct {
		name, state string
		count       float64
		want        int
		fail        bool
	}{
		{"eight", "result_ready", 8, 8, false}, {"sixteen", "result_ready", 16, 16, false}, {"seventeen", "result_ready", 17, 17, false}, {"background", "result_ready", 0, 0, false}, {"unknown", "result_ready", -1, -1, false}, {"insufficient", "result_ready", 17, 17, true}, {"running_not_retried", "running", 8, 0, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, account, in := seedreamFixture(t)
			task, err := svc.SubmitAsync(context.Background(), in)
			require.NoError(t, err)
			repo.execution.State = tt.state
			repo.failSettlement = tt.fail
			repo.execution.ResultPayload = map[string]any{"data": []any{map[string]any{"url": "https://example.com/out.png", "z_index": 0.0, "bounding_box": map[string]any{"absolute": []any{1.0, 2.0, 3.0, 4.0}}}}, "usage": map[string]any{"generated_images": tt.count}}
			svc.bytedanceClientFactory = func(*Account) (bytedanceImageClient, error) {
				t.Error("recovery must not resubmit")
				return nil, errors.New("unexpected call")
			}
			require.NoError(t, svc.runBytedance(context.Background(), task.ID, account))
			require.Equal(t, tt.want, repo.count)
			if tt.state == "result_ready" {
				if tt.fail || tt.want < 0 {
					require.Equal(t, "billing_failed", repo.execution.State)
					require.Equal(t, task.HeldCost, repo.final)
				} else {
					require.InDelta(t, float64(tt.want)*0.1, repo.final, 1e-9)
				}
			}
		})
	}
}
func TestBytedanceAtomicClaimAndInterruptedRecovery(t *testing.T) {
	svc, repo, account, in := seedreamFixture(t)
	task, err := svc.SubmitAsync(context.Background(), in)
	require.NoError(t, err)
	var calls atomic.Int32
	svc.bytedanceClientFactory = func(*Account) (bytedanceImageClient, error) {
		return seedreamTestClient(func(context.Context, map[string]any) (map[string]any, error) {
			calls.Add(1)
			return nil, context.Canceled
		}), nil
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = svc.runBytedance(context.Background(), task.ID, account) }()
	}
	wg.Wait()
	require.Equal(t, int32(1), calls.Load())
	repo.execution.State = "running"
	past := time.Now().Add(-time.Minute)
	repo.byID[task.ID].FailDeadlineAt = &past
	require.NoError(t, svc.runBytedance(context.Background(), task.ID, nil))
	require.Equal(t, "refunded", repo.execution.State)
	require.Equal(t, int32(1), calls.Load())
}
func TestBytedanceQueuedCancellationAndRequestOwnership(t *testing.T) {
	svc, repo, account, in := seedreamFixture(t)
	task, err := svc.SubmitAsync(context.Background(), in)
	require.NoError(t, err)
	in.APIKeyID++
	_, err = svc.SubmitAsync(context.Background(), in)
	require.ErrorContains(t, err, "conflict")
	require.NoError(t, svc.CancelTask(context.Background(), task, account))
	require.Equal(t, "refunded", repo.execution.State)
}

func TestBytedanceUnknownOutcomeWaitsForDeadline(t *testing.T) {
	svc, repo, account, in := seedreamFixture(t)
	task, err := svc.SubmitAsync(context.Background(), in)
	require.NoError(t, err)
	svc.bytedanceClientFactory = func(*Account) (bytedanceImageClient, error) {
		return seedreamTestClient(func(context.Context, map[string]any) (map[string]any, error) { return nil, bytedance.ErrOutcomeUnknown }), nil
	}
	require.ErrorIs(t, svc.runBytedance(context.Background(), task.ID, account), bytedance.ErrOutcomeUnknown)
	require.Equal(t, "running", repo.execution.State)
	require.NoError(t, svc.runBytedance(context.Background(), task.ID, account))
	require.Equal(t, "running", repo.execution.State)
}

func TestBytedanceUsesSubmissionPriceSnapshot(t *testing.T) {
	svc, repo, account, in := seedreamFixture(t)
	task, err := svc.SubmitAsync(context.Background(), in)
	require.NoError(t, err)
	svc.resolver = newImageBillingResolver(t, 1, domain.SeedreamModel, 100)
	repo.execution.State = "result_ready"
	repo.execution.ResultPayload = map[string]any{"data": []any{map[string]any{"url": "https://example.com/layer.png", "z_index": 1.0, "size": "30x40"}}, "usage": map[string]any{"generated_images": 8.0}}
	require.NoError(t, svc.runBytedance(context.Background(), task.ID, account))
	require.InDelta(t, 0.8, repo.final, 1e-9)
}

func TestBytedanceRequiresConfiguredPricing(t *testing.T) {
	svc, _, _, in := seedreamFixture(t)
	svc.resolver = nil
	_, err := svc.SubmitAsync(context.Background(), in)
	require.ErrorIs(t, err, ErrAsyncMediaPricingMissing)
}
