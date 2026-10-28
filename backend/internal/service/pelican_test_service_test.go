package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestPelicanPayloadsDoNotChangeDefaultAccountTestPayloads(t *testing.T) {
	defaultClaude, err := createTestPayload("claude-sonnet-4-6")
	require.NoError(t, err)
	defaultMessages, ok := defaultClaude["messages"].([]map[string]any)
	require.True(t, ok)
	defaultContent, ok := defaultMessages[0]["content"].([]map[string]any)
	require.True(t, ok)
	require.Equal(t, "hi", defaultContent[0]["text"])
	require.Equal(t, 1024, defaultClaude["max_tokens"])
	require.NotContains(t, defaultClaude, "output_config")
	require.NotContains(t, defaultClaude, "thinking")

	defaultOpenAI := createOpenAITestPayload("gpt-6-astra", true)
	defaultInput, ok := defaultOpenAI["input"].([]map[string]any)
	require.True(t, ok)
	defaultOpenAIContent, ok := defaultInput[0]["content"].([]map[string]any)
	require.True(t, ok)
	require.Equal(t, "hi", defaultOpenAIContent[0]["text"])
	require.NotContains(t, defaultOpenAI, "reasoning")

	pelicanClaude, err := createPelicanClaudePayload("claude-sonnet-4-6", "draw the pelican animation", "")
	require.NoError(t, err)
	pelicanMessages, ok := pelicanClaude["messages"].([]map[string]any)
	require.True(t, ok)
	pelicanContent, ok := pelicanMessages[0]["content"].([]map[string]any)
	require.True(t, ok)
	require.Equal(t, "draw the pelican animation", pelicanContent[0]["text"])

	pelicanOpenAI := createPelicanOpenAIPayload("gpt-6-astra", true, "draw the pelican animation", "medium")
	pelicanInput, ok := pelicanOpenAI["input"].([]map[string]any)
	require.True(t, ok)
	pelicanOpenAIContent, ok := pelicanInput[0]["content"].([]map[string]any)
	require.True(t, ok)
	require.Equal(t, "draw the pelican animation", pelicanOpenAIContent[0]["text"])
	require.Equal(t, map[string]any{"effort": "medium"}, pelicanOpenAI["reasoning"])
}

func TestPelicanAntigravityPayloadKeepsPromptAndDrawingBudget(t *testing.T) {
	svc := &AntigravityGatewayService{}
	for _, model := range []string{"claude-sonnet-4-6", "gemini-2.5-pro"} {
		t.Run(model, func(t *testing.T) {
			body, err := svc.buildPelicanTestRequest("测试项目", model, pelicanTestOptions{prompt: "画出鹈鹕骑自行车"})
			require.NoError(t, err)
			require.Contains(t, string(body), "画出鹈鹕骑自行车")
			if model == "claude-sonnet-4-6" {
				require.Greater(t, gjson.GetBytes(body, "request.generationConfig.maxOutputTokens").Int(), int64(1024))
			}
		})
	}
}
