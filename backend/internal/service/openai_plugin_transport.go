package service

import (
	"context"
	"io"
	"net/http"
)

func (s *OpenAIGatewayService) SetPluginManager(manager *PluginManager) {
	s.pluginManager = manager
}

// doOpenAIUpstream 只在 OpenAI OAuth 能力绑定已启用时把真实请求交给插件。
// 插件返回标准 http.Response，响应解析、错误映射、SSE 和计费仍由现有核心链处理。
func (s *OpenAIGatewayService) doOpenAIUpstream(request *http.Request, proxyURL string, account *Account) (*http.Response, error) {
	// 响应关闭时取消本次上游读取，同时保留原请求上下文供正常重试使用。
	ctx, cancel := context.WithCancel(request.Context())
	response, err := s.doOpenAIUpstreamOnce(request.Clone(ctx), proxyURL, account)
	if err != nil || response == nil || response.Body == nil {
		cancel()
		return response, err
	}
	response.Body = &openAIUpstreamCancelableBody{ReadCloser: response.Body, cancel: cancel}
	return response, nil
}

type openAIUpstreamCancelableBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *openAIUpstreamCancelableBody) Close() error {
	b.cancel()
	return b.ReadCloser.Close()
}

func (s *OpenAIGatewayService) doOpenAIUpstreamOnce(request *http.Request, proxyURL string, account *Account) (response *http.Response, err error) {
	if ticket, _ := request.Context().Value(openAICodexTicketBindingKey{}).(*openAICodexTicket); ticket != nil {
		if !s.openAICodexTicketBindingUsable(account, ticket) {
			return nil, wrapOpenAITurnStateUnavailable(ErrOpenAICodexTicketUnavailable)
		}
		proxyURL = s.openAICodexTicketRequestProxy(request.Context(), ticket, proxyURL)
		observe := s.observeOpenAICodexTicketBinding(account, ticket)
		defer func() {
			status := 0
			if response != nil {
				status = response.StatusCode
			}
			observe(request.Context(), status, err, nil)
			if err == nil && response != nil && response.Body != nil {
				response.Body = &openAICodexTicketBody{ReadCloser: response.Body, ctx: request.Context(), observe: observe}
			}
		}()
	}
	if s.pluginManager != nil {
		response, handled, err := s.pluginManager.RoundTripOpenAIOAuth(request.Context(), request, proxyURL, account)
		if handled {
			return response, err
		}
	}
	return s.httpUpstream.Do(request, proxyURL, account.ID, account.Concurrency)
}

// doOpenAIAccountTestUpstream 让 OpenAI OAuth 账号测试与真实转发使用同一插件路径。
// API Key 和未命中插件的账号保持各自原有的 HTTPUpstream 行为。
func (s *AccountTestService) doOpenAIAccountTestUpstream(
	request *http.Request,
	proxyURL string,
	account *Account,
	useTLSFallback bool,
) (*http.Response, error) {
	if s.pluginManager != nil {
		response, handled, err := s.pluginManager.RoundTripOpenAIOAuth(request.Context(), request, proxyURL, account)
		if handled {
			return response, err
		}
	}
	if useTLSFallback {
		return s.httpUpstream.DoWithTLS(
			request,
			proxyURL,
			account.ID,
			account.Concurrency,
			s.tlsFPProfileService.ResolveTLSProfile(account),
		)
	}
	return s.httpUpstream.Do(request, proxyURL, account.ID, account.Concurrency)
}
