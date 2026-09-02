package service

import (
	"context"
	"net/http"
	"strings"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const openAIStreamCacheHitAdjustmentContextKey = "_openai_stream_cache_hit_adjustment"

// openAIStreamCacheHitAdjustmentState 保证一个 HTTP 流式请求只推进一次累计状态。
// 某些兼容上游会同时发送 response.completed 和 response.done；两帧都需要展示
// 同一划拨结果，但只有第一帧可以访问 Redis tracker。
type openAIStreamCacheHitAdjustmentState struct {
	mu                  sync.Mutex
	attempted           bool
	adjustment          CacheHitTargetAdjustment
	err                 error
	openAIUsageSnapshot *OpenAIUsage
	claudeUsageSnapshot *ClaudeUsage
}

var openAIStreamCacheHitEligiblePaths = map[string]struct{}{
	"/v1/responses":                {},
	"/responses":                   {},
	"/backend-api/codex/responses": {},
	"/openai/v1/responses":         {},
	"/v1/chat/completions":         {},
	"/chat/completions":            {},
	"/openai/v1/chat/completions":  {},
}

// isSuccessfulOpenAIChatFinishReason 只接受 OpenAI Chat Completions 明确表示
// 正常完成的终止原因。length、content_filter、空值及兼容上游的未知原因均不
// 推进划拨，避免把不完整或受拦截的请求计入累计控制。
func isSuccessfulOpenAIChatFinishReason(reason string) bool {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "stop", "tool_calls", "function_call":
		return true
	default:
		return false
	}
}

// isSuccessfulOpenAIResponsesTerminalPayload 校验 Responses 的成功终态语义。
// 兼容上游偶尔会发出 completed/done 类型但在载荷中标记 incomplete/failed，
// 或同时携带 error；这些帧可以继续按既有协议处理，但不能推进划拨。
func isSuccessfulOpenAIResponsesTerminalPayload(eventType string, body []byte) bool {
	eventType = strings.ToLower(strings.TrimSpace(eventType))
	if eventType != "response.completed" && eventType != "response.done" {
		return false
	}
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return false
	}

	for _, path := range []string{"status", "response.status", "data.status", "data.response.status"} {
		status := gjson.GetBytes(body, path)
		if status.Exists() && !strings.EqualFold(strings.TrimSpace(status.String()), "completed") {
			return false
		}
	}
	for _, path := range []string{"error", "response.error", "data.error", "data.response.error"} {
		errValue := gjson.GetBytes(body, path)
		if !errValue.Exists() {
			continue
		}
		raw := strings.TrimSpace(errValue.Raw)
		if raw != "" && !strings.EqualFold(raw, "null") {
			return false
		}
	}
	return true
}

// isOpenAIStreamCacheHitAdjustmentEligible 只识别客户端入站的 OpenAI HTTP
// Chat Completions 与 Responses 根端点。调用方仍需只在 stream=true 的处理路径调用；
// compact、input_tokens、Messages、非流式和 WebSocket 均不满足条件。
func isOpenAIStreamCacheHitAdjustmentEligible(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.URL == nil || c.Request.Method != http.MethodPost {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(c.GetHeader("Upgrade")), "websocket") {
		return false
	}
	if GetOpenAIClientTransport(c) == OpenAIClientTransportWS {
		return false
	}
	if IsOpenAINativeCompactionV2(c) {
		return false
	}
	if OpenAIImageGenerationIntentFromContext(c.Request.Context()) {
		return false
	}
	if imageIntent, known := getOpenAIStreamCacheHitAttemptImageIntent(c); known && imageIntent {
		return false
	}
	if imageIntent, known := getOpenAIImageIntentHint(c); known && imageIntent {
		return false
	}
	if _, ok := openAIVideoContextFromGin(c); ok {
		return false
	}
	if _, ok := seedanceVideoContextFromGin(c); ok {
		return false
	}
	path := strings.TrimRight(strings.TrimSpace(c.Request.URL.Path), "/")
	if path == "" {
		return false
	}
	_, ok := openAIStreamCacheHitEligiblePaths[path]
	return ok
}

// openAIStreamCacheHitTargetEnabled 只用于决定是否需要向兼容上游索取尾部
// usage。它不访问 Redis，也不占用请求级 once；真正划拨仍由成功终态触发。
func openAIStreamCacheHitTargetEnabled(c *gin.Context) bool {
	if !isOpenAIStreamCacheHitAdjustmentEligible(c) {
		return false
	}
	_, enabled := groupCacheHitTarget(getAPIKeyFromContext(c))
	return enabled
}

