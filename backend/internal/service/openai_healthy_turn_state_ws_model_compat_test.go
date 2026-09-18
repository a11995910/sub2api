package service

import (
	"context"
	"io"
	"testing"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

func TestOpenAIHealthyWSModelGatePreservesConcatenatedAndMalformedFrames(t *testing.T) {
	for _, test := range []struct {
		name, payload string
		healthy       bool
		mismatch      bool
	}{
		{"完整健康文档", astraHealthyCreate + healthyTurnStateDelta + healthyTurnStateDone, true, false},
		{"终止事件尾部文档", healthyTurnStateDelta + healthyTurnStateDone + `{"type":"error","error":{"message":"尾部错误"}}`, false, false},
		{"终止后其他模型文档", astraHealthyCreate + healthyTurnStateDone + lunaUnhealthyCreate, false, false},
		{"拼接文档模型不一致", lunaUnhealthyCreate + healthyTurnStateDelta + healthyTurnStateDone, false, true},
		{"非法 JSON", `{"type":"response.completed"`, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc := &OpenAIGatewayService{}
			account := &Account{ID: 56, Platform: PlatformOpenAI}
			attempt := svc.newOpenAIHealthyTurnStateAttempt(nil, account, "gpt-6-astra", "ws:测试上游", "", nil)
			observer := newOpenAIHealthyTurnStateObserver(attempt, healthyTurnStateResponse(200, "测试健康头", "").Header)
			gate := &openAIHealthyWSModelGate{observer: observer}
			gate.begin([]byte(`{"type":"response.create","model":"gpt-6-astra"}`))
			reads := 0
			kind, payload, err := gate.read(context.Background(), func() (coderws.MessageType, []byte, error) {
				reads++
				if reads > 1 {
					return 0, nil, io.EOF
				}
				return coderws.MessageText, []byte(test.payload), nil
			})
			require.Equal(t, 1, reads, "已完整收到的帧必须及时交回转发层")
			if test.mismatch {
				require.ErrorIs(t, err, errOpenAIUpstreamModelMismatch)
				require.Empty(t, payload)
			} else {
				require.NoError(t, err)
				require.Equal(t, coderws.MessageText, kind)
				require.Equal(t, test.payload, string(payload), "模型检查不能改写原始帧")
			}
			_, recorded := svc.openaiHealthyTurnStates.get(attempt.scope)
			require.Equal(t, test.healthy, recorded, "只有完整健康帧允许保存健康头")
		})
	}
}
