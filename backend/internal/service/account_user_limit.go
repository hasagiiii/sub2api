package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

// AccountMaxUsersExtraKey 账号允许调度的最大用户数（extra 键）。
// 配置后，只有已占用名额的用户或名额未满时的新用户才会被调度到该账号。
const AccountMaxUsersExtraKey = "max_users"

const (
	// accountMaxUsersUpperBound 防止误填过大的数值。
	accountMaxUsersUpperBound = 100000
	// AccountUserEvictCooldown 被管理员清理的用户在该时长内不能重新占用该账号的名额，
	// 避免仍在活跃的用户在下一次请求时立刻把刚空出的名额抢回去。
	AccountUserEvictCooldown = time.Minute
	// accountUserSnapshotTTL 调度预过滤使用的本地快照有效期；最终占位由 Redis 原子脚本裁决。
	accountUserSnapshotTTL = 2 * time.Second
	// accountScheduledUsersListLimit 用户调度管理弹窗最多展示的用户数。
	accountScheduledUsersListLimit = 200
)

// AccountBoundUser 已占用账号名额的用户。
type AccountBoundUser struct {
	UserID  int64
	BoundAt time.Time
}

// AccountUserBindingState 账号的用户名额占用状态。
type AccountUserBindingState struct {
	Bound []AccountBoundUser
	// Evicted 被清理且仍在冷却期内的用户 -> 冷却结束时间。
	Evicted map[int64]time.Time
}

// AccountUserBindingCache 账号用户名额的共享存储（多实例共享）。
type AccountUserBindingCache interface {
	// TryBindUser 原子地为用户占用账号名额：已占用返回 true；冷却中或名额已满返回 false。
	TryBindUser(ctx context.Context, accountID, userID int64, maxUsers int) (bool, error)
	// GetBindingState 读取账号当前的名额占用与冷却状态。
	GetBindingState(ctx context.Context, accountID int64) (*AccountUserBindingState, error)
	// UnbindUser 释放用户名额并进入冷却期；返回该用户此前是否占用名额。
	UnbindUser(ctx context.Context, accountID, userID int64, cooldown time.Duration) (bool, error)
}

// AccountScheduledUser 用户调度管理中的单个用户。
type AccountScheduledUser struct {
	UserID           int64      `json:"user_id"`
	Email            string     `json:"email"`
	Username         string     `json:"username"`
	Bound            bool       `json:"bound"`
	BoundAt          *time.Time `json:"bound_at,omitempty"`
	EvictedUntil     *time.Time `json:"evicted_until,omitempty"`
	Sessions         int64      `json:"sessions"`
	UpstreamSessions int64      `json:"upstream_sessions"`
	Requests         int64      `json:"requests"`
	LastActiveAt     *time.Time `json:"last_active_at"`
}

// AccountScheduledUsersResult 用户调度管理弹窗数据。
type AccountScheduledUsersResult struct {
	AccountID            int64                  `json:"account_id"`
	MaxUsers             int                    `json:"max_users"`
	BoundCount           int                    `json:"bound_count"`
	EvictCooldownSeconds int64                  `json:"evict_cooldown_seconds"`
	Users                []AccountScheduledUser `json:"users"`
}

// AccountUserActivityReader 读取用户在账号上的历史活动（来自 usage_logs）。
type AccountUserActivityReader interface {
	GetAccountUserActivityStats(ctx context.Context, accountID int64, includeUserIDs []int64, limit int) ([]usagestats.AccountUserActivityStat, error)
}

type accountUserSnapshot struct {
	bound     map[int64]struct{}
	evicted   map[int64]struct{}
	expiresAt time.Time
}

// AccountUserLimitService 负责账号最大用户数的调度判定与管理。
// 所有方法对 nil 接收者安全，缓存不可用时失败开放（不阻塞调度）。
type AccountUserLimitService struct {
	cache     AccountUserBindingCache
	activity  AccountUserActivityReader
	snapshots sync.Map // accountID -> *accountUserSnapshot
	now       func() time.Time
}

// NewAccountUserLimitService 创建账号用户名额服务；usageLogRepo 实现 AccountUserActivityReader 时提供对话统计。
func NewAccountUserLimitService(cache AccountUserBindingCache, usageLogRepo UsageLogRepository) *AccountUserLimitService {
	var activity AccountUserActivityReader
	if reader, ok := usageLogRepo.(AccountUserActivityReader); ok && reader != nil {
		activity = reader
	}
	return &AccountUserLimitService{cache: cache, activity: activity, now: time.Now}
}

func accountUserLimitUserID(ctx context.Context) int64 {
	if ctx == nil {
		return 0
	}
	if id, ok := ctx.Value(ctxkey.UserID).(int64); ok && id > 0 {
		return id
	}
	return 0
}

func (s *AccountUserLimitService) enabledFor(ctx context.Context, account *Account) (maxUsers int, userID int64, ok bool) {
	if s == nil || s.cache == nil || account == nil {
		return 0, 0, false
	}
	maxUsers = account.GetMaxUsers()
	if maxUsers <= 0 {
		return 0, 0, false
	}
	userID = accountUserLimitUserID(ctx)
	if userID <= 0 {
		return 0, 0, false
	}
	return maxUsers, userID, true
}

