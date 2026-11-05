package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestDeepSeekToolsRejectUnauthenticatedAndOtherPlatforms(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, kind := range []string{"search", "fetch"} {
		for _, platform := range []string{"", service.PlatformOpenAI, service.PlatformGrok, service.PlatformComposite} {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/tools/"+kind, strings.NewReader(`{}`))
			if platform != "" {
				c.Set("api_key", &service.APIKey{Group: &service.Group{Platform: platform}})
			}
			(&OpenAIGatewayHandler{}).deepSeekTool(c, kind)
			if platform == "" {
				require.Equal(t, http.StatusUnauthorized, w.Code)
			} else {
				require.Equal(t, http.StatusNotFound, w.Code)
			}
		}
	}
}

func TestDeepSeekMessagesPreflightPricingAndReservation(t *testing.T) {
	body := []byte(`{"model":"deepseek-flash","max_tokens":100,"messages":[{"role":"user","content":"fetch"}],"tools":[{"type":"web_fetch_20250910","name":"web_fetch","max_uses":2}]}`)
	_, err := deepSeekMessagesInflightEstimate(service.PlatformDeepseek, "deepseek-flash", body, &service.Group{})
	require.ErrorIs(t, err, service.ErrDeepSeekToolPricingRequired)
	price := 0.0
	estimate, err := deepSeekMessagesInflightEstimate(service.PlatformDeepseek, "deepseek-flash", body, &service.Group{SearchPricePer1k: &price})
	require.NoError(t, err)
	require.Equal(t, 2, estimate.DeepSeekToolCalls)
	require.Equal(t, 4, estimate.TokenRounds)
	require.Equal(t, len(body)+2*128*1024, estimate.BodyBytes)
	unchanged, err := deepSeekMessagesInflightEstimate(service.PlatformOpenAI, "deepseek-flash", body, &service.Group{})
	require.NoError(t, err)
	require.Zero(t, unchanged.DeepSeekToolCalls)
	require.Zero(t, unchanged.TokenRounds)
	_, err = deepSeekMessagesInflightEstimate(service.PlatformDeepseek, "deepseek-flash", []byte(`{"tools":[{"type":"web_fetch_20250910","name":"web_fetch","max_uses":999}]}`), &service.Group{SearchPricePer1k: &price})
	require.Error(t, err)
}
