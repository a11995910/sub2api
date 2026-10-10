package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

const (
	openAICodexTicketExtraKeyPrefix  = "codex_turn_ticket:"
	openAICodexAstraMinVersion       = "0.153.4"
	openAICodexTicketStatePrefix     = "gAAAAA"
	openAICodexTicketDefaultModel    = "gpt-6-astra"
	openAICodexTicketDefaultSolModel = "gpt-5.6-sol"
)

// ErrOpenAICodexTicketUnavailable 表示该号该模型没有可用的 292/332 门票，
// 且 fail_closed 禁止裸打业务请求。
var ErrOpenAICodexTicketUnavailable = errors.New("codex turn-state ticket unavailable")

type openAICodexTicket struct {
	AccountID     int64     `json:"account_id"`
	Model         string    `json:"model"`
	State         string    `json:"state"`
	Length        int       `json:"length"`
	CapturedAt    time.Time `json:"captured_at"`
	ExpiresAt     time.Time `json:"expires_at"`
	Attempts      int       `json:"attempts"`
	ProxyURL      string    `json:"proxy_url"`
	Invalidated   bool      `json:"invalidated,omitempty"`
	InvalidReason string    `json:"invalid_reason,omitempty"`
}

func openAICodexTicketKey(accountID int64, model string) string {
	return fmt.Sprintf("%d\x00%s", accountID, strings.TrimSpace(model))
}

func openAICodexTicketExtraKey(model string) string {
	return openAICodexTicketExtraKeyPrefix + strings.TrimSpace(model)
}

func normalizeOpenAICodexTicketModel(model string) string {
	return strings.TrimSpace(model)
}

func extractOpenAICodexTicketModel(body []byte) string {
	return normalizeOpenAICodexTicketModel(gjson.GetBytes(body, "model").String())
}

func (s *OpenAIGatewayService) openAICodexTicketConfig() config.OpenAICodexTicketConfig {
	cfg := config.OpenAICodexTicketConfig{}
	if s != nil && s.cfg != nil {
		cfg = s.cfg.Gateway.OpenAICodexTicket
	}
	if cfg.TargetLength <= 0 {
		cfg.TargetLength = 292
	}
	if cfg.TTLSeconds <= 0 {
		cfg.TTLSeconds = 3600
	}
	if cfg.RefreshBeforeSeconds <= 0 {
		cfg.RefreshBeforeSeconds = 600
	}
	if cfg.HarvestProbeIntervalSeconds <= 0 {
		cfg.HarvestProbeIntervalSeconds = 6
	}
	if cfg.HarvestAttemptTimeoutSeconds <= 0 {
		cfg.HarvestAttemptTimeoutSeconds = 25
	}
	if cfg.HarvestProxyConcurrency <= 0 {
		cfg.HarvestProxyConcurrency = 3
	}
	if cfg.HarvestProxyConcurrency > 8 {
		cfg.HarvestProxyConcurrency = 8
	}
	if cfg.HarvestGlobalConcurrency <= 0 {
		cfg.HarvestGlobalConcurrency = 8
	}
	if cfg.HarvestGlobalConcurrency > 32 {
		cfg.HarvestGlobalConcurrency = 32
	}
	if len(cfg.Models) == 0 {
		cfg.Models = []string{openAICodexTicketDefaultModel, openAICodexTicketDefaultSolModel}
	}
	return cfg
}

func (s *OpenAIGatewayService) openAICodexTicketGatedModel(model string) bool {
	model = normalizeOpenAICodexTicketModel(model)
	if model == "" || !s.openAICodexTicketEnabled() {
		return false
	}
	for _, item := range s.openAICodexTicketConfig().Models {
		if normalizeOpenAICodexTicketModel(item) == model {
			return true
		}
	}
	return false
}

