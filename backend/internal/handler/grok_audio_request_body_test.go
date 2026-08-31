package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGrokVoiceBodyReadReturns413ForActualOversize(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, path := range []string{"/tts", "/stt", "/custom-voices"} {
		t.Run(path, func(t *testing.T) {
			h := &OpenAIGatewayHandler{}
			router := gin.New()
			router.POST(path, middleware2.RequestBodyLimit(4), func(c *gin.Context) {
				_, err := readGrokVoiceGatewayBody(c)
				if err != nil {
					h.writeGrokVoiceBodyReadError(c, err)
					return
				}
				c.Status(http.StatusNoContent)
			})

			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"input":"hello"}`))
			req.ContentLength = -1
			req.Header.Del("Content-Length")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)

			require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
			require.Contains(t, recorder.Body.String(), "Request body too large")
		})
	}
}
