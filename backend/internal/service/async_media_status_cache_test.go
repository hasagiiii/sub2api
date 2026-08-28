//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type asyncMediaTaskStatusStoreStub struct {
	status *AsyncMediaTaskStatus
	err    error
	sets   int
}

func (s *asyncMediaTaskStatusStoreStub) GetAsyncMediaTaskStatus(context.Context, string) (*AsyncMediaTaskStatus, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.status, nil
}

func (s *asyncMediaTaskStatusStoreStub) SetAsyncMediaTaskStatus(_ context.Context, status *AsyncMediaTaskStatus, _ time.Duration) error {
	s.sets++
	s.status = status
	return s.err
}

func TestAsyncMediaGetTaskByUpstreamIDUsesStatusCacheBeforeDB(t *testing.T) {
	cache := &asyncMediaTaskStatusStoreStub{status: &AsyncMediaTaskStatus{
		RequestID: "req-1",
		Status:    AsyncMediaStatusSucceeded,
		APIKeyID:  17,
		Upstream:  PlatformLeonardo,
		COSURLs:   []string{"https://cdn.example/image.png"},
	}}
	svc := &AsyncMediaService{statusCache: cache}

	task, err := svc.GetTaskByUpstreamID(context.Background(), "req-1")
	require.NoError(t, err)
	require.True(t, task.IsStatusCacheHit())
	require.Equal(t, AsyncMediaStatusSucceeded, task.Status)
	require.Equal(t, PlatformLeonardo, task.StatusCacheUpstream())
	require.Equal(t, []string{"https://cdn.example/image.png"}, task.ResultURLs())
}

func TestAsyncMediaGetTaskByUpstreamIDFallsBackToDBOnCacheMiss(t *testing.T) {
	repo := newFakeTaskRepo()
	requestID := "req-2"
	task := &AsyncMediaTask{UpstreamRequestID: &requestID, Status: AsyncMediaStatusRunning}
	require.NoError(t, repo.Create(context.Background(), task))
	svc := &AsyncMediaService{
		taskRepo:    repo,
		statusCache: &asyncMediaTaskStatusStoreStub{err: ErrAsyncMediaTaskStatusNotFound},
	}

	got, err := svc.GetTaskByUpstreamID(context.Background(), requestID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.False(t, got.IsStatusCacheHit())
	require.Equal(t, AsyncMediaStatusRunning, got.Status)
}

func TestAsyncMediaGetTaskByUpstreamIDFallsBackToDBOnCacheError(t *testing.T) {
	repo := newFakeTaskRepo()
	requestID := "req-3"
	task := &AsyncMediaTask{UpstreamRequestID: &requestID, Status: AsyncMediaStatusRunning}
	require.NoError(t, repo.Create(context.Background(), task))
	svc := &AsyncMediaService{
		taskRepo:    repo,
		statusCache: &asyncMediaTaskStatusStoreStub{err: errors.New("redis down")},
	}

	got, err := svc.GetTaskByUpstreamID(context.Background(), requestID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.False(t, got.IsStatusCacheHit())
}

func TestAsyncMediaTaskStatusCacheTTLByStatus(t *testing.T) {
	require.Equal(t, AsyncMediaTaskStatusProcessingTTL, AsyncMediaTaskStatusCacheTTL(AsyncMediaStatusPending))
	require.Equal(t, AsyncMediaTaskStatusProcessingTTL, AsyncMediaTaskStatusCacheTTL(AsyncMediaStatusRunning))
	require.Equal(t, AsyncMediaTaskStatusTerminalTTL, AsyncMediaTaskStatusCacheTTL(AsyncMediaStatusSucceeded))
	require.Equal(t, AsyncMediaTaskStatusTerminalTTL, AsyncMediaTaskStatusCacheTTL(AsyncMediaStatusFailed))
	require.Equal(t, AsyncMediaTaskStatusTerminalTTL, AsyncMediaTaskStatusCacheTTL(AsyncMediaStatusRefunded))
	require.Equal(t, AsyncMediaTaskStatusTerminalTTL, AsyncMediaTaskStatusCacheTTL(AsyncMediaStatusExpired))
}
