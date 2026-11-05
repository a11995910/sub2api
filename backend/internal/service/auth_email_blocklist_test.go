//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestRegistrationEmailBlocklist(t *testing.T) {
	for _, tc := range []struct {
		name, raw, email string
		want             error
	}{
		{"空列表", `[]`, "u@example.com", nil},
		{"精确域名", `["@example.com"]`, "u@example.com", ErrEmailDomainBlocked},
		{"大小写和末尾点", `["EXAMPLE.COM"]`, " U@Example.Com. ", ErrEmailDomainBlocked},
		{"包含主域", `["*.example.com"]`, "u@example.com", ErrEmailDomainBlocked},
		{"包含子域", `["*.example.com"]`, "u@sub.example.com", ErrEmailDomainBlocked},
		{"域名边界", `["*.example.com"]`, "u@notexample.com", nil},
		{"不匹配邮箱前缀", `["*.example.com"]`, "example.com@gmail.com", nil},
		{"精确规则不匹配子域", `["@example.com"]`, "u@sub.example.com", nil},
		{"无效格式关闭放行", `[`, "u@gmail.com", ErrServiceUnavailable},
		{"无效域名关闭放行", `["bad_domain"]`, "u@gmail.com", ErrServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &settingRepoStub{values: map[string]string{SettingKeyRegistrationEmailSuffixBlocklist: tc.raw}}
			svc := &AuthService{settingService: NewSettingService(repo, &config.Config{})}
			require.ErrorIs(t, svc.validateRegistrationEmailBlocklist(context.Background(), tc.email), tc.want)
		})
	}
}

func TestRegistrationEmailBlocklistMissingAndReadFailure(t *testing.T) {
	repo := &settingRepoStub{values: map[string]string{}}
	svc := &AuthService{settingService: NewSettingService(repo, &config.Config{})}
	require.NoError(t, svc.validateRegistrationEmailBlocklist(context.Background(), "u@example.com"))
	repo.err = errors.New("数据库不可用")
	require.ErrorIs(t, svc.validateRegistrationEmailBlocklist(context.Background(), "u@example.com"), ErrServiceUnavailable)
}

func TestRegistrationEmailBlocklistRejectsSignupEntryPoints(t *testing.T) {
	ctx := context.Background()
	repo := &settingRepoStub{values: map[string]string{
		SettingKeyRegistrationEnabled:              "true",
		SettingKeyRegistrationEmailSuffixBlocklist: `["*.example.com"]`,
	}}
	users := &userRepoStub{getByEmailErr: ErrUserNotFound}
	svc := &AuthService{
		settingService:    NewSettingService(repo, &config.Config{}),
		userRepo:          users,
		refreshTokenCache: &refreshTokenCacheStub{},
	}
	// 未配置邮件服务：如果校验被绕过，会返回其他错误或触发依赖调用。
	require.ErrorIs(t, svc.SendVerifyCode(ctx, "u@example.com"), ErrEmailDomainBlocked)
	_, err := svc.SendVerifyCodeAsync(ctx, "u@example.com")
	require.ErrorIs(t, err, ErrEmailDomainBlocked)
	_, _, err = svc.LoginOrRegisterOAuth(ctx, "u@example.com", "用户")
	require.ErrorIs(t, err, ErrEmailDomainBlocked)
	_, _, err = svc.LoginOrRegisterOAuthWithTokenPair(ctx, "u@example.com", "用户", "", "", "oidc")
	require.ErrorIs(t, err, ErrEmailDomainBlocked)
	require.Empty(t, users.created)
}

func TestRegistrationEmailBlocklistOverridesWhitelistAndQuota(t *testing.T) {
	for _, quota := range []string{"false", "true"} {
		repo := &settingRepoStub{values: map[string]string{
			SettingKeyRegistrationEmailSuffixBlocklist:    `["*.example.com"]`,
			SettingKeyRegistrationEmailSuffixWhitelist:    `["@example.com"]`,
			SettingKeyRegistrationEmailDomainQuotaEnabled: quota,
		}}
		svc := &AuthService{settingService: NewSettingService(repo, &config.Config{})}
		ctx := context.Background()
		require.ErrorIs(t, svc.validateRegistrationEmailPolicy(ctx, "u@example.com"), ErrEmailDomainBlocked)
		require.ErrorIs(t, svc.validateRegistrationEmailQuota(ctx, "u@example.com"), ErrEmailDomainBlocked)
		// 不提供用户仓储和邮件服务，确保拦截发生在创建用户和发送邮件之前。
		require.ErrorIs(t, svc.createUserWithRegistrationEmailGuard(ctx, &User{Email: "u@example.com"}), ErrEmailDomainBlocked)
		_, _, err := svc.RegisterWithVerification(ctx, "u@example.com", "password", "", "", "", "")
		require.ErrorIs(t, err, ErrEmailDomainBlocked)
		// 运行时更新立即生效，不依赖进程缓存。
		repo.values[SettingKeyRegistrationEmailSuffixBlocklist] = `[]`
		require.NoError(t, svc.validateRegistrationEmailPolicy(ctx, "u@example.com"))
	}
}
