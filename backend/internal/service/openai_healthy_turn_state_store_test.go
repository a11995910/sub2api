package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type healthyStateStoreStub struct {
	HealthyTurnStateRepository
	starts, saves, releases int
	results                 []bool
	statuses                []int
	startErr, saveErr       error
	scopes                  []HealthyTurnStateScope
}

func (s *healthyStateStoreStub) Claim(_ context.Context, scope HealthyTurnStateScope, _ string) (*HealthyTurnStateValue, error) {
	s.scopes = append(s.scopes, scope)
	return &HealthyTurnStateValue{Value: "持久测试头", LeaseToken: "测试租约", ExpiresAt: time.Now().Add(time.Minute)}, nil
}
func (s *healthyStateStoreStub) Save(_ context.Context, scope HealthyTurnStateScope, _ HealthyTurnStateValue) (bool, error) {
	s.scopes = append(s.scopes, scope)
	s.saves++
	return s.saveErr == nil, s.saveErr
}
func (s *healthyStateStoreStub) Reject(_ context.Context, scope HealthyTurnStateScope, _ string) error {
	s.scopes = append(s.scopes, scope)
	return nil
}
func (s *healthyStateStoreStub) Start(_ context.Context, scope HealthyTurnStateScope, _ HealthyTurnStateValue, _ int) error {
	s.scopes = append(s.scopes, scope)
	s.starts++
	return s.startErr
}
func (s *healthyStateStoreStub) Complete(_ context.Context, scope HealthyTurnStateScope, _ HealthyTurnStateValue, success bool, status int) error {
	s.scopes = append(s.scopes, scope)
	s.results = append(s.results, success)
	s.statuses = append(s.statuses, status)
	return nil
}
func (s *healthyStateStoreStub) Release(_ context.Context, scope HealthyTurnStateScope, _ HealthyTurnStateValue) error {
	s.scopes = append(s.scopes, scope)
	s.releases++
	return nil
}

func TestHealthyTurnStateOrdinaryRequestsNeverCapture(t *testing.T) {
	store := &healthyStateStoreStub{}
	upstream := &healthyTurnStateUpstream{responses: []*http.Response{healthyTurnStateResponse(200, "默认采集头", healthyTurnStateSSE())}}
	svc := &OpenAIGatewayService{httpUpstream: upstream, openaiHealthyTurnStates: openAIHealthyTurnStateCache{repo: store}}
	proxyID := int64(12)
	account := &Account{ID: 7, Platform: PlatformOpenAI, ProxyID: &proxyID}
	_, req := healthyTurnStateRequest(t, svc, account, "默认记录会话")
	resp, err := svc.doOpenAIUpstreamWithHealthyTurnState(req, "", account)
	require.NoError(t, err)
	_, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Zero(t, store.saves, "普通请求不再自动记录健康头")
	require.Empty(t, store.scopes)
}

func TestHealthyTurnStatePersistentHTTPResults(t *testing.T) {
	for _, tc := range []struct {
		name      string
		response  *http.Response
		success   bool
		startFail bool
	}{
		{"完整响应成功", healthyTurnStateResponse(200, "", healthyTurnStateSSE()), true, false},
		{"首字后断流失败", healthyTurnStateResponse(200, "", "data: "+healthyTurnStateDelta+"\n\n"), false, false},
		{"空响应失败", healthyTurnStateResponse(200, "", ""), false, false},
		{"仍被限流", healthyTurnStateResponse(429, "", "限流"), false, false},
		{"持久化不可用时不发送替换", healthyTurnStateResponse(200, "", healthyTurnStateSSE()), false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &healthyStateStoreStub{}
			if tc.startFail {
				store.startErr = errors.New("测试存储不可用")
			}
			upstream := &healthyTurnStateUpstream{responses: []*http.Response{healthyTurnStateResponse(503, "", "忙"), tc.response}}
			svc := &OpenAIGatewayService{httpUpstream: upstream, openaiHealthyTurnStates: openAIHealthyTurnStateCache{repo: store}}
			account := &Account{ID: 1, Platform: PlatformOpenAI, Extra: map[string]any{openAIHealthyTurnStateReplaceKey: true}}
			_, req := healthyTurnStateRequest(t, svc, account, "会话")
			resp, err := svc.doOpenAIUpstreamWithHealthyTurnState(req, "", account)
			require.NoError(t, err)
			if tc.response.StatusCode == 200 {
				require.Empty(t, store.results, "响应头不代表完成")
			}
			_, err = io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			if tc.startFail {
				require.Len(t, upstream.requests, 1)
				require.Empty(t, store.results)
				require.Equal(t, 1, store.releases)
			} else {
				require.Len(t, upstream.requests, 2)
				require.Equal(t, []bool{tc.success}, store.results)
				require.Equal(t, []int{tc.response.StatusCode}, store.statuses)
			}
			require.Equal(t, 1, store.starts)
			for _, scope := range store.scopes {
				require.EqualValues(t, account.ID, scope.AccountID, "租约及结果必须保留当前账号")
				require.Equal(t, "gpt-test", scope.Model, "租约及结果必须保留当前实际模型")
			}
		})
	}
}

func TestHealthyTurnStateExplicitCaptureStorageFailure(t *testing.T) {
	store := &healthyStateStoreStub{saveErr: errors.New("测试存储不可用")}
	cache := &openAIHealthyTurnStateCache{repo: store}
	stored := cache.store(openAIHealthyTurnStateScope{accountID: 7, model: "gpt-test"}, openAIHealthyTurnStateEntry{value: "测试状态头", expiresAt: time.Now().Add(time.Minute)})
	require.False(t, stored)
	require.Equal(t, 1, store.saves)
	require.Empty(t, cache.entries, "持久化失败不能降级到内存")
}

func TestHealthyTurnStatePersistentWSResults(t *testing.T) {
	for _, success := range []bool{true, false} {
		store := &healthyStateStoreStub{}
		attempt := &openAIHealthyTurnStateAttempt{cache: &openAIHealthyTurnStateCache{repo: store}, borrowed: openAIHealthyTurnStateEntry{value: "测试头", leaseToken: "测试租约"}}
		require.True(t, attempt.started(503))
		require.True(t, attempt.started(503))
		require.Equal(t, 1, store.starts, "不重复累计发送")
		attempt.httpStatus = 101
		lease := &openAIWSConnLease{healthyTurnState: newOpenAIHealthyTurnStateObserver(attempt, nil)}
		lease.observeHealthyTurnState([]byte(healthyTurnStateDelta), nil)
		require.Empty(t, store.results)
		if success {
			lease.observeHealthyTurnState([]byte(healthyTurnStateDone), nil)
		} else {
			lease.observeHealthyTurnState(nil, io.ErrUnexpectedEOF)
		}
		lease.observeHealthyTurnState(nil, io.EOF)
		require.Equal(t, []bool{success}, store.results)
		require.Equal(t, []int{101}, store.statuses)
	}
}
