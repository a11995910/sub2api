package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"golang.org/x/sync/singleflight"
)

var (
	ErrRegistrationDisabled   = infraerrors.Forbidden("REGISTRATION_DISABLED", "registration is currently disabled")
	ErrSettingNotFound        = infraerrors.NotFound("SETTING_NOT_FOUND", "setting not found")
	ErrDefaultSubGroupInvalid = infraerrors.BadRequest(
		"DEFAULT_SUBSCRIPTION_GROUP_INVALID",
		"default subscription group must exist and be subscription type",
	)
	ErrDefaultSubGroupDuplicate = infraerrors.BadRequest(
		"DEFAULT_SUBSCRIPTION_GROUP_DUPLICATE",
		"default subscription group cannot be duplicated",
	)
	ErrAPIKeyDefaultGroupInvalid = infraerrors.BadRequest(
		"API_KEY_DEFAULT_GROUP_INVALID",
		"api key default group must exist and be active",
	)
	ErrAffiliateSubscriptionRewardGroupInvalid = infraerrors.BadRequest(
		"AFFILIATE_SUBSCRIPTION_REWARD_GROUP_INVALID",
		"affiliate reward group must exist and be active subscription or exclusive standard group",
	)
)

type SettingRepository interface {
	Get(ctx context.Context, key string) (*Setting, error)
	GetValue(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string) error
	GetMultiple(ctx context.Context, keys []string) (map[string]string, error)
	SetMultiple(ctx context.Context, settings map[string]string) error
	GetAll(ctx context.Context) (map[string]string, error)
	Delete(ctx context.Context, key string) error
}

// cachedVersionBounds 缓存 Claude Code 版本号上下限（进程内缓存，60s TTL）

// 空字符串 = 不检查
// 空字符串 = 不检查
// unix nano

// versionBoundsCache 版本号上下限进程内缓存
// *cachedVersionBounds

// versionBoundsSF 防止缓存过期时 thundering herd

// versionBoundsCacheTTL 缓存有效期

// versionBoundsErrorTTL DB 错误时的短缓存，快速重试

// versionBoundsDBTimeout singleflight 内 DB 查询超时，独立于请求 context

// cachedBackendMode Backend Mode cache (in-process, 60s TTL)

// unix nano

// *cachedBackendMode

// cachedGatewayForwardingSettings 缓存网关转发行为设置（进程内缓存，60s TTL）

// unix nano

// *cachedGatewayForwardingSettings

// cachedAntigravityUserAgentVersion 缓存 Antigravity UA 版本号（进程内缓存，60s TTL）

// unix nano

// DefaultOpenAICodexUserAgent OpenAI Codex 默认 User-Agent（用于规避 Cloudflare 对浏览器 UA 的质询）

// cachedOpenAICodexUserAgent 缓存 OpenAI Codex UA（进程内缓存，60s TTL）

// unix nano

// cachedCodexRestrictionPolicy codex_cli_only 全局加固策略缓存（进程内，60s TTL）。
// GetCodexRestrictionPolicy 在每个 codex_cli_only 账号的网关请求热路径上被调用，避免每次访问 DB。

// unix nano

// cachedCyberSessionBlockRuntime cyber 会话屏蔽开关+TTL 进程内缓存（60s TTL）。
// GetCyberSessionBlockRuntime 在网关请求热路径上被调用，避免每次访问 DB。

// unix nano

// SettingsGroupReader validates group references used by default subscriptions.
type SettingsGroupReader interface {
	GetByID(ctx context.Context, id int64) (*Group, error)
}

// WebSearchManagerBuilder creates a websearch.Manager from config (injected by infra layer).
// proxyURLs maps proxy ID to resolved URL for provider-level proxy support.
type WebSearchManagerBuilder func(cfg *WebSearchEmulationConfig, proxyURLs map[int64]string)

