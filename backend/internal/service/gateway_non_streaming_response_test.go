package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type nonJSONTempUnschedAccountRepo struct {
	AccountRepository
	tempUnschedCalls    int
	tempReason          string
	modelRateLimitCalls int
	modelScope          string
	modelReason         string
}

func (r *nonJSONTempUnschedAccountRepo) SetTempUnschedulable(_ context.Context, _ int64, _ time.Time, reason string) error {
	r.tempUnschedCalls++
	r.tempReason = reason
	return nil
}

func (r *nonJSONTempUnschedAccountRepo) SetModelRateLimit(_ context.Context, _ int64, scope string, _ time.Time, reason ...string) error {
	r.modelRateLimitCalls++
	r.modelScope = scope
	if len(reason) > 0 {
		r.modelReason = reason[0]
	}
	return nil
}

func TestHandleNonStreamingResponse_NonJSON2xxTriggersFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	body := []byte("(upstream request failed)")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/plain"},
			"X-Request-Id": []string{"rid-invalid-json"},
		},
		Body: io.NopCloser(bytes.NewReader(body)),
	}
	svc := &GatewayService{
		cfg:              &config.Config{},
		rateLimitService: &RateLimitService{},
	}

	usage, err := svc.handleNonStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, "claude-sonnet-4-6", "claude-sonnet-4-6")

	require.Nil(t, usage)
	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr))
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.Equal(t, body, failoverErr.ResponseBody)
	require.Equal(t, "rid-invalid-json", failoverErr.ResponseHeaders.Get("x-request-id"))
	require.False(t, c.Writer.Written(), "invalid upstream response must not be committed before failover")
}

func TestHandleNonStreamingResponse_ValidJSONUnchanged(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	body := []byte(`{"id":"msg_1","type":"message","usage":{"input_tokens":12,"output_tokens":7}}`)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(body)),
	}
	svc := &GatewayService{
		cfg:              &config.Config{},
		rateLimitService: &RateLimitService{},
	}

	usage, err := svc.handleNonStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, "claude-sonnet-4-6", "claude-sonnet-4-6")

	require.NoError(t, err)
	require.NotNil(t, usage)
	require.Equal(t, 12, usage.InputTokens)
	require.Equal(t, 7, usage.OutputTokens)
	require.JSONEq(t, string(body), rec.Body.String())
}

func TestHandleNonStreamingResponseAnthropicAPIKeyPassthrough_NonJSON2xxTriggersFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	body := []byte("(upstream request failed)")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/plain"}},
		Body:       io.NopCloser(bytes.NewReader(body)),
	}
	svc := &GatewayService{cfg: &config.Config{}}

	usage, err := svc.handleNonStreamingResponseAnthropicAPIKeyPassthrough(context.Background(), resp, c, &Account{ID: 2})

	require.Nil(t, usage)
	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr))
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.Equal(t, body, failoverErr.ResponseBody)
	require.False(t, c.Writer.Written(), "invalid passthrough response must not be committed before failover")
}

func TestHandleNonStreamingResponseAnthropicAPIKeyPassthrough_ValidJSONUnchanged(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	body := []byte(`{"id":"msg_1","type":"message","usage":{"input_tokens":5,"output_tokens":3}}`)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(body)),
	}
	svc := &GatewayService{cfg: &config.Config{}}

	usage, err := svc.handleNonStreamingResponseAnthropicAPIKeyPassthrough(context.Background(), resp, c, &Account{ID: 2})

	require.NoError(t, err)
	require.NotNil(t, usage)
	require.Equal(t, 5, usage.InputTokens)
	require.Equal(t, 3, usage.OutputTokens)
	require.JSONEq(t, string(body), rec.Body.String())
}

