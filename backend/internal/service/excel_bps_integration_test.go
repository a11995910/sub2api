package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func excelTestAccount() *Account {
	return &Account{ID: 300, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 2, Status: StatusActive, Schedulable: true, Credentials: map[string]any{"client_id": openai.ExcelClientID, "access_token": "test-bps-token", "chatgpt_account_id": "test-workspace"}, Extra: map[string]any{"openai_excel_bps": true}}
}
func excelTestWire() string {
	return "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_bps\",\"status\":\"completed\",\"model\":\"gpt-6-astra\",\"output\":[{\"type\":\"message\",\"id\":\"msg_test\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"你好\"}]}],\"usage\":{\"input_tokens\":10,\"output_tokens\":2}}}\n\n"
}
func excelTestContext(path string) (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("POST", path, nil)
	return c, rec
}
func excelTestUpstream(wire string, status int) *httpUpstreamRecorder {
	return &httpUpstreamRecorder{resp: &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(wire))}}
}

func TestExcelBPSRoutingAndUsage(t *testing.T) {
	for _, stream := range []bool{true, false} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			u := excelTestUpstream(excelTestWire(), 200)
			s := openAIClientToolsTestService(u)
			a := excelTestAccount()
			a.Proxy = &Proxy{Protocol: "http", Host: "127.0.0.1", Port: 8888}
			c, rec := excelTestContext("/v1/responses")
			c.Request.Header.Set("x-codex-turn-state", "不应转发")
			c.Request.Header.Set("Cookie", "不应转发")
			body := []byte(fmt.Sprintf(`{"model":"gpt-6-astra","input":"你好","stream":%t,"reasoning":{"effort":"max"}}`, stream))
			result, err := s.Forward(context.Background(), c, a, body)
			require.NoError(t, err)
			require.Contains(t, rec.Body.String(), "你好")
			require.Equal(t, 10, result.Usage.InputTokens)
			require.Equal(t, 2, result.Usage.OutputTokens)
			require.Equal(t, "bps.openai.com", u.lastReq.URL.Host)
			require.Equal(t, a.Proxy.URL(), u.lastProxyURL)
			require.Empty(t, u.lastReq.Header.Get("Cookie"))
			require.Empty(t, u.lastReq.Header.Get("x-codex-turn-state"))
			require.True(t, HTTPUpstreamRedirectsDisabled(u.lastReq.Context()))
			require.Equal(t, "xhigh", gjson.GetBytes(u.lastBody, "reasoning_effort").String())
			require.Equal(t, "/basispoints/api/responses", result.UpstreamEndpoint)
		})
	}
}
func TestExcelBPSFailuresNeverReplayAcceptedGeneration(t *testing.T) {
	for _, tc := range []struct {
		name, wire string
		status     int
	}{
		{"拒绝访问", `{"error":{"code":"model_not_allowed","message":"secret-token"}}`, 403},
		{"未完成", "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_test\"}}\n\n", 200},
		{"带内限流", "data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_test\",\"status\":\"failed\",\"error\":{\"code\":\"rate_limit_exceeded\",\"message\":\"secret-token\"}}}\n\n", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := excelTestUpstream(tc.wire, tc.status)
			s := openAIClientToolsTestService(u)
			c, rec := excelTestContext("/v1/responses")
			_, err := s.Forward(context.Background(), c, excelTestAccount(), []byte(`{"model":"gpt-6-astra","input":"你好"}`))
			require.Error(t, err)
			var failover *UpstreamFailoverError
			require.NotErrorAs(t, err, &failover)
			require.Len(t, u.requests, 1)
			require.NotContains(t, rec.Body.String(), "secret-token")
			require.GreaterOrEqual(t, rec.Code, 400)
		})
	}
}
func TestExcelBPSCompatibleEntrypoints(t *testing.T) {
	for _, format := range []string{"chat", "anthropic"} {
		for _, stream := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/%t", format, stream), func(t *testing.T) {
				u := excelTestUpstream(excelTestWire(), 200)
				s := openAIClientToolsTestService(u)
				c, rec := excelTestContext("/v1/" + format)
				body := []byte(fmt.Sprintf(`{"model":"gpt-6-astra","messages":[{"role":"user","content":"你好"}],"max_tokens":100,"stream":%t}`, stream))
				var result *OpenAIForwardResult
				var err error
				if format == "chat" {
					result, err = s.ForwardAsChatCompletions(context.Background(), c, excelTestAccount(), body, "", "")
				} else {
					result, err = s.ForwardAsAnthropic(context.Background(), c, excelTestAccount(), body, "", "")
				}
				require.NoError(t, err)
				require.Equal(t, "bps.openai.com", u.lastReq.URL.Host)
				require.Contains(t, rec.Body.String(), "你好")
				require.Equal(t, 10, result.Usage.InputTokens)
			})
		}
	}
}
func TestExcelBPSWebSocketAdapter(t *testing.T) {
	u := excelTestUpstream(excelTestWire(), 200)
	s := openAIClientToolsTestService(u)
	c, _ := excelTestContext("/v1/responses")
	var frames [][]byte
	result, err := s.proxyExcelBPSWSTurn(context.Background(), c, excelTestAccount(), []byte(`{"type":"response.create","model":"gpt-6-astra","input":"你好"}`), "gpt-6-astra", func(p []byte) error { frames = append(frames, p); return nil })
	require.NoError(t, err)
	require.True(t, result.OpenAIWSMode)
	require.NotEmpty(t, frames)
	require.NotEmpty(t, result.wsAccountFailoverReplayInput)
	for _, frame := range frames {
		require.True(t, json.Valid(frame))
	}
}
func TestExcelBPSAccountModelBoundary(t *testing.T) {
	a := excelTestAccount()
	a.Credentials["model_mapping"] = map[string]any{"alias": "gpt-6-astra"}
	a.Extra["openai_excel_bps_models"] = []any{"gpt-6-astra"}
	require.True(t, a.IsExcelBPSEnabledForModel("alias"))
	require.False(t, a.IsExcelBPSEnabledForModel("gpt-6-sol"))
	a.Type = AccountTypeAPIKey
	require.False(t, a.IsExcelBPSEnabled())
	require.Error(t, validateExcelBPSAccount(a))
}

