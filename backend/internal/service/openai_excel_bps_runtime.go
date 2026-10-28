package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/gin-gonic/gin"
)

// 本地容量预算只统计转换中的请求，不预分配内存。
type excelBPSImageAdmission struct {
	mu       sync.Mutex
	bytes    int64
	requests int
}

func (s *OpenAIGatewayService) admitExcelBPSImageRequest(_ *gin.Context, cfg ExcelBPSImageRelaySettings, length int) (func(), error) {
	if int64(length) > int64(cfg.BodyLimitMiB)<<20 {
		return nil, fmt.Errorf("Excel/BPS 请求体超过配置上限")
	}
	weight := int64(length)
	if weight < 1<<20 {
		weight = 1 << 20
	}
	weight *= 8
	b := &s.excelBPSImageAdmission
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.requests >= cfg.MaxRequests || weight+(b.bytes) > int64(cfg.BudgetMiB)<<20 {
		return nil, fmt.Errorf("Excel/BPS 图片处理容量繁忙，请稍后重试")
	}
	b.requests++
	b.bytes += weight
	var once sync.Once
	return func() { once.Do(func() { b.mu.Lock(); defer b.mu.Unlock(); b.requests--; b.bytes -= weight }) }, nil
}
func (s *OpenAIGatewayService) doExcelBPSRequest(ctx context.Context, a *Account, body []byte, token, id string) (*http.Response, error) {
	req, err := newExcelBPSRequest(ctx, body, token, id)
	if err != nil {
		return nil, err
	}
	proxy := ""
	if a.Proxy != nil {
		proxy = a.Proxy.URL()
	}
	resp, err := s.httpUpstream.Do(req, proxy, a.ID, a.Concurrency)
	if err == nil && resp != nil && resp.Body != nil {
		resp.Body = basispoints.WithProgressTimeout(ctx, resp.Body, 2*time.Minute)
	}
	return resp, err
}
func isExcelBPSClientCancellation(c *gin.Context, err error) bool {
	return errors.Is(err, context.Canceled) || (c != nil && c.Request.Context().Err() != nil)
}

func (s *OpenAIGatewayService) disableExcelBPSOn403(ctx context.Context, a *Account) bool {
	if a.Extra["openai_excel_bps_auto_disable_on_403"] != true || s.accountRepo == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	repo, ok := s.accountRepo.(AccountExcelBPSRepository)
	if !ok {
		return false
	}
	snapshot, ok := ctx.Value(excelBPSCredentialSnapshotKey{}).(excelBPSCredentialSnapshot)
	if !ok {
		return false
	}
	changed, err := repo.DisableExcelBPSOn403(ctx, snapshot.account, snapshot.grant)
	return err == nil && changed
}

type excelBPSCredentialSnapshotKey struct{}
type excelBPSCredentialSnapshot struct {
	account *Account
	grant   string
}

// 记录实际使用的凭据版本；本次请求内的刷新不影响关闭，后续重新授权不会被旧 403 关闭。
func (s *OpenAIGatewayService) withExcelBPSCredentialSnapshot(ctx context.Context, a *Account, token string) context.Context {
	if !a.IsExcelBPSAutoDisableOn403Enabled() || s.accountRepo == nil {
		return ctx
	}
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var snapshot excelBPSCredentialSnapshot
	if a.IsExcelOAuth() {
		fresh, err := s.accountRepo.GetByID(readCtx, a.ID)
		if err != nil || fresh == nil || fresh.GetOpenAIAccessToken() != token {
			return ctx
		}
		snapshot.account = fresh
	} else {
		if s.openAITokenProvider == nil || s.openAITokenProvider.openAIOAuthService == nil {
			return ctx
		}
		fresh, grant, info, err := s.openAITokenProvider.openAIOAuthService.readExcelGrant(readCtx, a.ID)
		if err != nil || info == nil || info.AccessToken != token {
			return ctx
		}
		snapshot.account, snapshot.grant = fresh, grant
	}
	return context.WithValue(ctx, excelBPSCredentialSnapshotKey{}, snapshot)
}

func (s *OpenAIGatewayService) coolDownExcelBPS(_ context.Context, a *Account, retryAfter string) {
	duration := time.Minute
	if seconds, err := strconv.Atoi(retryAfter); err == nil && seconds > 0 {
		duration = time.Duration(min(seconds, 3600)) * time.Second
	} else if date, err := http.ParseTime(retryAfter); err == nil && time.Until(date) > 0 {
		duration = min(time.Until(date), time.Hour)
	}
	until := time.Now().Add(duration)
	for {
		previous, loaded := s.excelBPSCooldownUntil.LoadOrStore(a.ID, until)
		if !loaded || !previous.(time.Time).Before(until) {
			return
		}
		if s.excelBPSCooldownUntil.CompareAndSwap(a.ID, previous, until) {
			return
		}
	}
}
func (s *OpenAIGatewayService) isExcelBPSCoolingDown(a *Account) bool {
	until, ok := s.excelBPSCooldownUntil.Load(a.ID)
	if !ok {
		return false
	}
	if time.Now().Before(until.(time.Time)) {
		return true
	}
	s.excelBPSCooldownUntil.Delete(a.ID)
	return false
}

func (s *OpenAIGatewayService) forwardExcelBPSCompat(ctx context.Context, c *gin.Context, a *Account, body []byte, original, upstream, identity string, stream bool, start time.Time, format string) (*OpenAIForwardResult, error) {
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	req["model"] = original
	req["stream"] = stream
	if identity != "" {
		req["prompt_cache_key"] = identity
	}
	delete(req, "previous_response_id")
	rewritten, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	result, err := s.forwardExcelBPS(ctx, c, a, rewritten, start, format, upstream)
	if result != nil {
		result.Model = original
		result.BillingModel = upstream
	}
	return result, err
}
