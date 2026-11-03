package dto

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUserPreferredAccountOnlyVisibleToAdmin(t *testing.T) {
	user := &service.User{ID: 42, PreferredAccountID: 7}
	admin, err := json.Marshal(UserFromServiceAdmin(user))
	require.NoError(t, err)
	require.Contains(t, string(admin), `"preferred_account_id":7`)
	public, err := json.Marshal(UserFromService(user))
	require.NoError(t, err)
	require.NotContains(t, string(public), "preferred_account_id")
}
