//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func zycaTestAccount() *Account {
	account := openAIVideoForwardTestAccount()
	account.Credentials["video_request_profile"] = "zyca"
	account.Credentials["model_mapping"] = map[string]any{"public-video": "minimax-h3"}
	return account
}

func zycaTestResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestZYCAProfileSelection(t *testing.T) {
	for _, tt := range []struct {
		host, configured string
		expected         OpenAIVideoRequestProfile
	}{
		{"https://api.zyca.top/v1", "", OpenAIVideoRequestProfileZYCA},
		{"https://CANVAS.ZYCA.TOP/v1", "auto", OpenAIVideoRequestProfileZYCA},
		{"https://api.zyca.top.evil.test/v1", "", OpenAIVideoRequestProfileLegacy},
		{"https://relay.test/v1", "zyca", OpenAIVideoRequestProfileZYCA},
		{"https://api.zyca.top/v1", "legacy", OpenAIVideoRequestProfileLegacy},
		{"https://api.zyca.top/v1", "unified_json", OpenAIVideoRequestProfileUnifiedJSON},
	} {
		account := zycaTestAccount()
		account.Credentials["base_url"], account.Credentials["video_request_profile"] = tt.host, tt.configured
		require.Equal(t, tt.expected, ResolveOpenAIVideoRequestProfile(account))
	}
}

func TestZYCACreateMapsRequestAndBindsOwner(t *testing.T) {
	body := []byte(`{"model":"public-video","prompt":"雨夜城市","duration":4,"resolution":"1080p","generate_audio":false,"reference_image_urls":["https://cdn.test/a.png"],"reference_videos":["https://cdn.test/a.mp4"],"reference_audios":["https://cdn.test/a.mp3"]}`)
	c, recorder := openAIVideoForwardTestContext(body)
	SetOpenAIVideoContext(c, OpenAIVideoContext{Model: "public-video", UserID: 10, APIKeyID: 20, GroupID: 7, BindTask: true})
	cache := &videoProtocolGatewayCacheStub{}
	upstream := &httpUpstreamRecorder{resp: zycaTestResponse(200, `{"success":true,"data":{"id":"task-1","status":"queued"}}`)}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream, cache: cache}
	result, err := svc.ForwardOpenAIVideoCreate(context.Background(), c, zycaTestAccount(), body, "")
	require.NoError(t, err)
	require.Equal(t, "/v1/generations", upstream.lastReq.URL.Path)
	require.Equal(t, "minimax-h3", gjson.GetBytes(upstream.lastBody, "model").String())
	require.Equal(t, int64(4), gjson.GetBytes(upstream.lastBody, "parameters.seconds").Int())
	require.Equal(t, int64(4), gjson.GetBytes(upstream.lastBody, "duration").Int())
	require.Equal(t, "false", gjson.GetBytes(upstream.lastBody, "parameters.generate_audio").Raw)
	require.Equal(t, "https://cdn.test/a.png", gjson.GetBytes(upstream.lastBody, "parameters.input_images.0").String())
	require.Equal(t, "https://cdn.test/a.mp4", gjson.GetBytes(upstream.lastBody, "parameters.input_videos.0").String())
	require.Equal(t, "https://cdn.test/a.mp3", gjson.GetBytes(upstream.lastBody, "parameters.input_audios.0").String())
	_, err = uuid.Parse(gjson.GetBytes(upstream.lastBody, "client_request_id").String())
	require.NoError(t, err)
	require.Equal(t, "public-video", result.BillingModel)
	require.Equal(t, "1080p", result.VideoResolution)
	require.Equal(t, 4, result.VideoDurationSeconds)
	require.Equal(t, 1, result.VideoInputImageCount)
	require.Equal(t, "task-1", gjson.Get(recorder.Body.String(), "id").String())
	require.Zero(t, cache.setCalls)
	group := int64(7)
	owner, err := svc.ResolveVideoTaskAccount(context.Background(), &group, "task-1", 10, 20)
	require.NoError(t, err)
	require.Equal(t, int64(88), owner)
	for _, identity := range [][3]int64{{7, 11, 20}, {7, 10, 21}, {8, 10, 20}} {
		_, err := svc.ResolveVideoTaskAccount(context.Background(), &identity[0], "task-1", identity[1], identity[2])
		require.Error(t, err)
	}
}

