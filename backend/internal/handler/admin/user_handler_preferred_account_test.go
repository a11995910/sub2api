package admin

import (
	"context"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type preferredAccountAdminStub struct {
	service.AdminService
	input *service.UpdateUserInput
}

func (s *preferredAccountAdminStub) UpdateUser(_ context.Context, id int64, input *service.UpdateUserInput) (*service.User, error) {
	s.input = input
	user := &service.User{ID: id}
	if input.PreferredAccountID != nil {
		user.PreferredAccountID = *input.PreferredAccountID
	}
	return user, nil
}
func TestUserHandlerPreferredAccount(t *testing.T) {
	for _, tc := range []struct {
		name   string
		body   map[string]any
		status int
		want   *int64
	}{
		{"省略", map[string]any{}, http.StatusOK, nil},
		{"null 保留", map[string]any{"preferred_account_id": nil}, http.StatusOK, nil},
		{"设置", map[string]any{"preferred_account_id": 7}, http.StatusOK, preferredHandlerID(7)},
		{"清除", map[string]any{"preferred_account_id": 0}, http.StatusOK, preferredHandlerID(0)},
		{"负数", map[string]any{"preferred_account_id": -1}, http.StatusBadRequest, nil},
		{"小数", map[string]any{"preferred_account_id": 1.5}, http.StatusBadRequest, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			svc := &preferredAccountAdminStub{}
			router := gin.New()
			handler := NewUserHandler(svc, nil, nil, nil, nil, nil, nil)
			router.PUT("/api/v1/admin/users/:id", handler.Update)
			res := doJSON(t, router, http.MethodPut, "/api/v1/admin/users/42", tc.body)
			require.Equal(t, tc.status, res.Code, res.Body.String())
			if tc.status == http.StatusOK {
				require.NotNil(t, svc.input)
				require.Equal(t, tc.want, svc.input.PreferredAccountID)
				require.Contains(t, res.Body.String(), "preferred_account_id")
			} else {
				require.Nil(t, svc.input)
			}
		})
	}
}
func preferredHandlerID(id int64) *int64 { return &id }
