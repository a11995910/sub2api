package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestAccountProtectionWebSocketWaitPreservesConnectionAndPayload(t *testing.T) {
	for _, mode := range []string{OpenAIWSIngressModePassthrough, OpenAIWSIngressModeCtxPool} {
		t.Run(mode, func(t *testing.T) {
			rotation, mr, rdb := protectionWaitFixture(t, 1)
			upstream := newStagedPassthroughConn()
			cfg := passthroughLifecycleConfig()
			cfg.Gateway.OpenAIWS.OAuthEnabled = true
			cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 1
			cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
			cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
			cfg.Gateway.OpenAIWS.QueueLimitPerConn = 8
			account := passthroughLifecycleAccount()
			account.ID, account.Type = 1, AccountTypeOAuth
			account.Credentials = map[string]any{"access_token": "测试令牌"}
			account.Extra = map[string]any{"openai_oauth_responses_websockets_v2_mode": mode}
			svc := newPassthroughLifecycleService(cfg, upstream)
			svc.accountTimeRotation = rotation
			if mode == OpenAIWSIngressModeCtxPool {
				pool := newOpenAIWSConnPool(cfg)
				pool.setClientDialerForTest(&stagedPassthroughDialer{conn: &protectionNativeWSConn{upstream}})
				svc.openaiWSPool = pool
				t.Cleanup(func() { pool.Close() })
			}
			server, serverErr := startPassthroughLifecycleServer(t, context.Background(), svc, account)
			defer server.Close()
			client := dialPassthroughLifecycleClientWithPayload(t, server, `{"type":"response.create","model":"gpt-5.1","stream":false,"prompt_cache_key":"固定缓存键","input":[{"role":"user","content":"开始"}]}`)
			defer client.CloseNow()
			first := requirePassthroughUpstreamWrite(t, upstream, time.Second)
			upstream.Send(`{"type":"response.completed","response":{"id":"resp_cached","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`)
			_, err := readPassthroughLifecycleFrame(t, client, time.Second)
			require.NoError(t, err)
			second := []byte(`{"type":"response.create","model":"gpt-5.1","stream":false,"previous_response_id":"resp_cached","prompt_cache_key":"固定缓存键","input":[{"role":"user","content":"开始"},{"role":"user","content":"继续"}]}`)
			require.NoError(t, client.Write(context.Background(), websocket.MessageText, second))
			waitProtectionQueue(t, rdb, 1)
			// 等待超过网络写超时和首字超时，仍不能关闭连接或提前发送。
			select {
			case frame := <-upstream.writes:
				t.Fatalf("等待额度时提前发送：%s", frame)
			case err := <-serverErr:
				t.Fatalf("排队被误判为连接故障：%v", err)
			case <-time.After(1200 * time.Millisecond):
			}
			// 客户端 ping 的 pong 仍由同一读循环处理。
			pingCtx, cancelPing := context.WithTimeout(context.Background(), time.Second)
			readDone := make(chan error, 1)
			go func() {
				_, _, err := client.Read(context.Background())
				readDone <- err
			}()
			require.NoError(t, client.Ping(pingCtx))
			cancelPing()
			mr.SetTime(time.Now().Add(61 * time.Second))
			forwarded := requirePassthroughUpstreamWrite(t, upstream, 3*time.Second)
			require.Equal(t, "resp_cached", gjson.GetBytes(forwarded, "previous_response_id").String())
			// 账号身份改写可能规范化缓存键，但不得因等待丢弃续链或当前输入。
			require.Equal(t, gjson.GetBytes(first, "prompt_cache_key").String(), gjson.GetBytes(forwarded, "prompt_cache_key").String())
			inputs := gjson.GetBytes(forwarded, "input").Array()
			require.NotEmpty(t, inputs)
			require.Equal(t, "继续", inputs[len(inputs)-1].Get("content").String())
			upstream.Send(`{"type":"response.completed","response":{"id":"resp_next","usage":{"input_tokens":1,"output_tokens":1}}}`)
			require.NoError(t, <-readDone)
			waitProtectionQueue(t, rdb, 0)
			require.NoError(t, client.CloseNow())
			select {
			case <-serverErr:
			case <-time.After(3 * time.Second):
				t.Fatal("客户端断线后连接没有回收")
			}
		})
	}
}

