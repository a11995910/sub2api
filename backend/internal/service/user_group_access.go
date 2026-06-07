package service

import (
	"context"
	"time"
)

const (
	UserAllowedGroupSourceManual                 = "manual"
	UserAllowedGroupSourceAffiliatePaymentReward = "affiliate_payment_reward"
)

// TemporaryAllowedGroupGrantInput 描述一次限时专属分组授权。
type TemporaryAllowedGroupGrantInput struct {
	UserID        int64
	GroupID       int64
	ValidityDays  int
	Source        string
	SourceOrderID *int64
	Notes         string
	Now           time.Time
}

type TemporaryAllowedGroupGrantResult struct {
	UserID    int64
	GroupID   int64
	ExpiresAt *time.Time
	Permanent bool
}

type ExpireTemporaryAllowedGroupsInput struct {
	Source             string
	ReplacementGroupID int64
	Now                time.Time
	Limit              int
}

type ExpiredTemporaryAllowedGroupResult struct {
	UserID             int64
	GroupID            int64
	ReplacementGroupID int64
	MigratedKeys       int64
}

// UserGroupAccessMeta 描述当前用户对某个分组的限时访问元数据。
// Permanent=true 表示存在永久授权，不应展示限时倒计时。
type UserGroupAccessMeta struct {
	GroupID   int64
	Source    string
	ExpiresAt *time.Time
	Permanent bool
}

// TemporaryAllowedGroupRepository 是 user_allowed_groups 限时授权的可选仓储能力。
type TemporaryAllowedGroupRepository interface {
	GrantTemporaryAllowedGroup(ctx context.Context, input TemporaryAllowedGroupGrantInput) (*TemporaryAllowedGroupGrantResult, error)
	ExpireTemporaryAllowedGroups(ctx context.Context, input ExpireTemporaryAllowedGroupsInput) ([]ExpiredTemporaryAllowedGroupResult, error)
}

// UserGroupAccessMetaReader 是 user_allowed_groups 限时授权的只读能力。
type UserGroupAccessMetaReader interface {
	ListActiveUserGroupAccessMeta(ctx context.Context, userID int64) (map[int64]UserGroupAccessMeta, error)
}
