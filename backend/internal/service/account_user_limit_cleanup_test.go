package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

type cleanupTestCache struct {
	*memoryAccountUserBindingCache
	scan      func(context.Context, uint64, int64) ([]int64, uint64, error)
	unbindErr error
	removed   int
	cooldown  time.Duration
}

func (c *cleanupTestCache) ScanBoundAccountIDs(ctx context.Context, cursor uint64, count int64) ([]int64, uint64, error) {
	if c.scan != nil {
		return c.scan(ctx, cursor, count)
	}
	return []int64{1}, 0, nil
}
func (c *cleanupTestCache) UnbindUserIfBoundAt(_ context.Context, accountID, userID int64, at time.Time, cooldown time.Duration) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.unbindErr != nil {
		return false, c.unbindErr
	}
	current, exists := c.bound[accountID][userID]
	if !exists || !current.Equal(at) {
		return false, nil
	}
	delete(c.bound[accountID], userID)
	c.removed++
	c.cooldown = cooldown
	return true, nil
}

type cleanupActivityFunc func(context.Context, int64, []int64, int) ([]usagestats.AccountUserActivityStat, error)

func (f cleanupActivityFunc) GetAccountUserActivityStats(ctx context.Context, id int64, ids []int64, limit int) ([]usagestats.AccountUserActivityStat, error) {
	return f(ctx, id, ids, limit)
}
func newCleanupTestService(now time.Time) (*AccountUserLimitService, *cleanupTestCache) {
	c := &cleanupTestCache{memoryAccountUserBindingCache: newMemoryAccountUserBindingCache()}
	s := NewAccountUserLimitService(c, nil)
	s.now = func() time.Time { return now }
	return s, c
}
func TestAccountUserCleanupBoundaries(t *testing.T) {
	now := time.Now()
	s, c := newCleanupTestService(now)
	old := now.Add(-8*time.Hour - time.Nanosecond)
	c.bound[1] = map[int64]time.Time{1: old, 2: old, 3: old, 4: old, 5: old, 6: now.Add(-8 * time.Hour), 7: now.Add(-7 * time.Hour)}
	s.snapshots.Store(int64(1), &accountUserSnapshot{})
	s.activity = cleanupActivityFunc(func(ctx context.Context, id int64, ids []int64, limit int) ([]usagestats.AccountUserActivityStat, error) {
		require.ElementsMatch(t, []int64{1, 2, 3, 4, 5}, ids)
		require.GreaterOrEqual(t, limit, len(ids))
		_, bounded := ctx.Deadline()
		require.True(t, bounded)
		return []usagestats.AccountUserActivityStat{{UserID: 1, Requests: 0}, {UserID: 2, Requests: 49}, {UserID: 3, Requests: 50}, {UserID: 4, Requests: -1}, {UserID: 6, Requests: 0}, {UserID: 7, Requests: 0}}, nil
	})
	s.cleanupBindings(context.Background(), c)
	require.Equal(t, 2, c.removed)
	require.Equal(t, time.Minute, c.cooldown)
	require.Len(t, c.bound[1], 5)
	for _, id := range []int64{3, 4, 5, 6, 7} {
		require.Contains(t, c.bound[1], id)
	}
	_, found := s.snapshots.Load(int64(1))
	require.False(t, found)
}
func TestAccountUserCleanupPaginationAndLargeCandidateSet(t *testing.T) {
	now := time.Now()
	s, c := newCleanupTestService(now)
	c.bound[2] = map[int64]time.Time{}
	for id := int64(1); id <= 251; id++ {
		c.bound[2][id] = now.Add(-9 * time.Hour)
	}
	var cursors []uint64
	c.scan = func(_ context.Context, cursor uint64, count int64) ([]int64, uint64, error) {
		require.Positive(t, count)
		cursors = append(cursors, cursor)
		switch cursor {
		case 0:
			return []int64{1}, 7, nil
		case 7:
			return nil, 13, nil
		default:
			return []int64{2}, 0, nil
		}
	}
	calls := 0
	s.activity = cleanupActivityFunc(func(_ context.Context, id int64, ids []int64, limit int) ([]usagestats.AccountUserActivityStat, error) {
		calls++
		require.Equal(t, int64(2), id)
		require.Len(t, ids, 251)
		require.GreaterOrEqual(t, limit, 251)
		stats := make([]usagestats.AccountUserActivityStat, len(ids))
		for i, id := range ids {
			stats[i] = usagestats.AccountUserActivityStat{UserID: id}
		}
		return stats, nil
	})
	s.cleanupBindings(context.Background(), c)
	require.Equal(t, []uint64{0, 7, 13}, cursors)
	require.Equal(t, 1, calls)
	require.Equal(t, 251, c.removed)
}
func TestAccountUserCleanupFailuresPreserveBindings(t *testing.T) {
	for _, mode := range []string{"scan", "state", "activity", "missing", "unbind", "no-candidates"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Now()
			s, c := newCleanupTestService(now)
			c.bound[1] = map[int64]time.Time{1: now.Add(-9 * time.Hour)}
			s.snapshots.Store(int64(1), &accountUserSnapshot{})
			calls := 0
			failure := errors.New("unavailable")
			s.activity = cleanupActivityFunc(func(context.Context, int64, []int64, int) ([]usagestats.AccountUserActivityStat, error) {
				calls++
				if mode == "activity" {
					return nil, failure
				}
				if mode == "missing" {
					return nil, nil
				}
				return []usagestats.AccountUserActivityStat{{UserID: 1}}, nil
			})
			switch mode {
			case "scan":
				c.scan = func(context.Context, uint64, int64) ([]int64, uint64, error) { return nil, 0, failure }
			case "state":
				c.err = failure
			case "unbind":
				c.unbindErr = failure
			case "no-candidates":
				c.bound[1][1] = now.Add(-8 * time.Hour)
			}
			s.cleanupBindings(context.Background(), c)
			require.Contains(t, c.bound[1], int64(1))
			require.Zero(t, c.removed)
			_, found := s.snapshots.Load(int64(1))
			require.True(t, found)
			if mode == "scan" || mode == "state" || mode == "no-candidates" {
				require.Zero(t, calls)
			}
		})
	}
}
func TestAccountUserCleanupConcurrentRebinding(t *testing.T) {
	now := time.Now()
	s, c := newCleanupTestService(now)
	c.bound[1] = map[int64]time.Time{1: now.Add(-9 * time.Hour)}
	queried, resume := make(chan struct{}), make(chan struct{})
	s.activity = cleanupActivityFunc(func(context.Context, int64, []int64, int) ([]usagestats.AccountUserActivityStat, error) {
		close(queried)
		<-resume
		return []usagestats.AccountUserActivityStat{{UserID: 1}}, nil
	})
	done := make(chan struct{})
	go func() { defer close(done); s.cleanupBindings(context.Background(), c) }()
	<-queried
	c.mu.Lock()
	c.bound[1][1] = now
	c.mu.Unlock()
	close(resume)
	<-done
	require.Equal(t, now, c.bound[1][1])
	require.Zero(t, c.removed)
}
func TestAccountUserCleanupMultipleInstancesCAS(t *testing.T) {
	now := time.Now()
	s, c := newCleanupTestService(now)
	c.bound[1] = map[int64]time.Time{1: now.Add(-9 * time.Hour)}
	s2 := NewAccountUserLimitService(c, nil)
	s2.now = s.now
	var queried sync.WaitGroup
	queried.Add(2)
	resume := make(chan struct{})
	reader := cleanupActivityFunc(func(context.Context, int64, []int64, int) ([]usagestats.AccountUserActivityStat, error) {
		queried.Done()
		<-resume
		return []usagestats.AccountUserActivityStat{{UserID: 1}}, nil
	})
	s.activity = reader
	s2.activity = reader
	var workers sync.WaitGroup
	workers.Add(2)
	for _, svc := range []*AccountUserLimitService{s, s2} {
		go func(svc *AccountUserLimitService) { defer workers.Done(); svc.cleanupBindings(context.Background(), c) }(svc)
	}
	queried.Wait()
	close(resume)
	workers.Wait()
	require.Equal(t, 1, c.removed)
}
func TestAccountUserCleanupStopCancelsActivity(t *testing.T) {
	s, c := newCleanupTestService(time.Now())
	c.bound[1] = map[int64]time.Time{1: time.Now().Add(-9 * time.Hour)}
	entered := make(chan struct{})
	s.activity = cleanupActivityFunc(func(ctx context.Context, _ int64, _ []int64, _ int) ([]usagestats.AccountUserActivityStat, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	s.Start()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("activity query did not start")
	}
	done := make(chan struct{})
	go func() { s.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop did not cancel activity")
	}
	require.Zero(t, c.removed)
	require.Contains(t, c.bound[1], int64(1))
}

func TestAccountUserCleanupLifecycleCancellation(t *testing.T) {
	s, c := newCleanupTestService(time.Now())
	s.activity = cleanupActivityFunc(func(context.Context, int64, []int64, int) ([]usagestats.AccountUserActivityStat, error) {
		return nil, nil
	})
	entered := make(chan struct{}, 2)
	c.scan = func(ctx context.Context, _ uint64, _ int64) ([]int64, uint64, error) {
		entered <- struct{}{}
		<-ctx.Done()
		return nil, 0, ctx.Err()
	}
	for i := 0; i < 2; i++ {
		s.Start()
		s.Start()
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("initial scan did not start")
		}
		stopped := make(chan struct{})
		go func() { s.Stop(); s.Stop(); close(stopped) }()
		select {
		case <-stopped:
		case <-time.After(time.Second):
			t.Fatal("Stop did not cancel scan")
		}
		require.Empty(t, entered, "idempotent Start must not create a second worker")
	}
	var nilService *AccountUserLimitService
	nilService.Start()
	nilService.Stop()
}
