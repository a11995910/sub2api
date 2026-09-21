package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

type openAICodexTicketBindingKey struct{}

func (s *OpenAIGatewayService) bindOpenAICodexTicketRequest(req *http.Request, account *Account, model string) error {
	ticket, err := s.bindOpenAICodexTicket(req.Context(), account, model, req.Header)
	if err == nil {
		*req = *req.WithContext(context.WithValue(req.Context(), openAICodexTicketBindingKey{}, ticket))
	}
	return err
}

// 出站前再次检查完整版本，排队或重试不能继续发送已经被淘汰的绑定。
func (s *OpenAIGatewayService) openAICodexTicketBindingUsable(account *Account, ticket *openAICodexTicket) bool {
	if ticket == nil || account == nil || ticket.AccountID != account.ID {
		return false
	}
	current := s.lookupOpenAICodexTicket(account, ticket.Model)
	return current.valid(time.Now(), s.openAICodexTicketConfig().TargetLength) && current.State == ticket.State && current.ProxyURL == ticket.ProxyURL && current.CapturedAt.Equal(ticket.CapturedAt)
}

// 同一代门票与代理共同退休；旧请求的迟到失败不能淘汰新采集的绑定。
func (s *OpenAIGatewayService) invalidateOpenAICodexTicket(account *Account, ticket *openAICodexTicket) {
	if ticket == nil || account == nil {
		return
	}
	mu := &s.openaiCodexTicketLocks[uint64(account.ID)%64]
	mu.Lock()
	defer mu.Unlock()
	current := s.lookupOpenAICodexTicketLocked(account, ticket.Model)
	if current == nil || current.Invalidated || current.State != ticket.State || current.ProxyURL != ticket.ProxyURL || !current.CapturedAt.Equal(ticket.CapturedAt) {
		return
	}
	retired := *current
	retired.Invalidated = true
	retired.ExpiresAt = time.Now()
	s.openaiCodexTickets.Store(openAICodexTicketKey(account.ID, ticket.Model), &retired)
	if s.accountRepo != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.accountRepo.UpdateExtra(ctx, account.ID, map[string]any{openAICodexTicketExtraKey(ticket.Model): &retired}); err != nil {
			logger.L().Warn("门票出口失效状态保存失败", zap.Int64("account_id", account.ID), zap.String("model", ticket.Model))
		}
	}
}

// 不把客户端取消、账号鉴权失败、限流或普通服务过载误判为出口失效。
func openAICodexTicketBindingFailed(ctx context.Context, status int, err error, payload []byte) bool {
	if status == http.StatusForbidden || status == http.StatusProxyAuthRequired || status == http.StatusBadGateway || status == http.StatusGatewayTimeout {
		return true
	}
	if err != nil && !errors.Is(ctx.Err(), context.Canceled) && !errors.Is(err, context.Canceled) {
		var netErr net.Error
		if errors.As(err, &netErr) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
			return true
		}
		message := strings.ToLower(err.Error())
		for _, marker := range []string{"proxyconnect", "socks", "proxy authentication", "proxy error"} {
			if strings.Contains(message, marker) {
				return true
			}
		}
	}
	for _, path := range []string{"error.code", "response.error.code"} {
		switch gjson.GetBytes(payload, path).String() {
		case "invalid_turn_state", "turn_state_invalid", "turn_state_expired", "invalid_codex_turn_state", "codex_turn_state_expired":
			return true
		}
	}
	return false
}

type openAICodexTicketObservation func(context.Context, int, error, []byte)

func (s *OpenAIGatewayService) observeOpenAICodexTicketBinding(account *Account, ticket *openAICodexTicket) openAICodexTicketObservation {
	if ticket == nil {
		return nil
	}
	var once sync.Once
	return func(ctx context.Context, status int, err error, payload []byte) {
		if openAICodexTicketBindingFailed(ctx, status, err, payload) {
			once.Do(func() { s.invalidateOpenAICodexTicket(account, ticket) })
		}
	}
}

// 只观察有限大小的错误事件，不预读或修改业务响应，不保存正文。
type openAICodexTicketBody struct {
	io.ReadCloser
	ctx      context.Context
	observe  openAICodexTicketObservation
	line     []byte
	overflow bool
}

func (b *openAICodexTicketBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	remaining := p[:n]
	for len(remaining) > 0 {
		end := bytes.IndexByte(remaining, '\n')
		part := remaining
		if end >= 0 {
			part = remaining[:end]
		}
		if !b.overflow && len(b.line)+len(part) <= 64<<10 {
			b.line = append(b.line, part...)
		} else {
			b.overflow = true
			b.line = nil
		}
		if end < 0 {
			break
		}
		b.observeLine()
		remaining = remaining[end+1:]
	}
	if err != nil {
		b.observeLine()
		if !errors.Is(err, io.EOF) {
			b.observe(b.ctx, 0, err, nil)
		}
	}
	return n, err
}

func (b *openAICodexTicketBody) observeLine() {
	if !b.overflow {
		payload := bytes.TrimSpace(b.line)
		payload = bytes.TrimSpace(bytes.TrimPrefix(payload, []byte("data:")))
		b.observe(b.ctx, 0, nil, payload)
	}
	b.line, b.overflow = b.line[:0], false
}
