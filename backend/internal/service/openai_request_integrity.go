package service

import (
	"crypto/sha256"
	"encoding/json"
	"log/slog"
	"sync"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/gin-gonic/gin"
)

const (
	openAIRequestIntegrityModeKey     = "request_integrity_mode"
	openAIRequestIntegritySnapshotKey = "openai_request_integrity_snapshot"
	openAIRequestIntegrityReportKey   = "openai_request_integrity_report"
	// 大体积图片和附件请求不做全量解码，跳过原因单独记录，不能冒充检查通过。
	openAIRequestIntegrityMaxBytes = 4 << 20
)

var openAIRequestIntegrityFields = [...]string{
	"model", "input", "instructions", "reasoning", "tools", "tool_choice",
	"parallel_tool_calls", "text", "previous_response_id", "max_output_tokens",
	"max_completion_tokens", "session", "functions", "function_call",
	"include", "context_management", "truncation", "max_tool_calls",
}

// OpenAIRequestIntegrityMode 只观察语义变化，不拦截请求，也不改变指纹和 TLS。
// 本轮仅支持 observe/off；未配置时 OpenAI 默认观察，其他平台关闭。
func (a *Account) OpenAIRequestIntegrityMode() string {
	if a == nil || a.Platform != PlatformOpenAI {
		return "off"
	}
	if mode, ok := a.Extra[openAIRequestIntegrityModeKey].(string); ok && mode == "off" {
		return "off"
	}
	return "observe"
}

// ValidateOpenAIRequestIntegrityExtra 拒绝未实现的拦截模式，避免保存成功但实际只观察。
func ValidateOpenAIRequestIntegrityExtra(extra map[string]any) error {
	value, exists := extra[openAIRequestIntegrityModeKey]
	if !exists {
		return nil
	}
	mode, ok := value.(string)
	if !ok || (mode != "off" && mode != "observe") {
		return infraerrors.BadRequest("INVALID_REQUEST_INTEGRITY_MODE", "请求完整性模式仅支持 observe 或 off")
	}
	return nil
}

type openAIRequestIntegrityReport struct {
	Status string   `json:"status"`
	Fields []string `json:"fields,omitempty"`
	Path   string   `json:"path"`
}

// 快照只保留受检字段的摘要，不在上下文或日志中另存原始对话。
type openAIRequestIntegritySnapshot struct {
	accountID  int64
	fields     map[string][32]byte
	status     string
	mu         sync.Mutex
	lastReport string
}

func newOpenAIRequestIntegritySnapshot(account *Account, body []byte, compact ...bool) *openAIRequestIntegritySnapshot {
	if account.OpenAIRequestIntegrityMode() == "off" {
		return nil
	}
	fields, status := openAIRequestIntegrityDigests(account, body, true, len(compact) > 0 && compact[0])
	return &openAIRequestIntegritySnapshot{accountID: account.ID, fields: fields, status: status}
}

// 原生 WS 透传有独立的模型解析规则；在执行模型替换前记录该路径已解析的目标。
// 只校正原本存在的模型预期，其余字段仍与最早入站快照比较。
func (s *openAIRequestIntegritySnapshot) expectMappedModel(model string) {
	if s == nil || s.status != "ready" || model == "" {
		return
	}
	if _, exists := s.fields["model"]; exists {
		encoded, _ := json.Marshal(model)
		s.fields["model"] = sha256.Sum256(encoded)
	}
}

func stageOpenAIRequestIntegrity(c *gin.Context, account *Account, body []byte) {
	if c != nil {
		// 每次换号都覆盖，关闭观察的账号也必须清掉上一账号的快照。
		c.Set(openAIRequestIntegritySnapshotKey, newOpenAIRequestIntegritySnapshot(account, body, isOpenAIResponsesCompactPath(c)))
		c.Set(openAIRequestIntegrityReportKey, nil)
	}
}

func observeStagedOpenAIRequestIntegrity(c *gin.Context, account *Account, body []byte, path string) {
	if c == nil {
		return
	}
	value, _ := c.Get(openAIRequestIntegritySnapshotKey)
	snapshot, _ := value.(*openAIRequestIntegritySnapshot)
	observeOpenAIRequestIntegrity(c, account, snapshot, body, path)
}

func observeOpenAIRequestIntegrity(c *gin.Context, account *Account, snapshot *openAIRequestIntegritySnapshot, body []byte, path string) {
	if snapshot == nil || account == nil || snapshot.accountID != account.ID || account.OpenAIRequestIntegrityMode() == "off" {
		return
	}
	report := snapshot.compare(account, body, path)
	if c != nil {
		c.Set(openAIRequestIntegrityReportKey, report)
	}
	if report.Status == "unchanged" {
		return
	}
	// 重试相同载荷只记一次；不同载荷仍记录新差异。字段名固定，不输出值、摘要或解析错误正文。
	signature, _ := json.Marshal(report)
	snapshot.mu.Lock()
	duplicate := snapshot.lastReport == string(signature)
	snapshot.lastReport = string(signature)
	snapshot.mu.Unlock()
	if !duplicate {
		slog.Info("openai_request_integrity_observation", "account_id", account.ID,
			"mode", "observe", "path", path, "status", report.Status, "fields", report.Fields)
	}
}

func (s *openAIRequestIntegritySnapshot) compare(account *Account, body []byte, path string) openAIRequestIntegrityReport {
	report := openAIRequestIntegrityReport{Status: s.status, Path: path}
	if s.status != "ready" {
		return report
	}
	fields, status := openAIRequestIntegrityDigests(account, body, false)
	if status != "ready" {
		report.Status = status
		return report
	}
	for _, key := range openAIRequestIntegrityFields {
		if expected, exists := s.fields[key]; exists {
			if actual, present := fields[key]; !present || actual != expected {
				report.Fields = append(report.Fields, key)
			}
		}
	}
	report.Status = "unchanged"
	if len(report.Fields) > 0 {
		// 变化也可能来自管理员策略或兼容降级，因此不命名为“降智”或账号故障。
		report.Status = "changed"
	}
	return report
}

func openAIRequestIntegrityDigests(account *Account, body []byte, inbound bool, compact ...bool) (map[string][32]byte, string) {
	if len(body) > openAIRequestIntegrityMaxBytes {
		return nil, "skipped_size_limit"
	}
	var request map[string]any
	if err := decodeOpenAIJSONUseNumber(body, &request); err != nil || request == nil {
		return nil, "skipped_invalid_json"
	}
	if inbound {
		if model, ok := request["model"].(string); ok {
			_, request["model"] = resolveOpenAIForwardMappedModels(account, model, len(compact) > 0 && compact[0])
		}
	}
	canonicalizeOpenAIRequestIntegrity(request, account)
	fields := make(map[string][32]byte, len(openAIRequestIntegrityFields))
	for _, key := range openAIRequestIntegrityFields {
		if value, exists := request[key]; exists {
			encoded, err := json.Marshal(value)
			if err != nil {
				return nil, "skipped_invalid_json"
			}
			fields[key] = sha256.Sum256(encoded)
		}
	}
	return fields, "ready"
}
