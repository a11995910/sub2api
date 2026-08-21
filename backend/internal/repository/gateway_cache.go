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
const (
	cacheHitTargetPrefix = "cache_hit_target:v3:"
	cacheHitTargetTTL    = 30 * 24 * time.Hour
)

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
	local tolerance = tonumber(ARGV[4])
	local half_life_seconds = tonumber(ARGV[5])
	local now_unix = tonumber(ARGV[6])
	local ttl_seconds = tonumber(ARGV[7])
	if prompt_delta == nil or cache_delta == nil or target == nil or tolerance == nil or
		half_life_seconds == nil or now_unix == nil or ttl_seconds == nil or
		prompt_delta <= 0 or cache_delta < 0 or target < 0 or target > 10000 or
		tolerance < 0 or target + tolerance > 10000 or half_life_seconds <= 0 or
		now_unix <= 0 or ttl_seconds <= 0 then
		return redis.error_reply('invalid cache hit target arguments')
	end

	local historical_prompt = tonumber(redis.call('HGET', key, 'prompt_tokens') or '0')
	local historical_cache = tonumber(redis.call('HGET', key, 'cache_read_tokens') or '0')
	local last_updated_unix = tonumber(redis.call('HGET', key, 'last_updated_unix') or '0')
	if last_updated_unix > 0 and now_unix > last_updated_unix then
		local decay = math.pow(0.5, (now_unix - last_updated_unix) / half_life_seconds)
		historical_prompt = historical_prompt * decay
		historical_cache = historical_cache * decay
	end

	local prompt_total = historical_prompt + prompt_delta
	local cache_available = historical_cache + cache_delta
	local target_allowed = math.floor(prompt_total * target / 10000)
	local trigger = target + tolerance
	local trigger_allowed = math.floor(prompt_total * trigger / 10000)
	local shifted = 0
	if cache_available > trigger_allowed then
		-- 容差带允许历史累计暂时高于目标；触发时只能重分类本次请求实际拥有的缓存读取 token，
		-- 不能回溯改写已落库请求，否则 Redis 累计会与真实账单不一致。
		shifted = math.min(math.ceil(cache_available - target_allowed), cache_delta)
	end
	local cache_kept = cache_available - shifted
	redis.call('HSET', key,
		'prompt_tokens', prompt_total,
		'cache_read_tokens', cache_kept,
		'last_updated_unix', now_unix)
	redis.call('EXPIRE', key, ttl_seconds)
	return {shifted, math.floor(prompt_total + 0.5), math.floor(cache_kept + 0.5)}
`)

// AdjustCacheHitToTarget 先按半衰期衰减历史权重，再原子累加本次提示词与缓存读取 token。
func (c *gatewayCache) AdjustCacheHitToTarget(
	ctx context.Context,
	userID, groupID, targetBasisPoints, toleranceBasisPoints, halfLifeSeconds, stateVersion, promptTokens, cacheReadTokens int64,
) (service.CacheHitTargetAdjustment, error) {
	return c.adjustCacheHitToTargetAt(ctx, userID, groupID, targetBasisPoints, toleranceBasisPoints, halfLifeSeconds, stateVersion, promptTokens, cacheReadTokens, time.Now())
}

func (c *gatewayCache) adjustCacheHitToTargetAt(
	ctx context.Context,
	userID, groupID, targetBasisPoints, toleranceBasisPoints, halfLifeSeconds, stateVersion, promptTokens, cacheReadTokens int64,
	observedAt time.Time,
) (service.CacheHitTargetAdjustment, error) {
	if c == nil || c.rdb == nil {
		return service.CacheHitTargetAdjustment{}, errors.New("gateway cache unavailable")
	}
	if userID <= 0 || groupID <= 0 || targetBasisPoints < 0 || targetBasisPoints > 10000 || toleranceBasisPoints < 0 || targetBasisPoints+toleranceBasisPoints > 10000 || halfLifeSeconds <= 0 || observedAt.Unix() <= 0 || promptTokens <= 0 || cacheReadTokens < 0 {
		return service.CacheHitTargetAdjustment{}, errors.New("invalid cache hit target arguments")
	}
	key := fmt.Sprintf("%s%d:%d:%d:%d:%d:%d", cacheHitTargetPrefix, userID, groupID, targetBasisPoints, toleranceBasisPoints, halfLifeSeconds, stateVersion)
	values, err := adjustCacheHitTargetScript.Run(
		ctx,
		c.rdb,
		[]string{key},
		promptTokens,
		cacheReadTokens,
		targetBasisPoints,
		toleranceBasisPoints,
		halfLifeSeconds,
		observedAt.Unix(),
		int64(cacheHitTargetTTL/time.Second),
	).Int64Slice()
	if err != nil {
		return service.CacheHitTargetAdjustment{}, err
	}
	if len(values) != 3 {
		return service.CacheHitTargetAdjustment{}, errors.New("invalid cache hit target result")
	}
	return service.CacheHitTargetAdjustment{
		Enabled:                   true,
		ShiftedTokens:             int(values[0]),
		CumulativePromptTokens:    values[1],
		CumulativeCacheReadTokens: values[2],
		StateVersion:              stateVersion,
	}, nil
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

const reasoningContentPrefix = "reasoning_content:"

// reasoningContentDefaultTTL 是 reasoning 缓存的默认过期时间。Codex 会话可能
// 跨多天恢复，取 7 天；调用方传入非正 TTL 时兜底。
const reasoningContentDefaultTTL = 7 * 24 * time.Hour

// SetReasoningContent 按 reasoning item id 缓存 reasoning 全文。
// itemID 或 content 为空时直接返回 nil（无可缓存内容，属正常情况而非错误）。
func (c *gatewayCache) SetReasoningContent(ctx context.Context, itemID string, content string, ttl time.Duration) error {
	if c == nil || c.rdb == nil {
		return errors.New("gateway cache unavailable")
	}
	itemID = strings.TrimSpace(itemID)
	if itemID == "" || content == "" {
		return nil
	}
	if ttl <= 0 {
		ttl = reasoningContentDefaultTTL
	}
	return c.rdb.Set(ctx, reasoningContentPrefix+itemID, content, ttl).Err()
}

// GetReasoningContent 返回缓存的 reasoning 全文；未命中返回
// service.ErrReasoningContentNotFound。
func (c *gatewayCache) GetReasoningContent(ctx context.Context, itemID string) (string, error) {
	if c == nil || c.rdb == nil {
		return "", errors.New("gateway cache unavailable")
	}
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return "", service.ErrReasoningContentNotFound
	}
	val, err := c.rdb.Get(ctx, reasoningContentPrefix+itemID).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", service.ErrReasoningContentNotFound
		}
		return "", err
	}
	return val, nil
}

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