// SettingService 系统设置服务
type SettingService struct {
	settingRepo                 SettingRepository
	settingsGroupReader         SettingsGroupReader
	proxyRepo                   ProxyRepository // for resolving websearch provider proxy URLs
	cfg                         *config.Config
	onUpdate                    func() // Callback when settings are updated (for cache invalidation)
	version                     string // Application version
	webSearchManagerBuilder     WebSearchManagerBuilder
	antigravityUAVersionCache   atomic.Value // *cachedAntigravityUserAgentVersion
	antigravityUAVersionSF      singleflight.Group
	openAICodexUACache          atomic.Value // *cachedOpenAICodexUserAgent
	openAICodexUASF             singleflight.Group
	codexRestrictionPolicyCache atomic.Value // *cachedCodexRestrictionPolicy
	codexRestrictionPolicySF    singleflight.Group

	cyberSessionBlockRuntimeCache atomic.Value // *cachedCyberSessionBlockRuntime
	cyberSessionBlockRuntimeSF    singleflight.Group

	// openAIQuotaAutoPauseSettingsCache holds the most recently observed quota auto-pause
	// settings. GetOpenAIQuotaAutoPauseSettings reads this atomic.Value on the request hot
	// path without ever blocking on the DB; when the cached entry expires, a background
	// goroutine refreshes it via openAIQuotaAutoPauseSettingsSF (stale-while-revalidate).
	// This per-service field also gives tests natural isolation — each SettingService
	// instance owns its own cache, no shared package-level state.
	openAIQuotaAutoPauseSettingsCache atomic.Value // *cachedOpenAIQuotaAutoPauseSettings
	openAIQuotaAutoPauseSettingsSF    singleflight.Group
}

// DefaultPlatformQuotaSetting 单 platform 三档限额（nil = 沿用上层；0 = 显式禁用；>0 = 上限）
type DefaultPlatformQuotaSetting struct {
	DailyLimitUSD   *float64 `json:"daily"`
	WeeklyLimitUSD  *float64 `json:"weekly"`
	MonthlyLimitUSD *float64 `json:"monthly"`
}

type ProviderDefaultGrantSettings struct {
	Balance          float64
	Concurrency      int
	Subscriptions    []DefaultSubscriptionSetting
	GrantOnSignup    bool
	GrantOnFirstBind bool
	PlatformQuotas   map[string]*DefaultPlatformQuotaSetting // key = platform name
}

type AuthSourceDefaultSettings struct {
	Email                        ProviderDefaultGrantSettings
	LinuxDo                      ProviderDefaultGrantSettings
	OIDC                         ProviderDefaultGrantSettings
	WeChat                       ProviderDefaultGrantSettings
	GitHub                       ProviderDefaultGrantSettings
	Google                       ProviderDefaultGrantSettings
	DingTalk                     ProviderDefaultGrantSettings
	ForceEmailOnThirdPartySignup bool
}

type authSourceDefaultKeySet struct {
	// source 是 auth source 标识（如 "email"、"github"），仅用于 parse 时
	// slog.Warn 诊断输出，不再参与 key 拼接（platformQuotas 字段已存完整 key）。
	source           string
	balance          string
	concurrency      string
	subscriptions    string
	grantOnSignup    string
	grantOnFirstBind string
	platformQuotas   string // SettingKeyAuthSourcePlatformQuotas(source)
}

