package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	xdraw "golang.org/x/image/draw"
)

const (
	image4KEnhancementLogComponent = "service.image_4k_enhancement"
	image4KEnhancementMaxAttempts  = 3
	image4KEnhancementMaxResizePx  = int64(32 * 1024 * 1024)
)

func (s *OpenAIGatewayService) image4KEnhancementSkipReason(c *gin.Context, parsed *OpenAIImagesRequest) string {
	if s == nil {
		return "service_missing"
	}
	apiKey := getAPIKeyFromContext(c)
	if apiKey == nil {
		return "api_key_missing"
	}
	if apiKey.Group == nil {
		return "group_missing"
	}
	if !apiKey.Group.AllowImageGeneration {
		return "group_image_generation_disabled"
	}
	if !apiKey.Group.Image4KEnhancementEnabled {
		return "group_4k_enhancement_disabled"
	}
	if apiKey.Group.Image4KEnhancementGroupID == nil || *apiKey.Group.Image4KEnhancementGroupID <= 0 {
		return "target_group_missing"
	}
	if *apiKey.Group.Image4KEnhancementGroupID == apiKey.Group.ID {
		return "target_group_self"
	}
	if parsed == nil {
		return "request_missing"
	}
	if parsed.Stream {
		return "stream_not_supported"
	}
	if NormalizeImageBillingTierOrDefault(parsed.SizeTier) != ImageBillingSize4K {
		return "request_not_4k"
	}
	return ""
}

func (s *OpenAIGatewayService) shouldApplyImage4KEnhancement(c *gin.Context, parsed *OpenAIImagesRequest) bool {
	return s.image4KEnhancementSkipReason(c, parsed) == ""
}

func (s *OpenAIGatewayService) shouldBlockLegacyImageSuperResolutionFor4KEnhancement(c *gin.Context, parsed *OpenAIImagesRequest, requestSizeTier string) bool {
	if s == nil {
		return false
	}
	apiKey := getAPIKeyFromContext(c)
	if apiKey == nil || apiKey.Group == nil {
		return false
	}
	if !apiKey.Group.AllowImageGeneration || !apiKey.Group.Image4KEnhancementEnabled {
		return false
	}
	sizeTier := firstNonEmptyString(openAIImagesRequestSizeTier(parsed), requestSizeTier)
	if NormalizeImageBillingTierOrDefault(sizeTier) != ImageBillingSize4K {
		return false
	}
	if reason := s.image4KEnhancementSkipReason(c, parsed); reason != "" {
		logImage4KEnhancementDecision(c, "skip", reason, parsed)
	}
	return true
}

func logImage4KEnhancementDecision(c *gin.Context, phase, reason string, parsed *OpenAIImagesRequest) {
	apiKey := getAPIKeyFromContext(c)
	apiKeyID := int64(0)
	groupID := int64(0)
	targetGroupID := int64(0)
	enabled := false
	if apiKey != nil {
		apiKeyID = apiKey.ID
		if apiKey.Group != nil {
			groupID = apiKey.Group.ID
			enabled = apiKey.Group.Image4KEnhancementEnabled
			if apiKey.Group.Image4KEnhancementGroupID != nil {
				targetGroupID = *apiKey.Group.Image4KEnhancementGroupID
			}
		}
	}
	if strings.TrimSpace(reason) == "" {
		reason = "apply"
	}
	requestSizeTier := ""
	requestSize := ""
	if parsed != nil {
		requestSizeTier = parsed.SizeTier
		requestSize = parsed.Size
	}
	logger.LegacyPrintf(
		image4KEnhancementLogComponent,
		"image 4k enhancement %s: reason=%s api_key_id=%d group_id=%d target_group_id=%d enabled=%t request_size_tier=%s request_size=%s",
		phase,
		reason,
		apiKeyID,
		groupID,
		targetGroupID,
		enabled,
		NormalizeImageBillingTierOrDefault(requestSizeTier),
		strings.TrimSpace(requestSize),
	)
}

