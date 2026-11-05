package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 新接口会走完整 Key 认证链，补齐测试仓库的异步使用时间回写。
func (r *keyBillingRouteAPIKeyRepo) UpdateLastUsed(context.Context, int64, time.Time) error {
	return nil
}

func TestDeepSeekToolRoutesUseAPIKeyAuthAndPlatformGate(t *testing.T) {
	router, _, key := newKeyBillingRouteTestRouter("")
	for _, path := range []string{"/v1/tools/search", "/v1/tools/fetch"} {
		unauth := httptest.NewRecorder()
		router.ServeHTTP(unauth, httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`)))
		require.Equal(t, http.StatusUnauthorized, unauth.Code, path)
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
		request.Header.Set("Authorization", "Bearer "+key)
		wrongGroup := httptest.NewRecorder()
		router.ServeHTTP(wrongGroup, request)
		require.Equal(t, http.StatusNotFound, wrongGroup.Code, path)
		require.Contains(t, wrongGroup.Body.String(), "requires a DeepSeek group")
	}
}
