package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// SettingKeyRegistrationEmailSuffixBlocklist 是运维维护的注册邮箱黑名单。
// 使用 JSON 数组保存，格式与邮箱白名单相同；不受白名单和域名额度开关影响。
const SettingKeyRegistrationEmailSuffixBlocklist = "registration_email_suffix_blocklist"

var ErrEmailDomainBlocked = infraerrors.Forbidden("EMAIL_DOMAIN_BLOCKED", "该邮箱域名已禁止注册，请使用其他邮箱")

// validateRegistrationEmailBlocklist 每次读取运行时设置，更新后无需重启。
// 缺少配置表示未启用；读取或解析失败时拒绝注册，避免配置故障绕过限制。
func (s *AuthService) validateRegistrationEmailBlocklist(ctx context.Context, email string) error {
	if s == nil || s.settingService == nil {
		return nil
	}
	raw, err := s.settingService.settingRepo.GetValue(ctx, SettingKeyRegistrationEmailSuffixBlocklist)
	if errors.Is(err, ErrSettingNotFound) {
		return nil
	}
	if err != nil {
		return ErrServiceUnavailable
	}
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var entries []string
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return ErrServiceUnavailable
	}
	entries, err = NormalizeRegistrationEmailSuffixWhitelist(entries)
	if err != nil {
		return ErrServiceUnavailable
	}
	// 白名单匹配函数将空列表视为全放行，黑名单必须先排除空列表。
	if len(entries) > 0 && IsRegistrationEmailSuffixAllowed(email, entries) {
		return ErrEmailDomainBlocked
	}
	return nil
}
