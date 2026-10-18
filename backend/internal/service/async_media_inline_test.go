//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type inlineRepoFake struct {
	*fakeTaskRepo
	journalMu      sync.Mutex
	journals       map[int64]*InlineImageExecution
	finishFailures int
	finishes       int
}

func newInlineRepoFake() *inlineRepoFake {
	return &inlineRepoFake{fakeTaskRepo: newFakeTaskRepo(), journals: map[int64]*InlineImageExecution{}}
}
func cloneInlineJournal(e *InlineImageExecution) *InlineImageExecution {
	raw, _ := json.Marshal(e)
	var copy InlineImageExecution
	_ = json.Unmarshal(raw, &copy)
	return &copy
}
func (r *inlineRepoFake) CreateInlineImage(ctx context.Context, t *AsyncMediaTask, e *InlineImageExecution) error {
	r.journalMu.Lock()
	defer r.journalMu.Unlock()
	if err := r.Create(ctx, t); err != nil {
		return err
	}
	e.State = "pending"
	e.TaskID = t.ID
	r.journals[t.ID] = cloneInlineJournal(e)
	return nil
}
func (r *inlineRepoFake) GetInlineImage(_ context.Context, id int64) (*InlineImageExecution, error) {
	r.journalMu.Lock()
	defer r.journalMu.Unlock()
	e := r.journals[id]
	if e == nil {
		return nil, nil
	}
	return cloneInlineJournal(e), nil
}
func (r *inlineRepoFake) ClaimInlineImage(_ context.Context, id int64, _ string) (bool, error) {
	r.journalMu.Lock()
	defer r.journalMu.Unlock()
	e := r.journals[id]
	if e.State == "generating" || e.State == "settled" {
		return false, nil
	}
	if e.State == "pending" {
		e.State = "generating"
	}
	return true, nil
}
func (r *inlineRepoFake) RenewInlineImage(context.Context, int64, string) (bool, error) {
	return true, nil
}
func (r *inlineRepoFake) ReleaseInlineImage(context.Context, int64, string) error { return nil }
func (r *inlineRepoFake) SaveInlineImageResult(_ context.Context, id int64, _ string, result *AsyncMediaInlineResult, stored bool) error {
	r.journalMu.Lock()
	defer r.journalMu.Unlock()
	e := r.journals[id]
	e.Result = result
	e.State = "result_ready"
	if stored {
		e.State = "stored"
	}
	r.journals[id] = cloneInlineJournal(e)
	return nil
}
func (r *inlineRepoFake) SaveInlineImageFailure(_ context.Context, id int64, _, reason string, definite bool) error {
	r.journalMu.Lock()
	defer r.journalMu.Unlock()
	e := r.journals[id]
	e.State = "failure_ready"
	e.FailureReason = reason
	e.FailureDefinitive = definite
	return nil
}
func (r *inlineRepoFake) FinishInlineImage(ctx context.Context, t *AsyncMediaTask, in *TerminalUsageLogInput) (bool, error) {
	r.journalMu.Lock()
	defer r.journalMu.Unlock()
	if r.finishFailures > 0 {
		r.finishFailures--
		return false, errors.New("database temporarily unavailable")
	}
	if r.journals[t.ID].State == "settled" {
		return false, nil
	}
	r.finishes++
	r.journals[t.ID].State = "settled"
	return r.MarkSucceeded(ctx, t.ID, t.ImageURLs, t.CosURLs, t.ImageMetadata, t.ResultPayload, in.ActualCost)
}
func (r *inlineRepoFake) FailInlineImage(ctx context.Context, id int64, reason string, definite, _ bool) (bool, error) {
	r.journalMu.Lock()
	defer r.journalMu.Unlock()
	r.journals[id].State = "unknown"
	if definite {
		r.journals[id].State = "failed"
	}
	return r.MarkRefunded(ctx, id, AsyncMediaStatusFailed, reason)
}

