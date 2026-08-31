package handler

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type requestBodyLimitTypedNilBody struct{}

func (*requestBodyLimitTypedNilBody) Read([]byte) (int, error) { return 0, io.EOF }
func (*requestBodyLimitTypedNilBody) Close() error             { return nil }

func TestRequestBodyLimitTooLarge(t *testing.T) {
	gin.SetMode(gin.TestMode)

	limit := int64(16)
	router := gin.New()
	router.Use(middleware.RequestBodyLimit(limit))
	router.POST("/test", func(c *gin.Context) {
		_, err := io.ReadAll(c.Request.Body)
		if err != nil {
			if maxErr, ok := extractMaxBytesError(err); ok {
				c.JSON(http.StatusRequestEntityTooLarge, gin.H{
					"error": buildBodyTooLargeMessage(maxErr.Limit),
				})
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "read_failed",
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	payload := bytes.Repeat([]byte("a"), int(limit+1))
	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(payload))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
	require.Contains(t, recorder.Body.String(), buildBodyTooLargeMessage(limit))
}

func TestGatewayDirectBodyReadersReturn413ForActualOversize(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name    string
		path    string
		handler gin.HandlerFunc
	}{
		{name: "seedance video", path: "/videos", handler: (&OpenAIGatewayHandler{}).OpenAIVideoGeneration},
		{name: "web search", path: "/web_search", handler: (&GatewayHandler{}).WebSearch},
		{name: "batch images", path: "/v1/images/batches", handler: (&BatchImageHandler{}).Submit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.POST(tt.path, middleware.RequestBodyLimit(4), tt.handler)
			// The first JSON value is valid and fits within the limit. The trailing
			// bytes must still be read so they cannot bypass the body-size check.
			req := httptest.NewRequest(http.MethodPost, tt.path, bytes.NewBufferString("{}   "))
			req.ContentLength = -1
			req.Header.Del("Content-Length")
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)

			require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
			require.Contains(t, recorder.Body.String(), "Request body too large")
		})
	}
}

func TestRequestBodyLimitSkipsEmptyAndTypedNilBodies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name string
		body io.ReadCloser
	}{
		{name: "nil body", body: nil},
		{name: "http no body", body: http.NoBody},
		{name: "typed nil body", body: (*requestBodyLimitTypedNilBody)(nil)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/test", nil)
			req.Body = tt.body
			called := false
			router := gin.New()
			router.POST("/test", middleware.RequestBodyLimit(16), func(c *gin.Context) {
				called = true
				require.Equal(t, tt.body, c.Request.Body)
				c.Status(http.StatusNoContent)
			})

			recorder := httptest.NewRecorder()
			require.NotPanics(t, func() {
				router.ServeHTTP(recorder, req)
			})
			require.True(t, called)
			require.Equal(t, http.StatusNoContent, recorder.Code)
		})
	}
}

func TestRequestBodyLimitHandlesNilRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = nil

	require.NotPanics(t, func() {
		middleware.RequestBodyLimit(16)(c)
	})
}
