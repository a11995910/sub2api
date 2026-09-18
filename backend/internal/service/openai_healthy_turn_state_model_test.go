package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

const astraHealthyCreate = `{"type":"response.created","response":{"model":"gpt-6-astra"}}`
const lunaUnhealthyCreate = `{"type":"response.created","response":{"model":"gpt-5.6-luna"}}`

func modelHealthySSE(event string) string { return "data: " + event + "\n\n" + healthyTurnStateSSE() }

func TestOpenAIHealthyTurnStateModelMismatchHTTP(t *testing.T) {
	for _, tc := range []struct {
		name                                                 string
		replace, available, mismatchAgain, json, missingType bool
		wantStatus, wantCalls                                int
	}{
		{name: "模型降级后替换成功", replace: true, available: true, wantStatus: 200, wantCalls: 2},
		{name: "替换仍降级只试一次", replace: true, available: true, mismatchAgain: true, wantStatus: 502, wantCalls: 2},
		{name: "共享池为空", replace: true, wantStatus: 502, wantCalls: 1},
		{name: "只记录也拒绝错误模型", available: true, wantStatus: 502, wantCalls: 1},
		{name: "JSON 降级", replace: true, available: true, json: true, wantStatus: 200, wantCalls: 2},
		{name: "无类型分片 SSE", replace: true, available: true, missingType: true, wantStatus: 200, wantCalls: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := healthyTurnStateResponse(200, "降级状态", modelHealthySSE(lunaUnhealthyCreate))
			good := healthyTurnStateResponse(200, "", modelHealthySSE(astraHealthyCreate))
			if tc.json {
				bad.Header.Set("Content-Type", "application/json")
				bad.Body = io.NopCloser(strings.NewReader(`{"model":"gpt-5.6-luna","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"降级输出"}]}]}`))
			}
			if tc.missingType {
				bad.Header.Del("Content-Type")
				bad.Body = io.NopCloser(iotest.OneByteReader(strings.NewReader(modelHealthySSE(lunaUnhealthyCreate))))
			}
			if tc.mismatchAgain {
				good = healthyTurnStateResponse(200, "降级状态二", modelHealthySSE(lunaUnhealthyCreate))
			}
			upstream := &healthyTurnStateUpstream{responses: []*http.Response{bad, good}}
			svc := &OpenAIGatewayService{httpUpstream: upstream}
			account := &Account{ID: 1, Platform: PlatformOpenAI, Extra: map[string]any{openAIHealthyTurnStateRecordKey: true, openAIHealthyTurnStateReplaceKey: tc.replace}}
			c, req := healthyTurnStateRequest(t, svc, account, "客户端会话")
			req = svc.prepareOpenAIHealthyTurnStateRequest(c, account, req, "gpt-6-astra")
			req.Header.Set(openAICodexTurnStateHeader, "客户端旧头")
			entry := openAIHealthyTurnStateEntry{value: "可替换头", expiresAt: time.Now().Add(time.Minute)}
			if tc.available {
				require.True(t, svc.openaiHealthyTurnStates.store(openAIHealthyTurnStateScope{}, entry))
			}
			resp, err := svc.doOpenAIUpstreamWithHealthyTurnState(req, "", account)
			require.NoError(t, err)
			require.Equal(t, tc.wantStatus, resp.StatusCode)
			payload, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			require.Len(t, upstream.requests, tc.wantCalls)
			require.NotContains(t, string(payload), "gpt-5.6-luna")
			require.Equal(t, "客户端旧头", req.Header.Get(openAICodexTurnStateHeader))
			if tc.wantCalls == 2 {
				require.Equal(t, "可替换头", upstream.requests[1].Header.Get(openAICodexTurnStateHeader))
				require.Equal(t, upstream.bodies[0], upstream.bodies[1])
				require.Equal(t, upstream.requests[0].Header.Get("session_id"), upstream.requests[1].Header.Get("session_id"))
			}
			if tc.mismatchAgain {
				require.Empty(t, svc.openaiHealthyTurnStates.entries)
				require.False(t, svc.openaiHealthyTurnStates.store(openAIHealthyTurnStateScope{}, entry))
			}
			require.False(t, svc.openaiHealthyTurnStates.store(openAIHealthyTurnStateScope{}, openAIHealthyTurnStateEntry{value: "降级状态", expiresAt: time.Now().Add(time.Minute)}))
			require.Empty(t, svc.openaiHealthyTurnStates.held)
		})
	}
}

