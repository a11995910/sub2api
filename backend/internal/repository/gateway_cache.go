package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const stickySessionPrefix = "sticky_session:"
const openAIVideoProtocolPrefix = "openai_video_protocol:"
const liveCallPrefix = "live:call:"
const cacheHitTargetPrefix = "cache_hit_target:v1:"

type gatewayCache struct {
	rdb *redis.Client
}

func NewGatewayCache(rdb *redis.Client) service.GatewayCache {
	return &gatewayCache{rdb: rdb}
}

var adjustCacheHitTargetScript = redis.NewScript(`
	local key = KEYS[1]
	local prompt_delta = tonumber(ARGV[1])
	local cache_delta = tonumber(ARGV[2])
	local target = tonumber(ARGV[3])
	if prompt_delta == nil or cache_delta == nil or target == nil or
		prompt_delta <= 0 or cache_delta < 0 or target < 0 or target > 10000 then
		return redis.error_reply('invalid cache hit target arguments')
	end

	local prompt_total = tonumber(redis.call('HGET', key, 'prompt_tokens') or '0') + prompt_delta
	local cache_available = tonumber(redis.call('HGET', key, 'cache_read_tokens') or '0') + cache_delta
	-- 分段乘法减少累计 token 较大时的浮点误差，并与 Go 降级算法保持向下取整口径。
	local allowed = math.floor(prompt_total / 10000) * target +
		math.floor((prompt_total % 10000) * target / 10000)
	local cache_kept = cache_available
	if cache_kept > allowed then
		cache_kept = allowed
	end
	local shifted = cache_available - cache_kept
	redis.call('HSET', key, 'prompt_tokens', prompt_total, 'cache_read_tokens', cache_kept)
	return shifted
`)

// AdjustCacheHitToTarget 原子累加用户在分组内的提示词与调整后缓存读取 token。
// 目标值放入 key，使不同目标的累计状态相互隔离；每个用户/分组/目标仅一条 Hash。
func (c *gatewayCache) AdjustCacheHitToTarget(
	ctx context.Context,
	userID, groupID, targetBasisPoints, promptTokens, cacheReadTokens int64,
) (int64, error) {
	if c == nil || c.rdb == nil {
		return 0, errors.New("gateway cache unavailable")
	}
	if userID <= 0 || groupID <= 0 || targetBasisPoints < 0 || targetBasisPoints > 10000 || promptTokens <= 0 || cacheReadTokens < 0 {
		return 0, errors.New("invalid cache hit target arguments")
	}
	key := fmt.Sprintf("%s%d:%d:%d", cacheHitTargetPrefix, userID, groupID, targetBasisPoints)
	return adjustCacheHitTargetScript.Run(
		ctx,
		c.rdb,
		[]string{key},
		promptTokens,
		cacheReadTokens,
		targetBasisPoints,
	).Int64()
}

// buildSessionKey 构建 session key，包含 groupID 实现分组隔离
// 格式: sticky_session:{groupID}:{sessionHash}
func buildSessionKey(groupID int64, sessionHash string) string {
	return fmt.Sprintf("%s%d:%s", stickySessionPrefix, groupID, sessionHash)
}

func (c *gatewayCache) GetSessionAccountID(ctx context.Context, groupID int64, sessionHash string) (int64, error) {
	key := buildSessionKey(groupID, sessionHash)
	accountID, err := c.rdb.Get(ctx, key).Int64()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return 0, service.ErrStickySessionNotFound
		}
		return 0, err
	}
	return accountID, nil
}

func (c *gatewayCache) SetSessionAccountID(ctx context.Context, groupID int64, sessionHash string, accountID int64, ttl time.Duration) error {
	key := buildSessionKey(groupID, sessionHash)
	return c.rdb.Set(ctx, key, accountID, ttl).Err()
}