// OpenAICodexTicketStatus 是给管理端看的门票摘要，不含 state blob。
type OpenAICodexTicketStatus struct {
	Model                 string     `json:"model"`
	Length                int        `json:"length,omitempty"`
	InvalidReason         string     `json:"invalid_reason,omitempty"`
	Ready                 bool       `json:"ready"`
	RemainingSeconds      int64      `json:"remaining_seconds"`
	Blocked               bool       `json:"blocked"`
	ExpiresAt             *time.Time `json:"expires_at,omitempty"`
	HarvestStatus         string     `json:"harvest_status,omitempty"`
	LastAttemptAt         *time.Time `json:"last_attempt_at,omitempty"`
	LastResult            string     `json:"last_result,omitempty"`
	LastHTTPStatus        int        `json:"last_http_status,omitempty"`
	NextAttemptAt         *time.Time `json:"next_attempt_at,omitempty"`
	RefreshDueAt          *time.Time `json:"refresh_due_at,omitempty"`
	RetryIntervalSeconds  int        `json:"retry_interval_seconds,omitempty"`
	AttemptTimeoutSeconds int        `json:"attempt_timeout_seconds,omitempty"`
	AttemptIndex          int        `json:"attempt_index,omitempty"`
	AttemptTotal          int        `json:"attempt_total,omitempty"`
	PauseReason           string     `json:"pause_reason,omitempty"`
}

func OpenAICodexTicketStatuses(account *Account, cfg config.OpenAICodexTicketConfig, now time.Time) []OpenAICodexTicketStatus {
	if !cfg.Enabled || !isOpenAICodexTicketAccount(account) {
		return nil
	}
	models, targetLen := cfg.Models, cfg.TargetLength
	if len(models) == 0 {
		models = []string{openAICodexTicketDefaultModel, openAICodexTicketDefaultSolModel}
	}
	if targetLen <= 0 {
		targetLen = 292
	}
	out := make([]OpenAICodexTicketStatus, 0, len(models))
	for _, model := range models {
		model = normalizeOpenAICodexTicketModel(model)
		if model == "" {
			continue
		}
		status := OpenAICodexTicketStatus{Model: model}
		ticket := parseOpenAICodexTicketFromAny(0, model, nil)
		if account != nil && account.Extra != nil {
			ticket = parseOpenAICodexTicketFromAny(account.ID, model, account.Extra[openAICodexTicketExtraKey(model)])
		}
		if ticket != nil && ticket.Invalidated {
			status.InvalidReason = ticket.InvalidReason
		}
		if ticket.valid(now, targetLen) {
			status.Ready = true
			status.Length = ticket.Length
			remaining := int64(ticket.ExpiresAt.Sub(now) / time.Second)
			if remaining < 0 {
				remaining = 0
			}
			status.RemainingSeconds = remaining
			exp := ticket.ExpiresAt
			status.ExpiresAt = &exp
		}
		status.Blocked = openAICodexTicketFailClosed(account, cfg.FailClosed) && !status.Ready
		out = append(out, status)
	}
	return out
}

func (s *OpenAIGatewayService) openAICodexTicketEnabled() bool {
	return s.openAICodexTicketEnabledContext(context.Background())
}

func (s *OpenAIGatewayService) openAICodexTicketEnabledContext(ctx context.Context) bool {
	if s == nil {
		return false
	}
	fallback := s.cfg != nil && s.cfg.Gateway.OpenAICodexTicket.Enabled
	if s.settingService != nil {
		return s.settingService.GetOpenAICodexTicketEnabled(ctx, fallback)
	}
	return fallback
}

func (s *OpenAIGatewayService) openAICodexTicketHarvestProxyURL() string {
	return s.openAICodexTicketHarvestProxyURLContext(context.Background())
}

func (s *OpenAIGatewayService) openAICodexTicketHarvestProxyURLContext(ctx context.Context) string {
	if s.settingService != nil {
		if proxy := s.settingService.GetOpenAICodexTicketHarvestProxyURL(ctx); proxy != "" {
			return proxy
		}
	}
	return strings.TrimSpace(s.openAICodexTicketConfig().HarvestProxyURL)
}

