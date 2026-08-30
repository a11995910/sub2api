package service

import (
	"strings"
	"time"
)

// ResolveOAuthAccountDisplayIdentifier 返回允许向有权用户展示的真实账号标识。
// 只读取邮箱类字段，不回退管理员自定义名称，也不会返回任何凭据原文。
func ResolveOAuthAccountDisplayIdentifier(account *Account) string {
	if account == nil {
		return ""
	}
	for _, value := range []string{
		account.GetExtraString("email_address"),
		account.GetExtraString("email"),
		account.GetCredential("email"),
		account.ParentDisplayIdentifier,
	} {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

// ResolveOAuthAccountPlanType 返回账号自身或影子母账号的原始套餐值。
func ResolveOAuthAccountPlanType(account *Account) string {
	if account == nil {
		return ""
	}
	if value := strings.TrimSpace(account.GetCredential("plan_type")); value != "" {
		return value
	}
	return strings.TrimSpace(account.ParentPlanType)
}

// ResolveOAuthAccountDisplayExpiresAt 返回与展示套餐对应的到期时间。
// 订阅到期时间优先；缺失时回退账号调度到期时间。影子账号复用母账号解析结果。
func ResolveOAuthAccountDisplayExpiresAt(account *Account) *time.Time {
	if account == nil {
		return nil
	}
	if expiresAt := account.GetCredentialAsTime("subscription_expires_at"); expiresAt != nil {
		return expiresAt
	}
	if account.ParentDisplayExpiresAt != nil {
		return account.ParentDisplayExpiresAt
	}
	return account.ExpiresAt
}

// OAuthAccountPlanLabel 将已知套餐归一化为用户可读标签。
// 未知值原样保留，避免把新的上游套餐误判为现有套餐。
func OAuthAccountPlanLabel(planType string) string {
	planType = strings.TrimSpace(planType)
	normalized := strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.ToLower(planType))
	switch normalized {
	case "pro", "chatgptpro":
		return "Pro 20x"
	case "team":
		return "Team"
	case "plus":
		return "Plus"
	case "k12", "chatgptk12":
		return "K12"
	case "free", "basic":
		return "Free"
	default:
		return planType
	}
}