var (
	emailAuthSourceDefaultKeys = authSourceDefaultKeySet{
		source:           "email",
		balance:          SettingKeyAuthSourceDefaultEmailBalance,
		concurrency:      SettingKeyAuthSourceDefaultEmailConcurrency,
		subscriptions:    SettingKeyAuthSourceDefaultEmailSubscriptions,
		grantOnSignup:    SettingKeyAuthSourceDefaultEmailGrantOnSignup,
		grantOnFirstBind: SettingKeyAuthSourceDefaultEmailGrantOnFirstBind,
		platformQuotas:   SettingKeyAuthSourcePlatformQuotas("email"),
	}
	linuxDoAuthSourceDefaultKeys = authSourceDefaultKeySet{
		source:           "linuxdo",
		balance:          SettingKeyAuthSourceDefaultLinuxDoBalance,
		concurrency:      SettingKeyAuthSourceDefaultLinuxDoConcurrency,
		subscriptions:    SettingKeyAuthSourceDefaultLinuxDoSubscriptions,
		grantOnSignup:    SettingKeyAuthSourceDefaultLinuxDoGrantOnSignup,
		grantOnFirstBind: SettingKeyAuthSourceDefaultLinuxDoGrantOnFirstBind,
		platformQuotas:   SettingKeyAuthSourcePlatformQuotas("linuxdo"),
	}
	oidcAuthSourceDefaultKeys = authSourceDefaultKeySet{
		source:           "oidc",
		balance:          SettingKeyAuthSourceDefaultOIDCBalance,
		concurrency:      SettingKeyAuthSourceDefaultOIDCConcurrency,
		subscriptions:    SettingKeyAuthSourceDefaultOIDCSubscriptions,
		grantOnSignup:    SettingKeyAuthSourceDefaultOIDCGrantOnSignup,
		grantOnFirstBind: SettingKeyAuthSourceDefaultOIDCGrantOnFirstBind,
		platformQuotas:   SettingKeyAuthSourcePlatformQuotas("oidc"),
	}
	weChatAuthSourceDefaultKeys = authSourceDefaultKeySet{
		source:           "wechat",
		balance:          SettingKeyAuthSourceDefaultWeChatBalance,
		concurrency:      SettingKeyAuthSourceDefaultWeChatConcurrency,
		subscriptions:    SettingKeyAuthSourceDefaultWeChatSubscriptions,
		grantOnSignup:    SettingKeyAuthSourceDefaultWeChatGrantOnSignup,
		grantOnFirstBind: SettingKeyAuthSourceDefaultWeChatGrantOnFirstBind,
		platformQuotas:   SettingKeyAuthSourcePlatformQuotas("wechat"),
	}
	gitHubAuthSourceDefaultKeys = authSourceDefaultKeySet{
		source:           "github",
		balance:          SettingKeyAuthSourceDefaultGitHubBalance,
		concurrency:      SettingKeyAuthSourceDefaultGitHubConcurrency,
		subscriptions:    SettingKeyAuthSourceDefaultGitHubSubscriptions,
		grantOnSignup:    SettingKeyAuthSourceDefaultGitHubGrantOnSignup,
		grantOnFirstBind: SettingKeyAuthSourceDefaultGitHubGrantOnFirstBind,
		platformQuotas:   SettingKeyAuthSourcePlatformQuotas("github"),
	}
	googleAuthSourceDefaultKeys = authSourceDefaultKeySet{
		source:           "google",
		balance:          SettingKeyAuthSourceDefaultGoogleBalance,
		concurrency:      SettingKeyAuthSourceDefaultGoogleConcurrency,
		subscriptions:    SettingKeyAuthSourceDefaultGoogleSubscriptions,
		grantOnSignup:    SettingKeyAuthSourceDefaultGoogleGrantOnSignup,
		grantOnFirstBind: SettingKeyAuthSourceDefaultGoogleGrantOnFirstBind,
		platformQuotas:   SettingKeyAuthSourcePlatformQuotas("google"),
	}
	dingTalkAuthSourceDefaultKeys = authSourceDefaultKeySet{
		source:           "dingtalk",
		balance:          SettingKeyAuthSourceDefaultDingTalkBalance,
		concurrency:      SettingKeyAuthSourceDefaultDingTalkConcurrency,
		subscriptions:    SettingKeyAuthSourceDefaultDingTalkSubscriptions,
		grantOnSignup:    SettingKeyAuthSourceDefaultDingTalkGrantOnSignup,
		grantOnFirstBind: SettingKeyAuthSourceDefaultDingTalkGrantOnFirstBind,
		platformQuotas:   SettingKeyAuthSourcePlatformQuotas("dingtalk"),
	}
)