func TestZYCAValidationBeforeUpstreamSubmission(t *testing.T) {
	for _, extra := range []string{
		`"duration":3`, `"duration":4.5`, `"duration":0`, `"duration":true`, `"duration":16`,
		`"seconds":"4.5"`, `"resolution":"720p"`, `"resolution":"2K"`, `"resolution":"4K"`,
		`"aspect_ratio":"2:1"`, `"size":"1280x720"`, `"seed":1`, `"first_image_url":"https://cdn.test/a.png"`,
		`"aspect_ratio":"16:9","size":"1080x1920"`, `"aspect_ratio":16`,
		`"reference_videos":["http://cdn.test/a.mp4"]`,
		`"reference_audios":["https://user:pass@cdn.test/a.mp3"]`,
	} {
		t.Run(extra, func(t *testing.T) {
			payload := map[string]any{"model": "public-video", "prompt": "视频", "duration": 4, "resolution": "1080p"}
			var overrides map[string]any
			require.NoError(t, json.Unmarshal([]byte("{"+extra+"}"), &overrides))
			for key, value := range overrides {
				payload[key] = value
			}
			body, err := json.Marshal(payload)
			require.NoError(t, err)
			require.Error(t, ValidateOpenAIVideoCreateBodyForAccount(zycaTestAccount(), body))
			c, _ := openAIVideoForwardTestContext(body)
			upstream := &httpUpstreamRecorder{}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			_, err = svc.ForwardOpenAIVideoCreate(context.Background(), c, zycaTestAccount(), body, "")
			require.Error(t, err)
			require.Empty(t, upstream.requests)
		})
	}
}

func TestZYCAQueryStrictEnvelopeAndBillingOutcome(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		status     string
		invalid    bool
	}{
		{"处理中", `{"success":true,"data":{"id":"task-1","status":"processing","result":{"progress":35}}}`, VideoTaskStatusProcessing, false},
		{"已完成", `{"success":true,"data":{"id":"task-1","status":"succeeded","result":{"urls":["https://8.8.8.8/video.mp4"]}}}`, VideoTaskStatusCompleted, false},
		{"完成但无结果", `{"success":true,"data":{"id":"task-1","status":"succeeded"}}`, VideoTaskStatusUnknown, false},
		{"完成但结果为内网地址", `{"success":true,"data":{"id":"task-1","status":"succeeded","result":{"urls":["https://127.0.0.1/video.mp4"]}}}`, VideoTaskStatusUnknown, false},
		{"失败但含结果", `{"success":true,"data":{"id":"task-1","status":"failed","result":{"urls":["https://8.8.8.8/video.mp4"]}}}`, VideoTaskStatusFailed, false},
		{"取消", `{"success":true,"data":{"id":"task-1","status":"cancelled"}}`, VideoTaskStatusFailed, false},
		{"业务失败", `{"success":false,"data":{"id":"task-1","status":"succeeded"}}`, "", true},
		{"任务不匹配", `{"success":true,"data":{"id":"another-task","status":"succeeded"}}`, "", true},
		{"未知状态", `{"success":true,"data":{"id":"task-1","status":"unknown"}}`, "", true},
		{"缺少状态", `{"success":true,"data":{"id":"task-1"}}`, "", true},
		{"错误协议", `{"id":"task-1","status":"completed"}`, "", true},
		{"无效数据", `{"success":true,"data":[]}`, "", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: zycaTestResponse(200, tt.body)}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			result, err := svc.QueryOpenAIVideoTask(context.Background(), zycaTestAccount(), "task-1")
			if tt.invalid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.status, ClassifyVideoTaskResult(result).Status)
			require.Equal(t, "/v1/generations/task-1", upstream.lastReq.URL.Path)
		})
	}
}