// 标准门票兼容 292 和 332 字符；显式配置其他长度时仍按配置精确校验。
// 采集、调度、状态展示和请求绑定必须使用同一套格式规则。
func validOpenAICodexTicketState(state string, targetLen int) bool {
	if targetLen <= 0 {
		targetLen = 292
	}
	lengthOK := len(state) == targetLen
	if targetLen == 292 || targetLen == 332 {
		lengthOK = len(state) == 292 || len(state) == 332
	}
	return lengthOK && strings.HasPrefix(state, openAICodexTicketStatePrefix)
}

func (t *openAICodexTicket) valid(now time.Time, targetLen int) bool {
	if t == nil || t.Invalidated || t.ProxyURL == "" || ValidateOpenAICodexTicketHarvestProxyURL(t.ProxyURL) != nil {
		return false
	}
	state := strings.TrimSpace(t.State)
	if t.Length != len(state) || !validOpenAICodexTicketState(state, targetLen) {
		return false
	}
	if t.ExpiresAt.IsZero() || !now.Before(t.ExpiresAt) {
		return false
	}
	return true
}

func (s *OpenAIGatewayService) lookupOpenAICodexTicket(account *Account, model string) *openAICodexTicket {
	if s == nil || account == nil || account.ID <= 0 {
		return nil
	}
	model = normalizeOpenAICodexTicketModel(model)
	if model == "" {
		return nil
	}
	mu := &s.openaiCodexTicketLocks[uint64(account.ID)%64]
	mu.Lock()
	defer mu.Unlock()
	return s.lookupOpenAICodexTicketLocked(account, model)
}

// 失效记录也参与版本比较，避免调度缓存中的旧账号快照把已淘汰的票重新启用。
func (s *OpenAIGatewayService) lookupOpenAICodexTicketLocked(account *Account, model string) *openAICodexTicket {
	key := openAICodexTicketKey(account.ID, model)
	var mem *openAICodexTicket
	if raw, ok := s.openaiCodexTickets.Load(key); ok {
		mem, _ = raw.(*openAICodexTicket)
	}
	extra := parseOpenAICodexTicketFromAny(account.ID, model, account.Extra[openAICodexTicketExtraKey(model)])
	if extra != nil && (mem == nil || extra.CapturedAt.After(mem.CapturedAt)) {
		s.openaiCodexTickets.Store(key, extra)
		return extra
	}
	return mem
}

