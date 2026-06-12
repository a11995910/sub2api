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
	UserID        int64
	GroupID       int64
	Source        string
	SourceOrderID *int64
	Notes         string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	ExpiresAt     *time.Time
	Permanent     bool
	UserEmail     string
	Username      string
	UserStatus    string
}

// UserAllowedGroupAccessInput 是管理员保存用户专属分组授权时的元数据。
// ExpiresAtSet=false 表示沿用现有授权时间；ExpiresAtSet=true 且 ExpiresAt=nil 表示永久授权。
type UserAllowedGroupAccessInput struct {
	GroupID      int64
	ExpiresAtSet bool
	ExpiresAt    *time.Time
	Source       string
	Notes        string
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

// UserGroupAccessAdminRepository 是管理员侧读写 user_allowed_groups 元数据的仓储能力。
type UserGroupAccessAdminRepository interface {
	ListActiveUserGroupAccessMetaByUserIDs(ctx context.Context, userIDs []int64) (map[int64]map[int64]UserGroupAccessMeta, error)
	ListActiveUserGroupAccessMetaByGroupID(ctx context.Context, groupID int64, page, pageSize int) ([]UserGroupAccessMeta, int64, error)
	SyncUserAllowedGroupAccess(ctx context.Context, userID int64, entries []UserAllowedGroupAccessInput) error
}
