package service

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

type openAICodexTicketBindingKey struct{}

// 将模型审计绑定到本次请求使用的票，重试时必须覆盖上一请求的观察回调。
func (s *OpenAIGatewayService) observeOpenAICodexTicketRequestModel(c *gin.Context, req *http.Request, account *Account) {
	if c == nil {
		return
	}
	observer := upstreamResponseModelObserverFromContext(c)
	if observer == nil {
		observer = beginUpstreamResponseModelObservation(c)
	}
	observer.codexTicketModelObserved = nil
	ticket, _ := req.Context().Value(openAICodexTicketBindingKey{}).(*openAICodexTicket)
	if ticket != nil {
		observer.codexTicketModelObserved = func(model string) {
			if mismatch := upstreamModelMismatch(ticket.Model, model); mismatch != nil && *mismatch && s.openAICodexTicketPolicy(req.Context()).ModelMismatchInvalidation {
				s.invalidateOpenAICodexTicket(account, ticket, "model_mismatch")
			}
		}
	}
}

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
func (s *OpenAIGatewayService) invalidateOpenAICodexTicket(account *Account, ticket *openAICodexTicket, reason string) {
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
	retired.InvalidReason = reason
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
		reason := ""
		if codexTicketResponseModelMismatch(ticket.Model, payload) && s.openAICodexTicketPolicy(ctx).ModelMismatchInvalidation {
			reason = "model_mismatch"
		} else if openAICodexTicketBindingFailed(ctx, status, err, payload) {
			reason = "upstream_failure"
		}
		if reason != "" {
			once.Do(func() { s.invalidateOpenAICodexTicket(account, ticket, reason) })
		}
	}
}

// 同时观察模型声明和失效错误，不预读或修改业务响应。
type openAICodexTicketBody struct {
	io.ReadCloser
	ctx     context.Context
	observe openAICodexTicketObservation
	parser  *codexTicketResponseParser
}

func (b *openAICodexTicketBody) Read(p []byte) (int, error) {
	if b.parser == nil {
		b.parser = &codexTicketResponseParser{observe: func(payload []byte, _ string) { b.observe(b.ctx, 0, nil, payload) }}
	}
	n, err := b.ReadCloser.Read(p)
	b.parser.feed(p[:n])
	if err != nil {
		b.parser.finish()
		if !errors.Is(err, io.EOF) {
			b.observe(b.ctx, 0, err, nil)
		}
	}
	return n, err
}
