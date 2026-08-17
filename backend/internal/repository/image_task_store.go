package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const (
	imageTaskKeyPrefix         = "image_task:"
	imageTaskIdempotencyPrefix = "image_task:idempotency:"
	imageTaskReadyQueueKey     = "image_task:queue:ready"
	imageTaskActiveQueueKey    = "image_task:queue:active"
)

var imageTaskCreateOrGetScript = redis.NewScript(`
local mapping_raw = redis.call("GET", KEYS[2])
if mapping_raw then
  local mapping = cjson.decode(mapping_raw)
  if mapping.fingerprint ~= ARGV[2] then
    return {"conflict", ""}
  end
  local existing = redis.call("GET", ARGV[4] .. mapping.task_id)
  if existing then
    return {"existing", existing}
  end
  redis.call("DEL", KEYS[2])
end

redis.call("SET", KEYS[1], ARGV[1], "PX", ARGV[3])
redis.call("SET", KEYS[2], cjson.encode({task_id=ARGV[5], fingerprint=ARGV[2]}), "PX", ARGV[3])
redis.call("LPUSH", KEYS[3], ARGV[5])
return {"created", ARGV[1]}
`)

var imageTaskReserveScript = redis.NewScript(`
for i = 1, 100 do
  local task_id = redis.call("RPOP", KEYS[1])
  if not task_id then
    return nil
  end
  local task_key = ARGV[1] .. task_id
  local raw = redis.call("GET", task_key)
  if raw then
    local task = cjson.decode(raw)
    if task.status == "queued" then
      task.status = "running"
      task.execution_token = ARGV[2]
      task.updated_at = tonumber(ARGV[3])
      local encoded = cjson.encode(task)
      redis.call("SET", task_key, encoded, "KEEPTTL")
      redis.call("ZADD", KEYS[2], ARGV[3], task_id)
      return encoded
    end
  end
end
return nil
`)

var imageTaskHeartbeatScript = redis.NewScript(`
local raw = redis.call("GET", KEYS[1])
if not raw then
  return 0
end
local task = cjson.decode(raw)
if task.status ~= "running" or task.execution_token ~= ARGV[1] then
  return 0
end
task.updated_at = tonumber(ARGV[2])
redis.call("SET", KEYS[1], cjson.encode(task), "KEEPTTL")
redis.call("ZADD", KEYS[2], ARGV[2], ARGV[3])
return 1
`)

var imageTaskSaveClaimScript = redis.NewScript(`
local raw = redis.call("GET", KEYS[1])
if not raw then
  return 0
end
local current = cjson.decode(raw)
if current.status ~= "running" or current.execution_token ~= ARGV[2] then
  return 0
end
redis.call("SET", KEYS[1], ARGV[1], "PX", ARGV[3])
redis.call("ZREM", KEYS[2], ARGV[4])
redis.call("PEXPIRE", KEYS[3], ARGV[3])
return 1
`)

var imageTaskRecoverStaleScript = redis.NewScript(`
local jobs = redis.call("ZRANGEBYSCORE", KEYS[1], "-inf", ARGV[1], "LIMIT", 0, ARGV[2])
local recovered = 0
for _, task_id in ipairs(jobs) do
  local raw = redis.call("GET", ARGV[4] .. task_id)
  redis.call("ZREM", KEYS[1], task_id)
  if raw then
    local task = cjson.decode(raw)
    if task.status == "running" and tonumber(task.updated_at or 0) <= tonumber(ARGV[1]) then
      task.status = "failed"
      task.error = cjson.decode(ARGV[3])
      task.execution_token = nil
      task.updated_at = tonumber(ARGV[5])
      task.completed_at = tonumber(ARGV[5])
      redis.call("SET", ARGV[4] .. task_id, cjson.encode(task), "PX", ARGV[6])
      local idem_key = ARGV[7] .. tostring(task.user_id) .. ":" .. tostring(task.api_key_id) .. ":" .. task.client_request_id
      redis.call("PEXPIRE", idem_key, ARGV[6])
      recovered = recovered + 1
    end
  end
end
return recovered
`)

type imageTaskStore struct {
	rdb *redis.Client
}

func NewImageTaskStore(rdb *redis.Client) service.ImageTaskRepository {
	return &imageTaskStore{rdb: rdb}
}

func (s *imageTaskStore) Save(ctx context.Context, task *service.ImageTaskRecord, ttl time.Duration) error {
	data, err := json.Marshal(task)
	if err != nil {
		return err
	}
	return s.rdb.Set(ctx, imageTaskKey(task.ID), data, ttl).Err()
}

