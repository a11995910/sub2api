//go:build unit

package middleware

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRequestBodyBudgetReservesAndReleasesKnownLength(t *testing.T) {
	gin.SetMode(gin.TestMode)
	budget := NewBodyMemoryBudget(8, 0)

	r := gin.New()
	r.Use(RequestBodyLimit(4, budget))
	r.POST("/t", func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		require.Equal(t, []byte("1234"), body)
		c.Status(http.StatusOK)
	})

	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/t", bytes.NewBufferString("1234"))
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
	}
}

func TestRequestBodyBudgetRejectsWhenFull(t *testing.T) {
	gin.SetMode(gin.TestMode)
	budget := NewBodyMemoryBudget(8, 0)
	// 4 字节请求体会预留 8 字节（请求体本身加解析/改写余量）。
	if err := budget.acquire(nil, 8); err != nil {
		t.Fatal(err)
	}
	defer budget.release(8)

	r := gin.New()
	r.Use(RequestBodyLimit(4, budget))
	r.POST("/t", func(c *gin.Context) {
		t.Fatal("handler must not run when the body budget is full")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/t", bytes.NewBufferString("1234"))
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusTooManyRequests, w.Code)
	require.Contains(t, w.Body.String(), "request_body_memory_budget_exhausted")
}

func TestRequestBodyBudgetReservesRouteLimitForUnknownLength(t *testing.T) {
	budget := NewBodyMemoryBudget(8, 0)
	require.Equal(t, int64(8), budget.reservationBytes(-1, 4))
	require.Equal(t, int64(8), budget.reservationBytes(100, 4))
	require.Equal(t, int64(0), budget.reservationBytes(0, 4))
}

func TestWriteBodyBudgetErrorUsesProtocolEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name string
		path string
		want string
	}{
		{name: "openai", path: "/v1/responses", want: "request_body_memory_budget_exhausted"},
		{name: "anthropic", path: "/v1/messages", want: `"type":"error"`},
		{name: "gemini", path: "/v1beta/models/gemini:generateContent", want: `"status":"RESOURCE_EXHAUSTED"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.POST("/*path", func(c *gin.Context) {
				writeBodyBudgetError(c)
			})
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, tc.path, nil)
			r.ServeHTTP(w, req)
			require.Equal(t, http.StatusTooManyRequests, w.Code)
			require.Contains(t, w.Body.String(), tc.want)
		})
	}
}