type excelSettingsRepo struct {
	SettingRepository
	raw string
	err error
}

func (r *excelSettingsRepo) GetValue(context.Context, string) (string, error) {
	if r.err != nil {
		return "", r.err
	}
	if r.raw == "" {
		return "", ErrSettingNotFound
	}
	return r.raw, nil
}
func (r *excelSettingsRepo) Set(_ context.Context, _ string, v string) error { r.raw = v; return r.err }
func TestExcelBPSSettingsImmediateAndFailClosed(t *testing.T) {
	repo := &excelSettingsRepo{}
	settings := &SettingService{settingRepo: repo}
	cfg, err := settings.GetExcelBPSConfig(context.Background())
	require.NoError(t, err)
	require.True(t, cfg.Enabled)
	cfg.Enabled = false
	require.NoError(t, settings.SaveExcelBPSConfig(context.Background(), cfg))
	s := &OpenAIGatewayService{settingService: settings}
	require.False(t, s.excelBPSGloballyEnabled(context.Background()))
	images, err := settings.GetExcelBPSImageRelaySettings(context.Background())
	require.NoError(t, err)
	require.False(t, images.Enabled)
	cfg.Images.Mode = "relay"
	cfg.Images.BaseURL = "http://127.0.0.1"
	require.Error(t, settings.SaveExcelBPSConfig(context.Background(), cfg))
	repo.err = errors.New("数据库不可用")
	require.False(t, s.excelBPSGloballyEnabled(context.Background()))
}
func TestExcelBPSImageAdmissionRelease(t *testing.T) {
	cfg := DefaultExcelBPSConfig().Images
	cfg.MaxRequests = 1
	s := &OpenAIGatewayService{}
	release, err := s.admitExcelBPSImageRequest(nil, cfg, 100)
	require.NoError(t, err)
	_, err = s.admitExcelBPSImageRequest(nil, cfg, 100)
	require.Error(t, err)
	release()
	release()
	next, err := s.admitExcelBPSImageRequest(nil, cfg, 100)
	require.NoError(t, err)
	next()
}
func TestExcelOAuthProfileUsesOwnClientAndCallback(t *testing.T) {
	s := NewOpenAIOAuthService(nil, nil)
	defer s.sessionStore.Stop()
	result, err := s.GenerateAuthURL(context.Background(), nil, "http://untrusted.invalid", "openai", "excel")
	require.NoError(t, err)
	require.Contains(t, result.AuthURL, "client_id="+openai.ExcelClientID)
	session, ok := s.sessionStore.Get(result.SessionID)
	require.True(t, ok)
	require.Equal(t, openai.ExcelRedirectURI, session.RedirectURI)
	require.Equal(t, openai.ExcelClientID, session.ClientID)
	require.True(t, time.Since(session.CreatedAt) < time.Minute)
}