func parseOpenAICodexTicketFromAny(accountID int64, model string, raw any) *openAICodexTicket {
	if raw == nil {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var ticket openAICodexTicket
	if err := json.Unmarshal(b, &ticket); err != nil {
		return nil
	}
	ticket.AccountID = accountID
	if strings.TrimSpace(model) != "" {
		ticket.Model = model
	}
	ticket.State = strings.TrimSpace(ticket.State)
	if ticket.Length == 0 {
		ticket.Length = len(ticket.State)
	}
	if ticket.State == "" {
		return nil
	}
	return &ticket
}

func (s *OpenAIGatewayService) storeOpenAICodexTicket(ctx context.Context, account *Account, ticket *openAICodexTicket) {
	if s == nil || account == nil || ticket == nil || account.ID <= 0 {
		return
	}
	mu := &s.openaiCodexTicketLocks[uint64(account.ID)%64]
	mu.Lock()
	defer mu.Unlock()
	copyTicket := *ticket
	ticket = &copyTicket
	model := normalizeOpenAICodexTicketModel(ticket.Model)
	ticket.Model = model
	ticket.AccountID = account.ID
	s.openaiCodexTickets.Store(openAICodexTicketKey(account.ID, model), ticket)
	if s.accountRepo == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.accountRepo.UpdateExtra(ctx, account.ID, map[string]any{
		openAICodexTicketExtraKey(model): ticket,
	}); err != nil {
		logger.L().Warn("openai_codex_ticket persist failed",
			zap.Int64("account_id", account.ID),
			zap.String("model", model),
			zap.String("reason", "persist_failed"),
		)
	}
}

// applyOpenAICodexTicket 在出站请求上覆盖 x-codex-turn-state。
// 请求路径只注入已捕获的有效门票，不现场打票；无票则返回
// ErrOpenAICodexTicketUnavailable。打票由后台 harvester 完成。
func (s *OpenAIGatewayService) applyOpenAICodexTicket(ctx context.Context, account *Account, model string, h http.Header) error {
	_, err := s.bindOpenAICodexTicket(ctx, account, model, h)
	return err
}

// 一次读取同时决定门票与出口，不能分别查询后拼接不同版本。
func (s *OpenAIGatewayService) bindOpenAICodexTicket(ctx context.Context, account *Account, model string, h http.Header) (*openAICodexTicket, error) {
	if IsOpenAITurnStateProbe(ctx) || s == nil || h == nil || !isOpenAICodexTicketAccount(account) || !s.openAICodexTicketEnabledContext(ctx) {
		return nil, nil
	}
	model = normalizeOpenAICodexTicketModel(model)
	if model == "" || !s.openAICodexTicketGatedModel(model) {
		return nil, nil
	}
	cfg := s.openAICodexTicketConfig()
	ticket := s.lookupOpenAICodexTicket(account, model)
	if ticket.valid(time.Now(), cfg.TargetLength) {
		h.Set(openAICodexTurnStateHeader, ticket.State)
		return ticket, nil
	}
	s.requestOpenAICodexTicketHarvest(account, model)
	if !openAICodexTicketFailClosed(account, cfg.FailClosed) {
		return nil, nil
	}
	return nil, wrapOpenAITurnStateUnavailable(ErrOpenAICodexTicketUnavailable)
}

// openAICodexTicketOutboundModel 预测本请求真正出站的模型名，也就是
// applyOpenAICodexTicket 注入时读到的 body.model。
//
// 调度门控与注入必须按同一个模型名判定门票。普通请求下二者同源：Forward 的
// upstreamModel 与本函数都走 resolveOpenAIAccountUpstreamModelForRequest，且
// Forward 会把 body.model 改写成该值后才注入。但 /responses/compact 例外——
// Forward 会把出站模型进一步改写为 compact 映射或 gateway.openai_compact_model
// （默认非空），此时若门控仍按客户端原始模型判定，就会把「实际出站是非门控
// 模型、根本不需要票」的 compact 请求整片误拦成不可调度。
func (s *OpenAIGatewayService) openAICodexTicketOutboundModel(account *Account, requestedModel string, requireCompact bool) string {
	model := strings.TrimSpace(requestedModel)
	if account == nil || model == "" {
		return model
	}
	if !account.IsOpenAI() {
		return canonicalOpenAIAccountSchedulingModel(account, model)
	}
	_, upstreamModel := resolveOpenAIForwardMappedModels(account, model, requireCompact)
	if requireCompact {
		// 与 Forward 同序：compact 兜底模型优先于普通/compact 映射结果。
		if compactModel := strings.TrimSpace(s.resolveOpenAICompactFallbackModel(account, model)); compactModel != "" {
			upstreamModel = compactModel
		}
	}
	if upstreamModel = strings.TrimSpace(upstreamModel); upstreamModel != "" {
		return upstreamModel
	}
	return model
}

// outboundModel 必须是真正会发给上游的模型名（openAICodexTicketOutboundModel），
// 不是客户端原始模型：注入侧读的是出站 body.model，两侧口径必须一致。
func (s *OpenAIGatewayService) openAICodexTicketBlocksAccount(account *Account, outboundModel string) bool {
	if s == nil || !isOpenAICodexTicketAccount(account) || !s.openAICodexTicketEnabled() {
		return false
	}
	cfg := s.openAICodexTicketConfig()
	if !openAICodexTicketFailClosed(account, cfg.FailClosed) {
		return false
	}
	model := normalizeOpenAICodexTicketModel(outboundModel)
	if !s.openAICodexTicketGatedModel(model) {
		return false
	}
	ticket := s.lookupOpenAICodexTicket(account, model)
	missing := !ticket.valid(time.Now(), cfg.TargetLength)
	if missing {
		s.requestOpenAICodexTicketHarvest(account, model)
	}
	return missing
}

func (s *OpenAIGatewayService) fireOpenAICodexTicketProbe(ctx context.Context, account *Account, token, model, proxyURL string, attemptTimeout time.Duration) (state string, status int, err error) {
	attemptCtx, cancel := context.WithTimeout(ctx, attemptTimeout)
	defer cancel()

	body := []byte(`{"model":` + jsonString(model) + `,"store":false,"stream":true,"instructions":"Reply with exactly: pong","input":[{"role":"user","content":[{"type":"input_text","text":"ping"}]}]}`)
	req, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, chatgptCodexURL, bytes.NewReader(body))
	if err != nil {
		return "", 0, err
	}
	req = req.WithContext(WithHTTPUpstreamProfile(req.Context(), HTTPUpstreamProfileOpenAIHarvest))
	req.Close = true
	req.Host = "chatgpt.com"
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("OpenAI-Beta", "responses=experimental")
	req.Header.Set("session_id", uuid.NewString())
	if err := resolveAndSetOpenAIChatGPTAccountHeaders(attemptCtx, s.accountRepo, req.Header, account); err != nil {
		return "", 0, err
	}
	applyOpenAICodexTicketHarvestIdentity(req.Header, model)

	// 合成探测始终使用独立的不复用连接传输，避免业务插件和构造期插件初始化干扰。
	resp, err := s.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		return "", 0, err
	}
	if resp == nil {
		return "", 0, errors.New("nil upstream response")
	}
	// 先取消读取再关闭正文，成功终态之后不等待连接结束。
	defer func() {
		cancel()
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
	}()
	state = extractOpenAICodexTurnState(resp.Header)
	if resp.StatusCode == http.StatusOK && validOpenAICodexTicketState(state, s.openAICodexTicketConfig().TargetLength) {
		if err := validateCodexTicketProbeResponseWithPolicy(resp.Body, model, s.openAICodexTicketPolicy(attemptCtx).ModelMismatchInvalidation); err != nil {
			return "", resp.StatusCode, err
		}
	}
	return state, resp.StatusCode, nil
}

