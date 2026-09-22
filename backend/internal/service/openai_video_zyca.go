package service

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

type zycaVideoRejectedError struct {
	message string
}

func (e *zycaVideoRejectedError) Error() string { return e.message }

type zycaVideoModelProfile struct {
	minSeconds                      int
	maxSecondsByResolution          map[string]int
	maxImages, maxVideos, maxAudios int
	maxReferences                   int
	requiresReference               map[string]bool
}

var zycaVideoModelProfiles = map[string]zycaVideoModelProfile{
	"auto-video": {
		minSeconds: 1, maxSecondsByResolution: map[string]int{VideoBillingResolution720P: 12}, maxImages: 5,
	},
	"agnes-video-2.5-flash": {
		minSeconds: 1, maxSecondsByResolution: map[string]int{VideoBillingResolution720P: 12}, maxImages: 5,
	},
	"grok-imagine-video-1.5": {
		minSeconds: 1, maxSecondsByResolution: map[string]int{
			VideoBillingResolution480P: 15, VideoBillingResolution720P: 15, VideoBillingResolution1080P: 15,
		}, maxImages: 7,
	},
	"kling-video-v3-omni": {
		minSeconds: 3, maxSecondsByResolution: map[string]int{
			VideoBillingResolution480P: 15, VideoBillingResolution720P: 15, VideoBillingResolution1080P: 15,
		}, maxImages: 7,
	},
	"minimax-h3": {
		minSeconds: 4, maxSecondsByResolution: map[string]int{
			VideoBillingResolution720P: 15, VideoBillingResolution1080P: 15,
			VideoBillingResolution2K: 15, VideoBillingResolution4K: 15,
		}, maxImages: 9, maxVideos: 3, maxAudios: 3, maxReferences: 12,
		requiresReference: map[string]bool{VideoBillingResolution2K: true, VideoBillingResolution4K: true},
	},
	"minimax-h3-903": {
		minSeconds: 1, maxSecondsByResolution: map[string]int{
			VideoBillingResolution720P: 15, VideoBillingResolution1080P: 15,
		}, maxImages: 9, maxAudios: 3, maxReferences: 12,
	},
	"seedance-2.0mini_933": {
		minSeconds: 1, maxSecondsByResolution: map[string]int{
			VideoBillingResolution480P: 15, VideoBillingResolution720P: 12,
		}, maxImages: 9, maxVideos: 3, maxAudios: 3,
	},
}

// ZYCA 参数依据统一生图生视频接口文档，复用对外视频契约。
func PrepareZYCAVideoCreateBody(payload map[string]any, request OpenAIVideoRequest, mappedModel string) (OpenAIVideoPreparedRequest, error) {
	for field := range payload {
		if _, ok := openAIVideoUnifiedAcceptedFields[field]; !ok {
			return OpenAIVideoPreparedRequest{}, fmt.Errorf("ZYCA 不支持视频字段 %q", field)
		}
	}
	model := strings.TrimSpace(mappedModel)
	if model == "" {
		model = request.Model
	}
	profile, configured := zycaVideoModelProfiles[model]
	if !configured {
		return OpenAIVideoPreparedRequest{}, fmt.Errorf("尚未配置 ZYCA 视频模型 %q", model)
	}
	resolution, knownResolution := LookupVideoBillingResolution(request.Resolution)
	maxSeconds, supportedResolution := profile.maxSecondsByResolution[resolution]
	if !knownResolution || !supportedResolution {
		return OpenAIVideoPreparedRequest{}, fmt.Errorf("请显式指定 ZYCA 模型支持的清晰度")
	}
	for _, field := range []string{"duration", "seconds"} {
		if value, exists := payload[field]; exists {
			seconds, err := strconv.Atoi(fmt.Sprint(value))
			if err != nil || seconds < profile.minSeconds || seconds > maxSeconds {
				return OpenAIVideoPreparedRequest{}, fmt.Errorf("%s 必须为 %d 到 %d 的整数秒", field, profile.minSeconds, maxSeconds)
			}
		}
	}
	if request.DurationSeconds < profile.minSeconds || request.DurationSeconds > maxSeconds {
		return OpenAIVideoPreparedRequest{}, fmt.Errorf("视频时长必须为 %d 到 %d 秒", profile.minSeconds, maxSeconds)
	}
	referenceCount := len(request.ImageURLs) + len(request.VideoURLs) + len(request.AudioURLs)
	if len(request.ImageURLs) > profile.maxImages || len(request.VideoURLs) > profile.maxVideos || len(request.AudioURLs) > profile.maxAudios ||
		(profile.maxReferences > 0 && referenceCount > profile.maxReferences) {
		return OpenAIVideoPreparedRequest{}, fmt.Errorf("参考素材数量超过 ZYCA 模型限制")
	}
	if request.FirstImageURL != "" || request.LastImageURL != "" {
		return OpenAIVideoPreparedRequest{}, fmt.Errorf("ZYCA 首尾帧协议尚未支持")
	}
	if profile.requiresReference[resolution] && referenceCount == 0 {
		return OpenAIVideoPreparedRequest{}, fmt.Errorf("ZYCA 2K 和 4K 视频至少需要一个参考素材")
	}
	request.Resolution = resolution
	size, err := zycaVideoSize(payload, resolution)
	if err != nil {
		return OpenAIVideoPreparedRequest{}, err
	}
	parameters := map[string]any{"seconds": request.DurationSeconds, "resolution": resolution}
	if request.GenerateAudio != nil {
		parameters["generate_audio"] = *request.GenerateAudio
	}
	for field, urls := range map[string][]string{
		"input_images": request.ImageURLs, "input_videos": request.VideoURLs, "input_audios": request.AudioURLs,
	} {
		for _, raw := range urls {
			if validOpenAIVideoURL(raw) == "" {
				return OpenAIVideoPreparedRequest{}, fmt.Errorf("参考素材必须使用不含凭据的 HTTPS URL")
			}
		}
		if len(urls) > 0 {
			parameters[field] = urls
		}
	}
	upstream := map[string]any{
		"client_request_id": uuid.NewString(),
		"model":             model, "prompt": request.Prompt, "duration": request.DurationSeconds, "parameters": parameters,
	}
	if size != "" {
		upstream["size"] = size
	}
	body, err := json.Marshal(upstream)
	return OpenAIVideoPreparedRequest{Body: body, Request: request}, err
}

