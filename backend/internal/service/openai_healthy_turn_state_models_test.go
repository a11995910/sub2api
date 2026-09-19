package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

func TestFetchOpenAIHealthyTurnStateModelsUsesUpstreamCatalog(t *testing.T) {
	_, calls := newCodexModelsOAuthCacheServer(t, `{"models":[{"slug":"future-text-model","display_name":"上游新模型"},{"slug":"gpt-6-astra"},{"slug":"gpt-image-2.5-flare"}]}`)
	gateway := &OpenAIGatewayService{}
	svc := &AccountTestService{openaiGatewayService: gateway}
	account := newCodexModelsTestAccount()
	models, err := svc.FetchOpenAIHealthyTurnStateModels(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, []OpenAIHealthyTurnStateModel{{ID: "future-text-model", DisplayName: "上游新模型"}, {ID: "gpt-6-astra", DisplayName: "GPT-6 Astra"}}, models)
	_, err = svc.FetchOpenAIHealthyTurnStateModels(context.Background(), account)
	require.NoError(t, err)
	require.EqualValues(t, 1, calls.Load(), "打开模型选项复用账号模型缓存")
	response, err := gateway.FetchOpenAIModelsList(context.Background(), account)
	require.NoError(t, err)
	require.Contains(t, string(response.Body), "gpt-image-2.5-flare", "健康头筛选不修改共享目录")
}

func TestHealthyTurnStateModelsRequireSupportedMappedTarget(t *testing.T) {
	account := newCodexModelsTestAccount()
	account.Credentials["model_mapping"] = map[string]any{
		"编码别名":                "gpt-6-astra",
		"失效别名":                "missing-model",
		"视频别名":                "sora-2",
		"语音别名":                "gpt-audio",
		"图片别名":                "gpt-image-2.5-flare",
		"gpt-5.6-*":           "gpt-5.6-sol",
		"gpt-6":               "gpt-6",
		"gpt-5.4":             "missing-model",
		"gpt-6-astra":         "gpt-6-astra",
		"gpt-image-2.5-flare": "gpt-image-2.5-flare",
	}
	models := healthyTurnStateModelsForAccount(account, []openai.Model{
		{ID: "gpt-6-astra"}, {ID: "gpt-5.6-sol"}, {ID: "gpt-5.4"}, {ID: "gpt-5.5"},
		{ID: "gpt-image-2.5-flare"}, {ID: "sora-2"}, {ID: "gpt-audio"}, {ID: "unlisted-*"},
	})
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	require.ElementsMatch(t, []string{"gpt-6-astra", "gpt-5.6-sol", "gpt-6", "编码别名"}, ids,
		"映射允许的上游模型与有效精确别名可选；通配符、媒体、未列出目标不可选")
}

func TestFetchOpenAIHealthyTurnStateModelsDoesNotFallback(t *testing.T) {
	for _, body := range []string{`{"models":[]}`, `{"error":"敏感上游正文"}`} {
		t.Run(body, func(t *testing.T) {
			newCodexModelsOAuthCacheServer(t, body)
			svc := &AccountTestService{openaiGatewayService: &OpenAIGatewayService{}}
			models, err := svc.FetchOpenAIHealthyTurnStateModels(context.Background(), newCodexModelsTestAccount())
			if body == `{"models":[]}` {
				require.NoError(t, err)
				require.NotNil(t, models, "空目录返回数组而非 null")
				require.Empty(t, models)
			} else {
				require.Error(t, err)
				require.NotContains(t, err.Error(), "敏感上游正文")
				require.Nil(t, models)
			}
		})
	}
}