func (s *OpenAIGatewayService) applyOpenAIImages4KEnhancementToJSON(
	ctx context.Context,
	c *gin.Context,
	body []byte,
	opts openAIImagesNonStreamingResponseOptions,
) []byte {
	if reason := s.image4KEnhancementSkipReason(c, opts.parsed); reason != "" {
		logImage4KEnhancementDecision(c, "skip", reason, opts.parsed)
		return body
	}
	if len(body) == 0 || !gjson.ValidBytes(body) {
		logImage4KEnhancementDecision(c, "skip", "invalid_json", opts.parsed)
		return body
	}
	data := gjson.GetBytes(body, "data")
	if !data.IsArray() {
		logImage4KEnhancementDecision(c, "skip", "data_not_array", opts.parsed)
		return body
	}

	results := make([]openAIResponsesImageResult, 0, len(data.Array()))
	for _, item := range data.Array() {
		result := openAIResponsesImageResult{
			Result:        normalizeOpenAIImageBase64(item.Get("b64_json").String()),
			URL:           firstNonEmptyString(item.Get("url").String(), item.Get("image_url").String(), item.Get("download_url").String()),
			RevisedPrompt: strings.TrimSpace(item.Get("revised_prompt").String()),
		}
		if strings.TrimSpace(result.Result) == "" && strings.TrimSpace(result.URL) == "" {
			continue
		}
		results = append(results, result)
	}
	if len(results) == 0 {
		logImage4KEnhancementDecision(c, "skip", "image_missing", opts.parsed)
		return body
	}
	results = s.applyOpenAIResponses4KEnhancement(ctx, c, results, opts.parsed)
	firstMeta := openAIResponsesImageResult{
		Model:        firstNonEmptyString(gjson.GetBytes(body, "model").String(), openAIImagesRequestModel(opts.parsed)),
		Size:         firstNonEmptyString(gjson.GetBytes(body, "size").String(), openAIImagesRequestSize(opts.parsed)),
		OutputFormat: gjson.GetBytes(body, "output_format").String(),
		Quality:      gjson.GetBytes(body, "quality").String(),
		Background:   gjson.GetBytes(body, "background").String(),
	}
	if len(results) > 0 {
		mergeOpenAIResponsesImageMeta(&firstMeta, results[0])
	}
	responseBody, err := buildOpenAIImagesAPIResponse(results, gjson.GetBytes(body, "created").Int(), nil, firstMeta, openAIImagesResponseFormat(opts.parsed))
	if err != nil {
		logger.LegacyPrintf(image4KEnhancementLogComponent, "rewrite images 4k enhancement response failed: err=%v", err)
		return body
	}
	if usage := gjson.GetBytes(body, "usage"); usage.Exists() && usage.IsObject() {
		responseBody, _ = sjson.SetRawBytes(responseBody, "usage", []byte(usage.Raw))
	}
	return responseBody
}

func (s *OpenAIGatewayService) applyOpenAIResponses4KEnhancement(
	ctx context.Context,
	c *gin.Context,
	results []openAIResponsesImageResult,
	parsed *OpenAIImagesRequest,
) []openAIResponsesImageResult {
	if reason := s.image4KEnhancementSkipReason(c, parsed); reason != "" {
		logImage4KEnhancementDecision(c, "skip", reason, parsed)
		return results
	}
	if len(results) == 0 {
		logImage4KEnhancementDecision(c, "skip", "empty_results", parsed)
		return results
	}
	logImage4KEnhancementDecision(c, "apply", "", parsed)

	out := make([]openAIResponsesImageResult, len(results))
	copy(out, results)
	for index := range out {
		imageBytes, mimeType, err := openAIResponsesImageResultBytes(out[index])
		if err != nil {
			logger.LegacyPrintf(image4KEnhancementLogComponent, "skip image 4k enhancement: index=%d err=%v", index, err)
			continue
		}
		enhanced, err := s.enhanceOpenAIImageViaTargetGroup(ctx, c, imageBytes, mimeType, parsed, index)
		if err != nil {
			logger.LegacyPrintf(image4KEnhancementLogComponent, "image 4k enhancement failed: index=%d err=%v", index, err)
			continue
		}
		out[index] = enhanced
	}
	return out
}

func openAIResponsesImageResultBytes(result openAIResponsesImageResult) ([]byte, string, error) {
	if b64 := normalizeOpenAIImageBase64(result.Result); b64 != "" {
		data, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return nil, "", err
		}
		return data, firstNonEmptyString(result.MimeType, "image/png"), nil
	}
	if b64, mimeType := openAIImageBase64FromDataURL(result.URL); b64 != "" {
		data, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return nil, "", err
		}
		return data, firstNonEmptyString(mimeType, result.MimeType, "image/png"), nil
	}
	return nil, "", fmt.Errorf("image result has no inline image bytes")
}

