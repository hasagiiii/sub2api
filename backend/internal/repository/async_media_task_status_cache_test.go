package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newAsyncMediaTaskStatusTestCache(t *testing.T) (*asyncMediaTaskStatusCache, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	store, ok := NewAsyncMediaTaskStatusStore(rdb).(*asyncMediaTaskStatusCache)
	require.True(t, ok, "NewAsyncMediaTaskStatusStore should return *asyncMediaTaskStatusCache")
	return store, mr
}

func TestAsyncMediaTaskStatusCacheSetGetAndTTL(t *testing.T) {
	cache, mr := newAsyncMediaTaskStatusTestCache(t)
	now := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	status := &service.AsyncMediaTaskStatus{
		RequestID: "req-1",
		Status:    service.AsyncMediaStatusRunning,
		APIKeyID:  17,
		Upstream:  service.PlatformLeonardo,
		FinalCost: 1.25,
		CreatedAt: now,
		UpdatedAt: now,
	}

	require.NoError(t, cache.SetAsyncMediaTaskStatus(context.Background(), status, service.AsyncMediaTaskStatusProcessingTTL))
	require.Greater(t, mr.TTL(AsyncMediaTaskStatusKey("req-1")), service.AsyncMediaTaskStatusProcessingTTL-time.Second)
	require.LessOrEqual(t, mr.TTL(AsyncMediaTaskStatusKey("req-1")), service.AsyncMediaTaskStatusProcessingTTL)

	got, err := cache.GetAsyncMediaTaskStatus(context.Background(), " req-1 ")
	require.NoError(t, err)
	require.Equal(t, status.RequestID, got.RequestID)
	require.Equal(t, status.Status, got.Status)
	require.Equal(t, status.Upstream, got.Upstream)
	require.Equal(t, status.FinalCost, got.FinalCost)
	require.Equal(t, status.Version, got.Version)
}

func TestAsyncMediaTaskStatusCacheRejectsStaleWrite(t *testing.T) {
	cache, _ := newAsyncMediaTaskStatusTestCache(t)
	ctx := context.Background()

	newer := &service.AsyncMediaTaskStatus{
		RequestID: "req-versioned",
		Status:    service.AsyncMediaStatusSucceeded,
		Version:   200,
		COSURLs:   []string{"https://cdn.example/new.png"},
	}
	older := &service.AsyncMediaTaskStatus{
		RequestID: "req-versioned",
		Status:    service.AsyncMediaStatusRunning,
		Version:   100,
	}
	require.NoError(t, cache.SetAsyncMediaTaskStatus(ctx, newer, service.AsyncMediaTaskStatusTerminalTTL))
	require.NoError(t, cache.SetAsyncMediaTaskStatus(ctx, older, service.AsyncMediaTaskStatusProcessingTTL))

	got, err := cache.GetAsyncMediaTaskStatus(ctx, "req-versioned")
	require.NoError(t, err)
	require.Equal(t, int64(200), got.Version)
	require.Equal(t, service.AsyncMediaStatusSucceeded, got.Status)
	require.Equal(t, []string{"https://cdn.example/new.png"}, got.COSURLs)
}

func TestAsyncMediaTaskStatusCacheMissingAndInvalidJSON(t *testing.T) {
	cache, mr := newAsyncMediaTaskStatusTestCache(t)
	_, err := cache.GetAsyncMediaTaskStatus(context.Background(), "missing")
	require.ErrorIs(t, err, service.ErrAsyncMediaTaskStatusNotFound)

	require.NoError(t, mr.Set(AsyncMediaTaskStatusKey("bad"), "{"))
	_, err = cache.GetAsyncMediaTaskStatus(context.Background(), "bad")
	require.Error(t, err)
	require.False(t, errors.Is(err, service.ErrAsyncMediaTaskStatusNotFound))
}
