package service

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"
)

const (
	BillingTypeBalance      int8 = 0 // 钱包余额
	BillingTypeSubscription int8 = 1 // 订阅套餐
)

type RequestType int16

const (
	RequestTypeUnknown      RequestType = 0
	RequestTypeSync         RequestType = 1
	RequestTypeStream       RequestType = 2
	RequestTypeWSV2         RequestType = 3
	RequestTypeCyberBlocked RequestType = 4 // cyber_policy 命中（透传但被上游安全策略拒绝）
	RequestTypeLive         RequestType = 5
)

func (t RequestType) IsValid() bool {
	switch t {
	case RequestTypeUnknown, RequestTypeSync, RequestTypeStream, RequestTypeWSV2, RequestTypeCyberBlocked, RequestTypeLive:
		return true
	default:
		return false
	}
}

func (t RequestType) Normalize() RequestType {
	if t.IsValid() {
		return t
	}
	return RequestTypeUnknown
}

func (t RequestType) String() string {
	switch t.Normalize() {
	case RequestTypeSync:
		return "sync"
	case RequestTypeStream:
		return "stream"
	case RequestTypeWSV2:
		return "ws_v2"
	case RequestTypeCyberBlocked:
		return "cyber"
	case RequestTypeLive:
		return "live"
	default:
		return "unknown"
	}
}

func RequestTypeFromInt16(v int16) RequestType {
	return RequestType(v).Normalize()
}

func ParseUsageRequestType(value string) (RequestType, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "unknown":
		return RequestTypeUnknown, nil
	case "sync":
		return RequestTypeSync, nil
	case "stream":
		return RequestTypeStream, nil
	case "ws_v2":
		return RequestTypeWSV2, nil
	case "cyber":
		return RequestTypeCyberBlocked, nil
	case "live":
		return RequestTypeLive, nil
	default:
		return RequestTypeUnknown, fmt.Errorf("invalid request_type, allowed values: unknown, sync, stream, ws_v2, cyber, live")
	}
}

func RequestTypeFromLegacy(stream bool, openAIWSMode bool) RequestType {
	if openAIWSMode {
		return RequestTypeWSV2
	}
	if stream {
		return RequestTypeStream
	}
	return RequestTypeSync
}

func ApplyLegacyRequestFields(requestType RequestType, fallbackStream bool, fallbackOpenAIWSMode bool) (stream bool, openAIWSMode bool) {
	switch requestType.Normalize() {
	case RequestTypeSync:
		return false, false
	case RequestTypeStream:
		return true, false
	case RequestTypeWSV2:
		return true, true
	default:
		return fallbackStream, fallbackOpenAIWSMode
	}
}