func (s *OpenAIGatewayService) enhanceOpenAIImageViaTargetGroup(
	ctx context.Context,
	c *gin.Context,
	imageBytes []byte,
	mimeType string,
	sourceParsed *OpenAIImagesRequest,
	index int,
) (openAIResponsesImageResult, error) {
	apiKey := getAPIKeyFromContext(c)
	if apiKey == nil || apiKey.Group == nil || apiKey.Group.Image4KEnhancementGroupID == nil {
		return openAIResponsesImageResult{}, fmt.Errorf("target group is not configured")
	}
	targetGroupID := *apiKey.Group.Image4KEnhancementGroupID
	if targetGroupID <= 0 {
		return openAIResponsesImageResult{}, fmt.Errorf("target group is not configured")
	}
	targetChannel, err := s.GetChannelForGroup(ctx, &targetGroupID)
	if err != nil {
		return openAIResponsesImageResult{}, fmt.Errorf("lookup target channel: %w", err)
	}
	if targetChannel == nil {
		return openAIResponsesImageResult{}, fmt.Errorf("target group has no active channel")
	}
	requestModel := openAIImagesRequestModel(sourceParsed)
	mapping := s.ResolveChannelMapping(ctx, targetGroupID, requestModel)
	targetRequestModel := strings.TrimSpace(requestModel)
	if strings.TrimSpace(mapping.MappedModel) != "" && strings.TrimSpace(mapping.MappedModel) != strings.TrimSpace(requestModel) {
		targetRequestModel = strings.TrimSpace(mapping.MappedModel)
	} else if fallbackModel := s.resolveImage4KEnhancementTargetRequestModel(ctx, targetGroupID, requestModel); fallbackModel != "" {
		targetRequestModel = fallbackModel
		mapping.MappedModel = fallbackModel
	}
	var lastErr error
	for attempt := 1; attempt <= image4KEnhancementMaxAttempts; attempt++ {
		selection, _, err := s.SelectAccountWithSchedulerForImages(
			WithOpenAIImageGenerationIntent(ctx),
			&targetGroupID,
			"",
			targetRequestModel,
			nil,
			OpenAIImagesCapabilityNative,
		)
		if err != nil {
			lastErr = err
			logger.LegacyPrintf(image4KEnhancementLogComponent, "select target account failed: group_id=%d attempt=%d err=%v", targetGroupID, attempt, err)
			break
		}
		if selection == nil || selection.Account == nil {
			lastErr = fmt.Errorf("target group has no available account")
			break
		}
		result, err := s.callOpenAIImages4KEnhancementAttempt(ctx, c, selection.Account, targetChannel, mapping.MappedModel, imageBytes, mimeType, sourceParsed, index)
		if selection.ReleaseFunc != nil {
			selection.ReleaseFunc()
		}
		if err == nil {
			return result, nil
		}
		lastErr = err
		logger.LegacyPrintf(
			image4KEnhancementLogComponent,
			"target account enhancement attempt failed: group_id=%d account_id=%d attempt=%d err=%v",
			targetGroupID,
			selection.Account.ID,
			attempt,
			err,
		)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("image 4k enhancement failed")
	}
	return openAIResponsesImageResult{}, lastErr
}