func TestZYCACreateFailureNeverRetries(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 429, 502} {
		body := []byte(`{"model":"public-video","prompt":"视频","duration":4,"resolution":"1080p"}`)
		c, _ := openAIVideoForwardTestContext(body)
		upstream := &httpUpstreamRecorder{resp: zycaTestResponse(status, `{"error":{"message":"rejected"}}`)}
		svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
		_, err := svc.ForwardOpenAIVideoCreate(context.Background(), c, zycaTestAccount(), body, "")
		require.Error(t, err)
		require.Len(t, upstream.requests, 1)
		var failover *UpstreamFailoverError
		if errors.As(err, &failover) {
			require.False(t, failover.ShouldRetryNextAccount())
		}
	}
}

func TestZYCADownloadAfterRestartQueriesResultWithoutForwardingKey(t *testing.T) {
	c, recorder := openAIVideoForwardTestContext(nil)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/task-1/content", nil)
	c.Request.Header.Set("Range", "bytes=0-3")
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		zycaTestResponse(200, `{"success":true,"data":{"id":"task-1","status":"succeeded","result":{"urls":["https://8.8.8.8/result.mp4?signature=test"]}}}`),
		{StatusCode: 206, Header: http.Header{"Content-Type": {"video/mp4"}, "Content-Range": {"bytes 0-3/8"}}, Body: io.NopCloser(strings.NewReader("test")), ContentLength: 4},
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	_, err := svc.ForwardOpenAIVideoContent(context.Background(), c, zycaTestAccount(), "task-1")
	require.NoError(t, err)
	require.Equal(t, "test", recorder.Body.String())
	require.Equal(t, 206, recorder.Code)
	require.Len(t, upstream.requests, 2)
	require.Equal(t, "/v1/generations/task-1", upstream.requests[0].URL.Path)
	require.NotEmpty(t, upstream.requests[0].Header.Get("Authorization"))
	require.Empty(t, upstream.requests[1].Header.Get("Authorization"))
	require.Equal(t, "bytes=0-3", upstream.requests[1].Header.Get("Range"))
	require.True(t, HTTPUpstreamRedirectsDisabled(upstream.requests[1].Context()))
}

func TestZYCAReconciliationUsesExistingSettlement(t *testing.T) {
	for _, tt := range []struct {
		status   string
		expected []string
		charged  bool
	}{
		{"succeeded", []string{"poll:completed", "settling", "capture"}, true},
		{"failed", []string{"poll:failed", "release"}, false},
	} {
		t.Run(tt.status, func(t *testing.T) {
			task := &VideoTaskBilling{ID: 9, Platform: PlatformOpenAI, AccountID: 88, UpstreamTaskID: "task-1", TaskStatus: VideoTaskStatusPending, BillingStatus: VideoTaskBillingReserved}
			repo := &fakeReconciliationBillingRepo{fakeVideoTaskBillingRepo: fakeVideoTaskBillingRepo{task: task}, due: []*VideoTaskBilling{task}}
			usage := &fakeVideoTaskUsageRecorder{}
			upstream := &httpUpstreamRecorder{resp: zycaTestResponse(200, `{"success":true,"data":{"id":"task-1","status":"`+tt.status+`","result":{"urls":["https://8.8.8.8/video.mp4"]}}}`)}
			gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			worker := NewVideoTaskReconciliationService(NewVideoTaskBillingService(repo, nil, usage), gateway, &fakeVideoTaskAccountRepo{account: zycaTestAccount()})
			require.NoError(t, worker.ReconcileOnce(context.Background()))
			require.Equal(t, tt.expected, repo.events)
			require.Equal(t, tt.charged, len(usage.events) > 0)
		})
	}
}