func jsonString(v string) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `""`
	}
	return string(b)
}

func applyOpenAICodexTicketHarvestIdentity(h http.Header, model string) {
	ensureCodexIdentityHeaders(h)
	enforceCodexIdentityHeaders(h)
	version := strings.TrimSpace(h.Get("version"))
	if needsOpenAICodexAstraVersion(model) && (version == "" || CompareVersions(version, openAICodexAstraMinVersion) < 0) {
		h.Set("version", openAICodexAstraMinVersion)
		h.Set("user-agent", buildCodexCLIUserAgent(openAICodexAstraMinVersion))
		h.Set("originator", openai.CodexDefaultOriginator)
	}
}

func needsOpenAICodexAstraVersion(model string) bool {
	m := strings.ToLower(normalizeOpenAICodexTicketModel(model))
	return strings.Contains(m, "gpt-6") || strings.Contains(m, "astra")
}

func (s *OpenAIGatewayService) StartOpenAICodexTicketHarvester() {
	if s == nil {
		return
	}
	s.openaiCodexTicketLifecycleMu.Lock()
	defer s.openaiCodexTicketLifecycleMu.Unlock()
	if s.openaiCodexTicketStopped || s.openaiCodexTicketDone != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.openaiCodexTicketContext = ctx
	s.openaiCodexTicketJobs = make(map[string]*openAICodexTicketJob)
	s.openaiCodexTicketCancel = cancel
	s.openaiCodexTicketDone = done
	go func() {
		defer close(done)
		s.openAICodexTicketHarvestLoop(ctx)
		s.openaiCodexTicketWorkers.Wait()
	}()
	logger.L().Info("openai_codex_ticket harvester started",
		zap.Int("ttl_seconds", s.openAICodexTicketConfig().TTLSeconds),
		zap.Int("target_length", s.openAICodexTicketConfig().TargetLength),
		zap.Strings("models", s.openAICodexTicketConfig().Models),
	)
}