func (s *OpenAIGatewayService) resolveImage4KEnhancementTargetRequestModel(ctx context.Context, targetGroupID int64, sourceModel string) string {
	if s == nil || s.accountRepo == nil || targetGroupID <= 0 {
		return ""
	}
	accounts, err := s.accountRepo.ListSchedulableByGroupIDAndPlatform(ctx, targetGroupID, PlatformOpenAI)
	if err != nil {
		logger.LegacyPrintf(image4KEnhancementLogComponent, "list target accounts for model resolution failed: group_id=%d err=%v", targetGroupID, err)
		return ""
	}
	sourceModel = strings.TrimSpace(sourceModel)
	candidates := make([]string, 0, len(accounts))
	seen := map[string]struct{}{}
	for _, account := range accounts {
		if !account.IsSchedulable() || normalizeOpenAICompatiblePlatform(account.Platform) != PlatformOpenAI {
			continue
		}
		for model := range account.GetModelMapping() {
			model = strings.TrimSpace(model)
			if model == "" || strings.Contains(model, "*") || model == sourceModel {
				continue
			}
			if _, exists := seen[model]; exists {
				continue
			}
			seen[model] = struct{}{}
			candidates = append(candidates, model)
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	sort.Strings(candidates)
	for _, model := range candidates {
		if strings.Contains(strings.ToLower(model), "banana") {
			return model
		}
	}
	return candidates[0]
}

func (s *OpenAIGatewayService) callOpenAIImages4KEnhancementAttempt(
	ctx context.Context,
	parent *gin.Context,
	account *Account,
	channel *Channel,
	mappedModel string,
	imageBytes []byte,
	mimeType string,
	sourceParsed *OpenAIImagesRequest,
	index int,
) (openAIResponsesImageResult, error) {
	enhanceParsed := buildOpenAIImages4KEnhancementParsed(sourceParsed, imageBytes, mimeType, index)
	enhanceBody, err := buildOpenAIImages4KEnhancementJSONBody(enhanceParsed)
	if err != nil {
		return openAIResponsesImageResult{}, err
	}
	rec := httptest.NewRecorder()
	inner, _ := gin.CreateTestContext(rec)
	inner.Request = httptest.NewRequest(http.MethodPost, openAIImagesEditsEndpoint, bytes.NewReader(enhanceBody))
	inner.Request.Header.Set("Content-Type", "application/json")
	if parent != nil && parent.Request != nil {
		inner.Request = inner.Request.WithContext(parent.Request.Context())
		inner.Request.Header.Set("User-Agent", parent.Request.Header.Get("User-Agent"))
	}
	result, err := s.ForwardImages(ctx, inner, account, enhanceBody, enhanceParsed, mappedModel, channel)
	if err != nil {
		return openAIResponsesImageResult{}, err
	}
	if result == nil || result.ImageCount <= 0 {
		return openAIResponsesImageResult{}, fmt.Errorf("target group returned no image")
	}
	if rec.Code < http.StatusOK || rec.Code >= http.StatusMultipleChoices {
		return openAIResponsesImageResult{}, fmt.Errorf("target group returned status %d", rec.Code)
	}
	enhanced, err := firstOpenAIImageResultFromAPIResponse(rec.Body.Bytes())
	if err != nil {
		return openAIResponsesImageResult{}, err
	}
	enhanced.Model = openAIImagesRequestModel(sourceParsed)
	enhanced.Size = openAIImagesRequestSize(sourceParsed)
	if resized, err := resizeOpenAIImage4KEnhancementResultToRequestedSize(enhanced, sourceParsed); err != nil {
		logger.LegacyPrintf(image4KEnhancementLogComponent, "image 4k enhancement pixel resize skipped: err=%v", err)
	} else {
		enhanced = resized
	}
	if strings.TrimSpace(enhanced.OutputFormat) == "" {
		enhanced.OutputFormat = "png"
	}
	return enhanced, nil
}

func buildOpenAIImages4KEnhancementParsed(source *OpenAIImagesRequest, imageBytes []byte, mimeType string, index int) *OpenAIImagesRequest {
	if strings.TrimSpace(mimeType) == "" {
		mimeType = "image/png"
	}
	size := openAIImagesRequestSize(source)
	sizeTier := openAIImagesRequestSizeTier(source)
	model := openAIImagesRequestModel(source)
	return &OpenAIImagesRequest{
		Endpoint:           openAIImagesEditsEndpoint,
		ContentType:        "application/json",
		Model:              model,
		ExplicitModel:      true,
		Prompt:             buildOpenAIImages4KEnhancementPrompt(size),
		N:                  1,
		Size:               size,
		ExplicitSize:       strings.TrimSpace(size) != "",
		SizeTier:           sizeTier,
		ResponseFormat:     "b64_json",
		OutputFormat:       "png",
		InputFidelity:      firstNonEmptyString(sourceInputFidelity(source), "high"),
		RequiredCapability: OpenAIImagesCapabilityNative,
		Uploads: []OpenAIImagesUpload{{
			FieldName:   "image",
			FileName:    fmt.Sprintf("source-%d.png", index+1),
			ContentType: mimeType,
			Data:        imageBytes,
		}},
	}
}

func buildOpenAIImages4KEnhancementPrompt(size string) string {
	size = strings.TrimSpace(size)
	if size == "" {
		size = "the exact requested output size"
	}
	return "Upscale and enhance the attached image to the requested output size " + size + ". Preserve the original content, subject identity, composition, camera angle, colors, lighting, aspect ratio, and all visible text exactly. Do not change the image content. Do not add, remove, crop, redraw, reinterpret, or change any semantic content. Only improve resolution, sharpness, fine detail, and clean compression artifacts."
}

func buildOpenAIImages4KEnhancementJSONBody(parsed *OpenAIImagesRequest) ([]byte, error) {
	if parsed == nil || len(parsed.Uploads) == 0 {
		return nil, fmt.Errorf("enhancement image input is required")
	}
	imageURL, err := openAIImageUploadToDataURL(parsed.Uploads[0])
	if err != nil {
		return nil, err
	}
	body := []byte(`{"model":"","prompt":"","size":"","response_format":"b64_json","images":[{"image_url":""}]}`)
	body, _ = sjson.SetBytes(body, "model", parsed.Model)
	body, _ = sjson.SetBytes(body, "prompt", parsed.Prompt)
	body, _ = sjson.SetBytes(body, "size", parsed.Size)
	body, _ = sjson.SetBytes(body, "images.0.image_url", imageURL)
	if strings.TrimSpace(parsed.OutputFormat) != "" {
		body, _ = sjson.SetBytes(body, "output_format", parsed.OutputFormat)
	}
	if strings.TrimSpace(parsed.InputFidelity) != "" {
		body, _ = sjson.SetBytes(body, "input_fidelity", parsed.InputFidelity)
	}
	return body, nil
}

func firstOpenAIImageResultFromAPIResponse(body []byte) (openAIResponsesImageResult, error) {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return openAIResponsesImageResult{}, fmt.Errorf("target group returned invalid image response")
	}
	item := gjson.GetBytes(body, "data.0")
	if !item.Exists() {
		return openAIResponsesImageResult{}, fmt.Errorf("target group returned no image")
	}
	result := openAIResponsesImageResult{
		Result:        normalizeOpenAIImageBase64(item.Get("b64_json").String()),
		URL:           firstNonEmptyString(item.Get("url").String(), item.Get("image_url").String(), item.Get("download_url").String()),
		RevisedPrompt: strings.TrimSpace(item.Get("revised_prompt").String()),
		OutputFormat:  strings.TrimSpace(gjson.GetBytes(body, "output_format").String()),
		Size:          strings.TrimSpace(gjson.GetBytes(body, "size").String()),
		Model:         strings.TrimSpace(gjson.GetBytes(body, "model").String()),
	}
	if strings.TrimSpace(result.Result) == "" && strings.TrimSpace(result.URL) == "" {
		return openAIResponsesImageResult{}, fmt.Errorf("target group returned no image")
	}
	return result, nil
}

func resizeOpenAIImage4KEnhancementResultToRequestedSize(result openAIResponsesImageResult, sourceParsed *OpenAIImagesRequest) (openAIResponsesImageResult, error) {
	width, height, ok := parseImageBillingDimensions(openAIImagesRequestSize(sourceParsed))
	if !ok {
		return result, nil
	}
	b64 := normalizeOpenAIImageBase64(result.Result)
	if b64 == "" {
		return result, nil
	}
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return result, err
	}
	resized, format, changed, err := resizeOpenAIImageBytesToDimensions(data, width, height)
	if err != nil || !changed {
		return result, err
	}
	result.Result = base64.StdEncoding.EncodeToString(resized)
	result.URL = ""
	result.MimeType = "image/" + format
	if format == "jpeg" {
		result.MimeType = "image/jpeg"
	}
	result.OutputFormat = format
	return result, nil
}

