//go:build integration

package repository

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/suite"
)

type AccountUserBindingCacheSuite struct {
	IntegrationRedisSuite
	cache service.AccountUserBindingCache
}

func (s *AccountUserBindingCacheSuite) SetupTest() {
	s.IntegrationRedisSuite.SetupTest()
	s.cache = NewAccountUserBindingCache(s.rdb)
}

func (s *AccountUserBindingCacheSuite) TestBindRespectsMaxUsersAndEvictFreesSlot() {
	const accountID = int64(9101)

	ok, err := s.cache.TryBindUser(s.ctx, accountID, 1, 2)
	s.RequireNoError(err)
	s.True(ok)
	ok, err = s.cache.TryBindUser(s.ctx, accountID, 2, 2)
	s.RequireNoError(err)
	s.True(ok)

	// 名额已满：新用户被拒，已占用用户继续放行
	ok, err = s.cache.TryBindUser(s.ctx, accountID, 3, 2)
	s.RequireNoError(err)
	s.False(ok)
	ok, err = s.cache.TryBindUser(s.ctx, accountID, 1, 2)
	s.RequireNoError(err)
	s.True(ok)

	state, err := s.cache.GetBindingState(s.ctx, accountID)
	s.RequireNoError(err)
	s.Len(state.Bound, 2)
	for _, u := range state.Bound {
		s.False(u.BoundAt.IsZero())
	}

	// 清理用户 2：名额 -1，新用户 3 可进入；被清理的用户 2 冷却期内不能抢回
	removed, err := s.cache.UnbindUser(s.ctx, accountID, 2, time.Hour)
	s.RequireNoError(err)
	s.True(removed)

	state, err = s.cache.GetBindingState(s.ctx, accountID)
	s.RequireNoError(err)
	s.Len(state.Bound, 1)
	until, evicted := state.Evicted[2]
	s.True(evicted)
	s.True(until.After(time.Now().Add(50 * time.Minute)))

	ok, err = s.cache.TryBindUser(s.ctx, accountID, 2, 2)
	s.RequireNoError(err)
	s.False(ok, "冷却中的用户不能重新占用名额")
	ok, err = s.cache.TryBindUser(s.ctx, accountID, 3, 2)
	s.RequireNoError(err)
	s.True(ok, "空出的名额应允许新用户进入")

	removed, err = s.cache.UnbindUser(s.ctx, accountID, 99, time.Hour)
	s.RequireNoError(err)
	s.False(removed, "未占用名额的用户清理返回 false")
}

func (s *AccountUserBindingCacheSuite) TestEvictWithoutCooldownAllowsRebind() {
	const accountID = int64(9102)

	ok, err := s.cache.TryBindUser(s.ctx, accountID, 7, 1)
	s.RequireNoError(err)
	s.True(ok)
	removed, err := s.cache.UnbindUser(s.ctx, accountID, 7, 0)
	s.RequireNoError(err)
	s.True(removed)
	ok, err = s.cache.TryBindUser(s.ctx, accountID, 7, 1)
	s.RequireNoError(err)
	s.True(ok)
}

func TestAccountUserBindingCacheSuite(t *testing.T) {
	suite.Run(t, new(AccountUserBindingCacheSuite))
}