func inlineServiceFixture(t *testing.T) (*AsyncMediaService, *inlineRepoFake, *AsyncMediaSubmitInput) {
	repo := newInlineRepoFake()
	groupID := int64(1)
	resolver := newImageBillingResolver(t, groupID, "openai/gpt-image-2", 0.1)
	s := NewAsyncMediaService(repo, nil, nil, newTestBillingService(), resolver, nil)
	account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true}
	in := &AsyncMediaSubmitInput{Account: account, AccountID: 1, APIKeyID: 2, UserID: 3, GroupID: &groupID, InternalRequestID: "client-stable", RequestedModel: "openai/gpt-image-2", RateMultiplier: 1}
	in.Input.N = 2
	in.Input.Size = "1024x1024"
	return s, repo, in
}
func inlineTestResult() *AsyncMediaInlineResult {
	return &AsyncMediaInlineResult{ImageURLs: []string{"data:image/png;base64,YQ==", "data:image/png;base64,Yg=="}, ResultPayload: map[string]any{"images": []any{map[string]any{"url": "data:image/png;base64,YQ=="}, map[string]any{"url": "data:image/png;base64,Yg=="}}}}
}

func TestInlineImageSubmitDetachesGenerationAndPreservesTwoResults(t *testing.T) {
	s, repo, in := inlineServiceFixture(t)
	started := make(chan struct{})
	finish := make(chan struct{})
	var calls atomic.Int32
	s.SetInlineImageGenerator(func(ctx context.Context, _ *AsyncMediaTask, _ *Account, _ *APIKey, payload map[string]any) (*AsyncMediaInlineResult, error) {
		calls.Add(1)
		close(started)
		select {
		case <-finish:
			return inlineTestResult(), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	s.SetBackgroundPollingEnabled(true)
	t.Cleanup(s.StopInlineImageWorkers)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	task, err := s.SubmitOpenAIImageTask(ctx, in, map[string]any{"prompt": "full prompt"})
	require.NoError(t, err)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	cancel()
	duplicate, err := s.SubmitOpenAIImageTask(context.Background(), in, map[string]any{"prompt": "full prompt"})
	require.NoError(t, err)
	require.Equal(t, task.ID, duplicate.ID)
	_, err = s.SubmitOpenAIImageTask(context.Background(), in, map[string]any{"prompt": "different"})
	require.ErrorIs(t, err, ErrInlineImageConflict)
	close(finish)
	require.Eventually(t, func() bool {
		stored, _ := repo.GetByID(context.Background(), task.ID)
		return stored.Status == AsyncMediaStatusSucceeded
	}, time.Second, 5*time.Millisecond)
	stored, _ := repo.GetByID(context.Background(), task.ID)
	require.Len(t, stored.ImageURLs, 2)
	require.Equal(t, int32(1), calls.Load())
}

func TestInlineImageUploadAndSettlementRetryNeverRegenerates(t *testing.T) {
	s, repo, in := inlineServiceFixture(t)
	var calls atomic.Int32
	s.SetInlineImageGenerator(func(context.Context, *AsyncMediaTask, *Account, *APIKey, map[string]any) (*AsyncMediaInlineResult, error) {
		calls.Add(1)
		return inlineTestResult(), nil
	})
	store := &fakeObjectStore{failUploads: 1}
	cos, _ := newCOSServiceForTest(t, store)
	enableCOS(t, cos)
	s.cos = cos
	task, err := s.SubmitOpenAIImageTask(context.Background(), in, map[string]any{"prompt": "draw"})
	require.NoError(t, err)
	require.Error(t, s.runInlineImage(context.Background(), task.ID, in.Account))
	e, err := repo.GetInlineImage(context.Background(), task.ID)
	require.NoError(t, err)
	require.Equal(t, "result_ready", e.State)
	repo.finishFailures = 1
	require.Error(t, s.runInlineImage(context.Background(), task.ID, in.Account))
	e, err = repo.GetInlineImage(context.Background(), task.ID)
	require.NoError(t, err)
	require.Equal(t, "stored", e.State)
	uploads := atomic.LoadInt32(&store.uploads)
	// Simulate recovery by a new service instance using only the persisted journal.
	recovered, _, _ := inlineServiceFixture(t)
	recovered.taskRepo = repo
	recovered.cos = cos
	recovered.inlineGenerator = s.inlineGenerator
	require.NoError(t, recovered.runInlineImage(context.Background(), task.ID, in.Account))
	require.Equal(t, int32(1), calls.Load())
	require.Equal(t, uploads, atomic.LoadInt32(&store.uploads))
	require.Equal(t, 1, repo.finishes)
	saved, _ := repo.GetByID(context.Background(), task.ID)
	require.Len(t, saved.CosURLs, 2)
	require.NotContains(t, saved.CosURLs[0], "base64")
}

type failedCOSSettings struct {
	SettingRepository
	err error
}

func (r failedCOSSettings) GetValue(context.Context, string) (string, error) { return "", r.err }
func TestCOSConfigReadFailureIsNotReportedAsDisabled(t *testing.T) {
	for _, err := range []error{context.Canceled, context.DeadlineExceeded, errors.New("database down")} {
		svc := NewCOSImageTransferService(failedCOSSettings{err: err}, noopEncryptor{}, nil)
		_, actual := svc.UploadImageBytes(context.Background(), []byte("image"), "image/png", "")
		require.ErrorIs(t, actual, err)
	}
	svc := NewCOSImageTransferService(failedCOSSettings{err: ErrSettingNotFound}, noopEncryptor{}, nil)
	url, err := svc.UploadImageBytes(context.Background(), []byte("image"), "image/png", "")
	require.NoError(t, err)
	require.Empty(t, url)
}

func TestQueuedImageStorageHasOneOwnerAndDirectImagesStillUpload(t *testing.T) {
	store := &fakeObjectStore{}
	cos, _ := newCOSServiceForTest(t, store)
	enableCOS(t, cos)
	gateway := &OpenAIGatewayService{cosService: cos}
	queued := &OpenAIForwardResult{ImageOutputBase64: []string{"YQ=="}}
	gateway.scheduleOpenAIImageCosUpload(context.WithValue(context.Background(), openAIImagesTaskStorageKey{}, true), queued)
	require.Nil(t, queued.imageCosUploadDone)
	require.Zero(t, atomic.LoadInt32(&store.uploads))
	direct := &OpenAIForwardResult{ImageOutputBase64: []string{"YQ=="}}
	gateway.scheduleOpenAIImageCosUpload(context.Background(), direct)
	select {
	case <-direct.imageCosUploadDone:
	case <-time.After(time.Second):
		t.Fatal("direct image upload did not finish")
	}
	require.Equal(t, int32(1), atomic.LoadInt32(&store.uploads))
	require.Len(t, direct.ImageOutputCosURLs, 1)
}

func (r *inlineRepoFake) GetInlineImageState(_ context.Context, id int64) (string, error) {
	r.journalMu.Lock()
	defer r.journalMu.Unlock()
	return r.journals[id].State, nil
}

func TestInlineImagePartialStorageKeepsEveryImage(t *testing.T) {
	result := inlineTestResult()
	result.COSURLs = []string{"https://img.test/first.png", ""}
	applyInlineImageStorage(result)
	require.Len(t, result.ImageURLs, 2)
	require.Equal(t, "https://img.test/first.png", result.ImageURLs[0])
	require.Contains(t, result.ImageURLs[1], "base64")
	require.Empty(t, result.COSURLs)
	images, ok := result.ResultPayload["images"].([]any)
	require.True(t, ok)
	first, ok := images[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "https://img.test/first.png", first["url"])
}