func TestOpenAIHealthyTurnStateModelMismatchPersistentFailure(t *testing.T) {
	store := &healthyStateStoreStub{}
	upstream := &healthyTurnStateUpstream{responses: []*http.Response{healthyTurnStateResponse(503, "", "忙"), healthyTurnStateResponse(200, "", modelHealthySSE(lunaUnhealthyCreate))}}
	svc := &OpenAIGatewayService{httpUpstream: upstream, openaiHealthyTurnStates: openAIHealthyTurnStateCache{repo: store}}
	account := &Account{ID: 1, Platform: PlatformOpenAI, Extra: map[string]any{openAIHealthyTurnStateReplaceKey: true}}
	c, req := healthyTurnStateRequest(t, svc, account, "")
	req = svc.prepareOpenAIHealthyTurnStateRequest(c, account, req, "gpt-6-astra")
	resp, err := svc.doOpenAIUpstreamWithHealthyTurnState(req, "", account)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, 502, resp.StatusCode)
	require.Equal(t, []bool{false}, store.results)
	require.Equal(t, 1, store.starts)
	require.Len(t, upstream.requests, 2)
}

func TestOpenAIHealthyTurnStateModelMismatchProbe(t *testing.T) {
	for _, transport := range []string{"http", "websocket"} {
		t.Run(transport, func(t *testing.T) {
			upstream := &healthyTurnStateUpstream{responses: []*http.Response{healthyTurnStateResponse(200, "降级头", modelHealthySSE(lunaUnhealthyCreate))}}
			dialer := &healthyTurnStateProbeWSDialer{status: 101, conn: &healthyTurnStateProbeWSConn{events: [][]byte{[]byte(lunaUnhealthyCreate), []byte(healthyTurnStateDelta), []byte(healthyTurnStateDone)}}}
			svc := &AccountTestService{openaiGatewayService: &OpenAIGatewayService{httpUpstream: upstream, openaiWSPassthroughDialer: dialer}}
			result, err := svc.ProbeOpenAIHealthyTurnState(context.Background(), healthyTurnStateProbeAccount(), "", transport)
			require.NoError(t, err)
			require.Equal(t, "gpt-6-astra", result.Model)
			require.Equal(t, "unhealthy", result.Status)
			require.Contains(t, result.Message, "模型不一致")
			require.Empty(t, svc.openaiGatewayService.openaiHealthyTurnStates.entries)
		})
	}
}

type healthyModelSequenceDialer struct {
	conns   []openAIWSClientConn
	headers []http.Header
}

func (d *healthyModelSequenceDialer) Dial(_ context.Context, _ string, headers http.Header, _ string) (openAIWSClientConn, int, http.Header, error) {
	d.headers = append(d.headers, headers.Clone())
	if len(d.conns) == 0 {
		return nil, 503, nil, errors.New("未预期的握手")
	}
	conn := d.conns[0]
	d.conns = d.conns[1:]
	return conn, 101, http.Header{}, nil
}

