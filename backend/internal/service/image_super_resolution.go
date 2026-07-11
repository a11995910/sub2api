package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const imageSuperResolutionLogComponent = "service.image_super_resolution"

func (s *OpenAIGatewayService) imageSuperResolutionSkipReason(c *gin.Context, requestSizeTier string) string {
	if s == nil || s.cfg == nil {
		return "service_config_missing"
	}
	if strings.TrimSpace(s.cfg.Gateway.ImageSuperResolutionURL) == "" {
		return "url_not_configured"
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
	if !apiKey.Group.ImageSuperResolutionEnabled {
		return "group_super_resolution_disabled"
	}
	if NormalizeImageBillingTierOrDefault(requestSizeTier) != ImageBillingSize4K {
		return "request_not_4k"
	}
	return ""
}

func logImageSuperResolutionDecision(c *gin.Context, phase, reason, requestSizeTier string) {
	apiKey := getAPIKeyFromContext(c)
	apiKeyID := int64(0)
	groupID := int64(0)
	allowImageGeneration := false
	enabled := false
	if apiKey != nil {
		apiKeyID = apiKey.ID
		if apiKey.Group != nil {
			groupID = apiKey.Group.ID
			allowImageGeneration = apiKey.Group.AllowImageGeneration
			enabled = apiKey.Group.ImageSuperResolutionEnabled
		}
	}
	if strings.TrimSpace(reason) == "" {
		reason = "apply"
	}
	requestSizeTier = NormalizeImageBillingTierOrDefault(requestSizeTier)
	logger.LegacyPrintf(
		imageSuperResolutionLogComponent,
		"image super resolution %s: reason=%s api_key_id=%d group_id=%d allow_image_generation=%t enabled=%t request_size_tier=%s",
		phase,
		reason,
		apiKeyID,
		groupID,
		allowImageGeneration,
		enabled,
		requestSizeTier,
	)
}

func (s *OpenAIGatewayService) upscaleOpenAIImageBytes(ctx context.Context, imageBytes []byte, filename string) ([]byte, error) {
	if s == nil || s.cfg == nil {
		return nil, fmt.Errorf("image super resolution config is not available")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	endpoint := strings.TrimSpace(s.cfg.Gateway.ImageSuperResolutionURL)
	if endpoint == "" {
		return nil, fmt.Errorf("image super resolution url is not configured")
	}
	if len(imageBytes) == 0 {
		return nil, fmt.Errorf("image bytes is empty")
	}

	timeout := time.Duration(s.cfg.Gateway.ImageSuperResolutionTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 240 * time.Second
	}
	if strings.TrimSpace(filename) == "" {
		filename = "image.png"
	}

	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return nil, fmt.Errorf("create upscale multipart: %w", err)
	}
	if _, err := part.Write(imageBytes); err != nil {
		return nil, fmt.Errorf("write upscale multipart: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close upscale multipart: %w", err)
	}

	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, endpoint, &body)
	if err != nil {
		return nil, fmt.Errorf("build upscale request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "image/*")
	if apiKey := strings.TrimSpace(s.cfg.Gateway.ImageSuperResolutionAPIKey); apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}

	client := &http.Client{Timeout: timeout + 5*time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call upscale service: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msgBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		msg := sanitizeUpstreamErrorMessage(extractUpstreamErrorMessage(msgBytes))
		if msg == "" {
			msg = strings.TrimSpace(string(msgBytes))
		}
		if msg == "" {
			msg = fmt.Sprintf("status %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("upscale service failed: %s", msg)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, resolveUpstreamResponseReadLimit(s.cfg)+1))
	if err != nil {
		return nil, fmt.Errorf("read upscale response: %w", err)
	}
	if int64(len(data)) > resolveUpstreamResponseReadLimit(s.cfg) {
		return nil, fmt.Errorf("upscale response exceeds configured limit")
	}
	contentType := normalizeOpenAIImagesContentType(resp.Header.Get("Content-Type"), data)
	if !strings.HasPrefix(strings.ToLower(contentType), "image/") {
		return nil, fmt.Errorf("upscale service did not return an image")
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("upscale service returned empty image")
	}
	return data, nil
}

func (s *OpenAIGatewayService) applyOpenAIImagesSuperResolutionToJSON(
	ctx context.Context,
	c *gin.Context,
	body []byte,
	opts openAIImagesNonStreamingResponseOptions,
) []byte {
	requestSizeTier := ""
	if opts.parsed != nil {
		requestSizeTier = opts.parsed.SizeTier
	}
	if s.shouldBlockLegacyImageSuperResolutionFor4KEnhancement(c, opts.parsed, requestSizeTier) {
		return body
	}
	if reason := s.imageSuperResolutionSkipReason(c, requestSizeTier); reason != "" {
		logImageSuperResolutionDecision(c, "skip", reason, requestSizeTier)
		return body
	}
	if len(body) == 0 {
		logImageSuperResolutionDecision(c, "skip", "empty_body", requestSizeTier)
		return body
	}
	if !gjson.ValidBytes(body) {
		logImageSuperResolutionDecision(c, "skip", "invalid_json", requestSizeTier)
		return body
	}
	data := gjson.GetBytes(body, "data")
	if !data.IsArray() {
		logImageSuperResolutionDecision(c, "skip", "data_not_array", requestSizeTier)
		return body
	}
	logImageSuperResolutionDecision(c, "apply", "", requestSizeTier)

	rewritten := body
	processed := 0
	maxImages := 0
	if s != nil && s.cfg != nil {
		maxImages = s.cfg.Gateway.ImageSuperResolutionMaxImages
	}
	for index, item := range data.Array() {
		if maxImages > 0 && processed >= maxImages {
			break
		}
		imageBytes, err := s.imageBytesFromOpenAIImageItem(ctx, opts, item)
		if err != nil {
			logger.LegacyPrintf(imageSuperResolutionLogComponent, "skip image super resolution: index=%d err=%v", index, err)
			continue
		}
		upscaled, err := s.upscaleOpenAIImageBytes(ctx, imageBytes, fmt.Sprintf("openai-image-%d.png", index+1))
		if err != nil {
			logger.LegacyPrintf(imageSuperResolutionLogComponent, "image super resolution failed: index=%d err=%v", index, err)
			continue
		}
		encoded := base64.StdEncoding.EncodeToString(upscaled)
		if next, err := sjson.SetBytes(rewritten, fmt.Sprintf("data.%d.b64_json", index), encoded); err == nil {
			rewritten = next
		} else {
			logger.LegacyPrintf(imageSuperResolutionLogComponent, "rewrite upscaled image failed: index=%d err=%v", index, err)
			continue
		}
		rewritten, _ = sjson.SetBytes(rewritten, fmt.Sprintf("data.%d.mime_type", index), "image/png")
		rewritten, _ = sjson.DeleteBytes(rewritten, fmt.Sprintf("data.%d.url", index))
		rewritten, _ = sjson.DeleteBytes(rewritten, fmt.Sprintf("data.%d.image_url", index))
		rewritten, _ = sjson.DeleteBytes(rewritten, fmt.Sprintf("data.%d.download_url", index))
		logger.LegacyPrintf(
			imageSuperResolutionLogComponent,
			"image super resolution succeeded: index=%d input_bytes=%d output_bytes=%d",
			index,
			len(imageBytes),
			len(upscaled),
		)
		processed++
	}
	if processed > 0 {
		rewritten, _ = sjson.SetBytes(rewritten, "output_format", "png")
	}
	return rewritten
}

func (s *OpenAIGatewayService) imageBytesFromOpenAIImageItem(ctx context.Context, opts openAIImagesNonStreamingResponseOptions, item gjson.Result) ([]byte, error) {
	if normalized := normalizeOpenAIImageBase64(item.Get("b64_json").String()); normalized != "" {
		return base64.StdEncoding.DecodeString(normalized)
	}
	imageURL := firstNonEmptyString(
		item.Get("url").String(),
		item.Get("image_url").String(),
		item.Get("download_url").String(),
		imageURLFromInvalidOpenAIImageBase64(item.Get("b64_json").String()),
	)
	if imageURL == "" {
		return nil, fmt.Errorf("image item has no b64_json or url")
	}
	if opts.ctx == nil {
		opts.ctx = ctx
	}
	return s.downloadOpenAIImagesResponseURL(opts, imageURL)
}

func (s *OpenAIGatewayService) applyOpenAIResponsesSuperResolutionWithParsed(
	ctx context.Context,
	c *gin.Context,
	results []openAIResponsesImageResult,
	requestSizeTier string,
	parsed *OpenAIImagesRequest,
) []openAIResponsesImageResult {
	if s.shouldBlockLegacyImageSuperResolutionFor4KEnhancement(c, parsed, requestSizeTier) {
		return results
	}
	if len(results) == 0 {
		logImageSuperResolutionDecision(c, "skip", "empty_responses_results", requestSizeTier)
		return results
	}
	if reason := s.imageSuperResolutionSkipReason(c, requestSizeTier); reason != "" {
		logImageSuperResolutionDecision(c, "skip", reason, requestSizeTier)
		return results
	}
	logImageSuperResolutionDecision(c, "apply", "", requestSizeTier)
	out := make([]openAIResponsesImageResult, len(results))
	copy(out, results)
	maxImages := 0
	if s != nil && s.cfg != nil {
		maxImages = s.cfg.Gateway.ImageSuperResolutionMaxImages
	}
	processed := 0
	for index := range out {
		if maxImages > 0 && processed >= maxImages {
			break
		}
		normalized := normalizeOpenAIImageBase64(out[index].Result)
		if normalized == "" {
			continue
		}
		imageBytes, err := base64.StdEncoding.DecodeString(normalized)
		if err != nil {
			logger.LegacyPrintf(imageSuperResolutionLogComponent, "decode responses image failed: index=%d err=%v", index, err)
			continue
		}
		upscaled, err := s.upscaleOpenAIImageBytes(ctx, imageBytes, fmt.Sprintf("openai-responses-image-%d.png", index+1))
		if err != nil {
			logger.LegacyPrintf(imageSuperResolutionLogComponent, "responses image super resolution failed: index=%d err=%v", index, err)
			continue
		}
		out[index].Result = base64.StdEncoding.EncodeToString(upscaled)
		out[index].URL = ""
		out[index].OutputFormat = "png"
		out[index].MimeType = "image/png"
		out[index].Size = ""
		logger.LegacyPrintf(
			imageSuperResolutionLogComponent,
			"responses image super resolution succeeded: index=%d input_bytes=%d output_bytes=%d",
			index,
			len(imageBytes),
			len(upscaled),
		)
		processed++
	}
	return out
}
