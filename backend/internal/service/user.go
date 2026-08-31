package service

import (
	"time"

	"golang.org/x/crypto/bcrypt"
)

type User struct {
	ID             int64
	Email          string
	Username       string
	Notes          string
	AvatarURL      string
	AvatarSource   string
	AvatarMIME     string
	AvatarByteSize int
	AvatarSHA256   string
	PasswordHash   string
	Role           string
	Balance        float64
	FrozenBalance  float64
	Concurrency    int
	Status         string
	AllowedGroups  []int64
	// BlockedGroups 保存当前用户不可使用的公开标准分组 ID。
	// 黑名单优先于公开分组的默认开放规则，不影响专属分组和订阅分组的既有授权模型。
	BlockedGroups []int64
	// AllowedGroupAccess 保存管理员侧可见的专属分组授权元数据。
	// key 为 group_id；AllowedGroups 仍保留为兼容旧接口的授权分组 ID 列表。
	AllowedGroupAccess map[int64]UserGroupAccessMeta
	// RestrictPublicGroups 为 true 时，公开分组仅允许出现在 AllowedGroups 中。
	RestrictPublicGroups bool
	TokenVersion         int64 // Incremented on password change to invalidate existing tokens
	// TokenVersionResolved indicates TokenVersion already contains the fingerprint-derived
	// value expected in JWT claims and refresh-token state.
	TokenVersionResolved bool
	SignupSource         string
	LastLoginAt          *time.Time
	LastActiveAt         *time.Time
	LastUsedAt           *time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
	DeletedAt            *time.Time // 非 nil 表示用户已软删除

	// GroupRates 用户专属分组倍率配置
	// map[groupID]rateMultiplier
	GroupRates map[int64]float64

	// TOTP 双因素认证字段
	TotpSecretEncrypted *string    // AES-256-GCM 加密的 TOTP 密钥
	TotpEnabled         bool       // 是否启用 TOTP
	TotpEnabledAt       *time.Time // TOTP 启用时间

	// 余额不足通知
	BalanceNotifyEnabled       bool
	BalanceNotifyThresholdType string // "fixed" (default) | "percentage"
	BalanceNotifyThreshold     *float64
	BalanceNotifyExtraEmails   []NotifyEmailEntry
	TotalRecharged             float64

	// RPMLimit 用户级每分钟请求数上限（0 = 不限制）。仅在所用分组未设置 rpm_limit
	// 且该 (用户, 分组) 无 rpm_override 时作为全局兜底生效，计数键 rpm:u:{userID}:{min}。
	RPMLimit int

	// UserGroupRPMOverride 来自 auth cache snapshot 的 (user, group) RPM 覆盖值。
	// nil = 该 API Key 对应的 (user, group) 无 override；非 nil 时 checkRPM 直接使用，
	// 避免每请求查 DB。字段不持久化到数据库。
	UserGroupRPMOverride *int

	APIKeys       []APIKey
	Subscriptions []UserSubscription
}

func (u *User) IsAdmin() bool {
	return u.Role == RoleAdmin
}

func (u *User) IsActive() bool {
	return u.Status == StatusActive
}

// CanBindGroup 判断用户是否可以绑定标准分组：公开分组默认开放，可由黑名单或
// RestrictPublicGroups 收紧；专属分组始终使用白名单。
func (u *User) CanBindGroup(groupID int64, isExclusive bool) bool {
	if !isExclusive {
		for _, id := range u.BlockedGroups {
			if id == groupID {
				return false
			}
		}
		if !u.RestrictPublicGroups {
			return true
		}
	}
	// 专属分组，以及开启公开分组限制后的公开分组：需要在 AllowedGroups 中。
	for _, id := range u.AllowedGroups {
		if id == groupID {
			return true
		}
	}
	return false
}

func (u *User) SetPassword(password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u.PasswordHash = string(hash)
	return nil
}

func (u *User) CheckPassword(password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) == nil
}
