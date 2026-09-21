package service

import (
	"context"
	"time"

	openaiwsv2 "github.com/Wei-Shaw/sub2api/internal/service/openai_ws_v2"
	coderws "github.com/coder/websocket"
)

type openAICodexTicketFrameConn struct {
	openaiwsv2.FrameConn
	observe openAICodexTicketObservation
}

func (c *openAICodexTicketFrameConn) ReadFrame(ctx context.Context) (coderws.MessageType, []byte, error) {
	kind, payload, err := c.FrameConn.ReadFrame(ctx)
	c.observe(ctx, 0, err, payload)
	return kind, payload, err
}

func (c *openAICodexTicketFrameConn) WriteFrame(ctx context.Context, kind coderws.MessageType, payload []byte) error {
	err := c.FrameConn.WriteFrame(ctx, kind, payload)
	c.observe(ctx, 0, err, nil)
	return err
}

// 独立轮次切换模型、门票或出口时重新握手，不能在旧连接上仅更换请求体。
func (s *OpenAIGatewayService) openAICodexTicketLeaseMatches(account *Account, model string, lease *openAIWSConnLease) bool {
	if lease == nil || lease.conn == nil {
		return true
	}
	bound := lease.conn.codexTicket
	current := s.lookupOpenAICodexTicket(account, model)
	if bound == nil {
		return !current.valid(time.Now(), s.openAICodexTicketConfig().TargetLength)
	}
	return current.valid(time.Now(), s.openAICodexTicketConfig().TargetLength) && bound.Model == current.Model && bound.State == current.State && bound.ProxyURL == current.ProxyURL && bound.CapturedAt.Equal(current.CapturedAt)
}