func (s *OpenAIGatewayService) StopOpenAICodexTicketHarvester() {
	if s == nil {
		return
	}
	s.openaiCodexTicketLifecycleMu.Lock()
	s.openaiCodexTicketStopped = true
	cancel, done := s.openaiCodexTicketCancel, s.openaiCodexTicketDone
	s.openaiCodexTicketLifecycleMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (s *OpenAIGatewayService) openAICodexTicketHarvestLoop(ctx context.Context) {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			s.refreshOpenAICodexTickets(ctx)
			interval := time.Duration(s.openAICodexTicketConfig().HarvestProbeIntervalSeconds) * time.Second
			timer.Reset(interval)
		}
	}
}

type openAICodexTicketHarvestTarget struct {
	account Account
	model   string
}

// refreshOpenAICodexTickets 只发现目标，不等待任何目标的提取或采票结束。
func (s *OpenAIGatewayService) refreshOpenAICodexTickets(ctx context.Context) {
	if s == nil || s.accountRepo == nil || ctx.Err() != nil {
		return
	}
	if !s.openAICodexTicketEnabledContext(ctx) {
		s.reconcileOpenAICodexTicketJobs(nil)
		return
	}
	accounts, err := s.accountRepo.ListByPlatform(ctx, PlatformOpenAI)
	if err != nil {
		logger.L().Warn("门票采集目标读取失败")
		return
	}
	var targets []openAICodexTicketHarvestTarget
	for _, account := range accounts {
		if account.Status != StatusActive || !isOpenAICodexTicketAccount(&account) {
			continue
		}
		for _, model := range s.openAICodexTicketConfig().Models {
			model = normalizeOpenAICodexTicketModel(model)
			if model == "" {
				continue
			}
			targets = append(targets, openAICodexTicketHarvestTarget{account: account, model: model})
		}
	}
	s.reconcileOpenAICodexTicketJobs(targets)
}

// probeOnceOpenAICodexTicket 的提取和探测共同去重，同一账号模型仅有一组任务。
func (s *OpenAIGatewayService) probeOnceOpenAICodexTicket(ctx context.Context, account *Account, model string) bool {
	return s.collectOpenAICodexTicket(ctx, account, model, func(ctx context.Context) ([]string, OpenAICodexTicketHarvestSource, error) {
		source := s.openAICodexTicketHarvestSource(ctx)
		proxies, err := resolveOpenAICodexTicketHarvestProxiesFromSource(ctx, source)
		return proxies, source, err
	})
}

func (s *OpenAIGatewayService) probeOpenAICodexTicketWithProxies(ctx context.Context, account *Account, model string, proxies []string, source OpenAICodexTicketHarvestSource) {
	s.collectOpenAICodexTicket(ctx, account, model, func(context.Context) ([]string, OpenAICodexTicketHarvestSource, error) { return proxies, source, nil })
}

// IsOpenAICodexTicketExtraKey 识别服务端管理的门票字段。
func IsOpenAICodexTicketExtraKey(key string) bool {
	return strings.HasPrefix(key, openAICodexTicketExtraKeyPrefix)
}

// MergeOpenAICodexTicketExtra 只保留已持久化门票，拒绝账号编辑传入的门票。
// 仓储层持有行锁时再次合并，避免旧管理快照覆盖并发采集结果。
func MergeOpenAICodexTicketExtra(extra, current map[string]any) map[string]any {
	result := maps.Clone(extra)
	for key := range result {
		if IsOpenAICodexTicketExtraKey(key) {
			delete(result, key)
		}
	}
	for key, value := range current {
		if IsOpenAICodexTicketExtraKey(key) {
			if result == nil {
				result = make(map[string]any)
			}
			result[key] = value
		}
	}
	return result
}