const (
	defaultAuthSourceBalance     = 0
	defaultAuthSourceConcurrency = 5
	defaultWeChatConnectMode     = "open"
	defaultWeChatConnectScopes   = "snsapi_login"
	defaultWeChatConnectFrontend = "/auth/wechat/callback"
	defaultGitHubOAuthAuthorize  = "https://github.com/login/oauth/authorize"
	defaultGitHubOAuthToken      = "https://github.com/login/oauth/access_token"
	defaultGitHubOAuthUserInfo   = "https://api.github.com/user"
	defaultGitHubOAuthEmails     = "https://api.github.com/user/emails"
	defaultGitHubOAuthScopes     = "read:user user:email"
	defaultGitHubOAuthFrontend   = "/auth/oauth/callback"
	defaultGoogleOAuthAuthorize  = "https://accounts.google.com/o/oauth2/v2/auth"
	defaultGoogleOAuthToken      = "https://oauth2.googleapis.com/token"
	defaultGoogleOAuthUserInfo   = "https://openidconnect.googleapis.com/v1/userinfo"
	defaultGoogleOAuthScopes     = "openid email profile"
	defaultGoogleOAuthFrontend   = "/auth/oauth/callback"
	defaultLoginAgreementMode    = "modal"
	defaultLoginAgreementDate    = "2026-03-31"
)

func mustParseSMTPFallbacks(raw string) []SMTPFallbackConfig {
	fallbacks, err := ParseSMTPFallbacks(raw)
	if err != nil {
		slog.Warn("failed to parse smtp_fallbacks, using empty list", "err", err)
		return nil
	}
	return fallbacks
}

// NewSettingService 创建系统设置服务实例
func NewSettingService(settingRepo SettingRepository, cfg *config.Config) *SettingService {
	return &SettingService{
		settingRepo: settingRepo,
		cfg:         cfg,
	}
}

// SetSettingsGroupReader 注入设置保存时用于校验分组存在性和状态的 reader。
func (s *SettingService) SetSettingsGroupReader(reader SettingsGroupReader) {
	s.settingsGroupReader = reader
}

// SetProxyRepository injects a proxy repo for resolving websearch provider proxy URLs.
func (s *SettingService) SetProxyRepository(repo ProxyRepository) {
	s.proxyRepo = repo
}

func (s *SettingService) LoadAPIKeyACLTrustForwardedIPSetting(ctx context.Context) error {
	if s == nil || s.cfg == nil || s.settingRepo == nil {
		return nil
	}
	value, err := s.settingRepo.GetValue(ctx, SettingKeyAPIKeyACLTrustForwardedIP)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			s.cfg.SetTrustForwardedIPForAPIKeyACL(s.cfg.Security.TrustForwardedIPForAPIKeyACL)
			return nil
		}
		return fmt.Errorf("get api key acl forwarded ip setting: %w", err)
	}
	enabled := value == "true"
	s.cfg.SetTrustForwardedIPForAPIKeyACL(enabled)
	return nil
}

// GetAllSettings 获取所有系统设置
func (s *SettingService) GetAllSettings(ctx context.Context) (*SystemSettings, error) {
	settings, err := s.settingRepo.GetAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("get all settings: %w", err)
	}

	return s.parseSettings(settings), nil
}

// channelMonitorIntervalMin / channelMonitorIntervalMax bound the default interval
// (mirrors the monitor-level constraint but lives here so setting_service stays decoupled).

type parsedCheckinSettings struct {
	Enabled       bool
	Content       string
	DailyReward   float64
	ExtraReward4  float64
	ExtraReward16 float64
}

func parseCheckinSettings(settings map[string]string) parsedCheckinSettings {
	content := strings.TrimSpace(settings[SettingKeyCheckinContent])
	if content == "" {
		content = CheckinContentDefault
	}
	dailyReward, hasDailyReward := parseOptionalNonNegativeFloatSetting(settings, SettingKeyCheckinDailyReward)
	if !hasDailyReward {
		// 兼容旧版“随机区间”配置：升级后用旧 max 作为每日固定奖励，
		// 仅在新版字段尚未落库时回退，避免管理员把新版奖励设为 0 后仍显示旧值。
		dailyReward = parseNonNegativeFloatSetting(settings[SettingKeyCheckinRewardMax])
	}
	return parsedCheckinSettings{
		Enabled:       settings[SettingKeyCheckinEnabled] == "true",
		Content:       content,
		DailyReward:   dailyReward,
		ExtraReward4:  parseNonNegativeFloatSetting(settings[SettingKeyCheckinExtraReward4]),
		ExtraReward16: parseNonNegativeFloatSetting(settings[SettingKeyCheckinExtraReward16]),
	}
}

