package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

type memoryAccountUserBindingCache struct {
	mu        sync.Mutex
	bound     map[int64]map[int64]time.Time
	evicted   map[int64]map[int64]time.Time
	err       error
	bindCalls int
	stateCall int
}

func newMemoryAccountUserBindingCache() *memoryAccountUserBindingCache {
	return &memoryAccountUserBindingCache{
		bound:   map[int64]map[int64]time.Time{},
		evicted: map[int64]map[int64]time.Time{},
	}
}

func (c *memoryAccountUserBindingCache) TryBindUser(_ context.Context, accountID, userID int64, maxUsers int) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bindCalls++
	if c.err != nil {
		return true, c.err
	}
	if _, ok := c.bound[accountID][userID]; ok {
		return true, nil
	}
	if until, ok := c.evicted[accountID][userID]; ok && time.Now().Before(until) {
		return false, nil
	}
	if len(c.bound[accountID]) >= maxUsers {
		return false, nil
	}
	if c.bound[accountID] == nil {
		c.bound[accountID] = map[int64]time.Time{}
	}
	c.bound[accountID][userID] = time.Now()
	return true, nil
}

func (c *memoryAccountUserBindingCache) GetBindingState(_ context.Context, accountID int64) (*AccountUserBindingState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stateCall++
	if c.err != nil {
		return nil, c.err
	}
	state := &AccountUserBindingState{Evicted: map[int64]time.Time{}}
	for id, at := range c.bound[accountID] {
		state.Bound = append(state.Bound, AccountBoundUser{UserID: id, BoundAt: at})
	}
	for id, until := range c.evicted[accountID] {
		state.Evicted[id] = until
	}
	return state, nil
}

func (c *memoryAccountUserBindingCache) UnbindUser(_ context.Context, accountID, userID int64, cooldown time.Duration) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.bound[accountID][userID]
	delete(c.bound[accountID], userID)
	if cooldown > 0 {
		if c.evicted[accountID] == nil {
			c.evicted[accountID] = map[int64]time.Time{}
		}
		c.evicted[accountID][userID] = time.Now().Add(cooldown)
	}
	return ok, nil
}

type accountUserActivityRepoStub struct {
	UsageLogRepository
	stats      []usagestats.AccountUserActivityStat
	includeIDs []int64
}

func (r *accountUserActivityRepoStub) GetAccountUserActivityStats(_ context.Context, _ int64, includeUserIDs []int64, _ int) ([]usagestats.AccountUserActivityStat, error) {
	r.includeIDs = append([]int64(nil), includeUserIDs...)
	return r.stats, nil
}

func limitedAccount(id int64, maxUsers int) *Account {
	return &Account{ID: id, Extra: map[string]any{AccountMaxUsersExtraKey: float64(maxUsers)}}
}

func TestAccountUserLimit_AcquireAndEvict(t *testing.T) {
	cache := newMemoryAccountUserBindingCache()
	svc := NewAccountUserLimitService(cache, nil)
	account := limitedAccount(1, 2)

	require.True(t, svc.Acquire(userCtx(11), account))
	require.True(t, svc.Acquire(userCtx(12), account))
	require.False(t, svc.Acquire(userCtx(13), account), "名额已满时新用户被拒")
	require.True(t, svc.Acquire(userCtx(11), account), "已占用名额的用户继续放行")
	require.False(t, svc.Allows(userCtx(13), account))
	require.True(t, svc.Allows(userCtx(12), account))

	removed, err := svc.EvictUser(context.Background(), 1, 12)
	require.NoError(t, err)
	require.True(t, removed)

	require.False(t, svc.Allows(userCtx(12), account), "被清理的用户冷却期内不能回到该账号")
	require.False(t, svc.Acquire(userCtx(12), account))
	require.True(t, svc.Allows(userCtx(13), account), "清理后名额 -1，新用户可进入")
	require.True(t, svc.Acquire(userCtx(13), account))
	require.False(t, svc.Acquire(userCtx(14), account))
}

func TestAccountUserLimit_DisabledOrNoUserBypassesCache(t *testing.T) {
	cache := newMemoryAccountUserBindingCache()
	svc := NewAccountUserLimitService(cache, nil)

	require.True(t, svc.Acquire(userCtx(1), &Account{ID: 1}))
	require.True(t, svc.Allows(userCtx(1), &Account{ID: 1, Extra: map[string]any{AccountMaxUsersExtraKey: 0}}))
	require.True(t, svc.Acquire(context.Background(), limitedAccount(1, 1)), "没有用户上下文（如后台测试）不占名额")
	require.Zero(t, cache.bindCalls)
	require.Zero(t, cache.stateCall)

	var nilSvc *AccountUserLimitService
	require.True(t, nilSvc.Acquire(userCtx(1), limitedAccount(1, 1)))
	require.True(t, nilSvc.Allows(userCtx(1), limitedAccount(1, 1)))
}

