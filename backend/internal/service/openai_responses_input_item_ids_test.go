package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestSanitizeOpenAIResponsesInputItemIDs(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5.6-sol",
		"input":[
			{"type":"message","id":"item_bad_message","role":"assistant","content":[{"type":"output_text","text":"keep-message"}]},
			{"type":"message","id":"msg_valid","role":"assistant"},
			{"type":"function_call","id":"item_bad_call","call_id":"fc_pair","name":"shell","arguments":"{}"},
			{"type":"function_call","id":"fc_valid","call_id":"fc_valid","name":"shell","arguments":"{}"},
			{"type":"function_call_output","id":"item_output","call_id":"fc_pair","output":"keep-output"},
			{"type":"web_search_call","id":"item_search","status":"completed"}
		]
	}`)

	got, changed, err := sanitizeOpenAIResponsesInputItemIDs(body)
	require.NoError(t, err)
	require.True(t, changed)
	require.False(t, gjson.GetBytes(got, "input.0.id").Exists())
	require.Equal(t, "keep-message", gjson.GetBytes(got, "input.0.content.0.text").String())
	require.Equal(t, "msg_valid", gjson.GetBytes(got, "input.1.id").String())
	require.False(t, gjson.GetBytes(got, "input.2.id").Exists())
	require.Equal(t, "fc_pair", gjson.GetBytes(got, "input.2.call_id").String())
	require.Equal(t, "fc_valid", gjson.GetBytes(got, "input.3.id").String())
	require.Equal(t, "item_output", gjson.GetBytes(got, "input.4.id").String())
	require.Equal(t, "keep-output", gjson.GetBytes(got, "input.4.output").String())
	require.Equal(t, "item_search", gjson.GetBytes(got, "input.5.id").String())
}

func TestSanitizeOpenAIResponsesInputItemIDs_NoInvalidIDKeepsBody(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-sol","input":[{"type":"message","id":"msg_valid"},{"type":"function_call","id":"fc_valid","call_id":"fc_valid"}]}`)

	got, changed, err := sanitizeOpenAIResponsesInputItemIDs(body)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, body, got)
}

func TestOpenAIGatewayService_APIKeyResponsesStripsInvalidInputItemIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{
		"model":"gpt-5.6-sol",
		"stream":false,
		"instructions":"test",
		"input":[
			{"type":"message","id":"item_bad_message","role":"assistant","content":[{"type":"output_text","text":"keep-message"}]},
			{"type":"message","id":"msg_valid","role":"assistant"},
			{"type":"function_call","id":"item_bad_call","call_id":"fc_pair","name":"shell","arguments":"{}"},
			{"type":"function_call_output","id":"item_output","call_id":"fc_pair","output":"keep-output"}
		]
	}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"type":"invalid_request_error","message":"stop after capture"}}`)),
	}}
	service := &OpenAIGatewayService{
		cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{
			Enabled:           false,
			AllowInsecureHTTP: true,
		}}},
		httpUpstream: upstream,
	}
	account := &Account{
		ID:          9962,
		Name:        "openai-apikey",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "test-key",
			"base_url": "http://upstream.example",
		},
		Extra: map[string]any{
			openai_compat.ExtraKeyResponsesMode:      string(openai_compat.ResponsesSupportModeAuto),
			openai_compat.ExtraKeyResponsesSupported: true,
		},
		Status:      StatusActive,
		Schedulable: true,
	}

	result, err := service.Forward(context.Background(), c, account, body)
	require.Error(t, err)
	require.Nil(t, result)
	require.NotNil(t, upstream.lastReq)
	require.False(t, gjson.GetBytes(upstream.lastBody, "input.0.id").Exists())
	require.Equal(t, "keep-message", gjson.GetBytes(upstream.lastBody, "input.0.content.0.text").String())
	require.Equal(t, "msg_valid", gjson.GetBytes(upstream.lastBody, "input.1.id").String())
	require.False(t, gjson.GetBytes(upstream.lastBody, "input.2.id").Exists())
	require.Equal(t, "fc_pair", gjson.GetBytes(upstream.lastBody, "input.2.call_id").String())
	require.Equal(t, "item_output", gjson.GetBytes(upstream.lastBody, "input.3.id").String())
}
