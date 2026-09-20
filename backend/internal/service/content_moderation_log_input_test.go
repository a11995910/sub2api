package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestExtractContentModerationInput_LogInputPreservesSelectedTextAcrossProtocols(t *testing.T) {
	rawText := "第一行\n  缩进正文\n\n最后一行"
	encodedText, err := json.Marshal(rawText)
	require.NoError(t, err)
	cases := []struct {
		name     string
		protocol string
		body     string
	}{
		{"Anthropic", ContentModerationProtocolAnthropicMessages, `{"messages":[{"role":"user","content":"历史输入"},{"role":"assistant","content":"历史回答"},{"role":"user","content":[{"type":"text","text":%s},{"type":"image","source":{"media_type":"image/png","data":"aW1hZ2U="}}]}]}`},
		{"Chat", ContentModerationProtocolOpenAIChat, `{"messages":[{"role":"system","content":"系统说明"},{"role":"user","content":"历史输入"},{"role":"user","content":[{"type":"text","text":%s},{"type":"image_url","image_url":{"url":"https://example.com/private.png"}}]}]}`},
		{"Responses", ContentModerationProtocolOpenAIResponses, `{"instructions":"系统说明","input":[{"role":"user","content":"历史输入"},{"role":"user","content":[{"type":"input_text","text":%s},{"type":"input_image","image_url":"data:image/png;base64,aW1hZ2U="}]}]}`},
		{"Gemini", ContentModerationProtocolGemini, `{"contents":[{"role":"user","parts":[{"text":"历史输入"}]},{"role":"user","parts":[{"text":%s},{"inlineData":{"mimeType":"image/png","data":"aW1hZ2U="}}]}]}`},
		{"Images", ContentModerationProtocolOpenAIImages, `{"prompt":%s,"images":[{"url":"https://example.com/private.png"}]}`},
		{"Video", ContentModerationProtocolOpenAIVideo, `{"prompt":%s,"image_urls":["https://example.com/private.png"]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			content := ExtractContentModerationInput(tc.protocol, []byte(fmt.Sprintf(tc.body, encodedText)))
			require.Equal(t, "第一行 缩进正文 最后一行", content.Text)
			require.Equal(t, &ContentModerationLogInput{
				Text: rawText, TextRunes: utf8.RuneCountInString(rawText), ImageCount: 1,
			}, content.logInput)
			stored, err := json.Marshal(content.logInput)
			require.NoError(t, err)
			require.NotContains(t, string(stored), "aW1hZ2U=")
			require.NotContains(t, string(stored), "private.png")
		})
	}
}

func TestExtractContentModerationInput_LogInputLimitsAndRedactsBeforeTruncation(t *testing.T) {
	for _, size := range []int{maxModerationLogInputRunes, maxModerationLogInputRunes + 1} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			body, err := json.Marshal(map[string]string{"input": strings.Repeat("文", size)})
			require.NoError(t, err)
			content := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)
			require.NotNil(t, content.logInput)
			require.Equal(t, maxModerationInputRunes, utf8.RuneCountInString(content.Text))
			require.Equal(t, size, content.logInput.TextRunes)
			require.Equal(t, size > maxModerationLogInputRunes, content.logInput.TextTruncated)
			require.Equal(t, maxModerationLogInputRunes, utf8.RuneCountInString(content.logInput.Text))
		})
	}

	secret := "sk-proj-example1234567890"
	rawText := strings.Repeat("文", maxModerationLogInputRunes-4) + "\n" + secret + "\n尾部内容"
	body, err := json.Marshal(map[string]string{"input": rawText})
	require.NoError(t, err)
	content := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)
	require.NotContains(t, content.logInput.Text, "sk-")
	require.Contains(t, content.logInput.Text, "[已")
	require.Equal(t, utf8.RuneCountInString(redactContentModerationSecrets(rawText)), content.logInput.TextRunes)
	require.True(t, content.logInput.TextTruncated)
}

func TestExtractContentModerationInput_LogInputRedactionAndNULRemoval(t *testing.T) {
	rawText := "开始\x00\npassword=example123456\nBearer samplecredential123456\nhttps://example.com/private?token=secret\n结束"
	body, err := json.Marshal(map[string]string{"input": rawText})
	require.NoError(t, err)
	content := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)
	require.Equal(t, "开始\npassword=[已脱敏]\nBearer [已脱敏]\n[已脱敏]\n结束", content.logInput.Text)
	require.Equal(t, utf8.RuneCountInString(content.logInput.Text), content.logInput.TextRunes)
	require.False(t, content.logInput.TextTruncated)
	log := (&ContentModerationService{}).buildLog(ContentModerationCheckInput{}, defaultContentModerationConfig(), ContentModerationActionAllow, false, "", 0, nil, content, nil, nil, "")
	require.NotContains(t, log.InputExcerpt, "\x00")
	require.NotContains(t, log.InputExcerpt, "example123456")
}

func TestExtractContentModerationInput_LogInputImagesOnlyAndUncollected(t *testing.T) {
	content := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, []byte(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/one.png"}},{"type":"image_url","image_url":{"url":"https://example.com/two.png"}}]}]}`))
	require.Equal(t, &ContentModerationLogInput{ImageCount: 2}, content.logInput)
	require.Nil(t, ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, []byte(`{"messages":[{"role":"user","content":"历史输入"},{"role":"tool","content":"工具输出"}]}`)).logInput)
	require.Nil(t, ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, []byte(`无效 JSON`)).logInput)
}