func parseNonNegativeFloatSetting(raw string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return 0
	}
	return v
}

func parseOptionalNonNegativeFloatSetting(settings map[string]string, key string) (float64, bool) {
	raw, ok := settings[key]
	if !ok {
		return 0, false
	}
	return parseNonNegativeFloatSetting(raw), true
}

func normalizeCheckinSettings(settings *SystemSettings) error {
	if settings == nil {
		return nil
	}
	settings.CheckinContent = strings.TrimSpace(settings.CheckinContent)
	if settings.CheckinContent == "" {
		settings.CheckinContent = CheckinContentDefault
	}
	if math.IsNaN(settings.CheckinDailyReward) || math.IsInf(settings.CheckinDailyReward, 0) || settings.CheckinDailyReward < 0 {
		return infraerrors.BadRequest("INVALID_CHECKIN_REWARD", "checkin daily reward must be a non-negative number")
	}
	if math.IsNaN(settings.CheckinExtraReward4) || math.IsInf(settings.CheckinExtraReward4, 0) || settings.CheckinExtraReward4 < 0 {
		return infraerrors.BadRequest("INVALID_CHECKIN_REWARD", "checkin day 4 extra reward must be a non-negative number")
	}
	if math.IsNaN(settings.CheckinExtraReward16) || math.IsInf(settings.CheckinExtraReward16, 0) || settings.CheckinExtraReward16 < 0 {
		return infraerrors.BadRequest("INVALID_CHECKIN_REWARD", "checkin day 16 extra reward must be a non-negative number")
	}
	if settings.CheckinEnabled && settings.CheckinDailyReward <= 0 {
		return infraerrors.BadRequest("INVALID_CHECKIN_REWARD", "checkin daily reward must be greater than 0 when checkin is enabled")
	}
	return nil
}

// ChannelMonitorRuntime is the lightweight view of the channel monitor feature
// consumed by the runner and user-facing handlers.

// AvailableChannelsRuntime is the lightweight view of the available-channels feature
// switch consumed by the user-facing handler.

// SetOnUpdateCallback sets a callback function to be called when settings are updated
// This is used for cache invalidation (e.g., HTML cache in frontend server)
func (s *SettingService) SetOnUpdateCallback(callback func()) {
	s.onUpdate = callback
}

// SetVersion sets the application version for injection into public settings
func (s *SettingService) SetVersion(version string) {
	s.version = version
}

// PublicSettingsInjectionPayload is the JSON shape embedded into HTML as
// `window.__APP_CONFIG__` so the frontend can hydrate feature flags & site
// config before the first XHR finishes.
//
// INVARIANT: every `json` tag here MUST also exist on handler/dto.PublicSettings.
// If you forget a feature-flag field here, the frontend's
// `cachedPublicSettings.xxx_enabled` will be `undefined` on refresh until the
// async `/api/v1/settings/public` call returns — which causes opt-in menus
// (strict `=== true`) to flicker off/on. See
// frontend/src/utils/featureFlags.ts for the matching registry.
//
// A unit test diffs this struct's JSON keys against dto.PublicSettings to catch
// drift automatically (see setting_service_injection_test.go).

// 服务器全局时区（IANA 名称与当前 UTC 偏移），高峰时段等服务端本地时间窗口的展示标注用

// Feature flags — MUST match the opt-in/opt-out registry in
// frontend/src/utils/featureFlags.ts. Missing a field here is the bug
// that hid the "可用渠道" menu on page refresh.

