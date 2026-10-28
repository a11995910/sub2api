package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/gin-gonic/gin"
)

const pelicanClaudeMaxTokens = 32000

// 账号测智使用独立默认模型，避免影响普通连通性测试。
const AccountIntelligenceDefaultModel = "gpt-6-astra"

type pelicanTestContextKey struct{}

type pelicanTestOptions struct {
	prompt          string
	reasoningEffort string
}

func withPelicanTestOptions(ctx context.Context, options pelicanTestOptions) context.Context {
	return context.WithValue(ctx, pelicanTestContextKey{}, options)
}

func pelicanTestOptionsFromContext(ctx context.Context) (pelicanTestOptions, bool) {
	options, ok := ctx.Value(pelicanTestContextKey{}).(pelicanTestOptions)
	return options, ok
}

func (s *AccountTestService) TestPelicanAccountConnection(c *gin.Context, accountID int64, modelID, prompt, reasoningEffort string) error {
	options, _ := pelicanTestOptionsFromContext(c.Request.Context())
	options.prompt = strings.TrimSpace(prompt)
	options.reasoningEffort = normalizePelicanReasoningEffort(reasoningEffort)
	ctx := withPelicanTestOptions(c.Request.Context(), options)
	c.Request = c.Request.WithContext(ctx)
	return s.TestAccountConnection(c, accountID, modelID, options.prompt, AccountTestModeDefault)
}

func normalizePelicanReasoningEffort(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func pelicanTestRequested(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	_, ok := pelicanTestOptionsFromContext(c.Request.Context())
	return ok
}

func createPelicanClaudePayload(modelID, prompt, reasoningEffort string) (map[string]any, error) {
	payload, err := createTestPayload(modelID)
	if err != nil {
		return nil, err
	}
	payload["max_tokens"] = pelicanClaudeMaxTokens
	if effort := pelicanClaudeEffort(modelID, reasoningEffort); effort != "" {
		payload["output_config"] = map[string]any{"effort": effort}
		if claude.SupportsAdaptiveThinking(modelID) {
			payload["thinking"] = map[string]any{"type": "adaptive"}
		}
	}
	messages, ok := payload["messages"].([]map[string]any)
	if !ok || len(messages) == 0 {
		return payload, nil
	}
	content, ok := messages[0]["content"].([]map[string]any)
	if ok && len(content) > 0 {
		content[0]["text"] = promptOrDefault(prompt)
	}
	return payload, nil
}

func pelicanClaudeEffort(modelID, reasoningEffort string) string {
	effort := normalizePelicanReasoningEffort(reasoningEffort)
	if effort == "" {
		return ""
	}
	for _, level := range claude.EffortLevelsForModel(modelID) {
		if level == effort {
			return effort
		}
	}
	return ""
}

func pelicanClaudeStopFailure(stopReason, category string) string {
	switch stopReason {
	case "max_tokens":
		return pelicanErrMaxTokens
	case "refusal":
		if category != "" {
			return fmt.Sprintf("%s (category: %s)", pelicanErrRefused, category)
		}
		return pelicanErrRefused
	}
	return ""
}

func createPelicanOpenAIPayload(modelID string, isOAuth bool, prompt, reasoningEffort string) map[string]any {
	payload := createOpenAITestPayload(modelID, isOAuth)
	input, ok := payload["input"].([]map[string]any)
	if ok && len(input) > 0 {
		content, ok := input[0]["content"].([]map[string]any)
		if ok && len(content) > 0 {
			content[0]["text"] = promptOrDefault(prompt)
		}
	}
	if effort := normalizePelicanReasoningEffort(reasoningEffort); effort != "" {
		payload["reasoning"] = map[string]any{"effort": effort}
	}
	return payload
}

func promptOrDefault(prompt string) string {
	if value := strings.TrimSpace(prompt); value != "" {
		return value
	}
	return "hi"
}

// 复用现有 Antigravity 请求包装和协议转换，只替换绘图任务的题目及输出预算。
func (s *AntigravityGatewayService) buildPelicanTestRequest(projectID, model string, options pelicanTestOptions) ([]byte, error) {
	if strings.HasPrefix(model, "gemini-") {
		payload := createGeminiTestPayload(model, options.prompt)
		return s.wrapV1InternalRequest(projectID, model, payload)
	}
	content, err := json.Marshal(options.prompt)
	if err != nil {
		return nil, err
	}
	return antigravity.TransformClaudeToGemini(&antigravity.ClaudeRequest{
		Model: model, Messages: []antigravity.ClaudeMessage{{Role: "user", Content: content}}, MaxTokens: pelicanClaudeMaxTokens,
	}, projectID, model)
}