func TestZYCADownloadRejectsInvalidTaskAndArtifact(t *testing.T) {
	for _, task := range []string{
		`{"id":"task-1","status":"running","result":{"urls":["https://8.8.8.8/video.mp4"]}}`,
		`{"id":"task-1","status":"failed","result":{"urls":["https://8.8.8.8/video.mp4"]}}`,
		`{"id":"other","status":"succeeded","result":{"urls":["https://8.8.8.8/video.mp4"]}}`,
		`{"id":"task-1","status":"succeeded","result":{"urls":["https://127.0.0.1/video.mp4"]}}`,
		`{"id":"task-1","status":"succeeded","result":{"urls":["http://8.8.8.8/video.mp4"]}}`,
		`{"id":"task-1","status":"succeeded","result":{"urls":[]}}`,
	} {
		c, _ := openAIVideoForwardTestContext(nil)
		upstream := &httpUpstreamRecorder{resp: zycaTestResponse(200, `{"success":true,"data":`+task+`}`)}
		svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
		_, err := svc.ForwardOpenAIVideoContent(context.Background(), c, zycaTestAccount(), "task-1")
		require.Error(t, err)
		require.Len(t, upstream.requests, 1)
	}
}

func TestZYCAModelLimits(t *testing.T) {
	for _, tt := range []struct {
		model            string
		min, max, images int
	}{
		{"auto-video", 1, 12, 5}, {"grok-imagine-video-1.5", 1, 15, 7},
		{"kling-video-v3-omni", 3, 15, 7}, {"minimax-h3", 4, 15, 9},
	} {
		t.Run(tt.model, func(t *testing.T) {
			for _, duration := range []int{tt.min, tt.max} {
				payload := map[string]any{"model": tt.model, "prompt": "视频", "duration": duration, "resolution": "1080p"}
				body, err := json.Marshal(payload)
				require.NoError(t, err)
				require.NoError(t, ValidateOpenAIVideoCreateBodyForAccount(zycaTestAccount(), body))
			}
			refs := make([]string, tt.images+1)
			for i := range refs {
				refs[i] = "https://cdn.test/" + uuid.NewString() + ".png"
			}
			body, err := json.Marshal(map[string]any{"model": tt.model, "prompt": "视频", "duration": tt.min, "resolution": "1080p", "reference_image_urls": refs})
			require.NoError(t, err)
			require.Error(t, ValidateOpenAIVideoCreateBodyForAccount(zycaTestAccount(), body))
		})
	}
}

func TestZYCADocumentedSizesAndResolutions(t *testing.T) {
	for _, tt := range []struct{ model, resolution, ratio, size string }{
		{"auto-video", "720p", "16:9", "1280x720"},
		{"grok-imagine-video-1.5", "480p", "9:16", "480x854"},
		{"kling-video-v3-omni", "1080p", "1:1", "1080x1080"},
		{"minimax-h3", "768p", "4:3", "1024x768"},
		{"minimax-h3", "1080p", "3:4", "1080x1440"},
		{"minimax-h3", "2K", "21:9", "3360x1440"},
		{"minimax-h3", "4K", "16:9", "3840x2160"},
	} {
		t.Run(tt.model+tt.resolution, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{"model": tt.model, "prompt": "视频", "duration": 5,
				"resolution": tt.resolution, "aspect_ratio": tt.ratio, "size": tt.size,
				"reference_image_urls": []string{"https://cdn.test/ref.png"}})
			require.NoError(t, err)
			payload, request, err := ParseOpenAIVideoCreateBody(body)
			require.NoError(t, err)
			prepared, err := PrepareZYCAVideoCreateBody(payload, request, tt.model)
			require.NoError(t, err)
			require.Equal(t, tt.size, gjson.GetBytes(prepared.Body, "size").String())
			require.Equal(t, tt.resolution, prepared.Request.Resolution)
			require.Equal(t, tt.resolution, gjson.GetBytes(prepared.Body, "parameters.resolution").String())
			require.False(t, gjson.GetBytes(prepared.Body, "parameters.watermark").Exists())
		})
	}
	for _, model := range []string{"auto-video", "grok-imagine-video-1.5", "kling-video-v3-omni"} {
		for _, resolution := range []string{"768p", "2K", "4K"} {
			body, err := json.Marshal(map[string]any{"model": model, "prompt": "视频", "duration": 5, "resolution": resolution})
			require.NoError(t, err)
			require.Error(t, ValidateOpenAIVideoCreateBodyForAccount(zycaTestAccount(), body))
		}
	}
}

