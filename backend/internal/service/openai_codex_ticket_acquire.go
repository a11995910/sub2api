package service

import (
	"context"
	"net/http"
)

// acquireOpenAIWSWithCodexTicket 在获取连接前应用门票及其绑定代理。
func (s *OpenAIGatewayService) acquireOpenAIWSWithCodexTicket(ctx context.Context, req openAIWSAcquireRequest, model string) (*openAIWSConnLease, error) {
	req.Headers = req.Headers.Clone()
	if req.Headers == nil {
		req.Headers = make(http.Header)
	}
	// 账号模式开启时，新握手不沿用上一模型或失效门票遗留的状态头。
	if isOpenAICodexTicketAccount(req.Account) && s.openAICodexTicketEnabledContext(ctx) {
		req.Headers.Del(openAICodexTurnStateHeader)
	}
	ticket, ticketErr := s.bindOpenAICodexTicket(ctx, req.Account, model, req.Headers)
	if ticketErr != nil {
		return nil, ticketErr
	}
	if ticket != nil {
		req.ProxyURL = s.openAICodexTicketRequestProxy(ctx, ticket, req.ProxyURL)
		req.codexTicket = ticket
		req.codexTicketUsable = func() bool { return s.openAICodexTicketBindingUsable(req.Account, ticket) }
		req.observeCodexTicket = s.observeOpenAICodexTicketBinding(req.Account, ticket)
	}

	return s.getOpenAIWSConnPool().Acquire(ctx, req)
}
