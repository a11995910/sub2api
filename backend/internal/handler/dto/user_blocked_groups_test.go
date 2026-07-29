package dto

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUserFromServiceAdmin_BlockedGroupsAlwaysUsesArray(t *testing.T) {
	user := UserFromServiceAdmin(&service.User{ID: 42})
	require.NotNil(t, user)
	require.NotNil(t, user.BlockedGroups)

	payload, err := json.Marshal(user)
	require.NoError(t, err)
	require.Contains(t, string(payload), `"blocked_groups":[]`)
}
