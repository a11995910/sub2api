package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUserCanBindGroup_PublicGroupBlacklist(t *testing.T) {
	user := &User{
		AllowedGroups: []int64{12},
		BlockedGroups: []int64{7, 12},
	}

	require.True(t, user.CanBindGroup(6, false), "公开分组未命中黑名单时应保持默认可用")
	require.False(t, user.CanBindGroup(7, false), "公开分组命中黑名单时应拒绝")
	require.True(t, user.CanBindGroup(12, true), "专属分组仍应使用白名单，不受公开分组黑名单影响")
	require.False(t, user.CanBindGroup(13, true), "未授权的专属分组应继续拒绝")
}
