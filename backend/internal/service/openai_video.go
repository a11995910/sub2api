package service

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

type OpenAIVideoRequest struct {
	Model           string
	Prompt          string
	Resolution      string
	DurationSeconds int
	ImageURLs       []string
}

type OpenAIVideoResult struct {
	TaskID       string
	Model        string
	Status       string
	Progress     int
	VideoURL     string
	ErrorMessage string
}

type OpenAIVideoContext struct {
	Model               string
	Resolution          string
	DurationSeconds     int
	ReferenceImageCount int
	UserID              int64
	APIKeyID            int64
	GroupID             int64
	BindTask            bool
}

const openAIVideoContextKey = "openai_video_context"

func SetOpenAIVideoContext(c *gin.Context, meta OpenAIVideoContext) {
	if c != nil {
		c.Set(openAIVideoContextKey, meta)
	}
}

func openAIVideoContextFromGin(c *gin.Context) (OpenAIVideoContext, bool) {
	if c == nil {
		return OpenAIVideoContext{}, false
	}
	value, ok := c.Get(openAIVideoContextKey)
	if !ok {
		return OpenAIVideoContext{}, false
	}
	meta, ok := value.(OpenAIVideoContext)
	return meta, ok && strings.TrimSpace(meta.Model) != ""
}

// HasOpenAIVideoContext 供共用 Chat handler 选择视频审核与账号能力。
func HasOpenAIVideoContext(c *gin.Context) bool {
	_, ok := openAIVideoContextFromGin(c)
	return ok
}

func NormalizeOpenAIVideoCreateBody(body []byte, mappedModel string) ([]byte, OpenAIVideoRequest, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, OpenAIVideoRequest{}, fmt.Errorf("decode video request: %w", err)
	}

	request := OpenAIVideoRequest{
		Model:      strings.TrimSpace(stringValue(payload["model"])),
		Prompt:     strings.TrimSpace(stringValue(payload["prompt"])),
		Resolution: strings.ToLower(strings.TrimSpace(stringValue(payload["resolution"]))),
	}
	if request.Model == "" {
		return nil, OpenAIVideoRequest{}, fmt.Errorf("model is required")
	}
	if request.Prompt == "" {
		return nil, OpenAIVideoRequest{}, fmt.Errorf("prompt is required")
	}
	if request.Resolution == "" {
		request.Resolution = VideoBillingResolution720P
	}

	request.DurationSeconds = openAIVideoInt(payload["duration"])
	if request.DurationSeconds <= 0 {
		request.DurationSeconds = openAIVideoInt(payload["seconds"])
	}
	if request.DurationSeconds <= 0 {
		request.DurationSeconds = VideoBillingDefaultDurationSeconds
	}
	if request.DurationSeconds > 15 {
		return nil, OpenAIVideoRequest{}, fmt.Errorf("duration must not exceed 15 seconds")
	}

	request.ImageURLs = collectOpenAIVideoImageURLs(payload)
	upstreamModel := strings.TrimSpace(mappedModel)
	if upstreamModel == "" {
		upstreamModel = request.Model
	}
	payload["model"] = upstreamModel
	payload["prompt"] = request.Prompt
	payload["resolution"] = request.Resolution
	payload["seconds"] = strconv.Itoa(request.DurationSeconds)
	delete(payload, "duration")
	delete(payload, "image")
	delete(payload, "reference_images")
	delete(payload, "reference_image_urls")
	if len(request.ImageURLs) > 0 {
		payload["image_urls"] = request.ImageURLs
	} else {
		delete(payload, "image_urls")
	}

	normalized, err := json.Marshal(payload)
	if err != nil {
		return nil, OpenAIVideoRequest{}, fmt.Errorf("encode video request: %w", err)
	}
	return normalized, request, nil
}