func TestOpenAIHealthyTurnStateModelMismatchWS(t *testing.T) {
	for _, mode := range []string{"pool", "passthrough"} {
		for _, scenario := range []string{"替换成功", "再次降级", "严格续链", "关闭替换", "声明晚于首字"} {
			t.Run(mode+"/"+scenario, func(t *testing.T) {
				first := &healthyTurnStateProbeWSConn{events: [][]byte{[]byte(lunaUnhealthyCreate)}}
				second := &healthyTurnStateProbeWSConn{events: [][]byte{[]byte(astraHealthyCreate), []byte(healthyTurnStateDelta), []byte(healthyTurnStateDone)}}
				firstFrame, secondFrame := newStagedPassthroughConn(), newStagedPassthroughConn()
				if scenario == "声明晚于首字" {
					first.events = [][]byte{[]byte(healthyTurnStateDelta), []byte(lunaUnhealthyCreate)}
					firstFrame.Send(healthyTurnStateDelta)
				}
				firstFrame.Send(lunaUnhealthyCreate)
				nextEvent := astraHealthyCreate
				if scenario == "再次降级" {
					second.events = [][]byte{[]byte(lunaUnhealthyCreate)}
					nextEvent = lunaUnhealthyCreate
				}
				secondFrame.Send(nextEvent)
				secondFrame.Send(healthyTurnStateDelta)
				secondFrame.Send(healthyTurnStateDone)
				dialer := &healthyModelSequenceDialer{conns: []openAIWSClientConn{first, second}}
				if mode == "passthrough" {
					dialer.conns = []openAIWSClientConn{firstFrame, secondFrame}
				}
				svc := &OpenAIGatewayService{cfg: &config.Config{}}
				account := &Account{ID: 3, Platform: PlatformOpenAI, Concurrency: 2, Extra: map[string]any{openAIHealthyTurnStateRecordKey: true, openAIHealthyTurnStateReplaceKey: scenario != "关闭替换"}}
				c, _ := healthyTurnStateRequest(t, svc, account, "")
				entry := openAIHealthyTurnStateEntry{value: "可替换状态", expiresAt: time.Now().Add(time.Minute)}
				require.True(t, svc.openaiHealthyTurnStates.store(openAIHealthyTurnStateScope{}, entry))
				request := []byte(`{"type":"response.create","model":"gpt-6-astra","input":"原请求"}`)
				if scenario == "严格续链" {
					request = []byte(`{"type":"response.create","model":"gpt-6-astra","previous_response_id":"resp_original"}`)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				headers := http.Header{"Session_id": []string{"原会话"}}
				var read func() ([]byte, error)
				if mode == "pool" {
					pool := svc.getOpenAIWSConnPool()
					defer pool.Close()
					pool.setClientDialerForTest(dialer)
					lease, err := svc.acquireOpenAIWSWithHealthyTurnState(ctx, c, openAIWSAcquireRequest{Account: account, WSURL: "wss://chatgpt.com/backend-api/codex/responses", Headers: headers}, "gpt-6-astra")
					require.NoError(t, err)
					defer lease.Release()
					require.NoError(t, lease.WriteJSONContext(ctx, json.RawMessage(request)))
					read = func() ([]byte, error) { return lease.ReadMessageContext(ctx) }
				} else {
					conn, _, _, observer, err := svc.dialOpenAIWSWithHealthyTurnState(ctx, c, account, "gpt-6-astra", "wss://chatgpt.com/backend-api/codex/responses", headers, "", dialer)
					require.NoError(t, err)
					wrapped := &openAIHealthyTurnStateFrameConn{FrameConn: conn.(*stagedPassthroughConn), observer: observer}
					svc.prepareHealthyWSFrameGate(wrapped, account, "wss://chatgpt.com/backend-api/codex/responses", headers, "", dialer)
					defer wrapped.Close()
					require.NoError(t, wrapped.WriteFrame(ctx, coderws.MessageText, request))
					read = func() ([]byte, error) { _, p, e := wrapped.ReadFrame(ctx); return p, e }
				}
				if scenario == "声明晚于首字" {
					payload, err := read()
					require.NoError(t, err)
					require.Equal(t, healthyTurnStateDelta, string(payload))
				}
				payload, err := read()
				if scenario == "替换成功" {
					require.NoError(t, err)
					require.Equal(t, astraHealthyCreate, string(payload))
					_, err = read()
					require.NoError(t, err)
					_, err = read()
					require.NoError(t, err)
					if mode == "pool" {
						sent, _ := json.Marshal(second.sent)
						require.JSONEq(t, string(request), string(sent))
					} else {
						require.Equal(t, string(request), string(<-secondFrame.writes))
					}
					require.Equal(t, entry, svc.openaiHealthyTurnStates.entries[openAIHealthyTurnStateScope{}])
				} else {
					require.ErrorIs(t, err, errOpenAIUpstreamModelMismatch)
					require.Empty(t, payload)
				}
				wantCalls := 1
				if scenario == "替换成功" || scenario == "再次降级" {
					wantCalls = 2
				}
				require.Len(t, dialer.headers, wantCalls)
				if wantCalls == 2 {
					require.Equal(t, "可替换状态", dialer.headers[1].Get(openAICodexTurnStateHeader))
					require.Equal(t, headers.Get("session_id"), dialer.headers[1].Get("session_id"))
				}
				if scenario == "再次降级" {
					require.Empty(t, svc.openaiHealthyTurnStates.entries)
				}
				require.Empty(t, svc.openaiHealthyTurnStates.held)
			})
		}
	}
}