// UpdateSettingsWithAuthSourceDefaultsAndOpenAIFastPolicy 将系统设置、认证来源默认值和
// OpenAI Fast/Flex 策略合并为一次仓储写入，避免后置策略校验或写入失败造成部分保存。
func (s *SettingService) UpdateSettingsWithAuthSourceDefaultsAndOpenAIFastPolicy(
	ctx context.Context,
	settings *SystemSettings,
	authDefaults *AuthSourceDefaultSettings,
	fastPolicy *OpenAIFastPolicySettings,
) error {
	updates, err := s.buildSystemSettingsUpdates(ctx, settings)
	if err != nil {
		return err
	}

	authSourceUpdates, err := s.buildAuthSourceDefaultUpdates(ctx, authDefaults)
	if err != nil {
		return err
	}
	for key, value := range authSourceUpdates {
		updates[key] = value
	}
	if fastPolicy != nil {
		value, err := normalizeAndMarshalOpenAIFastPolicySettings(fastPolicy)
		if err != nil {
			return err
		}
		updates[SettingKeyOpenAIFastPolicySettings] = value
	}

	err = s.settingRepo.SetMultiple(ctx, updates)
	if err == nil {
		s.refreshCachedSettings(settings)
	}
	return err
}

func (s *SettingService) validateAffiliateSubscriptionRewardGroup(ctx context.Context, groupID int64) error {
	if groupID <= 0 || s.settingsGroupReader == nil {
		return nil
	}
	group, err := s.settingsGroupReader.GetByID(ctx, groupID)
	if err != nil {
		if errors.Is(err, ErrGroupNotFound) {
			return ErrAffiliateSubscriptionRewardGroupInvalid.WithMetadata(map[string]string{
				"group_id": strconv.FormatInt(groupID, 10),
			})
		}
		return fmt.Errorf("get affiliate subscription reward group %d: %w", groupID, err)
	}
	if !group.IsActive() || (!group.IsSubscriptionType() && !group.IsExclusive) {
		return ErrAffiliateSubscriptionRewardGroupInvalid.WithMetadata(map[string]string{
			"group_id": strconv.FormatInt(groupID, 10),
		})
	}
	return nil
}

func (s *SettingService) validateAPIKeyDefaultGroup(ctx context.Context, groupID int64) error {
	if groupID <= 0 || s.settingsGroupReader == nil {
		return nil
	}
	group, err := s.settingsGroupReader.GetByID(ctx, groupID)
	if err != nil {
		if errors.Is(err, ErrGroupNotFound) {
			return ErrAPIKeyDefaultGroupInvalid.WithMetadata(map[string]string{
				"group_id": strconv.FormatInt(groupID, 10),
			})
		}
		return fmt.Errorf("get api key default group %d: %w", groupID, err)
	}
	if !group.IsActive() {
		return ErrAPIKeyDefaultGroupInvalid.WithMetadata(map[string]string{
			"group_id": strconv.FormatInt(groupID, 10),
		})
	}
	return nil
}

// GetAffiliateSubscriptionRewardConfig 返回邀请人充值奖励配置。
// groupID 或 days 任一为 0 时表示未启用该奖励。
func (s *SettingService) GetAffiliateSubscriptionRewardConfig(ctx context.Context) (int64, int) {
	if s == nil || s.settingRepo == nil {
		return AffiliateSubscriptionRewardGroupDefault, AffiliateSubscriptionRewardDaysDefault
	}
	groupID := AffiliateSubscriptionRewardGroupDefault
	if raw, err := s.settingRepo.GetValue(ctx, SettingKeyAffiliateSubscriptionRewardGroup); err == nil {
		if parsed, parseErr := strconv.ParseInt(strings.TrimSpace(raw), 10, 64); parseErr == nil && parsed > 0 {
			groupID = parsed
		}
	}
	days := AffiliateSubscriptionRewardDaysDefault
	if raw, err := s.settingRepo.GetValue(ctx, SettingKeyAffiliateSubscriptionRewardDays); err == nil {
		if parsed, parseErr := strconv.Atoi(strings.TrimSpace(raw)); parseErr == nil && parsed > 0 {
			if parsed > AffiliateSubscriptionRewardDaysMax {
				parsed = AffiliateSubscriptionRewardDaysMax
			}
			days = parsed
		}
	}
	return groupID, days
}

