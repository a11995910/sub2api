package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestIsAsyncImageTaskRead(t *testing.T) {
	require.True(t, isAsyncImageTaskRead(http.MethodGet, "/v1/images/tasks/imgtask_123"))
	require.True(t, isAsyncImageTaskRead(http.MethodGet, "/images/tasks/imgtask_123"))
	require.True(t, isAsyncImageTaskRead(http.MethodGet, "/v1/images/generations/imgtask_123"))
	require.True(t, isAsyncImageTaskRead(http.MethodGet, "/images/generations/imgtask_123"))
	require.False(t, isAsyncImageTaskRead(http.MethodPost, "/v1/images/tasks/imgtask_123"))
	require.False(t, isAsyncImageTaskRead(http.MethodGet, "/v1/images/generations"))
}

func TestIsAsyncImageTaskSubmitOnlyMatchesAsyncGeneration(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		body string
		want bool
	}{
		{name: "async generation", path: "/v1/images/generations", body: `{"async":true,"client_request_id":"request_1"}`, want: true},
		{name: "explicit sync", path: "/v1/images/generations", body: `{"async":false}`, want: false},
		{name: "legacy async path", path: "/v1/images/generations/async", body: `{"model":"gpt-image-2"}`, want: false},
		{name: "other endpoint", path: "/v1/images/edits", body: `{"async":true,"client_request_id":"request_1"}`, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = req
			require.Equal(t, tc.want, isAsyncImageTaskSubmit(c))
			restored, err := io.ReadAll(c.Request.Body)
			require.NoError(t, err)
			require.Equal(t, tc.body, string(restored))
		})
	}
}