func TestZYCAFailureMessages(t *testing.T) {
	result, err := parseZYCAVideoResult([]byte(`{"success":true,"data":{"id":"task-1","status":"failed","error_message":"参考素材无法下载 https://private.test/file?api_key=secretvalue"}}`))
	require.NoError(t, err)
	require.Contains(t, result.ErrorMessage, "参考素材无法下载")
	require.NotContains(t, result.ErrorMessage, "private.test")
	require.NotContains(t, result.ErrorMessage, "secretvalue")
	_, err = parseZYCAVideoResult([]byte(`{"success":false,"message":"模型不可用"}`))
	require.ErrorContains(t, err, "模型不可用")
}

func TestZYCAMiniMaxCombinedReferenceLimit(t *testing.T) {
	for _, count := range []int{12, 13} {
		images := make([]string, count-6)
		for i := range images {
			images[i] = "https://cdn.test/" + uuid.NewString() + ".png"
		}
		body, err := json.Marshal(map[string]any{"model": "minimax-h3", "prompt": "视频", "duration": 5, "resolution": "4K",
			"reference_image_urls": images,
			"reference_videos":     []string{"https://cdn.test/a.mp4", "https://cdn.test/b.mp4", "https://cdn.test/c.mp4"},
			"reference_audios":     []string{"https://cdn.test/a.mp3", "https://cdn.test/b.mp3", "https://cdn.test/c.mp3"}})
		require.NoError(t, err)
		err = ValidateOpenAIVideoCreateBodyForAccount(zycaTestAccount(), body)
		if count == 12 {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
	}
}

func TestZYCAExtendedResolutionPricing(t *testing.T) {
	prices := NormalizeVideoModelPrices(map[string]map[string]float64{"minimax-h3": {"768p": 0.2, "1080p": 0.3, "2k": 0.5, "4k": 0.8}})
	flat := 0.01
	group := &Group{VideoPrice480P: &flat, VideoModelPrices: prices}
	svc := newOpenAIRecordUsageServiceForTest(nil, nil, nil, nil)
	for resolution, price := range prices["minimax-h3"] {
		cost, err := svc.EstimateVideoCost(context.Background(), &APIKey{Group: group}, "minimax-h3", resolution, 5)
		require.NoError(t, err)
		require.InDelta(t, price*5, cost.TotalCost, 1e-9)
		if resolution != "1080p" {
			require.Nil(t, group.GetVideoPrice(resolution))
		}
		require.NoError(t, checkVideoPricingIntervals(ChannelModelPricing{BillingMode: BillingModeVideo,
			Intervals: []PricingInterval{{TierLabel: resolution, PerRequestPrice: &price}}}))
	}
	for _, resolution := range []string{"768p", "2K", "4K"} {
		_, err := svc.EstimateVideoCost(context.Background(), &APIKey{Group: &Group{VideoPrice480P: &flat}}, "minimax-h3", resolution, 5)
		require.ErrorContains(t, err, "每秒价格")
	}
	group.VideoModelPrices["minimax-h3"]["4K"] = 0
	cost, err := svc.EstimateVideoCost(context.Background(), &APIKey{Group: group}, "minimax-h3", "4k", 5)
	require.NoError(t, err)
	require.Zero(t, cost.TotalCost)
}

func TestZYCAExtendedChannelPricingPrecedence(t *testing.T) {
	for _, tt := range []struct {
		name         string
		channel      ChannelModelPricing
		groupPricing []ChannelModelPricing
		groupPrices  map[string]map[string]float64
		want         float64
		missing      bool
	}{
		{name: "渠道分档", channel: ChannelModelPricing{BillingMode: BillingModeVideo, Intervals: []PricingInterval{{TierLabel: "4K", PerRequestPrice: testPtrFloat64(0.8)}}}, want: 4},
		{name: "渠道默认", channel: ChannelModelPricing{BillingMode: BillingModeVideo, PerRequestPrice: testPtrFloat64(0.5)}, want: 2.5},
		{name: "渠道零价", channel: ChannelModelPricing{BillingMode: BillingModeVideo, PerRequestPrice: testPtrFloat64(0)}, want: 0},
		{name: "渠道缺档", channel: ChannelModelPricing{BillingMode: BillingModeVideo, Intervals: []PricingInterval{{TierLabel: "480p", PerRequestPrice: testPtrFloat64(0.1)}}}, missing: true},
		{name: "按次价不是每秒价", channel: ChannelModelPricing{BillingMode: BillingModePerRequest, PerRequestPrice: testPtrFloat64(0.5)}, missing: true},
		{name: "模型分组价优先", channel: ChannelModelPricing{BillingMode: BillingModeVideo, PerRequestPrice: testPtrFloat64(0.5)}, groupPrices: map[string]map[string]float64{"minimax-h3": {"4K": 0.2}}, want: 1},
		{name: "统一分组价缺档禁止回退", channel: ChannelModelPricing{BillingMode: BillingModeVideo, PerRequestPrice: testPtrFloat64(0.5)}, groupPricing: []ChannelModelPricing{{Models: []string{"minimax-h3"}, BillingMode: BillingModeVideo, Intervals: []PricingInterval{{TierLabel: "480p", PerRequestPrice: testPtrFloat64(0.1)}}}}, missing: true},
		{name: "统一分组价", channel: ChannelModelPricing{BillingMode: BillingModeVideo, PerRequestPrice: testPtrFloat64(0.5)}, groupPricing: []ChannelModelPricing{{Models: []string{"minimax-h3"}, BillingMode: BillingModeVideo, PerRequestPrice: testPtrFloat64(0.6)}}, want: 3},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tt.channel.Platform, tt.channel.Models = "anthropic", []string{"minimax-h3"}
			svc := newOpenAIRecordUsageServiceForTest(nil, nil, nil, nil)
			svc.resolver = newResolverWithChannel(t, []ChannelModelPricing{tt.channel})
			key := &APIKey{Group: &Group{ID: 100, ModelPricing: tt.groupPricing, VideoModelPrices: tt.groupPrices}}
			cost, err := svc.EstimateVideoCost(context.Background(), key, "minimax-h3", "4K", 5)
			if tt.missing {
				require.ErrorContains(t, err, "每秒价格")
			} else {
				require.NoError(t, err)
				require.InDelta(t, tt.want, cost.TotalCost, 1e-9)
			}
		})
	}
}