func (c *gatewayCache) RefreshSessionTTL(ctx context.Context, groupID int64, sessionHash string, ttl time.Duration) error {
	key := buildSessionKey(groupID, sessionHash)
	return c.rdb.Expire(ctx, key, ttl).Err()
}

// DeleteSessionAccountID 删除粘性会话与账号的绑定关系。
// 当检测到绑定的账号不可用（如状态错误、禁用、不可调度等）时调用，
// 以便下次请求能够重新选择可用账号。
//
// DeleteSessionAccountID removes the sticky session binding for the given session.
// Called when the bound account becomes unavailable (e.g., error status, disabled,
// or unschedulable), allowing subsequent requests to select a new available account.
func (c *gatewayCache) DeleteSessionAccountID(ctx context.Context, groupID int64, sessionHash string) error {
	key := buildSessionKey(groupID, sessionHash)
	return c.rdb.Del(ctx, key).Err()
}

func buildOpenAIVideoProtocolKey(accountID int64, mappedModel string, requestProfile service.OpenAIVideoRequestProfile) (string, error) {
	mappedModel = strings.TrimSpace(mappedModel)
	if accountID <= 0 || mappedModel == "" ||
		(requestProfile != service.OpenAIVideoRequestProfileLegacy && requestProfile != service.OpenAIVideoRequestProfileUnifiedJSON) {
		return "", fmt.Errorf("invalid video protocol cache key")
	}
	digest := sha256.Sum256([]byte(strings.ToLower(mappedModel) + "\x00" + string(requestProfile)))
	return fmt.Sprintf("%s%d:%x", openAIVideoProtocolPrefix, accountID, digest[:12]), nil
}

func (c *gatewayCache) GetOpenAIVideoProtocol(ctx context.Context, accountID int64, mappedModel string, requestProfile service.OpenAIVideoRequestProfile) (service.OpenAIVideoProtocol, error) {
	key, err := buildOpenAIVideoProtocolKey(accountID, mappedModel, requestProfile)
	if err != nil {
		return "", err
	}
	value, err := c.rdb.Get(ctx, key).Result()
	if err != nil {
		return "", err
	}
	protocol := service.OpenAIVideoProtocol(value)
	if protocol != service.OpenAIVideoProtocolVideos && protocol != service.OpenAIVideoProtocolChatCompletions {
		return "", fmt.Errorf("invalid cached video protocol %q", value)
	}
	return protocol, nil
}

func (c *gatewayCache) SetOpenAIVideoProtocol(
	ctx context.Context,
	accountID int64,
	mappedModel string,
	requestProfile service.OpenAIVideoRequestProfile,
	protocol service.OpenAIVideoProtocol,
	ttl time.Duration,
) error {
	if protocol != service.OpenAIVideoProtocolVideos && protocol != service.OpenAIVideoProtocolChatCompletions {
		return fmt.Errorf("invalid video protocol %q", protocol)
	}
	key, err := buildOpenAIVideoProtocolKey(accountID, mappedModel, requestProfile)
	if err != nil {
		return err
	}
	return c.rdb.Set(ctx, key, string(protocol), ttl).Err()
}

func (c *gatewayCache) DeleteOpenAIVideoProtocol(ctx context.Context, accountID int64, mappedModel string, requestProfile service.OpenAIVideoRequestProfile) error {
	key, err := buildOpenAIVideoProtocolKey(accountID, mappedModel, requestProfile)
	if err != nil {
		return err
	}
	return c.rdb.Del(ctx, key).Err()
}

const (
	grokVideoPendingBillingPrefix = "grok_video_pending:"
	grokVideoBilledPrefix         = "grok_video_billed:"
)

