package service

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

type OpenAIVideoRequest struct {
	Model              string
	Prompt             string
	Resolution         string
	AspectRatio        string
	DurationSeconds    int
	ResolutionExplicit bool
	GenerateAudio      *bool
	ImageURLs          []string
	VideoURLs          []string
	AudioURLs          []string
	FirstImageURL      string
	LastImageURL       string
}

type OpenAIVideoRequestProfile string

const (
	OpenAIVideoRequestProfileAuto        OpenAIVideoRequestProfile = "auto"
	OpenAIVideoRequestProfileUnifiedJSON OpenAIVideoRequestProfile = "unified_json"
	OpenAIVideoRequestProfileLegacy      OpenAIVideoRequestProfile = "legacy"
)

func ResolveOpenAIVideoRequestProfile(account *Account) OpenAIVideoRequestProfile {
	if account == nil {
		return OpenAIVideoRequestProfileLegacy
	}
	configured := strings.ToLower(strings.TrimSpace(account.GetCredential("video_request_profile")))
	switch OpenAIVideoRequestProfile(configured) {
	case OpenAIVideoRequestProfileUnifiedJSON:
		return OpenAIVideoRequestProfileUnifiedJSON
	case OpenAIVideoRequestProfileLegacy:
		return OpenAIVideoRequestProfileLegacy
	case "", OpenAIVideoRequestProfileAuto:
		// 继续精确识别官方主机。
	default:
		return OpenAIVideoRequestProfileLegacy
	}

	parsed, err := url.Parse(strings.TrimSpace(account.GetOpenAIBaseURL()))
	if err != nil {
		return OpenAIVideoRequestProfileLegacy
	}
	switch strings.ToLower(parsed.Hostname()) {
	case "ai.cangyuansuanli.cn", "vip-api.cangyuansuanli.cn":
		return OpenAIVideoRequestProfileUnifiedJSON
	default:
		return OpenAIVideoRequestProfileLegacy
	}
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
	Prompt              string
	Resolution          string
	DurationSeconds     int
	ReferenceImageCount int
	UserID              int64
	APIKeyID            int64
	GroupID             int64
	AccountID           int64
	BillingTaskID       int64
	BindTask            bool
	RecordModelTestTask bool
}

const openAIVideoContextKey = "openai_video_context"
const suppressOpenAIVideoResponseKey = "suppress_openai_video_response"

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

func OpenAIVideoContextFromGin(c *gin.Context) (OpenAIVideoContext, bool) {
	return openAIVideoContextFromGin(c)
}

func SetOpenAIVideoBillingTask(c *gin.Context, billingTaskID int64) bool {
	meta, ok := openAIVideoContextFromGin(c)
	if !ok || billingTaskID <= 0 {
		return false
	}
	meta.BillingTaskID = billingTaskID
	SetOpenAIVideoContext(c, meta)
	return true
}

// HasOpenAIVideoContext 供共用 Chat handler 选择视频审核与账号能力。
func HasOpenAIVideoContext(c *gin.Context) bool {
	_, ok := openAIVideoContextFromGin(c)
	return ok
}

func SuppressOpenAIVideoResponse(c *gin.Context) {
	if c != nil {
		c.Set(suppressOpenAIVideoResponseKey, true)
	}
}

func shouldWriteOpenAIVideoResponse(c *gin.Context) bool {
	if c == nil {
		return false
	}
	value, ok := c.Get(suppressOpenAIVideoResponseKey)
	suppressed, _ := value.(bool)
	return !ok || !suppressed
}

func ParseOpenAIVideoCreateBody(body []byte) (map[string]any, OpenAIVideoRequest, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, OpenAIVideoRequest{}, fmt.Errorf("decode video request: %w", err)
	}

	request := OpenAIVideoRequest{
		Model:              strings.TrimSpace(stringValue(payload["model"])),
		Prompt:             strings.TrimSpace(stringValue(payload["prompt"])),
		Resolution:         strings.ToLower(strings.TrimSpace(stringValue(payload["resolution"]))),
		AspectRatio:        strings.TrimSpace(stringValue(payload["aspect_ratio"])),
		ResolutionExplicit: strings.TrimSpace(stringValue(payload["resolution"])) != "",
		FirstImageURL:      strings.TrimSpace(stringValue(payload["first_image_url"])),
		LastImageURL:       strings.TrimSpace(stringValue(payload["last_image_url"])),
	}
	if request.Model == "" {
		return nil, OpenAIVideoRequest{}, fmt.Errorf("model is required")
	}
	if request.Prompt == "" {
		return nil, OpenAIVideoRequest{}, fmt.Errorf("prompt is required")
	}
	if request.AspectRatio == "" {
		request.AspectRatio = strings.TrimSpace(stringValue(payload["size"]))
	}

	duration, hasDuration := payload["duration"]
	seconds, hasSeconds := payload["seconds"]
	durationSeconds := openAIVideoInt(duration)
	secondsValue := openAIVideoInt(seconds)
	if hasDuration && hasSeconds && durationSeconds != secondsValue {
		return nil, OpenAIVideoRequest{}, fmt.Errorf("duration and seconds must match")
	}
	request.DurationSeconds = durationSeconds
	if !hasDuration || request.DurationSeconds <= 0 {
		request.DurationSeconds = secondsValue
	}
	if request.DurationSeconds <= 0 {
		request.DurationSeconds = VideoBillingDefaultDurationSeconds
	}
	if request.DurationSeconds > 30 {
		return nil, OpenAIVideoRequest{}, fmt.Errorf("duration must not exceed 30 seconds")
	}

	generateAudio, hasGenerateAudio, err := openAIVideoOptionalBool(payload, "generate_audio")
	if err != nil {
		return nil, OpenAIVideoRequest{}, err
	}
	audio, hasAudio, err := openAIVideoOptionalBool(payload, "audio")
	if err != nil {
		return nil, OpenAIVideoRequest{}, err
	}
	if hasGenerateAudio && hasAudio && *generateAudio != *audio {
		return nil, OpenAIVideoRequest{}, fmt.Errorf("generate_audio and audio must match")
	}
	if hasGenerateAudio {
		request.GenerateAudio = generateAudio
	} else if hasAudio {
		request.GenerateAudio = audio
	}

	request.ImageURLs = collectOpenAIVideoImageURLs(payload)
	request.VideoURLs, err = collectOpenAIVideoStringArray(payload, "reference_videos")
	if err != nil {
		return nil, OpenAIVideoRequest{}, err
	}
	request.AudioURLs, err = collectOpenAIVideoStringArray(payload, "reference_audios")
	if err != nil {
		return nil, OpenAIVideoRequest{}, err
	}
	return payload, request, nil
}