// Allows 调度候选预过滤（只读，使用短期本地快照）：已占用名额的用户放行；
// 冷却中的用户拒绝；否则名额未满时放行。
func (s *AccountUserLimitService) Allows(ctx context.Context, account *Account) bool {
	maxUsers, userID, ok := s.enabledFor(ctx, account)
	if !ok {
		return true
	}
	snap := s.loadSnapshot(ctx, account.ID)
	if snap == nil {
		return true
	}
	if _, bound := snap.bound[userID]; bound {
		return true
	}
	if _, evicted := snap.evicted[userID]; evicted {
		return false
	}
	return len(snap.bound) < maxUsers
}

// Acquire 在最终选定账号时原子占用名额；返回 false 表示该用户不能使用该账号。
func (s *AccountUserLimitService) Acquire(ctx context.Context, account *Account) bool {
	maxUsers, userID, ok := s.enabledFor(ctx, account)
	if !ok {
		return true
	}
	allowed, err := s.cache.TryBindUser(ctx, account.ID, userID, maxUsers)
	if err != nil {
		slog.Warn("account_user_limit.bind_failed", "account_id", account.ID, "user_id", userID, "error", err)
		return true
	}
	if allowed {
		s.rememberBound(account.ID, userID)
	} else {
		s.snapshots.Delete(account.ID)
	}
	return allowed
}

func (s *AccountUserLimitService) loadSnapshot(ctx context.Context, accountID int64) *accountUserSnapshot {
	now := s.now()
	if v, ok := s.snapshots.Load(accountID); ok {
		if snap, _ := v.(*accountUserSnapshot); snap != nil && now.Before(snap.expiresAt) {
			return snap
		}
	}
	state, err := s.cache.GetBindingState(ctx, accountID)
	if err != nil {
		slog.Debug("account_user_limit.snapshot_failed", "account_id", accountID, "error", err)
		return nil
	}
	snap := &accountUserSnapshot{
		bound:     make(map[int64]struct{}),
		evicted:   make(map[int64]struct{}),
		expiresAt: now.Add(accountUserSnapshotTTL),
	}
	if state != nil {
		for _, u := range state.Bound {
			snap.bound[u.UserID] = struct{}{}
		}
		for id, until := range state.Evicted {
			if now.Before(until) {
				snap.evicted[id] = struct{}{}
			}
		}
	}
	s.snapshots.Store(accountID, snap)
	return snap
}

func (s *AccountUserLimitService) rememberBound(accountID, userID int64) {
	v, ok := s.snapshots.Load(accountID)
	if !ok {
		return
	}
	snap, _ := v.(*accountUserSnapshot)
	if snap == nil {
		return
	}
	if _, bound := snap.bound[userID]; bound {
		return
	}
	next := &accountUserSnapshot{
		bound:     make(map[int64]struct{}, len(snap.bound)+1),
		evicted:   snap.evicted,
		expiresAt: snap.expiresAt,
	}
	for id := range snap.bound {
		next.bound[id] = struct{}{}
	}
	next.bound[userID] = struct{}{}
	s.snapshots.Store(accountID, next)
}

// ListScheduledUsers 返回进入过该账号的用户（名额占用者优先）及其对话统计。
func (s *AccountUserLimitService) ListScheduledUsers(ctx context.Context, account *Account) (*AccountScheduledUsersResult, error) {
	if account == nil {
		return nil, ErrAccountNotFound
	}
	result := &AccountScheduledUsersResult{
		AccountID:            account.ID,
		MaxUsers:             account.GetMaxUsers(),
		EvictCooldownSeconds: int64(AccountUserEvictCooldown / time.Second),
		Users:                []AccountScheduledUser{},
	}
	if s == nil {
		return result, nil
	}

	state := &AccountUserBindingState{}
	if s.cache != nil {
		loaded, err := s.cache.GetBindingState(ctx, account.ID)
		if err != nil {
			return nil, fmt.Errorf("get account user binding state: %w", err)
		}
		if loaded != nil {
			state = loaded
		}
	}
	now := s.now()
	boundAt := make(map[int64]time.Time, len(state.Bound))
	boundIDs := make([]int64, 0, len(state.Bound))
	for _, u := range state.Bound {
		boundAt[u.UserID] = u.BoundAt
		boundIDs = append(boundIDs, u.UserID)
	}
	result.BoundCount = len(boundIDs)

	var stats []usagestats.AccountUserActivityStat
	if s.activity != nil {
		loaded, err := s.activity.GetAccountUserActivityStats(ctx, account.ID, boundIDs, accountScheduledUsersListLimit)
		if err != nil {
			return nil, fmt.Errorf("get account user activity stats: %w", err)
		}
		stats = loaded
	}

	seen := make(map[int64]struct{}, len(stats))
	for _, st := range stats {
		seen[st.UserID] = struct{}{}
		result.Users = append(result.Users, s.buildScheduledUser(st, boundAt, state.Evicted, now))
	}
	// 活动统计不可用时，已占用名额的用户仍需展示以便清理。
	for _, id := range boundIDs {
		if _, ok := seen[id]; ok {
			continue
		}
		result.Users = append(result.Users, s.buildScheduledUser(usagestats.AccountUserActivityStat{UserID: id}, boundAt, state.Evicted, now))
	}
	sort.SliceStable(result.Users, func(i, j int) bool {
		return result.Users[i].Bound && !result.Users[j].Bound
	})
	return result, nil
}

