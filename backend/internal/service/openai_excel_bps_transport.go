package service

import (
	"context"
	"fmt"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/Wei-Shaw/sub2api/internal/util/transportdiag"
	"github.com/gin-gonic/gin"
)

// 请求沿用账号已有代理；发送后不因传输失败重放，避免重复计费或工具执行。
type excelBPSForwardError struct{ code string }

func (e *excelBPSForwardError) Error() string { return "excel BPS: " + e.code }
func (s *OpenAIGatewayService) doExcelBPSRequest(ctx context.Context, c *gin.Context, account *Account, scope string, body []byte, token, accountID string) (*http.Response, string, error) {
	proxy := ""
	if account.Proxy != nil {
		proxy = account.Proxy.URL()
	}
	req, err := newExcelBPSRequest(ctx, body, token, accountID)
	if err != nil {
		return nil, proxy, err
	}
	c.Set("excel_bps_upstream_attempt", c.GetInt("excel_bps_upstream_attempt")+1)
	resp, err := s.httpUpstream.Do(req, proxy, account.ID, account.Concurrency)
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		recordExcelBPSTransportFailure(ctx, c, account, scope, proxy, err, "transport", 1, false)
		return nil, proxy, err
	}
	if resp == nil || resp.Body == nil {
		return nil, proxy, fmt.Errorf("BPS 上游返回空响应")
	}
	s.guardExcelBPSProgress(ctx, resp)
	return resp, proxy, nil
}
func recordExcelBPSTransportFailure(ctx context.Context, c *gin.Context, account *Account, scope, proxy string, err error, stage string, attempt int, retry bool) {
	if isExcelBPSClientCancellation(c, err) {
		return
	}
	reason := transportdiag.Classify(err)
	setOpsUpstreamError(c, http.StatusBadGateway, "Excel BPS 传输失败："+reason, "")
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{Platform: account.Platform, AccountID: account.ID, UpstreamURL: basispoints.ResponsesURL, Kind: "request_error", Message: "Excel BPS 传输失败：" + reason})
}
func (s *OpenAIGatewayService) getExcelBPSAccessToken(ctx context.Context, account *Account) (string, error) {
	token, _, err := s.GetAccessToken(ctx, account)
	return token, err
}
func (s *OpenAIGatewayService) excelBPSGloballyEnabled(ctx context.Context) bool {
	if ctx != nil {
		if enabled, ok := ctx.Value(excelBPSRouteContextKey{}).(bool); ok {
			return enabled
		}
	}
	if s == nil || s.settingService == nil || s.settingService.settingRepo == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, gatewayForwardingDBTimeout)
	defer cancel()
	value, err := s.settingService.settingRepo.GetValue(ctx, SettingKeyExcelBPSEnabled)
	return err == nil && value == "true"
}
func excelBPSAccountEligible(a *Account) bool {
	if a == nil {
		return false
	}
	copy := *a
	copy.Extra = map[string]any{"openai_excel_bps": true}
	return copy.IsExcelBPSEnabled()
}

type AccountExcelBPSRepository interface {
	DisableExcelBPSOn403(context.Context, *Account) (bool, error)
}
type AccountExcelBPSGroupRepository interface {
	MoveExcelBPSOn403(context.Context, *Account) (bool, error)
}

// 每轮调度固定一次全局开关，避免逐账号读取设置。
type excelBPSRouteContextKey struct{}

func excelBPSRouteEnabled(ctx context.Context, account *Account, model string) bool {
	enabled, _ := ctx.Value(excelBPSRouteContextKey{}).(bool)
	return enabled && account.IsExcelBPSEnabledForModel(model)
}
func openAIRequestCompactSupportTier(ctx context.Context, account *Account, model string) int {
	if excelBPSRouteEnabled(ctx, account, model) {
		return 2
	}
	return openAICompactSupportTier(account)
}
func (r OpenAIAccountScheduleRequest) compactSupportTier(account *Account) int {
	if r.excelBPSEnabled && account.IsExcelBPSEnabledForModel(r.RequestedModel) {
		return 2
	}
	return openAICompactSupportTier(account)
}
