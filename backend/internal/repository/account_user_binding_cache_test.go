package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newAccountUserBindingTestCache(t *testing.T) (*accountUserBindingCache, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	mr.SetTime(time.Unix(1800000000, 0))
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	cache, ok := NewAccountUserBindingCache(rdb).(*accountUserBindingCache)
	require.True(t, ok)
	return cache, mr
}

func TestAccountUserBindingScanFiltersKeys(t *testing.T) {
	cache, _ := newAccountUserBindingTestCache(t)
	ctx := context.Background()
	want := make(map[int64]bool)
	for id := int64(1); id <= 25; id++ {
		want[id] = true
		require.NoError(t, cache.rdb.ZAdd(ctx, accountUserBoundKey(id), redis.Z{Score: 1, Member: "1"}).Err())
	}
	for _, key := range []string{
		"account_users:1:bound", "account_users:{0}:bound", "account_users:{-1}:bound",
		"account_users:{+1}:bound", "account_users:{01}:bound", "account_users:{}:bound",
		"account_users:{abc}:bound", "account_users:{9223372036854775808}:bound",
		"account_users:{1}:extra:bound", "account_users:{{1}}:bound",
		"account_users:{99}:evicted", "other:{99}:bound",
	} {
		require.NoError(t, cache.rdb.Set(ctx, key, "ignored", 0).Err())
	}
	got := make(map[int64]bool)
	var cursor uint64
	pages := 0
	for {
		ids, next, err := cache.ScanBoundAccountIDs(ctx, cursor, 2)
		require.NoError(t, err)
		for _, id := range ids {
			got[id] = true
		}
		pages++
		require.Less(t, pages, 100, "scan should finish")
		cursor = next
		if cursor == 0 {
			break
		}
	}
	require.Equal(t, want, got)
}

// miniredis ignores SCAN COUNT and returns all keys in one page, so this hook
// supplies multiple Redis responses to verify cursor forwarding and empty pages.
type accountBindingScanPageHook struct {
	t    *testing.T
	page int
}

func (h *accountBindingScanPageHook) DialHook(next redis.DialHook) redis.DialHook {
	return next
}

func (h *accountBindingScanPageHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

func (h *accountBindingScanPageHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		scan, ok := cmd.(*redis.ScanCmd)
		if !ok {
			return next(ctx, cmd)
		}
		require.Less(h.t, h.page, 3)
		cursors := []uint64{0, 17, 29}
		require.Equal(h.t, []any{"scan", cursors[h.page], "match", "account_users:*:bound", "count", int64(2)}, cmd.Args())
		keys := [][]string{{accountUserBoundKey(7)}, {"account_users:{bad}:bound"}, {accountUserBoundKey(9)}}
		nextCursors := []uint64{17, 29, 0}
		scan.SetVal(keys[h.page], nextCursors[h.page])
		h.page++
		return nil
	}
}

func TestAccountUserBindingScanPages(t *testing.T) {
	cache, _ := newAccountUserBindingTestCache(t)
	hook := &accountBindingScanPageHook{t: t}
	cache.rdb.AddHook(hook)
	ctx := context.Background()
	ids, cursor, err := cache.ScanBoundAccountIDs(ctx, 0, 2)
	require.NoError(t, err)
	require.Equal(t, []int64{7}, ids)
	require.Equal(t, uint64(17), cursor)
	ids, cursor, err = cache.ScanBoundAccountIDs(ctx, cursor, 2)
	require.NoError(t, err)
	require.Empty(t, ids)
	require.Equal(t, uint64(29), cursor)
	ids, cursor, err = cache.ScanBoundAccountIDs(ctx, cursor, 2)
	require.NoError(t, err)
	require.Equal(t, []int64{9}, ids)
	require.Zero(t, cursor)
	require.Equal(t, 3, hook.page)
}

func TestAccountUserBindingConditionalUnbind(t *testing.T) {
	ctx := context.Background()
	for _, cooldown := range []time.Duration{0, time.Minute} {
		t.Run(fmt.Sprint(cooldown), func(t *testing.T) {
			cache, _ := newAccountUserBindingTestCache(t)
			boundAt := time.Unix(1799999000, 456)
			require.NoError(t, cache.rdb.ZAdd(ctx, accountUserBoundKey(1), redis.Z{Score: float64(boundAt.Unix()), Member: "2"}).Err())
			removed, err := cache.UnbindUserIfBoundAt(ctx, 1, 2, boundAt, cooldown)
			require.NoError(t, err)
			require.True(t, removed)
			require.Zero(t, cache.rdb.ZCard(ctx, accountUserBoundKey(1)).Val())
			if cooldown > 0 {
				require.Equal(t, float64(1800000060), cache.rdb.ZScore(ctx, accountUserEvictedKey(1), "2").Val())
				require.Equal(t, 2*time.Minute, cache.rdb.TTL(ctx, accountUserEvictedKey(1)).Val())
			} else {
				require.Zero(t, cache.rdb.Exists(ctx, accountUserEvictedKey(1)).Val())
			}
			allowed, err := cache.TryBindUser(ctx, 1, 2, 1)
			require.NoError(t, err)
			require.Equal(t, cooldown == 0, allowed)
		})
	}
}