func (s *AccountUserLimitService) buildScheduledUser(st usagestats.AccountUserActivityStat, boundAt map[int64]time.Time, evicted map[int64]time.Time, now time.Time) AccountScheduledUser {
	item := AccountScheduledUser{
		UserID:           st.UserID,
		Email:            st.Email,
		Username:         st.Username,
		Sessions:         st.Sessions,
		UpstreamSessions: st.UpstreamSessions,
		Requests:         st.Requests,
		LastActiveAt:     st.LastActiveAt,
	}
	if at, ok := boundAt[st.UserID]; ok {
		item.Bound = true
		t := at
		item.BoundAt = &t
	} else if until, ok := evicted[st.UserID]; ok && now.Before(until) {
		t := until
		item.EvictedUntil = &t
	}
	return item
}

// EvictUser 清理占用账号名额的用户：名额立即释放，该用户进入冷却期。
func (s *AccountUserLimitService) EvictUser(ctx context.Context, accountID, userID int64) (bool, error) {
	if s == nil || s.cache == nil {
		return false, infraerrors.ServiceUnavailable("ACCOUNT_USER_LIMIT_UNAVAILABLE", "account user limit store is unavailable")
	}
	if accountID <= 0 || userID <= 0 {
		return false, infraerrors.BadRequest("INVALID_ACCOUNT_USER", "invalid account or user id")
	}
	removed, err := s.cache.UnbindUser(ctx, accountID, userID, AccountUserEvictCooldown)
	if err != nil {
		return false, fmt.Errorf("evict account user: %w", err)
	}
	s.snapshots.Delete(accountID)
	return removed, nil
}

// accountUserLimitMaxReselect 名额被拒后重新选号的上限，防止异常选号函数导致死循环。
const accountUserLimitMaxReselect = 64

// withExcludedAccount 返回追加了 accountID 的排除集合副本（不修改调用方的集合）。
func withExcludedAccount(excluded map[int64]struct{}, accountID int64) map[int64]struct{} {
	next := make(map[int64]struct{}, len(excluded)+1)
	for id := range excluded {
		next[id] = struct{}{}
	}
	next[accountID] = struct{}{}
	return next
}

// selectAccountWithUserLimit 为返回单个账号的选号函数补充最大用户数占位：
// 名额被拒时排除该账号后重选，直到选到可用账号或无账号可选。
func (s *GatewayService) selectAccountWithUserLimit(ctx context.Context, excludedIDs map[int64]struct{}, selectFn func(map[int64]struct{}) (*Account, error)) (*Account, error) {
	return s.accountUserLimit.SelectWithAcquire(ctx, excludedIDs, selectFn)
}

// SelectWithAcquire 对返回单个账号的选号函数补充名额占位；名额被拒时排除该账号后重选。
func (s *AccountUserLimitService) SelectWithAcquire(ctx context.Context, excludedIDs map[int64]struct{}, selectFn func(map[int64]struct{}) (*Account, error)) (*Account, error) {
	excluded := excludedIDs
	for attempt := 0; attempt < accountUserLimitMaxReselect; attempt++ {
		account, err := selectFn(excluded)
		if err != nil || account == nil {
			return account, err
		}
		if s.Acquire(ctx, account) {
			return account, nil
		}
		excluded = withExcludedAccount(excluded, account.ID)
	}
	return nil, ErrNoAvailableAccounts
}

// ValidateAccountMaxUsersExtra 校验并规范化 extra.max_users（非负整数，0 表示不限制）。
func ValidateAccountMaxUsersExtra(extra map[string]any) error {
	raw, exists := extra[AccountMaxUsersExtraKey]
	if !exists || raw == nil {
		return nil
	}
	invalid := infraerrors.BadRequest("INVALID_MAX_USERS", fmt.Sprintf("max_users must be an integer between 0 and %d", accountMaxUsersUpperBound))
	var value float64
	switch v := raw.(type) {
	case int:
		value = float64(v)
	case int64:
		value = float64(v)
	case float64:
		value = v
	case json.Number:
		f, err := v.Float64()
		if err != nil {
			return invalid
		}
		value = f
	default:
		return invalid
	}
	if math.IsNaN(value) || math.IsInf(value, 0) || value != math.Trunc(value) || value < 0 || value > accountMaxUsersUpperBound {
		return invalid
	}
	extra[AccountMaxUsersExtraKey] = int(value)
	return nil
}
