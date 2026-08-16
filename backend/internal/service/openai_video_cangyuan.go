package service

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type cangyuanVideoCapabilities struct {
	MinDuration        int
	MaxDuration        int
	AllowedDurations   map[int]struct{}
	FixedResolution    string
	OmitResolution     bool
	AllowedResolutions map[string]struct{}
	MaxImages          int
	MaxVideos          int
	MaxAudios          int
	AudioField         string
	SupportsFrames     bool
}

type OpenAIVideoPreparedRequest struct {
	Body    []byte
	Request OpenAIVideoRequest
}

func resolveCangyuanVideoCapabilities(model string) (cangyuanVideoCapabilities, bool) {
	model = strings.ToLower(strings.TrimSpace(model))
	variable720 := map[string]struct{}{
		VideoBillingResolution480P: {},
		VideoBillingResolution720P: {},
	}
	variable1080 := map[string]struct{}{
		VideoBillingResolution480P:  {},
		VideoBillingResolution720P:  {},
		VideoBillingResolution1080P: {},
	}

	switch {
	case strings.Contains(model, "seedance-2.5-480p"):
		return cangyuanVideoCapabilities{
			MinDuration: 4, MaxDuration: 30, FixedResolution: VideoBillingResolution480P, OmitResolution: true,
			MaxImages: 30, MaxVideos: 10, MaxAudios: 10, AudioField: "generate_audio", SupportsFrames: true,
		}, true
	case strings.Contains(model, "seedance-2.5-720p"):
		return cangyuanVideoCapabilities{
			MinDuration: 4, MaxDuration: 29, FixedResolution: VideoBillingResolution720P, OmitResolution: true,
			MaxImages: 30, MaxVideos: 10, MaxAudios: 10, AudioField: "generate_audio", SupportsFrames: true,
		}, true
	case strings.Contains(model, "seedance-2.0-mini") || strings.Contains(model, "seedance-2-0-mini"):
		return cangyuanVideoCapabilities{
			MinDuration: 4, MaxDuration: 15, AllowedResolutions: variable720,
			MaxImages: 4, MaxVideos: 3, MaxAudios: 1, AudioField: "audio", SupportsFrames: true,
		}, true
	case strings.Contains(model, "seedance-2.0-fast") || strings.Contains(model, "seedance-2-0-fast"):
		return cangyuanVideoCapabilities{
			MinDuration: 4, MaxDuration: 15, AllowedResolutions: variable720,
			MaxImages: 4, MaxVideos: 3, MaxAudios: 1, AudioField: "generate_audio", SupportsFrames: true,
		}, true
	case strings.Contains(model, "seedance-2.0-1080p") || strings.Contains(model, "seedance-2-0-1080p"):
		return cangyuanVideoCapabilities{
			MinDuration: 4, MaxDuration: 15, FixedResolution: VideoBillingResolution1080P, OmitResolution: true,
			MaxImages: 5, MaxVideos: 3, MaxAudios: 3, AudioField: "generate_audio",
		}, true
	case strings.Contains(model, "sd7-seedance-2.0-720p") || model == "seedance-2.0":
		return cangyuanVideoCapabilities{
			MinDuration: 4, MaxDuration: 15, FixedResolution: VideoBillingResolution720P, OmitResolution: true,
			MaxImages: 5, MaxVideos: 3, MaxAudios: 3, AudioField: "generate_audio",
		}, true
	case strings.Contains(model, "sd8-seedance-2.0"):
		return cangyuanVideoCapabilities{
			MinDuration: 5, MaxDuration: 15, AllowedDurations: durationSet(5, 10, 15), OmitResolution: true,
			MaxImages: 9, MaxVideos: 3, MaxAudios: 3,
		}, true
	case strings.Contains(model, "sd4-seedance-2.0"):
		return cangyuanVideoCapabilities{
			MinDuration: 4, MaxDuration: 15, AllowedResolutions: variable720,
			MaxImages: 4, MaxVideos: 3, MaxAudios: 1, AudioField: "generate_audio", SupportsFrames: true,
		}, true
	case strings.Contains(model, "dreamina-seedance-2-0"):
		return cangyuanVideoCapabilities{
			MinDuration: 4, MaxDuration: 15, AllowedResolutions: variable1080,
			MaxImages: 4, MaxVideos: 3, MaxAudios: 1, AudioField: "generate_audio", SupportsFrames: true,
		}, true
	default:
		return cangyuanVideoCapabilities{}, false
	}
}