func TestAccountUserLimit_FailOpenOnCacheError(t *testing.T) {
	cache := newMemoryAccountUserBindingCache()
	cache.err = errors.New("redis down")
	svc := NewAccountUserLimitService(cache, nil)
	account := limitedAccount(1, 1)

	require.True(t, svc.Allows(userCtx(1), account))
	require.True(t, svc.Acquire(userCtx(1), account))
}

func TestAccountUserLimit_SnapshotCachesPrecheck(t *testing.T) {
	cache := newMemoryAccountUserBindingCache()
	svc := NewAccountUserLimitService(cache, nil)
	now := time.Now()
	svc.now = func() time.Time { return now }
	account := limitedAccount(1, 1)

	require.True(t, svc.Acquire(userCtx(5), account))
	for i := 0; i < 5; i++ {
		require.True(t, svc.Allows(userCtx(5), account))
		require.False(t, svc.Allows(userCtx(6), account))
	}
	require.Equal(t, 1, cache.stateCall, "快照有效期内只读一次共享存储")

	now = now.Add(accountUserSnapshotTTL + time.Millisecond)
	require.True(t, svc.Allows(userCtx(5), account))
	require.Equal(t, 2, cache.stateCall)
}

func TestAccountUserLimit_ListScheduledUsers(t *testing.T) {
	cache := newMemoryAccountUserBindingCache()
	lastActive := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	repo := &accountUserActivityRepoStub{stats: []usagestats.AccountUserActivityStat{
		{UserID: 21, Email: "a@test.com", Sessions: 3, Requests: 9, LastActiveAt: &lastActive},
		{UserID: 22, Email: "b@test.com", Sessions: 1, Requests: 1},
		{UserID: 23, Email: "c@test.com", Sessions: 2, UpstreamSessions: 7, Requests: 4},
	}}
	svc := NewAccountUserLimitService(cache, repo)
	account := limitedAccount(7, 3)

	require.True(t, svc.Acquire(userCtx(23), account))
	require.True(t, svc.Acquire(userCtx(24), account), "已占用但无请求记录的用户")
	require.True(t, svc.Acquire(userCtx(22), account))
	_, err := svc.EvictUser(context.Background(), 7, 22)
	require.NoError(t, err)

	result, err := svc.ListScheduledUsers(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, 3, result.MaxUsers)
	require.Equal(t, 2, result.BoundCount)
	require.Equal(t, int64(time.Minute/time.Second), result.EvictCooldownSeconds)
	require.ElementsMatch(t, []int64{23, 24}, repo.includeIDs)
	require.Len(t, result.Users, 4)

	byID := map[int64]AccountScheduledUser{}
	for i, u := range result.Users {
		byID[u.UserID] = u
		if i < 2 {
			require.True(t, u.Bound, "占用名额的用户排在最前")
		}
	}
	require.True(t, byID[23].Bound)
	require.NotNil(t, byID[23].BoundAt)
	require.Equal(t, int64(2), byID[23].Sessions)
	require.Equal(t, int64(7), byID[23].UpstreamSessions)
	require.True(t, byID[24].Bound)
	require.False(t, byID[21].Bound)
	require.Equal(t, int64(3), byID[21].Sessions)
	require.False(t, byID[22].Bound)
	require.NotNil(t, byID[22].EvictedUntil)
}

func TestAccountUserLimit_EvictValidatesInput(t *testing.T) {
	svc := NewAccountUserLimitService(newMemoryAccountUserBindingCache(), nil)
	_, err := svc.EvictUser(context.Background(), 0, 1)
	require.Error(t, err)
	_, err = svc.EvictUser(context.Background(), 1, 0)
	require.Error(t, err)

	var nilSvc *AccountUserLimitService
	_, err = nilSvc.EvictUser(context.Background(), 1, 1)
	require.Error(t, err)
}