func NormalizeOpenAIVideoCreateBody(body []byte, mappedModel string) ([]byte, OpenAIVideoRequest, error) {
	payload, request, err := ParseOpenAIVideoCreateBody(body)
	if err != nil {
		return nil, OpenAIVideoRequest{}, err
	}
	upstreamModel := strings.TrimSpace(mappedModel)
	if upstreamModel == "" {
		upstreamModel = request.Model
	}
	if request.Resolution == "" {
		request.Resolution = VideoBillingResolution720P
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

var openAIVideoUnifiedAcceptedFields = map[string]struct{}{
	"model": {}, "prompt": {}, "duration": {}, "seconds": {},
	"resolution": {}, "aspect_ratio": {}, "size": {},
	"generate_audio": {}, "audio": {},
	"reference_image_urls": {}, "image_urls": {}, "image_url": {},
	"reference_images": {}, "images": {}, "image": {},
	"reference_videos": {}, "reference_audios": {},
	"first_image_url": {}, "last_image_url": {},
}

func BuildUnifiedOpenAIVideoCreateBody(payload map[string]any, request OpenAIVideoRequest, mappedModel string) ([]byte, error) {
	unknown := make([]string, 0)
	for field := range payload {
		if _, ok := openAIVideoUnifiedAcceptedFields[field]; !ok {
			unknown = append(unknown, field)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("unsupported video field %q", unknown[0])
	}

	upstreamModel := strings.TrimSpace(mappedModel)
	if upstreamModel == "" {
		upstreamModel = request.Model
	}
	body := map[string]any{
		"model":    upstreamModel,
		"prompt":   request.Prompt,
		"duration": request.DurationSeconds,
	}
	resolution := request.Resolution
	if resolution == "" {
		resolution = VideoBillingResolution720P
	}
	body["resolution"] = resolution
	if request.AspectRatio != "" {
		body["aspect_ratio"] = request.AspectRatio
	}
	if len(request.ImageURLs) > 0 {
		body["reference_image_urls"] = request.ImageURLs
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode video request: %w", err)
	}
	return encoded, nil
}

func ValidateOpenAIVideoCreateBodyForAccount(account *Account, body []byte) error {
	if ResolveOpenAIVideoRequestProfile(account) != OpenAIVideoRequestProfileUnifiedJSON {
		return nil
	}
	payload, request, err := ParseOpenAIVideoCreateBody(body)
	if err != nil {
		return err
	}
	_, err = BuildUnifiedOpenAIVideoCreateBody(payload, request, request.Model)
	return err
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
	appendValue := func(value any) {
		switch typed := value.(type) {
		case string:
			appendURL(typed)
		case map[string]any:
			appendURL(stringValue(typed["url"]))
		}
	}
	appendArray := func(value any) {
		items, _ := value.([]any)
		for _, item := range items {
			appendValue(item)
		}
	}

	appendArray(payload["images"])
	appendArray(payload["image_urls"])
	appendArray(payload["reference_image_urls"])
	appendValue(payload["image_url"])
	appendValue(payload["image"])
	appendArray(payload["reference_images"])
	return urls
}

func collectOpenAIVideoStringArray(payload map[string]any, field string) ([]string, error) {
	value, exists := payload[field]
	if !exists || value == nil {
		return nil, nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an array of strings", field)
	}
	urls := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		raw, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("%s must be an array of strings", field)
		}
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if _, exists := seen[raw]; exists {
			continue
		}
		seen[raw] = struct{}{}
		urls = append(urls, raw)
	}
	return urls, nil
}

func openAIVideoOptionalBool(payload map[string]any, field string) (*bool, bool, error) {
	value, exists := payload[field]
	if !exists {
		return nil, false, nil
	}
	typed, ok := value.(bool)
	if !ok {
		return nil, true, fmt.Errorf("%s must be a boolean", field)
	}
	return &typed, true, nil
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
			"data.metadata.url", "data.video_url", "data.result_url", "data.url", "data.0.url", "data.video_urls.0", "data.videos.0.url"),
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
	case "failed", "error", "cancelled", "canceled", "expired":
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
