//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestZYCARegisteredAliasesForwardMappedModels(t *testing.T) {
	for _, model := range []string{"wan-3.0", "kling-4.0", "seedance-2.5", "seedance-2.0", "seedance-2.0-mini", "minimax-h3-903"} {
		t.Run(model, func(t *testing.T) {
			alias := "zyca-" + model
			account := zycaTestAccount()
			account.Credentials["model_mapping"] = map[string]any{alias: model}
			body := []byte(fmt.Sprintf(`{"model":%q,"prompt":"测试","duration":5,"resolution":"720p","aspect_ratio":"16:9"}`, alias))
			require.NoError(t, ValidateOpenAIVideoCreateBodyForAccount(account, body))
			ctx, _ := openAIVideoForwardTestContext(body)
			upstream := &httpUpstreamRecorder{resp: zycaTestResponse(200, `{"success":true,"data":{"id":"task-1","status":"queued"}}`)}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			result, err := svc.ForwardOpenAIVideoCreate(context.Background(), ctx, account, body, "")
			require.NoError(t, err)
			require.Equal(t, model, gjson.GetBytes(upstream.lastBody, "model").String())
			require.Equal(t, alias, result.BillingModel)
			require.Equal(t, "/v1/generations", upstream.lastReq.URL.Path)
			account.Credentials["model_mapping"] = map[string]any{}
			require.ErrorContains(t, ValidateOpenAIVideoCreateBodyForAccount(account, body), "尚未配置")
		})
	}
}

func TestZYCANewModelParameterBoundaries(t *testing.T) {
	for _, test := range []struct {
		model, resolution, ratio string
		duration                 int
		valid                    bool
	}{
		{"wan-3.0", "480p", "16:9", 30, true},
		{"wan-3.0", "720p", "16:9", 6, false},
		{"wan-3.0", "1080p", "16:9", 5, false},
		{"kling-4.0", "1080p", "1:1", 30, true},
		{"kling-4.0", "480p", "16:9", 5, false},
		{"seedance-2.5", "720p", "16:9", 30, true},
		{"seedance-2.0", "480p", "9:16", 15, true},
		{"seedance-2.0", "720p", "1:1", 5, false},
		{"seedance-2.0-mini", "720p", "16:9", 16, false},
		{"seedance-2.0-mini", "720p", "1:1", 5, false},
	} {
		body := []byte(fmt.Sprintf(`{"model":%q,"prompt":"测试","duration":%d,"resolution":%q,"aspect_ratio":%q}`, test.model, test.duration, test.resolution, test.ratio))
		err := ValidateOpenAIVideoCreateBodyForAccount(zycaTestAccount(), body)
		if test.valid {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
	}
	for _, count := range []int{5000, 5001} {
		body := []byte(fmt.Sprintf(`{"model":"wan-3.0","prompt":%q,"duration":5,"resolution":"720p"}`, strings.Repeat("字", count)))
		err := ValidateOpenAIVideoCreateBodyForAccount(zycaTestAccount(), body)
		if count == 5000 {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
	}
}

func TestZYCANewModelRulesDoNotApplyToLegacyProtocol(t *testing.T) {
	account := zycaTestAccount()
	account.Credentials["video_request_profile"] = "legacy"
	for _, duration := range []int{6, 30} {
		body := []byte(fmt.Sprintf(`{"model":"wan-3.0","prompt":"测试","duration":%d,"resolution":"720p"}`, duration))
		err := ValidateOpenAIVideoCreateBodyForAccount(account, body)
		if duration == 6 {
			require.NoError(t, err)
		} else {
			require.ErrorContains(t, err, "15 seconds")
		}
	}
}

func TestZYCANewModelReferenceLimits(t *testing.T) {
	for _, test := range []struct {
		model                  string
		images, videos, audios int
	}{
		{"wan-3.0", 10, 5, 5},
		{"kling-4.0", 10, 5, 3},
		{"seedance-2.5", 30, 10, 10},
		{"seedance-2.0", 9, 3, 3},
		{"seedance-2.0-mini", 9, 3, 3},
	} {
		for field, limit := range map[string]int{"reference_image_urls": test.images, "reference_videos": test.videos, "reference_audios": test.audios} {
			for _, count := range []int{limit, limit + 1} {
				urls := make([]string, count)
				for index := range urls {
					urls[index] = fmt.Sprintf("https://cdn.example/media/%d", index)
				}
				body, err := json.Marshal(map[string]any{"model": test.model, "prompt": "测试", "duration": 5, "resolution": "720p", field: urls})
				require.NoError(t, err)
				err = ValidateOpenAIVideoCreateBodyForAccount(zycaTestAccount(), body)
				if count == limit {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
			}
		}
	}
}

func TestZYCASeedancePixelSizeCannotBypassRatioLimits(t *testing.T) {
	for _, size := range []string{"720x720", "1280x720", "720x1280"} {
		body := []byte(fmt.Sprintf(`{"model":"seedance-2.0-mini","prompt":"测试","duration":5,"resolution":"720p","size":%q}`, size))
		err := ValidateOpenAIVideoCreateBodyForAccount(zycaTestAccount(), body)
		if size == "720x720" {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
		}
	}
}
