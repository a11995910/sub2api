package service

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
)

var seedanceVideoURLPattern = regexp.MustCompile(`https://[^\s\]\)"']+`)

type SeedanceVideoRequest struct {
	Model                  string
	Prompt                 string
	Resolution             string
	Duration               int
	ReferenceImageDataURLs []string
}

type SeedanceVideoResult struct {
	VideoURL     string
	RequestID    string
	Status       string
	OutputTokens int
}

type SeedanceVideoContext struct {
	Model               string
	Resolution          string
	Duration            int
	ReferenceImageCount int
}

const seedanceVideoContextKey = "seedance_video_context"

func SetSeedanceVideoContext(c *gin.Context, meta SeedanceVideoContext) {
	if c != nil {
		c.Set(seedanceVideoContextKey, meta)
	}
}

func seedanceVideoContextFromGin(c *gin.Context) (SeedanceVideoContext, bool) {
	if c == nil {
		return SeedanceVideoContext{}, false
	}
	value, ok := c.Get(seedanceVideoContextKey)
	if !ok {
		return SeedanceVideoContext{}, false
	}
	meta, ok := value.(SeedanceVideoContext)
	return meta, ok && IsSeedanceVideoModel(meta.Model)
}

func IsSeedanceVideoModel(model string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "dreamina-seedance-")
}

func BuildSeedanceChatRequest(req SeedanceVideoRequest) ([]byte, error) {
	req.Model = strings.TrimSpace(req.Model)
	req.Prompt = strings.TrimSpace(req.Prompt)
	req.Resolution = strings.ToLower(strings.TrimSpace(req.Resolution))
	if !IsSeedanceVideoModel(req.Model) {
		return nil, fmt.Errorf("unsupported Seedance model: %s", req.Model)
	}
	if req.Prompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}
	if req.Duration < 4 || req.Duration > 15 {
		return nil, fmt.Errorf("duration must be between 4 and 15 seconds")
	}
	if !seedanceResolutionSupported(req.Model, req.Resolution) {
		return nil, fmt.Errorf("resolution %s is not supported by %s", req.Resolution, req.Model)
	}

	instruction := fmt.Sprintf("%s\n\n视频规格：%s，时长 %d 秒。", req.Prompt, req.Resolution, req.Duration)
	var content any = instruction
	if len(req.ReferenceImageDataURLs) > 0 {
		parts := []map[string]any{{"type": "text", "text": instruction}}
		for _, rawURL := range req.ReferenceImageDataURLs {
			rawURL = strings.TrimSpace(rawURL)
			if rawURL == "" {
				continue
			}
			parts = append(parts, map[string]any{
				"type":      "image_url",
				"image_url": map[string]string{"url": rawURL},
			})
		}
		content = parts
	}

	return json.Marshal(map[string]any{
		"model": req.Model,
		"messages": []map[string]any{{
			"role":    "user",
			"content": content,
		}},
		"stream": false,
	})
}

func seedanceResolutionSupported(model, resolution string) bool {
	if resolution != VideoBillingResolution480P && resolution != VideoBillingResolution720P && resolution != VideoBillingResolution1080P {
		return false
	}
	model = strings.ToLower(strings.TrimSpace(model))
	if strings.Contains(model, "seedance-2-0-fast") || strings.Contains(model, "seedance-2-0-mini") {
		return resolution != VideoBillingResolution1080P
	}
	return strings.Contains(model, "seedance-2-0")
}

func ParseSeedanceChatResponse(body []byte) (SeedanceVideoResult, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return SeedanceVideoResult{}, fmt.Errorf("decode Seedance response: %w", err)
	}
	if errValue, ok := payload["error"].(map[string]any); ok {
		message := strings.TrimSpace(seedanceStringValue(errValue["message"]))
		if message == "" {
			message = "Seedance upstream request failed"
		}
		return SeedanceVideoResult{}, fmt.Errorf("%s", message)
	}

	result := SeedanceVideoResult{
		RequestID: strings.TrimSpace(seedanceStringValue(payload["id"])),
		Status:    strings.TrimSpace(seedanceStringValue(payload["status"])),
	}
	if usage, ok := payload["usage"].(map[string]any); ok {
		result.OutputTokens = intValue(usage["completion_tokens"])
	}
	mergeSeedanceResult(&result, payload)

	if choices, ok := payload["choices"].([]any); ok {
		for _, choiceValue := range choices {
			choice, _ := choiceValue.(map[string]any)
			message, _ := choice["message"].(map[string]any)
			mergeSeedanceResult(&result, message["content"])
		}
	}
	if result.VideoURL == "" && result.RequestID == "" {
		return SeedanceVideoResult{}, fmt.Errorf("Seedance response did not include video URL or request ID")
	}
	return result, nil
}

func mergeSeedanceResult(result *SeedanceVideoResult, value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			switch strings.ToLower(key) {
			case "video_url", "output_url", "download_url":
				if rawURL := validSeedanceVideoURL(seedanceStringValue(item)); rawURL != "" {
					result.VideoURL = rawURL
				}
			case "request_id", "task_id":
				if id := strings.TrimSpace(seedanceStringValue(item)); id != "" {
					result.RequestID = id
				}
			case "status", "state":
				if status := strings.TrimSpace(seedanceStringValue(item)); status != "" {
					result.Status = status
				}
			case "url":
				if rawURL := validSeedanceVideoURL(seedanceStringValue(item)); rawURL != "" {
					result.VideoURL = rawURL
				}
			}
			mergeSeedanceResult(result, item)
		}
	case []any:
		for _, item := range typed {
			mergeSeedanceResult(result, item)
		}
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return
		}
		var nested any
		if json.Unmarshal([]byte(text), &nested) == nil {
			mergeSeedanceResult(result, nested)
		}
		for _, match := range seedanceVideoURLPattern.FindAllString(text, -1) {
			if rawURL := validSeedanceVideoURL(match); rawURL != "" {
				result.VideoURL = rawURL
				break
			}
		}
	}
}

func validSeedanceVideoURL(value string) string {
	value = strings.TrimRight(strings.TrimSpace(value), ".,;，。")
	lower := strings.ToLower(value)
	if !strings.HasPrefix(lower, "https://") {
		return ""
	}
	path := strings.SplitN(lower, "?", 2)[0]
	if strings.HasSuffix(path, ".mp4") || strings.HasSuffix(path, ".webm") || strings.HasSuffix(path, ".mov") {
		return value
	}
	return ""
}

func seedanceStringValue(value any) string {
	text, _ := value.(string)
	return text
}

func intValue(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	default:
		return 0
	}
}