type UsageLog struct {
	ID        int64
	UserID    int64
	APIKeyID  int64
	AccountID int64
	RequestID string
	Model     string
	// RequestedModel is the client-requested model name recorded for stable user/admin display.
	// Empty should be treated as Model for backward compatibility with historical rows.
	RequestedModel string
	// UpstreamModel is the actual model sent to the upstream provider after mapping.
	// Nil means no mapping was applied (requested model was used as-is).
	UpstreamModel *string
	// UpstreamResponseModel is the model declared by the successful upstream
	// response before client-facing model rewrites or protocol conversion.
	UpstreamResponseModel *string
	// UpstreamModelMismatch is nil when no upstream model was observed. Otherwise
	// it compares UpstreamResponseModel with the actual model sent upstream.
	UpstreamModelMismatch *bool
	// ChannelID 渠道 ID
	ChannelID *int64
	// ModelMappingChain 模型映射链，如 "a→b→c"
	ModelMappingChain *string
	// BillingTier 计费层级标签（per_request/image 模式）
	BillingTier *string
	// BillingMode 计费模式：token/image
	BillingMode *string
	// ServiceTier records the OpenAI service tier used for billing, e.g. "priority" / "flex".
	ServiceTier *string
	// ReasoningEffort is the request's reasoning effort level.
	// OpenAI: "low" / "medium" / "high" / "xhigh"; Claude: "low" / "medium" / "high" / "max".
	// Nil means not provided / not applicable.
	ReasoningEffort *string
	// InboundEndpoint is the client-facing API endpoint path, e.g. /v1/chat/completions.
	InboundEndpoint *string
	// UpstreamEndpoint is the normalized upstream endpoint path, e.g. /v1/responses.
	UpstreamEndpoint *string

	GroupID        *int64
	SubscriptionID *int64

	InputTokens         int
	OutputTokens        int
	CacheCreationTokens int
	CacheReadTokens     int

	CacheCreation5mTokens int `gorm:"column:cache_creation_5m_tokens"`
	CacheCreation1hTokens int `gorm:"column:cache_creation_1h_tokens"`

	ImageInputTokens  int
	ImageInputCost    float64
	ImageOutputTokens int
	ImageOutputCost   float64

	InputCost                 float64
	OutputCost                float64
	CacheCreationCost         float64
	CacheReadCost             float64
	TotalCost                 float64
	ActualCost                float64
	RateMultiplier            float64
	LongContextBillingApplied bool
	// AccountRateMultiplier 账号计费倍率快照（nil 表示历史数据，按 1.0 处理）
	AccountRateMultiplier *float64
	// AccountStatsCost 账号统计定价预计算费用（nil = 使用默认公式 total_cost × account_rate_multiplier）
	AccountStatsCost *float64

	BillingType  int8
	RequestType  RequestType
	Stream       bool
	OpenAIWSMode bool
	DurationMs   *int
	FirstTokenMs *int
	UserAgent    *string
	IPAddress    *string
	// SessionID is the explicit client-provided request correlation identifier
	// (e.g. the session_id / X-Session-Id headers). Nil when the client sent no
	// valid session header. It is never derived from prompt_cache_key or content.
	SessionID *string

	// Cache TTL Override 标记（管理员强制替换了缓存 TTL 计费）
	CacheTTLOverridden bool

	// 图片生成字段
	ImageCount         int
	ImageSize          *string
	ImageInputSize     *string
	ImageOutputSize    *string
	ImageSizeSource    *string
	ImageSizeBreakdown map[string]int
	MediaType          *string

	// 视频生成字段（Grok 视频按秒计费；video_count>0 的行不要求 image_size）
	VideoCount           int
	VideoResolution      *string
	VideoDurationSeconds *int

	CreatedAt time.Time

	User         *User
	APIKey       *APIKey
	Account      *Account
	Group        *Group
	Subscription *UserSubscription
}

// CanExposeOAuthAccountToUser 判断普通用户使用记录是否可以展示命中的 OAuth 账号名称。
// 判断必须基于 usage_logs 记录的实际分组和实际账号关联，不能使用 API Key 原始绑定分组。
func CanExposeOAuthAccountToUser(log *UsageLog, userID int64) bool {
	return log != nil &&
		log.UserID == userID &&
		log.Group != nil &&
		log.Group.OAuthPoolVisible &&
		log.Account != nil &&
		log.Account.Type == AccountTypeOAuth
}

func (u *UsageLog) TotalTokens() int {
	return u.InputTokens + u.OutputTokens + u.CacheCreationTokens + u.CacheReadTokens
}

func (u *UsageLog) EffectiveRequestType() RequestType {
	if u == nil {
		return RequestTypeUnknown
	}
	if normalized := u.RequestType.Normalize(); normalized != RequestTypeUnknown {
		return normalized
	}
	return RequestTypeFromLegacy(u.Stream, u.OpenAIWSMode)
}

func (u *UsageLog) SyncRequestTypeAndLegacyFields() {
	if u == nil {
		return
	}
	requestType := u.EffectiveRequestType()
	u.RequestType = requestType
	u.Stream, u.OpenAIWSMode = ApplyLegacyRequestFields(requestType, u.Stream, u.OpenAIWSMode)
}