func TestContentModerationCheck_CapturesLogInputForEveryRecordedOutcome(t *testing.T) {
	rawText := "ransomware\n" + strings.Repeat("审计正文。", 3000) + "\n原始输入尾部\npassword=example123456"
	wantText := redactContentModerationSecrets(rawText)
	bodyTemplate, err := json.Marshal(map[string]string{"input": rawText})
	require.NoError(t, err)
	cases := []struct {
		name    string
		mode    string
		action  string
		score   float64
		hashHit bool
		apiFail bool
	}{
		{name: "关键词拦截", mode: ContentModerationModePreBlock, action: ContentModerationActionKeywordBlock},
		{name: "哈希拦截", mode: ContentModerationModePreBlock, action: ContentModerationActionHashBlock, hashHit: true},
		{name: "API拦截", mode: ContentModerationModePreBlock, action: ContentModerationActionBlock, score: 1},
		{name: "API放行", mode: ContentModerationModePreBlock, action: ContentModerationActionAllow},
		{name: "API异常", mode: ContentModerationModePreBlock, action: ContentModerationActionError, apiFail: true},
		{name: "观察模式", mode: ContentModerationModeObserve, action: ContentModerationActionAllow, score: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var upstreamCalls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				upstreamCalls.Add(1)
				var request moderationAPIRequest
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if request.Input != trimRunes(normalizeContentModerationText(rawText), maxModerationInputRunes) {
					t.Error("正文采集不应改变提交给审核 API 的文本")
				}
				if tc.apiFail {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				_ = json.NewEncoder(w).Encode(moderationAPIResponse{Results: []moderationAPIResult{{CategoryScores: map[string]float64{"sexual": tc.score}}}})
			}))
			defer server.Close()
			cfg := defaultContentModerationConfig()
			cfg.Enabled, cfg.RecordNonHits = true, true
			cfg.Mode, cfg.BaseURL, cfg.APIKeys = tc.mode, server.URL, []string{"sk-test"}
			cfg.RetryCount = 0
			cfg.PreHashCheckEnabled = tc.hashHit
			if tc.action == ContentModerationActionKeywordBlock {
				cfg.BlockedKeywords = []string{"ransomware"}
			}
			rawCfg, err := json.Marshal(cfg)
			require.NoError(t, err)
			repo := &contentModerationTestRepo{}
			cache := &contentModerationTestHashCache{hashes: make(map[string]struct{})}
			if tc.hashHit {
				content := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, bodyTemplate)
				cache.hashes[content.Hash()] = struct{}{}
			}
			// 手工消费队列，确保原始请求体释放后观察任务仍能记录完整正文。
			svc := NewContentModerationService(nil, nil, cache, nil, nil, nil, nil, nil)
			svc.repo = repo
			svc.settingRepo = &contentModerationTestSettingRepo{values: map[string]string{
				SettingKeyRiskControlEnabled: "true", SettingKeyContentModerationConfig: string(rawCfg),
			}}
			body := append([]byte(nil), bodyTemplate...)
			decision, err := svc.Check(context.Background(), ContentModerationCheckInput{Protocol: ContentModerationProtocolOpenAIResponses, Body: body})
			require.NoError(t, err)
			require.NotNil(t, decision)
			for i := range body {
				body[i] = ' '
			}
			select {
			case task := <-svc.asyncQueue:
				require.Nil(t, task.input.Body)
				if task.log != nil {
					svc.persistContentModerationLog(context.Background(), cfg, task.log, task.inputHash, task.recordHash, task.applySideEffects)
				} else {
					delay := 1
					svc.checkSync(context.Background(), task.input, cfg, task.content, task.inputHash, &delay, false)
				}
			default:
				require.True(t, tc.apiFail, "除同步异常外，记录应通过队列保存")
			}
			logs := repo.snapshotLogs()
			require.Len(t, logs, 1)
			require.Equal(t, tc.action, logs[0].Action)
			require.Equal(t, &ContentModerationLogInput{Text: wantText, TextRunes: utf8.RuneCountInString(wantText)}, logs[0].InputContent)
			require.Contains(t, logs[0].InputContent.Text, "原始输入尾部")
			require.LessOrEqual(t, utf8.RuneCountInString(logs[0].InputExcerpt), maxModerationExcerptRunes)
			require.NotContains(t, logs[0].InputContent.Text, "example123456")
			if tc.hashHit || tc.action == ContentModerationActionKeywordBlock {
				require.Zero(t, upstreamCalls.Load())
			} else {
				require.Equal(t, int64(1), upstreamCalls.Load())
			}
		})
	}
}