func TestValidateAccountMaxUsersExtra(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  any
		want int
	}{
		{"float", float64(3), 3},
		{"int", 5, 5},
		{"json number", json.Number("7"), 7},
		{"zero disables", float64(0), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			extra := map[string]any{AccountMaxUsersExtraKey: tc.raw}
			require.NoError(t, ValidateAccountMaxUsersExtra(extra))
			require.Equal(t, tc.want, extra[AccountMaxUsersExtraKey])
		})
	}
	for _, raw := range []any{float64(-1), 1.5, "3", true, float64(accountMaxUsersUpperBound + 1)} {
		require.Error(t, ValidateAccountMaxUsersExtra(map[string]any{AccountMaxUsersExtraKey: raw}), "%v", raw)
	}
	require.NoError(t, ValidateAccountMaxUsersExtra(nil))
	require.NoError(t, ValidateAccountMaxUsersExtra(map[string]any{AccountMaxUsersExtraKey: nil}))
}

func TestGatewaySelectAccountWithUserLimit_ReselectsWhenFull(t *testing.T) {
	cache := newMemoryAccountUserBindingCache()
	limit := NewAccountUserLimitService(cache, nil)
	full := limitedAccount(1, 1)
	open := limitedAccount(2, 1)
	ctx := userCtx(100)
	require.True(t, limit.Acquire(userCtx(99), full))

	gateway := &GatewayService{}
	gateway.SetAccountUserLimitService(limit)

	var seenExcluded []map[int64]struct{}
	callerExcluded := map[int64]struct{}{}
	account, err := gateway.selectAccountWithUserLimit(ctx, callerExcluded, func(excluded map[int64]struct{}) (*Account, error) {
		seenExcluded = append(seenExcluded, excluded)
		if _, skip := excluded[full.ID]; !skip {
			return full, nil
		}
		if _, skip := excluded[open.ID]; !skip {
			return open, nil
		}
		return nil, ErrNoAvailableAccounts
	})
	require.NoError(t, err)
	require.Equal(t, open.ID, account.ID)
	require.Len(t, seenExcluded, 2)
	require.Empty(t, callerExcluded, "不修改调用方的排除集合")

	_, err = gateway.selectAccountWithUserLimit(userCtx(101), nil, func(excluded map[int64]struct{}) (*Account, error) {
		if _, skip := excluded[full.ID]; !skip {
			return full, nil
		}
		return nil, ErrNoAvailableAccounts
	})
	require.ErrorIs(t, err, ErrNoAvailableAccounts)
}

func TestGatewayCheckAndRegisterSession_EnforcesMaxUsersForAllPlatforms(t *testing.T) {
	limit := NewAccountUserLimitService(newMemoryAccountUserBindingCache(), nil)
	gateway := &GatewayService{}
	gateway.SetAccountUserLimitService(limit)
	account := limitedAccount(3, 1)
	account.Platform = PlatformOpenAI
	account.Type = AccountTypeAPIKey

	require.True(t, gateway.checkAndRegisterSession(userCtx(1), account, "s1"))
	require.False(t, gateway.checkAndRegisterSession(userCtx(2), account, "s2"))
	require.True(t, gateway.checkAndRegisterSession(userCtx(1), account, "s3"))
}

func TestOpenAISchedulerRequestCompatible_FiltersFullAccounts(t *testing.T) {
	limit := NewAccountUserLimitService(newMemoryAccountUserBindingCache(), nil)
	svc := &OpenAIGatewayService{}
	svc.SetAccountUserLimitService(limit)
	scheduler := &defaultOpenAIAccountScheduler{service: svc}
	account := limitedAccount(4, 1)
	account.Platform = PlatformOpenAI
	account.Type = AccountTypeAPIKey
	account.Status = StatusActive
	account.Schedulable = true
	require.True(t, limit.Acquire(userCtx(1), account))

	ok, reason := scheduler.isAccountRequestCompatibleReason(userCtx(2), account, OpenAIAccountScheduleRequest{})
	require.False(t, ok)
	require.Equal(t, "max_users_reached", reason)
	ok, _ = scheduler.isAccountRequestCompatibleReason(userCtx(1), account, OpenAIAccountScheduleRequest{})
	require.True(t, ok)
}

func TestAccountGetMaxUsers(t *testing.T) {
	require.Equal(t, 0, (&Account{}).GetMaxUsers())
	require.Equal(t, 0, (&Account{Extra: map[string]any{AccountMaxUsersExtraKey: float64(-3)}}).GetMaxUsers())
	require.Equal(t, 4, (&Account{Extra: map[string]any{AccountMaxUsersExtraKey: "4"}}).GetMaxUsers())
	var nilAccount *Account
	require.Equal(t, 0, nilAccount.GetMaxUsers())
}