func (s *SettingService) GetAPIKeyDefaultGroupID(ctx context.Context) int64 {
	if s == nil || s.settingRepo == nil {
		return 0
	}
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyAPIKeyDefaultGroupID)
	if err != nil {
		return 0
	}
	groupID, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || groupID <= 0 {
		return 0
	}
	return groupID
}

// getStringOrDefault 获取字符串值或默认值
func (s *SettingService) getStringOrDefault(settings map[string]string, key, defaultValue string) string {
	if value, ok := settings[key]; ok && value != "" {
		return value
	}
	return defaultValue
}

// JSON number 进入管理端后由 JavaScript number 承载，超出该范围会发生精度丢失。
const maxJavaScriptSafeInteger = int64(1<<53 - 1)

// SetOpenAIFastPolicySettings 设置 OpenAI fast 策略配置
func normalizeAndMarshalOpenAIFastPolicySettings(settings *OpenAIFastPolicySettings) (string, error) {
	if settings == nil {
		return "", fmt.Errorf("settings cannot be nil")
	}

	validActions := map[string]bool{
		BetaPolicyActionPass: true, BetaPolicyActionFilter: true, BetaPolicyActionBlock: true,
		OpenAIFastPolicyActionForcePriority: true,
	}
	validScopes := map[string]bool{
		BetaPolicyScopeAll: true, BetaPolicyScopeOAuth: true, BetaPolicyScopeAPIKey: true, BetaPolicyScopeBedrock: true,
	}
	validTiers := map[string]bool{
		OpenAIFastTierAny: true, OpenAIFastTierPriority: true, OpenAIFastTierFlex: true,
		"auto": true, "default": true, "scale": true,
	}

	for i, rule := range settings.Rules {
		tier := strings.ToLower(strings.TrimSpace(rule.ServiceTier))
		if tier == "" {
			tier = OpenAIFastTierAny
		}
		if !validTiers[tier] {
			return "", fmt.Errorf("rule[%d]: invalid service_tier %q", i, rule.ServiceTier)
		}
		settings.Rules[i].ServiceTier = tier
		if !validActions[rule.Action] {
			return "", fmt.Errorf("rule[%d]: invalid action %q", i, rule.Action)
		}
		if !validScopes[rule.Scope] {
			return "", fmt.Errorf("rule[%d]: invalid scope %q", i, rule.Scope)
		}
		seenUserIDs := make(map[int64]struct{}, len(rule.UserIDs))
		for j, userID := range rule.UserIDs {
			if userID <= 0 {
				return "", fmt.Errorf("rule[%d]: user_ids[%d] must be positive", i, j)
			}
			if userID > maxJavaScriptSafeInteger {
				return "", fmt.Errorf("rule[%d]: user_ids[%d] exceeds JavaScript safe integer range", i, j)
			}
			if _, exists := seenUserIDs[userID]; exists {
				return "", fmt.Errorf("rule[%d]: user_ids[%d] duplicates user_id %d", i, j, userID)
			}
			seenUserIDs[userID] = struct{}{}
		}
		for j, pattern := range rule.ModelWhitelist {
			trimmed := strings.TrimSpace(pattern)
			if trimmed == "" {
				return "", fmt.Errorf("rule[%d]: model_whitelist[%d] cannot be empty", i, j)
			}
			settings.Rules[i].ModelWhitelist[j] = trimmed
		}
		if rule.FallbackAction != "" && !validActions[rule.FallbackAction] {
			return "", fmt.Errorf("rule[%d]: invalid fallback_action %q", i, rule.FallbackAction)
		}
	}

	data, err := json.Marshal(settings)
	if err != nil {
		return "", fmt.Errorf("marshal openai fast policy settings: %w", err)
	}
	return string(data), nil
}

// ValidateOpenAIFastPolicySettings 在写入任何系统设置前验证并规范化策略。
func (s *SettingService) ValidateOpenAIFastPolicySettings(settings *OpenAIFastPolicySettings) error {
	_, err := normalizeAndMarshalOpenAIFastPolicySettings(settings)
	return err
}