func TestHandleNonStreamingResponseAnthropicAPIKeyPassthrough_NormalizesCompatibilityBodies(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name              string
		contentType       string
		body              []byte
		wantID            string
		wantInputTokens   int
		wantOutputTokens  int
		wantCacheCreation int
		wantCacheRead     int
	}{
		{
			name:              "括号 JSON",
			contentType:       "application/json",
			body:              []byte(`({"id":"msg_parenthesized","type":"message","usage":{"input_tokens":106,"output_tokens":7,"cache_creation":{"ephemeral_5m_input_tokens":1200},"cached_tokens":69000}})`),
			wantID:            "msg_parenthesized",
			wantInputTokens:   106,
			wantOutputTokens:  7,
			wantCacheCreation: 1200,
			wantCacheRead:     69000,
		},
		{
			name:        "SSE 终态消息",
			contentType: "text/event-stream",
			body: []byte(strings.Join([]string{
				`event: message_start`,
				`data: {"type":"message_start","message":{"usage":{"input_tokens":2,"cached_tokens":71000,"cache_creation":{"ephemeral_5m_input_tokens":104}}}}`,
				``,
				`event: message_delta`,
				`data: {"type":"message_delta","usage":{"output_tokens":7}}`,
				``,
				`event: message_stop`,
				`data: {"type":"message","id":"msg_sse","usage":{"input_tokens":2,"output_tokens":7,"cached_tokens":71000,"cache_creation":{"ephemeral_5m_input_tokens":104}}}`,
				``,
			}, "\n")),
			wantID:            "msg_sse",
			wantInputTokens:   2,
			wantOutputTokens:  7,
			wantCacheCreation: 104,
			wantCacheRead:     71000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{tt.contentType}},
				Body:       io.NopCloser(bytes.NewReader(tt.body)),
			}

			usage, err := (&GatewayService{cfg: &config.Config{}}).handleNonStreamingResponseAnthropicAPIKeyPassthrough(context.Background(), resp, c, &Account{ID: 2})
			require.NoError(t, err)
			require.NotNil(t, usage)
			require.Equal(t, tt.wantInputTokens, usage.InputTokens)
			require.Equal(t, tt.wantOutputTokens, usage.OutputTokens)
			require.Equal(t, tt.wantCacheCreation, usage.CacheCreationInputTokens)
			require.Equal(t, tt.wantCacheRead, usage.CacheReadInputTokens)
			require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
			require.True(t, gjson.Valid(rec.Body.String()))
			require.Equal(t, tt.wantID, gjson.Get(rec.Body.String(), "id").String())
		})
	}
}

func TestHandleNonStreamingResponseAnthropicAPIKeyPassthrough_ForceCacheBillingResponse(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "converts input tokens for downstream billing",
			body: `{"id":"msg_1","type":"message","content":[{"type":"text","text":"unchanged"}],"usage":{"input_tokens":5,"output_tokens":3}}`,
			want: `{"id":"msg_1","type":"message","content":[{"type":"text","text":"unchanged"}],"usage":{"input_tokens":0,"output_tokens":3,"cache_read_input_tokens":5}}`,
		},
		{
			name: "adds to genuine cache reads",
			body: `{"id":"msg_2","type":"message","usage":{"input_tokens":5,"output_tokens":3,"cache_read_input_tokens":7,"cache_creation_input_tokens":11}}`,
			want: `{"id":"msg_2","type":"message","usage":{"input_tokens":0,"output_tokens":3,"cache_read_input_tokens":12,"cache_creation_input_tokens":11}}`,
		},
		{
			name: "zero input leaves response unchanged",
			body: `{"id":"msg_3","type":"message","usage":{"input_tokens":0,"output_tokens":3,"cache_read_input_tokens":7}}`,
			want: `{"id":"msg_3","type":"message","usage":{"input_tokens":0,"output_tokens":3,"cache_read_input_tokens":7}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(bytes.NewBufferString(tt.body)),
			}
			svc := &GatewayService{cfg: &config.Config{}}

			usage, err := svc.handleNonStreamingResponseAnthropicAPIKeyPassthrough(WithForceCacheBilling(context.Background()), resp, c, &Account{ID: 2})

			require.NoError(t, err)
			require.Equal(t, int(gjson.Get(tt.body, "usage.input_tokens").Int()), usage.InputTokens, "local accounting must retain the unclassified usage")
			require.Equal(t, int(gjson.Get(tt.body, "usage.cache_read_input_tokens").Int()), usage.CacheReadInputTokens, "local accounting must convert exactly once in RecordUsage")
			require.JSONEq(t, tt.want, rec.Body.String())
		})
	}
}

func TestHandleNonStreamingResponse_NonJSON2xxMatchesModelScopedTempUnschedulableRule(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	repo := &nonJSONTempUnschedAccountRepo{}
	rateLimitService := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	svc := &GatewayService{
		cfg:              &config.Config{},
		rateLimitService: rateLimitService,
	}
	account := &Account{
		ID:       3,
		Platform: PlatformAnthropic,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"temp_unschedulable_enabled": true,
			"temp_unschedulable_rules": []any{
				map[string]any{
					"error_code":       float64(http.StatusBadGateway),
					"keywords":         []any{"upstream request failed"},
					"duration_minutes": float64(10),
				},
			},
		},
	}
	body := []byte("(upstream request failed)")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader(body)),
	}

	_, err := svc.handleNonStreamingResponse(context.Background(), resp, c, account, "claude-sonnet-4-6", "claude-sonnet-4-6")

	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr))
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.Equal(t, body, failoverErr.ResponseBody)
	require.Zero(t, repo.tempUnschedCalls)
	require.Equal(t, 1, repo.modelRateLimitCalls)
	require.Equal(t, "claude-sonnet-4-6", repo.modelScope)
	require.Contains(t, repo.modelReason, `"status_code":502`)
	require.Contains(t, repo.modelReason, `"matched_keyword":"upstream request failed"`)
}