func TestExcelBPSNativeAttachmentsAndUploadFailure(t *testing.T) {
	var pngBytes bytes.Buffer
	require.NoError(t, png.Encode(&pngBytes, image.NewRGBA(image.Rect(0, 0, 1, 1))))
	imageURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes.Bytes())
	for _, status := range []int{200, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			u := excelTestUpstream(excelTestWire(), 200)
			u.responses = []*http.Response{
				{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"openai_file_id":"file-test"}`))},
				{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(excelTestWire()))},
			}
			s := openAIClientToolsTestService(u)
			c, rec := excelTestContext("/v1/responses")
			a := excelTestAccount()
			a.ID += int64(status)
			body, _ := json.Marshal(map[string]any{"model": "gpt-6-astra", "input": []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_image", "image_url": imageURL}}}}})
			_, err := s.Forward(context.Background(), c, a, body)
			require.Contains(t, u.requests[0].URL.Path, "attachments")
			require.Contains(t, u.requests[0].Header.Get("Content-Type"), "multipart/form-data")
			if status == 200 {
				require.NoError(t, err)
				require.Len(t, u.requests, 2)
				require.Contains(t, string(u.bodies[1]), "file-test")
				require.NotContains(t, string(u.bodies[1]), "base64")
				require.Contains(t, rec.Body.String(), "你好")
			} else {
				require.Error(t, err)
				require.Len(t, u.requests, 1)
			}
		})
	}
}
func TestExcelBPSIncompleteStreamUsesClientProtocol(t *testing.T) {
	for _, format := range []string{"chat", "anthropic"} {
		u := excelTestUpstream("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_test\"}}\n\n", 200)
		s := openAIClientToolsTestService(u)
		c, rec := excelTestContext("/v1/" + format)
		_, err := s.forwardExcelBPS(context.Background(), c, excelTestAccount(), []byte(`{"model":"gpt-6-astra","input":"你好","stream":true}`), time.Now(), format)
		require.Error(t, err)
		require.Contains(t, rec.Body.String(), "basispoints_stream_incomplete")
		require.NotContains(t, rec.Body.String(), "event: response.failed")
		if format == "anthropic" {
			require.Contains(t, rec.Body.String(), "event: error")
		} else {
			require.Contains(t, rec.Body.String(), "data: [DONE]")
		}
	}
}
func TestExcelBPSColdDownKeepsLongerRetryAfter(t *testing.T) {
	s := &OpenAIGatewayService{}
	a := excelTestAccount()
	s.coolDownExcelBPS(context.Background(), a, "600")
	s.coolDownExcelBPS(context.Background(), a, "1")
	until, _ := s.excelBPSCooldownUntil.Load(a.ID)
	require.Greater(t, time.Until(until.(time.Time)), 9*time.Minute)
}
func TestExcelBPSExcelOnlyCannotUseNativeRoute(t *testing.T) {
	u := excelTestUpstream(excelTestWire(), 200)
	s := openAIClientToolsTestService(u)
	cfg := DefaultExcelBPSConfig()
	cfg.Enabled = false
	raw, _ := json.Marshal(cfg)
	s.settingService = &SettingService{settingRepo: &excelSettingsRepo{raw: string(raw)}}
	c, rec := excelTestContext("/v1/responses")
	_, err := s.Forward(context.Background(), c, excelTestAccount(), []byte(`{"model":"gpt-6-astra","input":"你好"}`))
	require.Error(t, err)
	require.Equal(t, 400, rec.Code)
	require.Empty(t, u.requests)
}

func TestExcelBPSRealWebSocketMultiTurn(t *testing.T) {
	u := excelTestUpstream("", 200)
	for _, id := range []string{"resp_bps", "resp_next"} {
		u.responses = append(u.responses, &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(strings.ReplaceAll(excelTestWire(), "resp_bps", id)))})
	}
	s := openAIClientToolsTestService(u)
	s.cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 5
	a := excelTestAccount()
	a.Credentials["model_mapping"] = map[string]any{"alias": "gpt-6-astra"}
	a.Extra["openai_excel_bps_models"] = []any{"gpt-6-astra"}
	results := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			results <- err
			return
		}
		defer conn.CloseNow()
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		_, first, err := conn.Read(ctx)
		if err != nil {
			results <- err
			return
		}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = r
		token, _, err := s.GetWebSocketRequestCredential(ctx, c, a, "alias")
		if err != nil {
			results <- err
			return
		}
		results <- s.ProxyResponsesWebSocketFromClient(ctx, c, conn, a, token, first, nil)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	defer client.CloseNow()
	for _, payload := range []string{
		`{"type":"response.create","model":"alias","input":[{"role":"user","content":"第一轮"}],"tools":[{"type":"function","name":"web_search","parameters":{"type":"object","properties":{}}}]}`,
		`{"type":"response.create","previous_response_id":"resp_bps","input":[{"role":"user","content":"第二轮"}]}`,
	} {
		require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(payload)))
		for {
			_, event, err := client.Read(ctx)
			require.NoError(t, err)
			if gjson.GetBytes(event, "type").String() == "response.completed" {
				break
			}
		}
	}
	require.NoError(t, client.Close(coderws.StatusNormalClosure, "完成"))
	require.NoError(t, <-results)
	require.Len(t, u.requests, 2)
	for _, req := range u.requests {
		require.Equal(t, "bps.openai.com", req.URL.Host)
		require.Empty(t, req.Header.Get("X-Codex-Turn-State"))
	}
	require.Contains(t, string(u.bodies[0]), "web_search")
	require.Contains(t, string(u.bodies[1]), "web_search", "后续 WS 帧继承本连接的工具目录")
	require.Contains(t, string(u.bodies[1]), "第一轮")
	require.Contains(t, string(u.bodies[1]), "第二轮")
	require.NotContains(t, string(u.bodies[1]), "previous_response_id")
}

func TestExcelBPSWebSocketNativeModelUsesPrimaryCredential(t *testing.T) {
	u := excelTestUpstream(excelTestWire(), 200)
	s := openAIClientToolsTestService(u)
	a := excelTestAccount()
	a.Credentials["client_id"] = openai.ClientID
	a.Credentials["access_token"] = "synthetic-codex-primary"
	a.Extra["openai_excel_bps_models"] = []any{"gpt-6-astra"}
	c, _ := excelTestContext("/v1/responses")
	body := []byte(`{"type":"response.create","model":"gpt-5.6-sol","input":"继续","stream":true}`)
	_, err := s.proxyOpenAIWSHTTPBridgeTurn(context.Background(), c, a, "", body, len(body), "gpt-5.6-sol", "", "", "", "", 2, func([]byte) error { return nil })
	require.NoError(t, err)
	require.NotEqual(t, "bps.openai.com", u.lastReq.URL.Host)
	require.Equal(t, "Bearer synthetic-codex-primary", u.lastReq.Header.Get("Authorization"))
}
func TestExcelBPSConfigurationFailureDoesNotFallBack(t *testing.T) {
	u := excelTestUpstream(excelTestWire(), 200)
	s := openAIClientToolsTestService(u)
	s.settingService = &SettingService{settingRepo: &excelSettingsRepo{err: errors.New("unavailable")}}
	a := excelTestAccount()
	a.Credentials["client_id"] = openai.ClientID
	c, rec := excelTestContext("/v1/responses")
	_, err := s.Forward(context.Background(), c, a, []byte(`{"model":"gpt-6-astra","input":"你好"}`))
	require.Error(t, err)
	require.Equal(t, 503, rec.Code)
	require.Empty(t, u.requests)
}

func TestExcelBPSRealWebSocketChannelMapping(t *testing.T) {
	u := excelTestUpstream("", 200)
	for _, id := range []string{"resp_bps", "resp_next"} {
		u.responses = append(u.responses, &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(strings.ReplaceAll(excelTestWire(), "resp_bps", id)))})
	}
	s := openAIClientToolsTestService(u)
	s.cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 5
	a := excelTestAccount()
	a.Credentials["model_mapping"] = map[string]any{"alias": "gpt-6-astra"}
	a.Extra["openai_excel_bps_models"] = []any{"gpt-6-astra"}
	results := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			results <- err
			return
		}
		defer conn.CloseNow()
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		_, first, err := conn.Read(ctx)
		if err != nil {
			results <- err
			return
		}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = r
		token, _, err := s.GetWebSocketRequestCredential(ctx, c, a, "alias")
		if err != nil {
			results <- err
			return
		}
		results <- s.ProxyResponsesWebSocketFromClient(ctx, c, conn, a, token, first, &OpenAIWSIngressHooks{MapRequestModel: func(turn int, model string) (string, error) {
			if model != "public-model" {
				return "", fmt.Errorf("意外重复映射或丢失原始模型: %s", model)
			}
			return "alias", nil
		}})
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	defer client.CloseNow()
	for _, payload := range []string{
		`{"type":"response.create","model":"public-model","input":[{"role":"user","content":"第一轮"}],"tools":[{"type":"function","name":"web_search","parameters":{"type":"object","properties":{}}}]}`,
		`{"type":"response.create","previous_response_id":"resp_bps","input":[{"role":"user","content":"第二轮"}]}`,
	} {
		require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(payload)))
		for {
			_, event, err := client.Read(ctx)
			require.NoError(t, err)
			if gjson.GetBytes(event, "type").String() == "response.completed" {
				break
			}
		}
	}
	require.NoError(t, client.Close(coderws.StatusNormalClosure, "完成"))
	require.NoError(t, <-results)
	require.Len(t, u.requests, 2)
	for _, req := range u.requests {
		require.Equal(t, "bps.openai.com", req.URL.Host)
		require.Empty(t, req.Header.Get("X-Codex-Turn-State"))
	}
	require.Contains(t, string(u.bodies[0]), "web_search")
	require.Contains(t, string(u.bodies[1]), "web_search", "后续 WS 帧继承本连接的工具目录")
	require.Contains(t, string(u.bodies[1]), "第一轮")
	require.Contains(t, string(u.bodies[1]), "第二轮")
	require.NotContains(t, string(u.bodies[1]), "previous_response_id")
}