func (c *gatewayCache) SetGrokVideoPendingBilling(ctx context.Context, key string, payload []byte, ttl time.Duration) error {
	if c == nil || c.rdb == nil {
		return errors.New("gateway cache unavailable")
	}
	key = strings.TrimSpace(key)
	if key == "" || len(payload) == 0 {
		return errors.New("invalid grok video pending billing payload")
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return c.rdb.Set(ctx, grokVideoPendingBillingPrefix+key, payload, ttl).Err()
}

func (c *gatewayCache) GetGrokVideoPendingBilling(ctx context.Context, key string) ([]byte, error) {
	if c == nil || c.rdb == nil {
		return nil, errors.New("gateway cache unavailable")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, errors.New("invalid grok video pending billing key")
	}
	val, err := c.rdb.Get(ctx, grokVideoPendingBillingPrefix+key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, err
	}
	return val, nil
}

func (c *gatewayCache) ClaimGrokVideoBilled(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	if c == nil || c.rdb == nil {
		return false, errors.New("gateway cache unavailable")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return false, errors.New("invalid grok video billed key")
	}
	if ttl <= 0 {
		ttl = 48 * time.Hour
	}
	return c.rdb.SetNX(ctx, grokVideoBilledPrefix+key, "1", ttl).Result()
}

func (c *gatewayCache) ReleaseGrokVideoBilled(ctx context.Context, key string) error {
	if c == nil || c.rdb == nil {
		return errors.New("gateway cache unavailable")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("invalid grok video billed key")
	}
	return c.rdb.Del(ctx, grokVideoBilledPrefix+key).Err()
}

// Compile-time assertion: gatewayCache must implement CyberSessionBlockStore.
var _ service.CyberSessionBlockStore = (*gatewayCache)(nil)
var _ service.LiveCallStore = (*gatewayCache)(nil)
var _ service.CacheHitTargetTracker = (*gatewayCache)(nil)

const cyberSessionBlockPrefix = "cyber_session_block:"

// SetCyberSessionBlocked 把被 cyber_policy 命中的会话写入屏蔽表（TTL 自动过期）。
// 存储值 "1" 作为存在标记（IsCyberSessionBlocked 只检查 key 是否存在，不读值）。
func (c *gatewayCache) SetCyberSessionBlocked(ctx context.Context, key string, ttl time.Duration) error {
	return c.rdb.Set(ctx, cyberSessionBlockPrefix+key, "1", ttl).Err()
}

// IsCyberSessionBlocked 查询会话是否在屏蔽表中。
func (c *gatewayCache) IsCyberSessionBlocked(ctx context.Context, key string) (bool, error) {
	n, err := c.rdb.Exists(ctx, cyberSessionBlockPrefix+key).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

var claimLiveControllerScript = redis.NewScript(`
	local key = KEYS[1]
	local target = ARGV[1]
	local owner = ARGV[2]
	local current = redis.call('HGET', key, 'controller')
	if current == false or current == 'closed' then
		return 0
	end
	if target == 'observer' and current ~= 'pending' then
		return 0
	end
	if target == 'proxy' and current ~= 'pending' and current ~= 'observer' and
		(current ~= 'proxy' or redis.call('HGET', key, 'controller_owner') ~= owner) then
		return 0
	end
	redis.call('HSET', key, 'controller', target, 'controller_owner', owner)
	return 1
`)

var markLiveCallClosedScript = redis.NewScript(`
	local key = KEYS[1]
	if redis.call('EXISTS', key) == 0 then
		return 0
	end
	if redis.call('HGET', key, 'controller') == 'closed' then
		return 0
	end
	redis.call('HSET', key, 'controller', 'closed', 'controller_owner', '')
	redis.call('EXPIRE', key, ARGV[1])
	return 1
`)

var releaseLiveControllerScript = redis.NewScript(`
	local key = KEYS[1]
	if redis.call('HGET', key, 'controller') ~= 'proxy' or
		redis.call('HGET', key, 'controller_owner') ~= ARGV[1] then
		return 0
	end
	redis.call('HSET', key, 'controller', 'pending', 'controller_owner', '')
	return 1
`)

func liveCallKey(callHash string) string {
	return liveCallPrefix + callHash
}

func HashLiveCallID(callID string) string {
	sum := sha256.Sum256([]byte(callID))
	return hex.EncodeToString(sum[:])
}

func (c *gatewayCache) SaveLiveCall(ctx context.Context, record *service.LiveCallRecord, ttl time.Duration) error {
	if record == nil || record.CallHash == "" || record.CallID == "" {
		return fmt.Errorf("invalid live call record")
	}
	values := map[string]any{
		"call_id":          record.CallID,
		"account_id":       record.AccountID,
		"api_key_id":       record.APIKeyID,
		"user_id":          record.UserID,
		"group_id":         record.GroupID,
		"subscription_id":  record.SubscriptionID,
		"lease_id":         record.LeaseID,
		"model":            record.Model,
		"created_at":       record.CreatedAt.UnixMilli(),
		"expires_at":       record.ExpiresAt.UnixMilli(),
		"controller":       record.Controller,
		"controller_owner": record.ControllerOwner,
		"user_agent":       record.UserAgent,
		"ip_address":       record.IPAddress,
		"inbound_endpoint": record.InboundEndpoint,
		"attestation":      record.AttestationCiphertext,
	}
	key := liveCallKey(record.CallHash)
	pipe := c.rdb.TxPipeline()
	pipe.HSet(ctx, key, values)
	pipe.Expire(ctx, key, ttl)
	_, err := pipe.Exec(ctx)
	return err
}

func (c *gatewayCache) GetLiveCall(ctx context.Context, callHash string) (*service.LiveCallRecord, error) {
	values, err := c.rdb.HGetAll(ctx, liveCallKey(callHash)).Result()
	if err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return nil, service.ErrLiveCallNotFound
	}
	parseInt := func(field string) int64 {
		value, _ := strconv.ParseInt(values[field], 10, 64)
		return value
	}
	createdAt := time.UnixMilli(parseInt("created_at"))
	expiresAt := time.UnixMilli(parseInt("expires_at"))
	return &service.LiveCallRecord{
		CallID:                values["call_id"],
		CallHash:              callHash,
		AccountID:             parseInt("account_id"),
		APIKeyID:              parseInt("api_key_id"),
		UserID:                parseInt("user_id"),
		GroupID:               parseInt("group_id"),
		SubscriptionID:        parseInt("subscription_id"),
		LeaseID:               values["lease_id"],
		Model:                 values["model"],
		CreatedAt:             createdAt,
		ExpiresAt:             expiresAt,
		Controller:            values["controller"],
		ControllerOwner:       values["controller_owner"],
		UserAgent:             values["user_agent"],
		IPAddress:             values["ip_address"],
		InboundEndpoint:       values["inbound_endpoint"],
		AttestationCiphertext: values["attestation"],
	}, nil
}

func (c *gatewayCache) ClaimLiveController(ctx context.Context, callHash, controller, owner string) (bool, error) {
	result, err := claimLiveControllerScript.Run(ctx, c.rdb, []string{liveCallKey(callHash)}, controller, owner).Int()
	return result == 1, err
}

func (c *gatewayCache) GetLiveController(ctx context.Context, callHash string) (string, error) {
	value, err := c.rdb.HGet(ctx, liveCallKey(callHash), "controller").Result()
	if err == redis.Nil {
		return "", service.ErrLiveCallNotFound
	}
	return value, err
}

func (c *gatewayCache) ReleaseLiveController(ctx context.Context, callHash, owner string) (bool, error) {
	result, err := releaseLiveControllerScript.Run(ctx, c.rdb, []string{liveCallKey(callHash)}, owner).Int()
	return result == 1, err
}

func (c *gatewayCache) MarkLiveCallClosed(ctx context.Context, callHash string, ttl time.Duration) (bool, error) {
	result, err := markLiveCallClosedScript.Run(ctx, c.rdb, []string{liveCallKey(callHash)}, int64(ttl.Seconds())).Int()
	return result == 1, err
}