func TestAccountUserBindingConditionalUnbindNoMatchPreservesCooldown(t *testing.T) {
	ctx := context.Background()
	for _, bound := range []bool{false, true} {
		for _, existingCooldown := range []bool{false, true} {
			t.Run(fmt.Sprintf("bound=%t/cooldown=%t", bound, existingCooldown), func(t *testing.T) {
				cache, _ := newAccountUserBindingTestCache(t)
				if bound {
					require.NoError(t, cache.rdb.ZAdd(ctx, accountUserBoundKey(1), redis.Z{Score: 100, Member: "2"}).Err())
				}
				key := accountUserEvictedKey(1)
				if existingCooldown {
					require.NoError(t, cache.rdb.ZAdd(ctx, key, redis.Z{Score: 1800003600, Member: "2"}, redis.Z{Score: 1, Member: "3"}).Err())
					require.NoError(t, cache.rdb.Expire(ctx, key, time.Hour).Err())
				}
				before, err := cache.rdb.ZRangeWithScores(ctx, key, 0, -1).Result()
				require.NoError(t, err)
				ttl := cache.rdb.TTL(ctx, key).Val()
				removed, err := cache.UnbindUserIfBoundAt(ctx, 1, 2, time.Unix(99, 0), time.Minute)
				require.NoError(t, err)
				require.False(t, removed)
				after, err := cache.rdb.ZRangeWithScores(ctx, key, 0, -1).Result()
				require.NoError(t, err)
				require.Equal(t, before, after)
				require.Equal(t, ttl, cache.rdb.TTL(ctx, key).Val())
				if bound {
					require.Equal(t, float64(100), cache.rdb.ZScore(ctx, accountUserBoundKey(1), "2").Val())
				}
			})
		}
	}
}

func TestAccountUserBindingConditionalUnbindProtectsRebind(t *testing.T) {
	cache, mr := newAccountUserBindingTestCache(t)
	ctx := context.Background()
	allowed, err := cache.TryBindUser(ctx, 1, 2, 1)
	require.NoError(t, err)
	require.True(t, allowed)
	state, err := cache.GetBindingState(ctx, 1)
	require.NoError(t, err)
	require.Len(t, state.Bound, 1)
	oldBoundAt := state.Bound[0].BoundAt
	removed, err := cache.UnbindUser(ctx, 1, 2, time.Minute)
	require.NoError(t, err)
	require.True(t, removed)
	mr.SetTime(oldBoundAt.Add(2 * time.Minute))
	mr.FastForward(2 * time.Minute)
	allowed, err = cache.TryBindUser(ctx, 1, 2, 1)
	require.NoError(t, err)
	require.True(t, allowed)
	removed, err = cache.UnbindUserIfBoundAt(ctx, 1, 2, oldBoundAt, time.Minute)
	require.NoError(t, err)
	require.False(t, removed)
	require.Equal(t, float64(oldBoundAt.Add(2*time.Minute).Unix()), cache.rdb.ZScore(ctx, accountUserBoundKey(1), "2").Val())
	require.Zero(t, cache.rdb.ZCard(ctx, accountUserEvictedKey(1)).Val())
}

func TestAccountUserBindingConditionalUnbindConcurrent(t *testing.T) {
	cache, _ := newAccountUserBindingTestCache(t)
	ctx := context.Background()
	boundAt := time.Unix(1799999000, 0)
	require.NoError(t, cache.rdb.ZAdd(ctx, accountUserBoundKey(1), redis.Z{Score: float64(boundAt.Unix()), Member: "2"}).Err())
	type result struct {
		removed bool
		err     error
	}
	const workers = 16
	results := make(chan result, workers)
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		go func() {
			<-start
			removed, err := cache.UnbindUserIfBoundAt(ctx, 1, 2, boundAt, time.Minute)
			results <- result{removed: removed, err: err}
		}()
	}
	close(start)
	removedCount := 0
	for i := 0; i < workers; i++ {
		r := <-results
		require.NoError(t, r.err)
		if r.removed {
			removedCount++
		}
	}
	require.Equal(t, 1, removedCount)
	require.Equal(t, float64(1800000060), cache.rdb.ZScore(ctx, accountUserEvictedKey(1), "2").Val())
}

func TestAccountUserBindingManualUnbindMissingStillSetsCooldown(t *testing.T) {
	cache, _ := newAccountUserBindingTestCache(t)
	ctx := context.Background()
	removed, err := cache.UnbindUser(ctx, 1, 2, time.Minute)
	require.NoError(t, err)
	require.False(t, removed)
	require.Equal(t, float64(1800000060), cache.rdb.ZScore(ctx, accountUserEvictedKey(1), "2").Val())
}