func durationSet(values ...int) map[int]struct{} {
	result := make(map[int]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func PrepareUnifiedOpenAIVideoCreateBody(payload map[string]any, request OpenAIVideoRequest, mappedModel string) (OpenAIVideoPreparedRequest, error) {
	unknown := make([]string, 0)
	for field := range payload {
		if _, ok := openAIVideoUnifiedAcceptedFields[field]; !ok {
			unknown = append(unknown, field)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return OpenAIVideoPreparedRequest{}, fmt.Errorf("unsupported video field %q", unknown[0])
	}

	upstreamModel := strings.TrimSpace(mappedModel)
	if upstreamModel == "" {
		upstreamModel = request.Model
	}
	capabilities, knownModel := resolveCangyuanVideoCapabilities(upstreamModel)
	if !knownModel {
		capabilities = cangyuanVideoCapabilities{
			MinDuration: 1, MaxDuration: 30,
			MaxImages: 30, MaxVideos: 10, MaxAudios: 10,
			AudioField: "generate_audio", SupportsFrames: true,
		}
	}
	if _, hasDuration := payload["duration"]; !hasDuration {
		if _, hasSeconds := payload["seconds"]; !hasSeconds && len(capabilities.AllowedDurations) > 0 {
			request.DurationSeconds = capabilities.MinDuration
		}
	}
	if err := validateCangyuanVideoRequest(request, upstreamModel, capabilities, knownModel); err != nil {
		return OpenAIVideoPreparedRequest{}, err
	}

	if capabilities.FixedResolution != "" {
		request.Resolution = capabilities.FixedResolution
	} else if request.Resolution == "" {
		request.Resolution = VideoBillingResolution720P
	}
	body := map[string]any{
		"model":    upstreamModel,
		"prompt":   request.Prompt,
		"duration": request.DurationSeconds,
	}
	if !capabilities.OmitResolution {
		body["resolution"] = request.Resolution
	}
	if request.AspectRatio != "" {
		body["aspect_ratio"] = request.AspectRatio
	}
	if request.GenerateAudio != nil && capabilities.AudioField != "" {
		body[capabilities.AudioField] = *request.GenerateAudio
	}
	if len(request.ImageURLs) > 0 {
		body["reference_image_urls"] = request.ImageURLs
	}
	if len(request.VideoURLs) > 0 {
		body["reference_videos"] = request.VideoURLs
	}
	if len(request.AudioURLs) > 0 {
		body["reference_audios"] = request.AudioURLs
	}
	if request.FirstImageURL != "" {
		body["first_image_url"] = request.FirstImageURL
		body["last_image_url"] = request.LastImageURL
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return OpenAIVideoPreparedRequest{}, fmt.Errorf("encode video request: %w", err)
	}
	return OpenAIVideoPreparedRequest{Body: encoded, Request: request}, nil
}

func validateCangyuanVideoRequest(request OpenAIVideoRequest, model string, capabilities cangyuanVideoCapabilities, knownModel bool) error {
	if len(capabilities.AllowedDurations) > 0 {
		if _, ok := capabilities.AllowedDurations[request.DurationSeconds]; !ok {
			values := make([]int, 0, len(capabilities.AllowedDurations))
			for value := range capabilities.AllowedDurations {
				values = append(values, value)
			}
			sort.Ints(values)
			return fmt.Errorf("duration must be one of %s seconds", joinVideoDurationValues(values))
		}
	} else if request.DurationSeconds < capabilities.MinDuration || request.DurationSeconds > capabilities.MaxDuration {
		return fmt.Errorf("duration must be between %d and %d seconds", capabilities.MinDuration, capabilities.MaxDuration)
	}

	if capabilities.FixedResolution != "" && request.ResolutionExplicit && request.Resolution != capabilities.FixedResolution {
		return fmt.Errorf("resolution %s is not supported by %s", request.Resolution, model)
	}
	if !capabilities.OmitResolution && len(capabilities.AllowedResolutions) > 0 {
		resolution := request.Resolution
		if resolution == "" {
			resolution = VideoBillingResolution720P
		}
		if _, ok := capabilities.AllowedResolutions[resolution]; !ok {
			return fmt.Errorf("resolution %s is not supported by %s", resolution, model)
		}
	}
	if len(request.ImageURLs) > capabilities.MaxImages {
		return fmt.Errorf("reference_image_urls must not contain more than %d items", capabilities.MaxImages)
	}
	if len(request.VideoURLs) > capabilities.MaxVideos {
		return fmt.Errorf("reference_videos must not contain more than %d items", capabilities.MaxVideos)
	}
	if len(request.AudioURLs) > capabilities.MaxAudios {
		return fmt.Errorf("reference_audios must not contain more than %d items", capabilities.MaxAudios)
	}
	if (request.FirstImageURL == "") != (request.LastImageURL == "") {
		return fmt.Errorf("first_image_url and last_image_url must be provided together")
	}
	if request.FirstImageURL != "" {
		if knownModel && !capabilities.SupportsFrames {
			return fmt.Errorf("frame inputs are not supported by %s", model)
		}
		if len(request.ImageURLs)+len(request.VideoURLs)+len(request.AudioURLs) > 0 {
			return fmt.Errorf("frame inputs cannot be combined with reference media")
		}
	}
	if knownModel && request.GenerateAudio != nil && capabilities.AudioField == "" {
		return fmt.Errorf("generate_audio is not supported by %s", model)
	}
	return nil
}

func joinVideoDurationValues(values []int) string {
	parts := make([]string, len(values))
	for index, value := range values {
		parts[index] = fmt.Sprintf("%d", value)
	}
	return strings.Join(parts, ", ")
}