func collectOpenAIVideoImageURLs(payload map[string]any) []string {
	urls := make([]string, 0, 4)
	seen := make(map[string]struct{})
	appendURL := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		if _, exists := seen[raw]; exists {
			return
		}
		seen[raw] = struct{}{}
		urls = append(urls, raw)
	}
	appendArray := func(value any) {
		items, _ := value.([]any)
		for _, item := range items {
			appendURL(stringValue(item))
		}
	}

	appendArray(payload["image_urls"])
	appendArray(payload["reference_image_urls"])
	if image, ok := payload["image"].(map[string]any); ok {
		appendURL(stringValue(image["url"]))
	}
	if items, ok := payload["reference_images"].([]any); ok {
		for _, item := range items {
			image, _ := item.(map[string]any)
			appendURL(stringValue(image["url"]))
		}
	}
	return urls
}

func ParseOpenAIVideoResult(body []byte) (OpenAIVideoResult, error) {
	if !gjson.ValidBytes(body) {
		return OpenAIVideoResult{}, fmt.Errorf("decode video response: invalid JSON")
	}
	result := OpenAIVideoResult{
		TaskID: firstGJSONVideoString(body,
			"task_id", "id", "request_id", "data.task_id", "data.id", "data.request_id"),
		Model:  firstGJSONVideoString(body, "model", "data.model"),
		Status: NormalizeOpenAIVideoStatus(firstGJSONVideoString(body, "status", "data.status", "state", "data.state")),
		VideoURL: firstValidOpenAIVideoURL(body,
			"metadata.url", "video_url", "result_url", "url", "video_urls.0", "videos.0.url",
			"data.metadata.url", "data.video_url", "data.result_url", "data.url", "data.video_urls.0", "data.videos.0.url"),
		ErrorMessage: firstGJSONVideoString(body, "error.message", "data.error.message", "message", "detail"),
	}
	progressText := firstGJSONVideoString(body, "progress", "data.progress")
	progressText = strings.TrimSuffix(strings.TrimSpace(progressText), "%")
	if progress, err := strconv.Atoi(progressText); err == nil {
		if progress < 0 {
			progress = 0
		}
		if progress > 100 {
			progress = 100
		}
		result.Progress = progress
	}
	return result, nil
}

func NormalizeOpenAIVideoStatus(status string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case "pending", "queued", "queueing":
		return "queued"
	case "in_progress", "processing", "running":
		return "in_progress"
	case "completed", "succeeded", "success", "done":
		return "completed"
	case "failed", "error", "cancelled", "canceled":
		return "failed"
	default:
		return status
	}
}

func IsOpenAIVideoEndpointUnsupported(status int, body []byte) bool {
	if status == 404 || status == 405 {
		return true
	}
	if status != 400 || len(body) == 0 {
		return false
	}
	text := strings.ToLower(strings.Join([]string{
		gjson.GetBytes(body, "error.code").String(),
		gjson.GetBytes(body, "error.type").String(),
		gjson.GetBytes(body, "error.message").String(),
		gjson.GetBytes(body, "code").String(),
		gjson.GetBytes(body, "type").String(),
		gjson.GetBytes(body, "message").String(),
	}, " "))
	for _, marker := range []string{
		"unsupported_endpoint",
		"endpoint_not_supported",
		"unsupported endpoint",
		"endpoint is not supported",
		"route not found",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func firstGJSONVideoString(body []byte, paths ...string) string {
	for _, path := range paths {
		if value := strings.TrimSpace(gjson.GetBytes(body, path).String()); value != "" {
			return value
		}
	}
	return ""
}

func firstValidOpenAIVideoURL(body []byte, paths ...string) string {
	for _, path := range paths {
		if value := validOpenAIVideoURL(gjson.GetBytes(body, path).String()); value != "" {
			return value
		}
	}
	return ""
}

func validOpenAIVideoURL(value string) string {
	value = strings.TrimSpace(value)
	parsed, err := url.Parse(value)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Host == "" || parsed.User != nil {
		return ""
	}
	return parsed.String()
}

func openAIVideoInt(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(typed))
		return parsed
	default:
		return 0
	}
}
