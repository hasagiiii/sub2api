package service

import (
	"context"
	"log/slog"
	"time"
)

const (
	accountUserCleanupAge             = 8 * time.Hour
	accountUserCleanupInterval        = time.Minute
	accountUserCleanupTimeout         = 30 * time.Second
	accountUserCleanupScanCount int64 = 100
)

// AccountUserBindingCleanupCache adds scanning and compare-and-delete to the
// binding cache. Comparing BoundAt protects a replacement binding from stale scans.
type AccountUserBindingCleanupCache interface {
	ScanBoundAccountIDs(ctx context.Context, cursor uint64, count int64) ([]int64, uint64, error)
	UnbindUserIfBoundAt(ctx context.Context, accountID, userID int64, boundAt time.Time, cooldown time.Duration) (bool, error)
}

// Start starts one background worker. It scans immediately and then every minute.
// Caches without cleanup support and services without activity data are skipped.
func (s *AccountUserLimitService) Start() {
	if s == nil || s.activity == nil {
		return
	}
	cache, ok := s.cache.(AccountUserBindingCleanupCache)
	if !ok {
		return
	}
	s.cleanupMu.Lock()
	defer s.cleanupMu.Unlock()
	if s.cleanupCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.cleanupCancel, s.cleanupDone = cancel, done
	go func() {
		defer close(done)
		ticker := time.NewTicker(accountUserCleanupInterval)
		defer ticker.Stop()
		s.cleanupBindings(ctx, cache)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.cleanupBindings(ctx, cache)
			}
		}
	}()
}

// Stop cancels pending cache/database work and waits for the worker to exit.
// Keeping the lifecycle lock while waiting prevents overlapping restart workers.
func (s *AccountUserLimitService) Stop() {
	if s == nil {
		return
	}
	s.cleanupMu.Lock()
	defer s.cleanupMu.Unlock()
	if s.cleanupCancel == nil {
		return
	}
	s.cleanupCancel()
	<-s.cleanupDone
	s.cleanupCancel, s.cleanupDone = nil, nil
}

func (s *AccountUserLimitService) cleanupBindings(ctx context.Context, cache AccountUserBindingCleanupCache) {
	var cursor uint64
	for ctx.Err() == nil {
		scanCtx, cancel := context.WithTimeout(ctx, accountUserCleanupTimeout)
		ids, next, err := cache.ScanBoundAccountIDs(scanCtx, cursor, accountUserCleanupScanCount)
		cancel()
		if err != nil {
			if ctx.Err() == nil {
				slog.Warn("account_user_limit.cleanup_scan_failed", "error", err)
			}
			return
		}
		for _, id := range ids {
			if ctx.Err() != nil {
				return
			}
			accountCtx, cancel := context.WithTimeout(ctx, accountUserCleanupTimeout)
			s.cleanupAccountBindings(accountCtx, cache, id)
			cancel()
		}
		if next == 0 {
			return
		}
		cursor = next
	}
}

func (s *AccountUserLimitService) cleanupAccountBindings(ctx context.Context, cache AccountUserBindingCleanupCache, accountID int64) {
	state, err := s.cache.GetBindingState(ctx, accountID)
	if err != nil {
		slog.Warn("account_user_limit.cleanup_state_failed", "account_id", accountID, "error", err)
		return
	}
	if state == nil || s.activity == nil {
		return
	}
	cutoff := s.now().Add(-accountUserCleanupAge)
	candidates := make([]AccountBoundUser, 0)
	ids := make([]int64, 0)
	for _, bound := range state.Bound {
		if bound.BoundAt.Before(cutoff) {
			candidates = append(candidates, bound)
			ids = append(ids, bound.UserID)
		}
	}
	if len(ids) == 0 {
		return
	}
	// The UI limit must not truncate cleanup candidates (including users with no logs).
	stats, err := s.activity.GetAccountUserActivityStats(ctx, accountID, ids, max(len(ids), accountScheduledUsersListLimit))
	if err != nil {
		slog.Warn("account_user_limit.cleanup_activity_failed", "account_id", accountID, "error", err)
		return
	}
	requests := make(map[int64]int64, len(stats))
	for _, stat := range stats {
		requests[stat.UserID] = stat.Requests
	}
	released := 0
	defer func() {
		if released > 0 {
			slog.Info("account_user_limit.cleanup_released", "account_id", accountID, "released", released)
		}
	}()
	for _, bound := range candidates {
		if ctx.Err() != nil {
			return
		}
		count, found := requests[bound.UserID]
		if !found || count < 0 || count >= 50 {
			continue
		}
		removed, err := cache.UnbindUserIfBoundAt(ctx, accountID, bound.UserID, bound.BoundAt, AccountUserEvictCooldown)
		if err != nil {
			slog.Warn("account_user_limit.cleanup_unbind_failed", "account_id", accountID, "user_id", bound.UserID, "error", err)
			continue
		}
		if removed {
			released++
			s.snapshots.Delete(accountID)
		}
	}
}