const cacheHitTargetBasisPoints = int64(10000)

// CacheHitTargetTracker 是 GatewayCache 的可选扩展。生产 Redis 实现通过原子脚本按
// 用户+分组累计控制；测试缓存与降级路径无需实现该接口。
type CacheHitTargetTracker interface {
	AdjustCacheHitToTarget(ctx context.Context, userID, groupID, targetBasisPoints, promptTokens, cacheReadTokens int64) (int64, error)
}

func groupCacheHitTarget(apiKey *APIKey) (int64, bool) {
	if apiKey == nil || apiKey.Group == nil || !apiKey.Group.CacheHitQuarterToInput {
		return 0, false
	}
	percent := apiKey.Group.CacheHitTargetPercent
	// 兼容直接构造旧 Group 的内部调用与升级前认证快照；正常持久化值由数据库默认值保证。
	if percent <= 0 {
		percent = DefaultCacheHitTargetPercent
	}
	if err := ValidateCacheHitTargetConfig(percent); err != nil {
		return 0, false
	}
	return int64(math.Round(percent * 100)), true
}

// cacheHitShiftForSingleRequest 是 Redis 不可用时的安全降级：只控制本次请求命中率，
// 因而可能比累计算法略低，但不会让新请求的调整后命中率超过目标。
func cacheHitShiftForSingleRequest(promptTokens, cacheReadTokens, targetBasisPoints int64) int64 {
	if promptTokens <= 0 || cacheReadTokens <= 0 || targetBasisPoints >= cacheHitTargetBasisPoints {
		return 0
	}
	allowed := (promptTokens/cacheHitTargetBasisPoints)*targetBasisPoints +
		(promptTokens%cacheHitTargetBasisPoints)*targetBasisPoints/cacheHitTargetBasisPoints
	if cacheReadTokens <= allowed {
		return 0
	}
	return cacheReadTokens - allowed
}

// applyCacheHitTargetToInput 保持 token 总量不变，只把达到累计目标所必需的缓存读取
// token 重分类为普通输入。即便本次 cache_read=0 也会更新累计分母，使后续请求不会被多调。
func applyCacheHitTargetToInput(ctx context.Context, tokens *UsageTokens, apiKey *APIKey, userID int64, tracker any) (shifted int, trackerErr error) {
	if tokens == nil {
		return 0, nil
	}
	target, enabled := groupCacheHitTarget(apiKey)
	if !enabled || target >= cacheHitTargetBasisPoints {
		return 0, nil
	}
	input := max(tokens.InputTokens, 0)
	cacheCreation := max(tokens.CacheCreationTokens, 0)
	cacheRead := max(tokens.CacheReadTokens, 0)
	promptTokens := int64(input) + int64(cacheCreation) + int64(cacheRead)
	if promptTokens <= 0 {
		return 0, nil
	}

	shift := cacheHitShiftForSingleRequest(promptTokens, int64(cacheRead), target)
	if cumulative, ok := tracker.(CacheHitTargetTracker); ok {
		groupID := valueOrZero(apiKey.GroupID)
		if groupID == 0 && apiKey.Group != nil {
			groupID = apiKey.Group.ID
		}
		if groupID > 0 && userID > 0 {
			var err error
			shift, err = cumulative.AdjustCacheHitToTarget(ctx, userID, groupID, target, promptTokens, int64(cacheRead))
			if err != nil {
				trackerErr = err
				shift = cacheHitShiftForSingleRequest(promptTokens, int64(cacheRead), target)
			}
		}
	}
	if shift < 0 {
		shift = 0
	}
	if shift > int64(cacheRead) {
		shift = int64(cacheRead)
	}
	tokens.CacheReadTokens -= int(shift)
	tokens.InputTokens += int(shift)
	return int(shift), trackerErr
}