func openAIStreamCacheHitState(c *gin.Context) *openAIStreamCacheHitAdjustmentState {
	if c == nil {
		return nil
	}
	if value, ok := c.Get(openAIStreamCacheHitAdjustmentContextKey); ok {
		if state, ok := value.(*openAIStreamCacheHitAdjustmentState); ok && state != nil {
			return state
		}
	}
	state := &openAIStreamCacheHitAdjustmentState{}
	c.Set(openAIStreamCacheHitAdjustmentContextKey, state)
	return state
}

// OpenAIStreamCacheHitAdjustmentFromContext 返回当前请求已成功确定的划拨快照。
// 返回值是副本，异步计费任务可以安全持有，不会与响应写协程共享可变状态。
func OpenAIStreamCacheHitAdjustmentFromContext(c *gin.Context) *CacheHitTargetAdjustment {
	state := openAIStreamCacheHitState(c)
	if state == nil {
		return nil
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if !state.attempted || state.err != nil || !state.adjustment.Enabled {
		return nil
	}
	adjustment := state.adjustment
	return &adjustment
}

// openAIStreamCacheHitOpenAIUsageFromContext 返回首次成功划拨时冻结的完整
// OpenAI usage。后续异常尾帧即使携带另一组计数，也不能污染最终计费结果。
func openAIStreamCacheHitOpenAIUsageFromContext(c *gin.Context) (OpenAIUsage, bool) {
	state := openAIStreamCacheHitState(c)
	if state == nil {
		return OpenAIUsage{}, false
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.err != nil || state.openAIUsageSnapshot == nil {
		return OpenAIUsage{}, false
	}
	return *state.openAIUsageSnapshot, true
}

// openAIStreamCacheHitClaudeUsageFromContext 返回首次成功划拨时冻结的完整
// Claude usage，供通用网关及 Anthropic 原生适配器统一收口。
func openAIStreamCacheHitClaudeUsageFromContext(c *gin.Context) (ClaudeUsage, bool) {
	state := openAIStreamCacheHitState(c)
	if state == nil {
		return ClaudeUsage{}, false
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.err != nil || state.claudeUsageSnapshot == nil {
		return ClaudeUsage{}, false
	}
	return *state.claudeUsageSnapshot, true
}

func openAIStreamCacheHitStateForAdjustment(c *gin.Context, apiKey *APIKey) (*openAIStreamCacheHitAdjustmentState, bool) {
	if !isOpenAIStreamCacheHitAdjustmentEligible(c) {
		return nil, false
	}
	if _, enabled := groupCacheHitTarget(apiKey); !enabled {
		return nil, false
	}
	state := openAIStreamCacheHitState(c)
	return state, state != nil
}

// adjustOpenAIStreamCacheHitOnceLocked 在调用方持有请求级锁时推进 tracker。
// usage 专用入口会在同一临界区内发布完整快照，避免并发重复终态抢先冻结另一组计数。
func adjustOpenAIStreamCacheHitOnceLocked(
	ctx context.Context,
	c *gin.Context,
	tokens UsageTokens,
	apiKey *APIKey,
	tracker any,
	state *openAIStreamCacheHitAdjustmentState,
) (CacheHitTargetAdjustment, error, bool) {
	if state.attempted {
		return state.adjustment, state.err, state.err == nil && state.adjustment.Enabled
	}
	if tokens.InputTokens <= 0 && tokens.CacheCreationTokens <= 0 && tokens.CacheReadTokens <= 0 {
		// 终态事件可以合法省略 usage，或先发送全零占位、再由后续终态补齐。
		// 此时不能消耗请求级 once。
		return CacheHitTargetAdjustment{}, nil, false
	}
	requestCtx := context.Background()
	if c != nil && c.Request != nil {
		requestCtx = c.Request.Context()
	}
	if ctx == nil {
		ctx = requestCtx
	}
	// 流式 handler 会在客户端断开后继续 drain 上游以保留原始 usage；这类
	// 尾帧不能推进划拨。既检查调用方 context，也检查 Gin 请求 context，避免
	// 调用方误传 Background 后绕过断连语义。
	if ctx.Err() != nil || requestCtx.Err() != nil {
		return CacheHitTargetAdjustment{}, nil, false
	}
	userID := int64(0)
	if apiKey != nil {
		userID = apiKey.UserID
		if userID <= 0 && apiKey.User != nil {
			userID = apiKey.User.ID
		}
	}
	adjustment, err := applyCacheHitTargetToInput(ctx, &tokens, apiKey, userID, tracker)
	state.attempted = true
	state.err = err
	if err != nil {
		state.adjustment = CacheHitTargetAdjustment{}
		logger.LegacyPrintf("service.openai_stream", "cache_hit_target tracker unavailable, skipping adjustment (group=%d user=%d): %v",
			valueOrZero(apiKeyGroupID(apiKey)), userID, err)
		return CacheHitTargetAdjustment{}, err, false
	}
	state.adjustment = adjustment
	if adjustment.ShiftedTokens > 0 {
		logger.LegacyPrintf("service.openai_stream", "cache_hit_target: %d cache_read_input_tokens -> input_tokens (group=%d user=%d)",
			adjustment.ShiftedTokens, valueOrZero(apiKeyGroupID(apiKey)), userID)
	}
	return adjustment, err, true
}

func apiKeyGroupID(apiKey *APIKey) *int64 {
	if apiKey == nil {
		return nil
	}
	return apiKey.GroupID
}

// applyOpenAIUsageCacheHitSnapshot 保持 OpenAI 的总 input_tokens 不变，只降低
// cached_tokens 明细。重复终止帧使用原始 usage 时会再次得到同一展示结果；已经
// 改写过的帧不会被二次扣减。
func applyOpenAIUsageCacheHitSnapshot(usage *OpenAIUsage, adjustment CacheHitTargetAdjustment) {
	if usage == nil || adjustment.ShiftedTokens <= 0 {
		return
	}
	originalCacheRead := max(adjustment.OriginalCacheReadTokens, 0)
	adjustedCacheRead := max(originalCacheRead-adjustment.ShiftedTokens, 0)
	if usage.CacheReadInputTokens == adjustedCacheRead {
		return
	}
	if usage.CacheReadInputTokens == originalCacheRead {
		usage.CacheReadInputTokens = adjustedCacheRead
		return
	}
	if usage.CacheReadInputTokens > adjustedCacheRead {
		usage.CacheReadInputTokens = max(usage.CacheReadInputTokens-adjustment.ShiftedTokens, 0)
	}
}

// applyClaudeUsageCacheHitSnapshot 用于 OpenAI 入站协议经通用网关转换的路径。
// ClaudeUsage 的 InputTokens 是不含缓存的普通输入，因此减少 cache_read 时需要
// 同量增加普通输入，转换后的 OpenAI prompt/input 总量才保持不变。
func applyClaudeUsageCacheHitSnapshot(usage *ClaudeUsage, adjustment CacheHitTargetAdjustment) {
	if usage == nil || adjustment.ShiftedTokens <= 0 {
		return
	}
	originalCacheRead := max(adjustment.OriginalCacheReadTokens, 0)
	adjustedCacheRead := max(originalCacheRead-adjustment.ShiftedTokens, 0)
	if usage.CacheReadInputTokens == adjustedCacheRead {
		return
	}
	if usage.CacheReadInputTokens == originalCacheRead {
		usage.CacheReadInputTokens = adjustedCacheRead
		usage.InputTokens += adjustment.ShiftedTokens
		return
	}
	if usage.CacheReadInputTokens > adjustedCacheRead {
		usage.CacheReadInputTokens = max(usage.CacheReadInputTokens-adjustment.ShiftedTokens, 0)
		usage.InputTokens += adjustment.ShiftedTokens
	}
}

// applyOpenAIResponsesUsageCacheHitSnapshot 将快照映射到 Responses API 的
// input_tokens_details.cached_tokens。Responses 的 input_tokens/total_tokens
// 已经是包含缓存的总量，因此这里只改明细，不改总数。
func applyOpenAIResponsesUsageCacheHitSnapshot(usage *apicompat.ResponsesUsage, adjustment CacheHitTargetAdjustment) {
	if usage == nil || adjustment.ShiftedTokens <= 0 {
		return
	}
	originalCacheRead := max(adjustment.OriginalCacheReadTokens, 0)
	adjustedCacheRead := max(originalCacheRead-adjustment.ShiftedTokens, 0)
	if usage.InputTokensDetails == nil {
		if originalCacheRead <= 0 {
			return
		}
		usage.InputTokensDetails = &apicompat.ResponsesInputTokensDetails{CachedTokens: originalCacheRead}
	}
	current := usage.InputTokensDetails.CachedTokens
	if current == adjustedCacheRead {
		return
	}
	if current == originalCacheRead {
		usage.InputTokensDetails.CachedTokens = adjustedCacheRead
		return
	}
	if current > adjustedCacheRead {
		usage.InputTokensDetails.CachedTokens = max(current-adjustment.ShiftedTokens, 0)
	}
}

// applyOpenAIChatUsageCacheHitSnapshot 将快照映射到 Chat Completions 的
// prompt_tokens_details.cached_tokens。总 prompt/total token 保持上游原值。
func applyOpenAIChatUsageCacheHitSnapshot(usage *apicompat.ChatUsage, adjustment CacheHitTargetAdjustment) {
	if usage == nil || adjustment.ShiftedTokens <= 0 {
		return
	}
	originalCacheRead := max(adjustment.OriginalCacheReadTokens, 0)
	adjustedCacheRead := max(originalCacheRead-adjustment.ShiftedTokens, 0)
	if usage.PromptTokensDetails == nil {
		if originalCacheRead <= 0 {
			return
		}
		usage.PromptTokensDetails = &apicompat.ChatTokenDetails{CachedTokens: originalCacheRead}
	}
	current := usage.PromptTokensDetails.CachedTokens
	if current == adjustedCacheRead {
		return
	}
	if current == originalCacheRead {
		usage.PromptTokensDetails.CachedTokens = adjustedCacheRead
		return
	}
	if current > adjustedCacheRead {
		usage.PromptTokensDetails.CachedTokens = max(current-adjustment.ShiftedTokens, 0)
	}
}

// applyAnthropicResponsesStateCacheHitSnapshot 保证 Anthropic→OpenAI 流式转换器
// 的终止 usage 与内部 ClaudeUsage 使用同一划拨快照。该操作幂等，兼容重复终止帧。
func applyAnthropicResponsesStateCacheHitSnapshot(state *apicompat.AnthropicEventToResponsesState, adjustment CacheHitTargetAdjustment) {
	if state == nil || adjustment.ShiftedTokens <= 0 {
		return
	}
	originalCacheRead := max(adjustment.OriginalCacheReadTokens, 0)
	adjustedCacheRead := max(originalCacheRead-adjustment.ShiftedTokens, 0)
	if state.CacheReadInputTokens == adjustedCacheRead {
		return
	}
	if state.CacheReadInputTokens == originalCacheRead {
		state.CacheReadInputTokens = adjustedCacheRead
		state.InputTokens += adjustment.ShiftedTokens
		return
	}
	if state.CacheReadInputTokens > adjustedCacheRead {
		shift := min(adjustment.ShiftedTokens, state.CacheReadInputTokens-adjustedCacheRead)
		state.CacheReadInputTokens -= shift
		state.InputTokens += shift
	}
}

// openAIUsageFromChatUsage 将 Chat Completions usage 归一化成内部 OpenAI
// usage。通过 apicompat 转换可以保留其私有的 cache-creation 别名字段。
func openAIUsageFromChatUsage(usage *apicompat.ChatUsage) OpenAIUsage {
	if usage == nil {
		return OpenAIUsage{}
	}
	return copyOpenAIUsageFromResponsesUsage(apicompat.ChatUsageToResponsesUsage(usage))
}

func responsesUsageFromOpenAIUsage(usage OpenAIUsage) *apicompat.ResponsesUsage {
	result := &apicompat.ResponsesUsage{
		InputTokens:              usage.InputTokens,
		OutputTokens:             usage.OutputTokens,
		TotalTokens:              usage.InputTokens + usage.OutputTokens,
		CacheCreationInputTokens: usage.CacheCreationInputTokens,
	}
	if usage.CacheReadInputTokens > 0 {
		result.InputTokensDetails = &apicompat.ResponsesInputTokensDetails{
			CachedTokens: usage.CacheReadInputTokens,
		}
	}
	return result
}

// syncOpenAIUsageToResponsesUsage 将流中累计的完整 token 口径写回终态对象。
// 某些兼容上游会在早期事件给出完整 usage，却在 completed/done 中只放空对象；
// Responses→Chat 转换器只读取终态对象，因此必须先同步再应用划拨快照。
func syncOpenAIUsageToResponsesUsage(dst *apicompat.ResponsesUsage, usage OpenAIUsage) {
	if dst == nil {
		return
	}
	dst.InputTokens = usage.InputTokens
	dst.OutputTokens = usage.OutputTokens
	dst.TotalTokens = usage.InputTokens + usage.OutputTokens
	dst.CacheCreationInputTokens = usage.CacheCreationInputTokens
	if usage.CacheReadInputTokens > 0 {
		if dst.InputTokensDetails == nil {
			dst.InputTokensDetails = &apicompat.ResponsesInputTokensDetails{}
		}
		dst.InputTokensDetails.CachedTokens = usage.CacheReadInputTokens
	} else if dst.InputTokensDetails != nil {
		dst.InputTokensDetails.CachedTokens = 0
	}
}

// openAIStreamUsagePayloadCanExposeAdjustment 在推进累计 tracker 前确认缓存
// 明细能够写回下游。无缓存命中时不需要改字段，仍可记录本次请求到累计分母。
func openAIStreamUsagePayloadCanExposeAdjustment(body []byte, usage OpenAIUsage) bool {
	if usage.CacheReadInputTokens <= 0 {
		return true
	}
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return false
	}
	usagePaths := []string{"usage", "response.usage", "data.usage", "data.response.usage"}
	cacheReadPaths := []string{
		"input_tokens_details.cached_tokens",
		"prompt_tokens_details.cached_tokens",
		"cache_read_input_tokens",
		"cache_read_tokens",
		"cached_tokens",
	}
	for _, usagePath := range usagePaths {
		if _, ok := openAIUsageFromGJSON(gjson.GetBytes(body, usagePath)); !ok {
			continue
		}
		for _, cacheReadPath := range cacheReadPaths {
			if gjson.GetBytes(body, usagePath+"."+cacheReadPath).Exists() {
				return true
			}
		}
	}
	return false
}

// adjustAndRewriteOpenAIStreamUsagePayload 先用原 usage 对同一 payload 做可写
// 预检，再推进请求级 tracker。Redis 状态一旦推进无法回滚，因此调用方只能在
// 已确认成功终态且客户端仍连接时调用本函数。
func (s *OpenAIGatewayService) adjustAndRewriteOpenAIStreamUsagePayload(
	ctx context.Context,
	c *gin.Context,
	body []byte,
	usage OpenAIUsage,
) ([]byte, OpenAIUsage, *CacheHitTargetAdjustment, error) {
	if !openAIStreamUsagePayloadCanExposeAdjustment(body, usage) {
		return body, usage, nil, nil
	}
	if _, err := rewriteOpenAIStreamUsagePayload(body, usage); err != nil {
		return body, usage, nil, err
	}
	// Redis 状态一旦推进就不能回滚；同一原始 payload 先验证非标准归因字段
	// 也能安全删除，避免划拨成功后再因 JSON 改写失败向下游回退原始 usage。
	if _, err := stripOpenAIStreamUsageAttribution(body); err != nil {
		return body, usage, nil, err
	}

	adjustedUsage := usage
	adjustment, _ := s.AdjustOpenAIStreamUsage(ctx, c, &adjustedUsage)
	if adjustment == nil {
		return body, usage, nil, nil
	}
	rewritten, err := rewriteOpenAIStreamUsagePayload(body, adjustedUsage)
	if err != nil {
		// 前面的同 payload 预检已验证全部目标路径可写；保留错误用于暴露
		// 不可预期的 sjson 行为，绝不能静默回退为下游/本站不一致。
		return body, usage, nil, err
	}
	if adjustment.ShiftedTokens > 0 {
		rewritten, err = stripOpenAIStreamUsageAttribution(rewritten)
		if err != nil {
			return body, usage, nil, err
		}
	}
	return rewritten, adjustedUsage, adjustment, nil
}

// adjustOpenAIResponsesUsage 在 Responses→Chat 等桥接路径中复用统一的
// OpenAI 快照逻辑，并把改写后的缓存明细写回协议对象。
func (s *OpenAIGatewayService) adjustOpenAIResponsesUsage(
	ctx context.Context,
	c *gin.Context,
	usage *apicompat.ResponsesUsage,
) (*CacheHitTargetAdjustment, error) {
	if usage == nil {
		return nil, nil
	}
	openAIUsage := copyOpenAIUsageFromResponsesUsage(usage)
	adjustment, err := s.AdjustOpenAIStreamUsage(ctx, c, &openAIUsage)
	if adjustment != nil {
		applyOpenAIResponsesUsageCacheHitSnapshot(usage, *adjustment)
	}
	return adjustment, err
}

func adjustOpenAIUsageForOpenAIStream(
	ctx context.Context,
	c *gin.Context,
	usage *OpenAIUsage,
	tracker any,
) (*CacheHitTargetAdjustment, error) {
	if usage == nil {
		return nil, nil
	}
	apiKey := getAPIKeyFromContext(c)
	state, ok := openAIStreamCacheHitStateForAdjustment(c, apiKey)
	if !ok {
		return nil, nil
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	if state.openAIUsageSnapshot != nil {
		*usage = *state.openAIUsageSnapshot
		adjustment := state.adjustment
		return &adjustment, state.err
	}

	actualInput := usage.InputTokens - usage.CacheCreationInputTokens - usage.CacheReadInputTokens
	if actualInput < 0 {
		actualInput = 0
	}
	adjustment, err, eligible := adjustOpenAIStreamCacheHitOnceLocked(ctx, c, UsageTokens{
		InputTokens:         actualInput,
		CacheCreationTokens: usage.CacheCreationInputTokens,
		CacheReadTokens:     usage.CacheReadInputTokens,
	}, apiKey, tracker, state)
	if !eligible {
		return nil, err
	}
	applyOpenAIUsageCacheHitSnapshot(usage, adjustment)
	snapshot := *usage
	state.openAIUsageSnapshot = &snapshot
	return &adjustment, err
}

func adjustClaudeUsageForOpenAIStreamWithTracker(
	ctx context.Context,
	c *gin.Context,
	usage *ClaudeUsage,
	tracker any,
) (*CacheHitTargetAdjustment, error) {
	if usage == nil {
		return nil, nil
	}
	apiKey := getAPIKeyFromContext(c)
	state, ok := openAIStreamCacheHitStateForAdjustment(c, apiKey)
	if !ok {
		return nil, nil
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	if state.claudeUsageSnapshot != nil {
		*usage = *state.claudeUsageSnapshot
		adjustment := state.adjustment
		return &adjustment, state.err
	}

	adjustment, err, eligible := adjustOpenAIStreamCacheHitOnceLocked(ctx, c, UsageTokens{
		InputTokens:         usage.InputTokens,
		CacheCreationTokens: usage.CacheCreationInputTokens,
		CacheReadTokens:     usage.CacheReadInputTokens,
	}, apiKey, tracker, state)
	if !eligible {
		return nil, err
	}
	applyClaudeUsageCacheHitSnapshot(usage, adjustment)
	snapshot := *usage
	state.claudeUsageSnapshot = &snapshot
	return &adjustment, err
}

// AdjustOpenAIStreamUsage 在 OpenAI 协议终止 usage 可改写时计算或复用划拨。
// OpenAI 原生 usage 的 input_tokens 已包含缓存 token，因此只修改缓存明细；
// 重复成功终态会整份复用首次调整后的 usage，而不是对新计数重复应用 shift。
func (s *OpenAIGatewayService) AdjustOpenAIStreamUsage(ctx context.Context, c *gin.Context, usage *OpenAIUsage) (*CacheHitTargetAdjustment, error) {
	return adjustOpenAIUsageForOpenAIStream(ctx, c, usage, s.cache)
}

func (s *OpenAIGatewayService) adjustOpenAIStreamUsage(ctx context.Context, c *gin.Context, usage *OpenAIUsage) (*CacheHitTargetAdjustment, error) {
	return s.AdjustOpenAIStreamUsage(ctx, c, usage)
}

// AdjustOpenAIStreamUsage 在通用网关承接 OpenAI 协议流式请求时计算或复用划拨。
func (s *GatewayService) AdjustOpenAIStreamUsage(ctx context.Context, c *gin.Context, usage *ClaudeUsage) (*CacheHitTargetAdjustment, error) {
	return adjustClaudeUsageForOpenAIStreamWithTracker(ctx, c, usage, s.cache)
}