// ValidateOpenAICodexTicketHarvestProxyURL 仅校验语法，不发送请求，错误不包含凭据。
func ValidateOpenAICodexTicketHarvestProxyURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" || parsed.Opaque != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return infraerrors.BadRequest("INVALID_CODEX_TICKET_PROXY", "采集代理必须为带主机的 HTTP(S) 或 SOCKS5(h) 地址，不能包含路径、查询参数或片段")
	}
	switch parsed.Scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return infraerrors.BadRequest("INVALID_CODEX_TICKET_PROXY", "采集代理协议必须为 http、https、socks5 或 socks5h")
	}
	if port := parsed.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return infraerrors.BadRequest("INVALID_CODEX_TICKET_PROXY", "采集代理端口必须在 1 至 65535 之间")
		}
	}
	return nil
}

// MaskProxyURL 不返回已保存的代理密码，旧数据不合法时返回空值。
func MaskProxyURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || ValidateOpenAICodexTicketHarvestProxyURL(raw) != nil {
		return ""
	}
	parsed, _ := url.Parse(raw)
	if parsed.User != nil {
		if _, ok := parsed.User.Password(); ok {
			parsed.User = url.UserPassword(parsed.User.Username(), "***")
		}
	}
	return parsed.String()
}

// IsMaskedProxyURL 识别接口返回的密码占位符。
func IsMaskedProxyURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return true
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User == nil {
		return false
	}
	password, ok := parsed.User.Password()
	return ok && password == "***"
}

// 凭据影子账号不拥有门票；仅显式选择 292/332 门票模式的独立账号参与打票和门控。
func isOpenAICodexTicketAccount(account *Account) bool {
	return account != nil && account.IsOpenAIOAuthLike() && !account.IsShadow() && account.OpenAICodexTicketEnabled()
}

// IsOpenAICodexTicketPrivateExtraKey 也覆盖旧账号级代理字段，防止历史凭据泄漏。
func IsOpenAICodexTicketPrivateExtraKey(key string) bool {
	return IsOpenAICodexTicketExtraKey(key) || key == "codex_harvest_proxy_url"
}

// RedactOpenAICodexTicketExtra 从导出中移除门票，不修改原账号和其他字段。
func RedactOpenAICodexTicketExtra(extra map[string]any) map[string]any {
	redacted := maps.Clone(extra)
	for key := range redacted {
		if IsOpenAICodexTicketPrivateExtraKey(key) {
			delete(redacted, key)
		}
	}
	return redacted
}

// openAICodexTicketFailClosed 允许账号覆盖全局缺票处理策略。
func openAICodexTicketFailClosed(account *Account, fallback bool) bool {
	if account != nil && account.Extra != nil {
		if value, ok := account.Extra["openai_codex_ticket_fail_closed"].(bool); ok {
			return value
		}
	}
	return fallback
}

// ValidateOpenAICodexTicketExtra 校验账号缺票覆盖配置。
func ValidateOpenAICodexTicketExtra(extra map[string]any) error {
	if raw, exists := extra["openai_codex_ticket_fail_closed"]; exists {
		if _, ok := raw.(bool); !ok {
			return infraerrors.BadRequest("INVALID_CODEX_TICKET_SETTING", "292/332 门票缺票策略必须为布尔值")
		}
	}
	return nil
}

// openAICodexTicketProbeStillEligible 仅在后台采集边界读取当前账号，不增加业务请求读库。
func (s *OpenAIGatewayService) openAICodexTicketProbeStillEligible(ctx context.Context, account *Account) bool {
	if ctx.Err() != nil || !s.openAICodexTicketEnabledContext(ctx) {
		return false
	}
	if s.accountRepo == nil {
		return isOpenAICodexTicketAccount(account)
	}
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	latest, err := s.accountRepo.GetByID(checkCtx, account.ID)
	return err == nil && latest != nil && latest.Status == StatusActive && isOpenAICodexTicketAccount(latest)
}
