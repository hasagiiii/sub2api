package repository

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

// 账号用户名额缓存
//
// 设计说明：
//   - account_users:{accountID}:bound   ZSET，member=userID，score=首次占用时间（Unix 秒），不过期，
//     只能由管理员清理释放；多实例共享，保证所有节点看到同一份名额占用。
//   - account_users:{accountID}:evicted ZSET，member=userID，score=冷却结束时间（Unix 秒）。
//
// 两个 key 共享 {accountID} hash tag，保证 Lua 脚本在 Redis Cluster 下也落在同一 slot。
const accountUserBindingKeyPrefix = "account_users:"

var (
	// tryBindAccountUserScript 原子占用账号名额。
	// KEYS[1] = bound, KEYS[2] = evicted
	// ARGV[1] = maxUsers, ARGV[2] = userID
	// 返回: 1 = 允许（已占用或新占用）, 0 = 拒绝（冷却中或名额已满）
	tryBindAccountUserScript = redis.NewScript(`
		redis.replicate_commands()
		local bound = KEYS[1]
		local evicted = KEYS[2]
		local maxUsers = tonumber(ARGV[1])
		local member = ARGV[2]

		if redis.call('ZSCORE', bound, member) ~= false then
			return 1
		end

		local now = tonumber(redis.call('TIME')[1])
		redis.call('ZREMRANGEBYSCORE', evicted, '-inf', now)
		if redis.call('ZSCORE', evicted, member) ~= false then
			return 0
		end

		if maxUsers > 0 and redis.call('ZCARD', bound) >= maxUsers then
			return 0
		end

		redis.call('ZADD', bound, now, member)
		return 1
	`)

	// unbindAccountUserScript 释放名额并写入冷却。
	// KEYS[1] = bound, KEYS[2] = evicted
	// ARGV[1] = userID, ARGV[2] = cooldown（秒）
	// 返回: 1 = 该用户此前占用名额, 0 = 未占用
	unbindAccountUserScript = redis.NewScript(`
		redis.replicate_commands()
		local bound = KEYS[1]
		local evicted = KEYS[2]
		local member = ARGV[1]
		local cooldown = tonumber(ARGV[2])

		local removed = redis.call('ZREM', bound, member)
		if cooldown > 0 then
			local now = tonumber(redis.call('TIME')[1])
			redis.call('ZREMRANGEBYSCORE', evicted, '-inf', now)
			redis.call('ZADD', evicted, now + cooldown, member)
			redis.call('EXPIRE', evicted, cooldown + 60)
		end
		return removed
	`)
)

type accountUserBindingCache struct {
	rdb *redis.Client
}

// NewAccountUserBindingCache 创建账号用户名额缓存。
func NewAccountUserBindingCache(rdb *redis.Client) service.AccountUserBindingCache {
	return &accountUserBindingCache{rdb: rdb}
}

func accountUserBoundKey(accountID int64) string {
	return fmt.Sprintf("%s{%d}:bound", accountUserBindingKeyPrefix, accountID)
}

func accountUserEvictedKey(accountID int64) string {
	return fmt.Sprintf("%s{%d}:evicted", accountUserBindingKeyPrefix, accountID)
}

func (c *accountUserBindingCache) TryBindUser(ctx context.Context, accountID, userID int64, maxUsers int) (bool, error) {
	if c == nil || c.rdb == nil || userID <= 0 || maxUsers <= 0 {
		return true, nil
	}
	keys := []string{accountUserBoundKey(accountID), accountUserEvictedKey(accountID)}
	result, err := tryBindAccountUserScript.Run(ctx, c.rdb, keys, maxUsers, strconv.FormatInt(userID, 10)).Int()
	if err != nil {
		return true, err
	}
	return result == 1, nil
}

func (c *accountUserBindingCache) GetBindingState(ctx context.Context, accountID int64) (*service.AccountUserBindingState, error) {
	state := &service.AccountUserBindingState{Evicted: map[int64]time.Time{}}
	if c == nil || c.rdb == nil {
		return state, nil
	}
	pipe := c.rdb.Pipeline()
	boundCmd := pipe.ZRangeWithScores(ctx, accountUserBoundKey(accountID), 0, -1)
	evictedCmd := pipe.ZRangeByScoreWithScores(ctx, accountUserEvictedKey(accountID), &redis.ZRangeBy{
		Min: strconv.FormatInt(time.Now().Unix(), 10),
		Max: "+inf",
	})
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, err
	}
	for _, z := range boundCmd.Val() {
		id, ok := parseAccountUserMember(z.Member)
		if !ok {
			continue
		}
		state.Bound = append(state.Bound, service.AccountBoundUser{UserID: id, BoundAt: time.Unix(int64(z.Score), 0)})
	}
	for _, z := range evictedCmd.Val() {
		id, ok := parseAccountUserMember(z.Member)
		if !ok {
			continue
		}
		state.Evicted[id] = time.Unix(int64(z.Score), 0)
	}
	return state, nil
}

func (c *accountUserBindingCache) UnbindUser(ctx context.Context, accountID, userID int64, cooldown time.Duration) (bool, error) {
	if c == nil || c.rdb == nil || userID <= 0 {
		return false, nil
	}
	keys := []string{accountUserBoundKey(accountID), accountUserEvictedKey(accountID)}
	removed, err := unbindAccountUserScript.Run(ctx, c.rdb, keys, strconv.FormatInt(userID, 10), int64(cooldown/time.Second)).Int()
	if err != nil {
		return false, err
	}
	return removed > 0, nil
}

func parseAccountUserMember(member any) (int64, bool) {
	s, ok := member.(string)
	if !ok {
		return 0, false
	}
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}