func resizeOpenAIImageBytesToDimensions(data []byte, width, height int) ([]byte, string, bool, error) {
	if width <= 0 || height <= 0 {
		return data, "", false, nil
	}
	pixels := int64(width) * int64(height)
	if pixels > image4KEnhancementMaxResizePx {
		return data, "", false, fmt.Errorf("requested resize dimensions %dx%d exceed safety limit", width, height)
	}
	src, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return data, "", false, err
	}
	if strings.EqualFold(format, "jpg") {
		format = "jpeg"
	}
	bounds := src.Bounds()
	if bounds.Dx() == width && bounds.Dy() == height {
		return data, format, false, nil
	}
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, bounds, xdraw.Over, nil)

	var out bytes.Buffer
	switch strings.ToLower(format) {
	case "jpeg":
		if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 95}); err != nil {
			return data, format, false, err
		}
		return out.Bytes(), "jpeg", true, nil
	case "png":
		if err := png.Encode(&out, dst); err != nil {
			return data, format, false, err
		}
		return out.Bytes(), "png", true, nil
	default:
		return data, format, false, fmt.Errorf("unsupported enhanced image format %q", format)
	}
}

func openAIImagesRequestModel(parsed *OpenAIImagesRequest) string {
	if parsed == nil {
		return ""
	}
	return strings.TrimSpace(parsed.Model)
}

func openAIImagesRequestSize(parsed *OpenAIImagesRequest) string {
	if parsed == nil {
		return ""
	}
	return strings.TrimSpace(parsed.Size)
}

func sourceInputFidelity(parsed *OpenAIImagesRequest) string {
	if parsed == nil {
		return ""
	}
	return strings.TrimSpace(parsed.InputFidelity)
}