func (s *imageTaskStore) Get(ctx context.Context, id string) (*service.ImageTaskRecord, error) {
	data, err := s.rdb.Get(ctx, imageTaskKey(id)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, service.ErrImageTaskNotFound
		}
		return nil, err
	}
	var task service.ImageTaskRecord
	if err := json.Unmarshal(data, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

func (s *imageTaskStore) CreateOrGet(ctx context.Context, task *service.ImageTaskRecord, ttl time.Duration) (*service.ImageTaskRecord, bool, error) {
	data, err := json.Marshal(task)
	if err != nil {
		return nil, false, err
	}
	result, err := imageTaskCreateOrGetScript.Run(
		ctx,
		s.rdb,
		[]string{
			imageTaskKey(task.ID),
			imageTaskIdempotencyKey(task.UserID, task.APIKeyID, task.ClientRequestID),
			imageTaskReadyQueueKey,
		},
		data,
		task.RequestFingerprint,
		ttl.Milliseconds(),
		imageTaskKeyPrefix,
		task.ID,
	).Slice()
	if err != nil {
		return nil, false, err
	}
	if len(result) != 2 {
		return nil, false, fmt.Errorf("unexpected image task create result: %v", result)
	}
	state := redisScriptString(result[0])
	if state == "conflict" {
		return nil, false, service.ErrImageTaskIdempotencyConflict
	}
	var stored service.ImageTaskRecord
	if err := json.Unmarshal([]byte(redisScriptString(result[1])), &stored); err != nil {
		return nil, false, err
	}
	return &stored, state == "created", nil
}

func (s *imageTaskStore) Reserve(ctx context.Context, executionToken string, now time.Time) (*service.ImageTaskRecord, error) {
	raw, err := imageTaskReserveScript.Run(
		ctx,
		s.rdb,
		[]string{imageTaskReadyQueueKey, imageTaskActiveQueueKey},
		imageTaskKeyPrefix,
		executionToken,
		now.Unix(),
	).Result()
	if errors.Is(err, redis.Nil) {
		return nil, service.ErrImageTaskQueueEmpty
	}
	if err != nil {
		return nil, err
	}
	var task service.ImageTaskRecord
	if err := json.Unmarshal([]byte(redisScriptString(raw)), &task); err != nil {
		return nil, err
	}
	return &task, nil
}

func (s *imageTaskStore) Heartbeat(ctx context.Context, taskID, executionToken string, now time.Time) error {
	updated, err := imageTaskHeartbeatScript.Run(
		ctx,
		s.rdb,
		[]string{imageTaskKey(taskID), imageTaskActiveQueueKey},
		executionToken,
		now.Unix(),
		taskID,
	).Int()
	if err != nil {
		return err
	}
	if updated == 0 {
		return service.ErrImageTaskClaimLost
	}
	return nil
}

func (s *imageTaskStore) SaveClaim(ctx context.Context, task *service.ImageTaskRecord, executionToken string, ttl time.Duration) error {
	stored := *task
	stored.ExecutionToken = ""
	data, err := json.Marshal(&stored)
	if err != nil {
		return err
	}
	updated, err := imageTaskSaveClaimScript.Run(
		ctx,
		s.rdb,
		[]string{
			imageTaskKey(task.ID),
			imageTaskActiveQueueKey,
			imageTaskIdempotencyKey(task.UserID, task.APIKeyID, task.ClientRequestID),
		},
		data,
		executionToken,
		ttl.Milliseconds(),
		task.ID,
	).Int()
	if err != nil {
		return err
	}
	if updated == 0 {
		return service.ErrImageTaskClaimLost
	}
	return nil
}

func (s *imageTaskStore) RecoverStale(ctx context.Context, before, now time.Time, ttl time.Duration) (int, error) {
	return imageTaskRecoverStaleScript.Run(
		ctx,
		s.rdb,
		[]string{imageTaskActiveQueueKey},
		before.Unix(),
		100,
		`{"code":"EXECUTION_INTERRUPTED","message":"Image generation was interrupted","retryable":true}`,
		imageTaskKeyPrefix,
		now.Unix(),
		ttl.Milliseconds(),
		imageTaskIdempotencyPrefix,
	).Int()
}

func imageTaskKey(id string) string {
	return imageTaskKeyPrefix + strings.TrimSpace(id)
}

func imageTaskIdempotencyKey(userID, apiKeyID int64, clientRequestID string) string {
	return imageTaskIdempotencyPrefix +
		strconv.FormatInt(userID, 10) + ":" +
		strconv.FormatInt(apiKeyID, 10) + ":" +
		clientRequestID
}

func redisScriptString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		return fmt.Sprint(typed)
	}
}
