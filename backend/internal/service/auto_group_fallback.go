package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
)

const maxAutoGroupFallbackHops = 8

type autoGroupFallbackContextKey struct{}
type autoGroupFallbackDisabledContextKey struct{}
type autoGroupFallbackMessagesModelContextKey struct{}

type autoGroupFallbackState struct {
	apiKey  *APIKey
	visited map[int64]struct{}
	hops    int
}

// WithAutoGroupFallbackState 为单次请求保存自动承接状态。APIKey 来自认证快照，
// 仅属于当前请求；承接后原地替换其有效分组，让后续粘性会话和计费读取同一结果。
func WithAutoGroupFallbackState(ctx context.Context, apiKey *APIKey) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if apiKey == nil {
		return ctx
	}
	state := &autoGroupFallbackState{
		apiKey:  apiKey,
		visited: make(map[int64]struct{}),
	}
	if apiKey.GroupID != nil && *apiKey.GroupID > 0 {
		state.visited[*apiKey.GroupID] = struct{}{}
	}
	return context.WithValue(ctx, autoGroupFallbackContextKey{}, state)
}

// WithoutAutoGroupFallback 禁止内部派生请求修改外层请求的有效计费分组。
func WithoutAutoGroupFallback(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, autoGroupFallbackDisabledContextKey{}, true)
}

// WithAutoGroupFallbackMessagesModel 保存 Messages 客户端请求模型，使每个承接组
// 都能按自身的分组映射重新计算实际调度模型。
func WithAutoGroupFallbackMessagesModel(ctx context.Context, requestedModel string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" {
		return ctx
	}
	return context.WithValue(ctx, autoGroupFallbackMessagesModelContextKey{}, requestedModel)
}

type modelAvailabilityDiagnosisFunc func(context.Context, *int64, string, string) ModelAvailabilityDiagnosis

func autoGroupFallbackRoutingModel(ctx context.Context, currentGroupID *int64, defaultModel string) string {
	if ctx == nil {
		return defaultModel
	}
	requestedModel, _ := ctx.Value(autoGroupFallbackMessagesModelContextKey{}).(string)
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" {
		return defaultModel
	}

	state, _ := ctx.Value(autoGroupFallbackContextKey{}).(*autoGroupFallbackState)
	if state == nil || state.apiKey == nil || state.apiKey.Group == nil || currentGroupID == nil ||
		state.apiKey.Group.ID != *currentGroupID {
		return defaultModel
	}
	if mappedModel := strings.TrimSpace(state.apiKey.Group.ResolveMessagesDispatchModel(requestedModel)); mappedModel != "" {
		return mappedModel
	}
	return NormalizeOpenAICompatRequestedModel(requestedModel)
}

func autoGroupFallbackDiagnosisPlatform(ctx context.Context, groupPlatform string) string {
	if ctx == nil {
		return groupPlatform
	}
	if forcePlatform, ok := ctx.Value(ctxkey.ForcePlatform).(string); ok && strings.TrimSpace(forcePlatform) != "" {
		return strings.TrimSpace(forcePlatform)
	}
	if platform, ok := ResolvedTargetPlatformFromContext(ctx); ok {
		return platform
	}
	return groupPlatform
}

func isAutoGroupFallbackSelectionError(err error) bool {
	if err == nil || !errors.Is(err, ErrNoAvailableAccounts) || errors.Is(err, ErrNoAvailableCompactAccounts) {
		return false
	}
	return !strings.Contains(strings.ToLower(err.Error()), "channel pricing restriction")
}

func advanceAutoGroupFallback(
	ctx context.Context,
	groupRepo GroupRepository,
	currentGroupID *int64,
	requestedModel string,
	diagnose modelAvailabilityDiagnosisFunc,
) (*int64, bool) {
	if ctx == nil || ctx.Err() != nil || groupRepo == nil || diagnose == nil {
		return currentGroupID, false
	}
	if disabled, _ := ctx.Value(autoGroupFallbackDisabledContextKey{}).(bool); disabled {
		return currentGroupID, false
	}
	state, _ := ctx.Value(autoGroupFallbackContextKey{}).(*autoGroupFallbackState)
	if state == nil || state.apiKey == nil || !state.apiKey.AutoGroupFallbackEnabled {
		return currentGroupID, false
	}
	apiKey := state.apiKey
	if currentGroupID == nil || apiKey.GroupID == nil || *currentGroupID <= 0 || *apiKey.GroupID != *currentGroupID {
		return currentGroupID, false
	}
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" || apiKey.Group == nil || apiKey.Group.ID != *currentGroupID {
		return currentGroupID, false
	}
	source := apiKey.Group
	if source.Status != StatusActive || source.SubscriptionType != SubscriptionTypeStandard || source.AutoFallbackGroupID == nil {
		return currentGroupID, false
	}

	// 没有持久化账号池时无法证明模型属于当前分组；模型不属于当前组时也不得承接。
	diagnosis := diagnose(ctx, currentGroupID, requestedModel, autoGroupFallbackDiagnosisPlatform(ctx, source.Platform))
	if !diagnosis.HasAccountsInPool || !diagnosis.HasModelSupport {
		return currentGroupID, false
	}
	if state.hops >= maxAutoGroupFallbackHops {
		return currentGroupID, false
	}

	targetID := *source.AutoFallbackGroupID
	if targetID <= 0 {
		return currentGroupID, false
	}
	if _, exists := state.visited[targetID]; exists {
		return currentGroupID, false
	}
	target, err := groupRepo.GetByIDLite(ctx, targetID)
	if err != nil || target == nil || target.ID != targetID {
		return currentGroupID, false
	}
	if target.Status != StatusActive || target.Platform != source.Platform || target.SubscriptionType != SubscriptionTypeStandard {
		return currentGroupID, false
	}
	if apiKey.User != nil && !apiKey.User.CanBindGroup(target.ID, target.IsExclusive) {
		return currentGroupID, false
	}

	state.visited[targetID] = struct{}{}
	state.hops++
	effectiveGroupID := targetID
	apiKey.GroupID = &effectiveGroupID
	apiKey.Group = target
	slog.Info("auto group fallback activated",
		"api_key_id", apiKey.ID,
		"source_group_id", source.ID,
		"target_group_id", targetID,
		"model", requestedModel,
		"hop", state.hops,
	)
	return apiKey.GroupID, true
}