func TestZYCAAllResolutionsRequireExplicitPrices(t *testing.T) {
	svc := newOpenAIRecordUsageServiceForTest(nil, nil, nil, nil)
	for _, tt := range []struct {
		model       string
		resolutions []string
	}{
		{"auto-video", []string{"480p", "720p", "1080p"}},
		{"grok-imagine-video-1.5", []string{"480p", "720p", "1080p"}},
		{"kling-video-v3-omni", []string{"480p", "720p", "1080p"}},
		{"minimax-h3", []string{"768p", "1080p", "2K", "4K"}},
	} {
		for _, resolution := range tt.resolutions {
			t.Run(tt.model+resolution, func(t *testing.T) {
				key := &APIKey{Group: &Group{}}
				_, err := svc.EstimateVideoCostForAccount(context.Background(), zycaTestAccount(), key, tt.model, resolution, 5)
				require.ErrorContains(t, err, "每秒价格")
				for _, price := range []float64{0, 0.2} {
					key.Group.VideoModelPrices = map[string]map[string]float64{tt.model: {resolution: price}}
					cost, err := svc.EstimateVideoCostForAccount(context.Background(), zycaTestAccount(), key, tt.model, resolution, 5)
					require.NoError(t, err)
					require.InDelta(t, price*5, cost.TotalCost, 1e-9)
				}
			})
		}
	}
	_, err := svc.EstimateVideoCostForAccount(context.Background(), openAIVideoForwardTestAccount(), &APIKey{Group: &Group{}}, "grok-imagine-video-1.5", "720p", 5)
	require.NoError(t, err, "历史协议保留既有系统视频定价")
}
