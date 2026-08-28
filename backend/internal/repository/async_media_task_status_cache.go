package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const asyncMediaTaskStatusKeyPrefix = "async_media:status:"

const asyncMediaTaskStatusSetScript = `
local current_version = redis.call('HGET', KEYS[1], 'version')
if current_version then
  local incoming_version = tonumber(ARGV[2])
  if tonumber(current_version) >= incoming_version then
    return 0
  end
  local current_status = redis.call('HGET', KEYS[1], 'status')
  local current_terminal = current_status == 'succeeded' or current_status == 'refunded' or current_status == 'expired'
  local incoming_terminal = ARGV[3] == 'succeeded' or ARGV[3] == 'refunded' or ARGV[3] == 'expired'
  if current_terminal and not incoming_terminal then
    return 0
  end
end
redis.call('HSET', KEYS[1], 'payload', ARGV[1], 'version', ARGV[2], 'status', ARGV[3])
redis.call('EXPIRE', KEYS[1], ARGV[4])
return 1
`

type asyncMediaTaskStatusCache struct {
	rdb *redis.Client
}

func NewAsyncMediaTaskStatusStore(rdb *redis.Client) service.AsyncMediaTaskStatusStore {
	return &asyncMediaTaskStatusCache{rdb: rdb}
}

func AsyncMediaTaskStatusKey(requestID string) string {
	return asyncMediaTaskStatusKeyPrefix + strings.TrimSpace(requestID)
}

func (c *asyncMediaTaskStatusCache) GetAsyncMediaTaskStatus(ctx context.Context, requestID string) (*service.AsyncMediaTaskStatus, error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return nil, service.ErrAsyncMediaTaskStatusNotFound
	}
	payload, err := c.rdb.HGet(ctx, AsyncMediaTaskStatusKey(requestID), "payload").Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, service.ErrAsyncMediaTaskStatusNotFound
	}
	if err != nil {
		return nil, err
	}
	var status service.AsyncMediaTaskStatus
	if err := json.Unmarshal(payload, &status); err != nil {
		return nil, err
	}
	if strings.TrimSpace(status.RequestID) == "" {
		status.RequestID = requestID
	}
	return &status, nil
}

func (c *asyncMediaTaskStatusCache) SetAsyncMediaTaskStatus(ctx context.Context, status *service.AsyncMediaTaskStatus, ttl time.Duration) error {
	if status == nil || strings.TrimSpace(status.RequestID) == "" {
		return nil
	}
	version := status.Version
	if version <= 0 {
		version = status.UpdatedAt.UnixNano()
	}
	if version <= 0 {
		version = time.Now().UTC().UnixNano()
	}
	status.Version = version
	payload, err := json.Marshal(status)
	if err != nil {
		return err
	}
	ttlSeconds := int64(ttl / time.Second)
	if ttlSeconds <= 0 {
		ttlSeconds = 1
	}
	key := AsyncMediaTaskStatusKey(status.RequestID)
	result, err := c.rdb.Eval(ctx, asyncMediaTaskStatusSetScript, []string{key},
		payload,
		strconv.FormatInt(version, 10),
		status.Status,
		strconv.FormatInt(ttlSeconds, 10),
	).Int()
	if err != nil {
		return err
	}
	if result == 0 {
		return nil
	}
	logger.L().Info("async_media.status_cache_redis_set",
		zap.String("upstream", "redis"),
		zap.String("component", "repository.async_media_task_status_cache"),
		zap.String("request_id", status.RequestID),
		zap.String("key", key),
		zap.String("status", status.Status),
		zap.Duration("ttl", ttl),
		zap.ByteString("payload", payload),
	)
	return nil
}