func TestAccountProtectionWebSocketCancelReleasesWaiter(t *testing.T) {
	for _, action := range []string{"断线", "取消轮次"} {
		t.Run(action, func(t *testing.T) {
			rotation, _, rdb := protectionWaitFixture(t, 1)
			upstream := newStagedPassthroughConn()
			cfg := passthroughLifecycleConfig()
			cfg.Gateway.OpenAIWS.OAuthEnabled = true
			account := passthroughLifecycleAccount()
			account.ID, account.Type = 1, AccountTypeOAuth
			account.Credentials = map[string]any{"access_token": "测试令牌"}
			account.Extra = map[string]any{"openai_oauth_responses_websockets_v2_mode": OpenAIWSIngressModePassthrough}
			svc := newPassthroughLifecycleService(cfg, upstream)
			svc.accountTimeRotation = rotation
			require.NoError(t, rotation.AdmitRequest(context.Background(), account))
			server, serverErr := startPassthroughLifecycleServer(t, context.Background(), svc, account)
			defer server.Close()
			client := dialPassthroughLifecycleClient(t, server)
			defer client.CloseNow()
			waitProtectionQueue(t, rdb, 1)
			if action == "断线" {
				require.NoError(t, client.CloseNow())
			} else {
				require.NoError(t, client.Write(context.Background(), websocket.MessageText, []byte(`{"type":"response.cancel"}`)))
			}
			select {
			case err := <-serverErr:
				require.Error(t, err)
			case <-time.After(2 * time.Second):
				t.Fatal("客户端取消没有及时停止等待")
			}
			waitProtectionQueue(t, rdb, 0)
			select {
			case <-upstream.writes:
				t.Fatal("已取消轮次不应发送到上游")
			default:
			}
		})
	}
}

// 复用同一可控上游，覆盖连接池路径的 JSON 写入和真实续链读取。
type protectionNativeWSConn struct{ *stagedPassthroughConn }

func (c *protectionNativeWSConn) WriteJSON(ctx context.Context, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return c.WriteFrame(ctx, websocket.MessageText, body)
}

