package service

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

// OpenAIHealthyTurnStateModel 只公开经过上游目录与账号映射核实的测试模型。
type OpenAIHealthyTurnStateModel struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

// FetchOpenAIHealthyTurnStateModels 复用账号模型发现缓存，不补入静态目录或图片模型。
// 上游读取失败时返回明确错误，避免将本地默认模型误认为该账号实际支持的模型。
func (s *AccountTestService) FetchOpenAIHealthyTurnStateModels(ctx context.Context, account *Account) ([]OpenAIHealthyTurnStateModel, error) {
	if account == nil || account.ID <= 0 || !account.IsOpenAI() {
		return nil, infraerrors.BadRequest("INVALID_HEALTHY_TURN_STATE_ACCOUNT", "仅支持 OpenAI 账号")
	}
	if s == nil || s.openaiGatewayService == nil {
		return nil, infraerrors.ServiceUnavailable("HEALTHY_TURN_STATE_MODELS_UNAVAILABLE", "健康状态头模型同步服务暂不可用")
	}
	response, err := s.openaiGatewayService.FetchOpenAIModelsList(ctx, account)
	if err != nil {
		return nil, infraerrors.New(http.StatusBadGateway, "HEALTHY_TURN_STATE_MODELS_SYNC_FAILED", "同步上游支持模型失败，请检查账号认证与代理后重试")
	}
	var payload struct {
		Data []openai.Model `json:"data"`
	}
	if response == nil || json.Unmarshal(response.Body, &payload) != nil {
		return nil, infraerrors.New(http.StatusBadGateway, "HEALTHY_TURN_STATE_MODELS_INVALID", "上游模型目录格式无效，请稍后重试")
	}
	return healthyTurnStateModelsForAccount(account, payload.Data), nil
}

func healthyTurnStateModelsForAccount(account *Account, upstream []openai.Model) []OpenAIHealthyTurnStateModel {
	models := make([]OpenAIHealthyTurnStateModel, 0, len(upstream))
	if account == nil {
		return models
	}
	// 账号别名只有映射后的实际发送模型存在于上游目录时才允许勾选。
	// 使用与采集请求相同的归一化，兼容 gpt-6 等上游别名。
	normalizeTarget := func(model string) string {
		model = strings.TrimSpace(model)
		if account.UsesOpenAICodexProtocol() {
			model = normalizeOpenAIModelForUpstream(account, model)
		}
		return model
	}
	available := make(map[string]bool, len(upstream))
	for _, model := range upstream {
		id := strings.TrimSpace(model.ID)
		if id != "" && !strings.Contains(id, "*") && !isHealthyTurnStateNonTextModel(id) {
			available[normalizeTarget(id)] = true
		}
	}
	seen := make(map[string]bool)
	appendModel := func(id, displayName string) {
		id = strings.TrimSpace(id)
		if id == "" || strings.Contains(id, "*") || seen[id] || isHealthyTurnStateNonTextModel(id) || !account.IsModelSupported(id) {
			return
		}
		mapped := account.GetMappedModel(id)
		if isHealthyTurnStateNonTextModel(mapped) {
			return
		}
		target := normalizeTarget(mapped)
		if !available[target] || isHealthyTurnStateNonTextModel(target) {
			return
		}
		seen[id] = true
		displayName = strings.TrimSpace(displayName)
		if displayName == "" {
			displayName = openaiCodexDisplayName(id)
		}
		models = append(models, OpenAIHealthyTurnStateModel{ID: id, DisplayName: displayName})
	}
	for _, model := range upstream {
		appendModel(model.ID, model.DisplayName)
	}
	// 精确别名不是上游 slug，但可以经账号映射调用已确认支持的实际模型。
	aliases := make([]string, 0, len(account.GetModelMapping()))
	for alias := range account.GetModelMapping() {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	for _, alias := range aliases {
		appendModel(alias, "")
	}
	return models
}

// 健康头通过文本 Responses 采集；排除已知媒体专用模型，同时允许上游新增文本型号。
func isHealthyTurnStateNonTextModel(model string) bool {
	model = strings.ToLower(codexProviderQualifiedModelID(model))
	if isCodexDedicatedMediaModel(model) {
		return true
	}
	for _, prefix := range []string{"sora", "whisper", "tts", "text-embedding"} {
		if strings.HasPrefix(model, prefix) {
			return true
		}
	}
	for _, family := range []string{"audio", "realtime", "transcribe"} {
		if strings.Contains(model, family) {
			return true
		}
	}
	return false
}