func zycaVideoSize(payload map[string]any, resolution string) (string, error) {
	// 项目尺寸策略：短边取清晰度像素值，长边按比例取最近偶数；不是上游固定尺寸枚举。
	shortEdge := map[string]int{"480p": 480, "720p": 720, "768p": 768, "1080p": 1080, "2K": 1440, "4K": 2160}[resolution]
	ratios := map[string][2]int{"21:9": {21, 9}, "16:9": {16, 9}, "4:3": {4, 3}, "1:1": {1, 1}, "3:4": {3, 4}, "9:16": {9, 16}}
	sizes := make(map[string]string, len(ratios))
	for ratio, parts := range ratios {
		width, height := shortEdge, shortEdge
		if parts[0] > parts[1] {
			width = int(math.Round(float64(shortEdge*parts[0])/float64(parts[1])/2)) * 2
		} else {
			height = int(math.Round(float64(shortEdge*parts[1])/float64(parts[0])/2)) * 2
		}
		sizes[ratio] = fmt.Sprintf("%dx%d", width, height)
	}
	var selected string
	for _, field := range []string{"aspect_ratio", "size"} {
		value, exists := payload[field]
		if !exists {
			continue
		}
		raw, ok := value.(string)
		if !ok || strings.TrimSpace(raw) == "" {
			return "", fmt.Errorf("%s 必须为有效画面比例或像素尺寸", field)
		}
		raw = strings.TrimSpace(raw)
		size := sizes[raw]
		if size == "" && field == "size" {
			for _, supported := range sizes {
				if raw == supported {
					size = raw
					break
				}
			}
		}
		if size == "" || (selected != "" && selected != size) {
			return "", fmt.Errorf("ZYCA 画面比例、size 与清晰度不匹配")
		}
		selected = size
	}
	return selected, nil
}

func parseZYCAVideoResult(body []byte) (OpenAIVideoResult, error) {
	var envelope struct {
		Success *bool  `json:"success"`
		Message string `json:"message"`
		Data    *struct {
			ID           string `json:"id"`
			Model        string `json:"model"`
			Status       string `json:"status"`
			ErrorMessage string `json:"error_message"`
			Result       *struct {
				Progress *float64 `json:"progress"`
				URLs     []string `json:"urls"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return OpenAIVideoResult{}, fmt.Errorf("ZYCA 视频响应格式无效")
	}
	if envelope.Success == nil {
		return OpenAIVideoResult{}, fmt.Errorf("ZYCA 视频响应缺少成功标志")
	}
	if !*envelope.Success {
		// 拒绝响应同时携带任务 ID 时不能确定任务是否已创建，继续保留待核查。
		if envelope.Data != nil && envelope.Data.ID != "" {
			return OpenAIVideoResult{}, fmt.Errorf("ZYCA 拒绝响应包含任务 ID")
		}
		return OpenAIVideoResult{}, &zycaVideoRejectedError{message: "ZYCA 视频请求失败：" + zycaVideoErrorMessage(envelope.Message)}
	}
	if envelope.Data == nil {
		return OpenAIVideoResult{}, fmt.Errorf("ZYCA 视频响应缺少任务数据")
	}
	data := envelope.Data
	if strings.TrimSpace(data.ID) == "" || len(data.ID) > 256 || strings.TrimSpace(data.ID) != data.ID {
		return OpenAIVideoResult{}, fmt.Errorf("ZYCA 视频响应缺少有效任务 ID")
	}
	result := OpenAIVideoResult{TaskID: data.ID, Model: data.Model, Status: NormalizeOpenAIVideoStatus(data.Status)}
	switch result.Status {
	case "queued", "in_progress", "completed", "failed":
	default:
		return OpenAIVideoResult{}, fmt.Errorf("ZYCA 视频响应包含未知任务状态")
	}
	if data.Result != nil {
		if data.Result.Progress != nil {
			result.Progress = int(math.Max(0, math.Min(100, *data.Result.Progress)))
		}
		if result.Status == "completed" && len(data.Result.URLs) > 0 {
			result.VideoURL = validOpenAIVideoURL(data.Result.URLs[0])
		}
	}
	if result.Status == "failed" {
		result.ErrorMessage = zycaVideoErrorMessage(data.ErrorMessage)
	}
	return result, nil
}

func zycaVideoErrorMessage(message string) string {
	message = strings.TrimSpace(sanitizeUpstreamErrorMessage(message))
	if message == "" {
		return "ZYCA 视频生成失败"
	}
	return truncateString(message, 1024)
}

func parseOpenAIVideoResultForAccount(account *Account, body []byte) (OpenAIVideoResult, error) {
	if ResolveOpenAIVideoRequestProfile(account) == OpenAIVideoRequestProfileZYCA {
		return parseZYCAVideoResult(body)
	}
	return ParseOpenAIVideoResult(body)
}

func openAIVideoUpstreamEndpoint(account *Account) string {
	if ResolveOpenAIVideoRequestProfile(account) == OpenAIVideoRequestProfileZYCA {
		return "/v1/generations"
	}
	return "/v1/videos"
}