func TestAccountRandomRestWebSocketDrainsBeforeRest(t *testing.T) {
	for _, mode := range []string{OpenAIWSIngressModePassthrough, OpenAIWSIngressModeCtxPool} {
		t.Run(mode, func(t *testing.T) {
			rotation, mr, rdb := protectionWaitFixture(t, 0)
			randomRestTestConfig(rotation, 100, 0)
			now := time.Now()
			mr.SetTime(now)
			upstream := newStagedPassthroughConn()
			cfg := passthroughLifecycleConfig()
			cfg.Gateway.OpenAIWS.OAuthEnabled = true
			cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
			cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
			cfg.Gateway.OpenAIWS.QueueLimitPerConn = 8
			account := passthroughLifecycleAccount()
			account.ID, account.Type = 1, AccountTypeOAuth
			account.Credentials = map[string]any{"access_token": "测试令牌"}
			account.Extra = map[string]any{"openai_oauth_responses_websockets_v2_mode": mode}
			svc := newPassthroughLifecycleService(cfg, upstream)
			svc.accountTimeRotation = rotation
			if mode == OpenAIWSIngressModeCtxPool {
				pool := newOpenAIWSConnPool(cfg)
				pool.setClientDialerForTest(&stagedPassthroughDialer{conn: &protectionNativeWSConn{upstream}})
				svc.openaiWSPool = pool
				t.Cleanup(func() { pool.Close() })
			}
			server, serverErr := startPassthroughLifecycleServer(t, context.Background(), svc, account)
			defer server.Close()
			client := dialPassthroughLifecycleClientWithPayload(t, server, `{"type":"response.create","model":"gpt-5.1","stream":false,"prompt_cache_key":"固定缓存键","input":[{"role":"user","content":"开始"}]}`)
			defer client.CloseNow()
			requirePassthroughUpstreamWrite(t, upstream, time.Second)
			require.EqualValues(t, 1, rdb.SCard(context.Background(), "account_random_rest:{1}:active").Val())
			mr.SetTime(now.Add(time.Minute))
			require.Error(t, rotation.checkRandomRest(context.Background(), account))
			require.Equal(t, "draining", rdb.HGet(context.Background(), "account_random_rest:{1}", "phase").Val())
			// 休息只阻止接单，已经开始的轮次仍能正常返回完整终态。
			upstream.Send(`{"type":"response.completed","response":{"id":"resp_random_rest","model":"gpt-5.1","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"第一轮答案"}]}],"usage":{"input_tokens":1,"output_tokens":1}}}`)
			_, err := readPassthroughLifecycleFrame(t, client, time.Second)
			require.NoError(t, err)
			require.Eventually(t, func() bool { return rdb.SCard(context.Background(), "account_random_rest:{1}:active").Val() == 0 }, time.Second, time.Millisecond)
			require.Equal(t, "resting", rdb.HGet(context.Background(), "account_random_rest:{1}", "phase").Val())
			require.NoError(t, client.Write(context.Background(), websocket.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","stream":false,"previous_response_id":"resp_random_rest","prompt_cache_key":"固定缓存键","input":[{"role":"user","content":"开始"},{"role":"user","content":"继续"}]}`)))
			select {
			case err := <-serverErr:
				require.Error(t, err)
				require.True(t, isAccountProtectionError(err))
				payload, current := OpenAIWSCurrentTurnRetryPayload(err)
				require.True(t, current)
				require.NotEmpty(t, payload)
				require.Contains(t, string(payload), "继续")
				require.Contains(t, string(payload), "第一轮答案")
				require.NotContains(t, string(payload), "previous_response_id")
			case frame := <-upstream.writes:
				t.Fatalf("休息中不能发送或重放请求：%s", frame)
			case <-time.After(3 * time.Second):
				t.Fatal("休息拒绝未及时返回")
			}
			require.EqualValues(t, 0, rdb.SCard(context.Background(), "account_random_rest:{1}:active").Val())
		})
	}
}

func TestAccountRandomRestPassthroughTerminalWithoutUsage(t *testing.T) {
	for _, typ := range []websocket.MessageType{websocket.MessageText, websocket.MessageBinary} {
		t.Run(typ.String(), func(t *testing.T) {
			rotation, mr, rdb := protectionWaitFixture(t, 0)
			now := time.Now()
			mr.SetTime(now)
			randomRestTestConfig(rotation, 100, 0)
			upstream := newStagedPassthroughConn()
			cfg := passthroughLifecycleConfig()
			cfg.Gateway.OpenAIWS.OAuthEnabled = true
			account := passthroughLifecycleAccount()
			account.ID, account.Type = 1, AccountTypeOAuth
			account.Credentials = map[string]any{"access_token": "测试令牌"}
			account.Extra = map[string]any{"openai_oauth_responses_websockets_v2_mode": OpenAIWSIngressModePassthrough}
			svc := newPassthroughLifecycleService(cfg, upstream)
			svc.accountTimeRotation = rotation
			server, serverErr := startPassthroughLifecycleServer(t, context.Background(), svc, account)
			defer server.Close()
			client := dialPassthroughLifecycleClient(t, server)
			defer client.CloseNow()
			requirePassthroughUpstreamWrite(t, upstream, time.Second)
			upstream.frames <- stagedPassthroughFrame{messageType: typ, payload: []byte(`{"type":"response.completed","response":{}}`)}
			_, err := readPassthroughLifecycleFrame(t, client, time.Second)
			require.NoError(t, err)
			require.Eventually(t, func() bool { return rdb.SCard(context.Background(), "account_random_rest:{1}:active").Val() == 0 }, time.Second, time.Millisecond)
			mr.SetTime(now.Add(time.Minute))
			require.NoError(t, client.Write(context.Background(), websocket.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","previous_response_id":"unknown","input":"下一轮"}`)))
			select {
			case err := <-serverErr:
				require.True(t, IsAccountRestError(err))
				payload, current := OpenAIWSCurrentTurnRetryPayload(err)
				require.True(t, current, "缺少用量不能误把下一轮当作首轮")
				require.Empty(t, payload, "缺少完整历史不能重放首轮")
			case <-time.After(3 * time.Second):
				t.Fatal("连接退出未完成")
			}
		})
	}
}
