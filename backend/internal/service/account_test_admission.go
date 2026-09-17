package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

// AccountTestObservation 只保存本次测试可以验证的恢复边界，不包含凭据。
type AccountTestObservation struct {
	AccountID          int64
	UpdatedAt          time.Time
	StartedAt          time.Time
	ClearError         bool
	ClearRateLimit     bool
	ClearOverload      bool
	ClearTempUnsched   bool
	ModelRateLimitKeys []string
	Succeeded          bool
	PendingExtra       map[string]any
	extraHandled       bool
}

type accountTestObservationKey struct{}

// SuccessfulTestRecoveryRepository 使用一次原子比较更新，拒绝清除观测之后的新状态。
type SuccessfulTestRecoveryRepository interface {
	RecoverAccountTestIfUnchanged(context.Context, *AccountTestObservation) (*SuccessfulTestRecoveryResult, error)
}

type accountTestObservationRepository interface {
	SaveAccountTestExtraIfUnchanged(context.Context, *AccountTestObservation) error
}

func (s *AccountTestService) SetConcurrencyService(concurrency *ConcurrencyService) {
	if s != nil {
		s.concurrencyService = concurrency
	}
}

func (s *AccountTestService) acquireTestAccountSlot(ctx context.Context, account *Account) (func(), error) {
	// 独立适配器单测可以不注入；正式服务由 Wire 接入业务使用的同一实例。
	if s.concurrencyService == nil {
		return func() {}, nil
	}
	result, err := s.concurrencyService.AcquireAccountSlot(ctx, account.ID, account.Concurrency)
	if err != nil {
		return nil, fmt.Errorf("测试并发准入不可用: %w", err)
	}
	if !result.Acquired {
		return nil, errors.New("账号业务并发已满，请等待空闲后重新测试")
	}
	return result.ReleaseFunc, nil
}

func accountTestCooldown(ctx context.Context, account *Account, model string, now time.Time) error {
	until, reason := now, ""
	for _, limit := range []struct {
		until  *time.Time
		reason string
	}{
		{account.RateLimitResetAt, "账号限流冷却尚未结束"},
		{account.OverloadUntil, "账号过载冷却尚未结束"},
		{account.TempUnschedulableUntil, "账号临时暂停尚未结束"},
	} {
		if limit.until != nil && limit.until.After(until) {
			until, reason = *limit.until, limit.reason
		}
	}
	for _, key := range accountTestModelRateLimitKeys(ctx, account, model) {
		if reset := account.modelRateLimitResetAt(key); reset != nil && reset.After(until) {
			until, reason = *reset, "所选模型限流冷却尚未结束"
		}
	}
	if reason != "" {
		return fmt.Errorf("%s；请在 %s 后重新测试", reason, until.Local().Format("2006-01-02 15:04:05"))
	}
	return nil
}

func newAccountTestObservation(ctx context.Context, account *Account, model string, now time.Time) *AccountTestObservation {
	expired := func(value *time.Time) bool { return value != nil && !value.After(now) }
	observation := &AccountTestObservation{
		AccountID: account.ID, UpdatedAt: account.UpdatedAt, StartedAt: now,
		ClearError:       account.Status == StatusError,
		ClearRateLimit:   expired(account.RateLimitResetAt),
		ClearOverload:    expired(account.OverloadUntil),
		ClearTempUnsched: expired(account.TempUnschedulableUntil),
	}
	for _, key := range accountTestModelRateLimitKeys(ctx, account, model) {
		if expired(account.modelRateLimitResetAt(key)) {
			observation.ModelRateLimitKeys = append(observation.ModelRateLimitKeys, key)
		}
	}
	return observation
}

func accountTestModelRateLimitKeys(ctx context.Context, account *Account, model string) []string {
	keys := account.modelRateLimitKeysForRequest(ctx, model)
	if account.UsesOpenAICodexProtocol() {
		// Codex 测试会在模型映射后归一化别名，不能用别名绕过实际模型的冷却。
		canonical := normalizeOpenAIModelForUpstream(account, account.GetMappedModel(model))
		for _, key := range keys {
			if key == canonical {
				return keys
			}
		}
		keys = append(keys, canonical)
	}
	return keys
}

// accountTestRequestedModel 与各平台测试入口的默认值一致，映射仍由原适配器负责。
func accountTestRequestedModel(account *Account, model, mode string) string {
	model = strings.TrimSpace(model)
	if account.Platform == PlatformGrok {
		switch normalizeGrokAccountTestMode(mode) {
		case AccountTestModeGrokSearch:
			return "grok-web-search"
		case AccountTestModeGrokTTS:
			return "grok-voice-tts"
		case AccountTestModeGrokSTT:
			return "grok-voice-stt"
		case AccountTestModeGrokRealtime:
			if model == "" {
				return defaultGrokRealtimeTestModel
			}
		case AccountTestModeGrokImage:
			if model == "" {
				return "grok-imagine-image"
			}
		case AccountTestModeGrokVideo:
			if model == "" {
				return "grok-imagine-video"
			}
		}
		if model == "" {
			return grokDefaultResponsesModel
		}
	}
	if model != "" {
		return model
	}
	switch {
	case account.IsCNProvider():
		if account.GetAPIProtocol() == APIProtocolAnthropic {
			return claude.DefaultTestModel
		}
		return openai.DefaultTestModel
	case account.IsOpenAI():
		return openai.DefaultTestModel
	case account.IsGemini():
		return geminicli.DefaultTestModel
	case account.IsOpenCodeGo():
		return DefaultOpenCodeGoTestModel
	case account.Platform == PlatformAntigravity && account.Type != AccountTypeAPIKey:
		return antigravityConnectionTestModel("")
	default:
		return claude.DefaultTestModel
	}
}

func (s *AccountTestService) persistAccountTestExtra(ctx context.Context, accountID int64, updates map[string]any) {
	observation, _ := ctx.Value(accountTestObservationKey{}).(*AccountTestObservation)
	if observation == nil || observation.AccountID != accountID {
		_ = s.accountRepo.UpdateExtra(ctx, accountID, updates)
		return
	}
	if observation.PendingExtra == nil {
		observation.PendingExtra = make(map[string]any)
	}
	for key, value := range updates {
		observation.PendingExtra[key] = value
	}
}

func (s *AccountTestService) flushAccountTestExtra(ctx context.Context, observation *AccountTestObservation) {
	if len(observation.PendingExtra) == 0 || observation.extraHandled {
		return
	}
	// 失败或未开启自动恢复时，观测也必须遵守同一行版本，避免覆盖较新的业务用量。
	if repo, ok := s.accountRepo.(accountTestObservationRepository); ok {
		_ = repo.SaveAccountTestExtraIfUnchanged(ctx, observation)
	}
}
