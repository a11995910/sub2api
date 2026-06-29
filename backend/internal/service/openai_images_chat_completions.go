package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

var (
	openAIImageDataURLRegexp = regexp.MustCompile(`data:image/[A-Za-z0-9.+-]+;base64,[A-Za-z0-9+/=_-]+`)
	openAIImageURLRegexp     = regexp.MustCompile(`https?://[^\s"'<>)]*`)
)

func (s *OpenAIGatewayService) forwardOpenAIImagesViaChatCompletions(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	parsed *OpenAIImagesRequest,
	channelMappedModel string,
) (*OpenAIForwardResult, error) {
	startTime := time.Now()
	if account == nil {
		return nil, fmt.Errorf("account is required")
	}
	if account.Type != AccountTypeAPIKey {
		return nil, &OpenAIImagesInputError{Err: fmt.Errorf("configured images chat_completions upstream only supports OpenAI APIKey accounts")}
	}
	if parsed == nil {
		return nil, fmt.Errorf("parsed images request is required")
	}
	if parsed.Stream {
		return nil, &OpenAIImagesInputError{Err: fmt.Errorf("stream is not supported by chat_completions upstream mode")}
	}

	requestModel := strings.TrimSpace(parsed.Model)
	if mapped := strings.TrimSpace(channelMappedModel); mapped != "" {
		requestModel = mapped
	}
	if requestModel == "" {
		return nil, fmt.Errorf("images endpoint requires a model")
	}
	upstreamModel := account.GetMappedModel(requestModel)
	if strings.TrimSpace(upstreamModel) == "" {
		return nil, fmt.Errorf("images endpoint requires a model")
	}

	upstreamBody, err := buildOpenAIImagesChatCompletionsBody(parsed, upstreamModel)
	if err != nil {
		return nil, err
	}
	apiKey := account.GetOpenAIApiKey()
	if apiKey == "" {
		return nil, fmt.Errorf("account %d missing api_key", account.ID)
	}
	baseURL := account.GetOpenAIBaseURL()
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	validatedURL, err := s.validateUpstreamBaseURL(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base_url: %w", err)
	}
	targetURL := buildOpenAIChatCompletionsURL(validatedURL)

	upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
	upstreamReq, err := http.NewRequestWithContext(upstreamCtx, http.MethodPost, targetURL, bytes.NewReader(upstreamBody))
	releaseUpstreamCtx()
	if err != nil {
		return nil, fmt.Errorf("build upstream request: %w", err)
	}
	upstreamReq = upstreamReq.WithContext(WithHTTPUpstreamProfile(upstreamReq.Context(), HTTPUpstreamProfileOpenAI))
	upstreamReq.Header.Set("Content-Type", "application/json")
	upstreamReq.Header.Set("Accept", "application/json")
	upstreamReq.Header.Set("Authorization", "Bearer "+apiKey)
	for key, values := range c.Request.Header {
		if !openaiCCRawAllowedHeaders[strings.ToLower(key)] {
			continue
		}
		for _, value := range values {
			upstreamReq.Header.Add(key, value)
		}
	}
	if customUA := account.GetOpenAIUserAgent(); customUA != "" {
		upstreamReq.Header.Set("User-Agent", customUA)
	}

	proxyURL := ""
	if account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	upstreamStart := time.Now()
	resp, err := s.httpUpstream.Do(upstreamReq, proxyURL, account.ID, account.Concurrency)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
	if err != nil {
		safeErr := sanitizeUpstreamErrorMessage(err.Error())
		setOpsUpstreamError(c, 0, safeErr, "")
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform:           account.Platform,
			AccountID:          account.ID,
			AccountName:        account.Name,
			UpstreamStatusCode: 0,
			UpstreamURL:        safeUpstreamURL(targetURL),
			Kind:               "request_error",
			Message:            safeErr,
		})
		return nil, fmt.Errorf("upstream request failed: %s", safeErr)
	}
	if resp.StatusCode >= 400 {
		respBody := s.readUpstreamErrorBody(resp)
		_ = resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewReader(respBody))
		upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(respBody))
		upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
		if s.shouldFailoverOpenAIUpstreamResponse(resp.StatusCode, upstreamMsg, respBody) {
			appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
				Platform:           account.Platform,
				AccountID:          account.ID,
				AccountName:        account.Name,
				UpstreamStatusCode: resp.StatusCode,
				UpstreamRequestID:  resp.Header.Get("x-request-id"),
				UpstreamURL:        safeUpstreamURL(targetURL),
				Kind:               "failover",
				Message:            upstreamMsg,
			})
			s.handleFailoverSideEffects(upstreamCtx, resp, account, respBody, upstreamModel)
			return nil, &UpstreamFailoverError{
				StatusCode:             resp.StatusCode,
				ResponseBody:           respBody,
				RetryableOnSameAccount: shouldRetryOpenAIImagesSameAccount(resp.StatusCode, account),
			}
		}
		return s.handleOpenAIImagesErrorResponse(upstreamCtx, resp, c, account, upstreamModel)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		if !errors.Is(err, ErrUpstreamResponseBodyTooLarge) {
			writeOpenAIImagesUpstreamErrorResponse(c, &OpenAIImagesUpstreamError{
				StatusCode: http.StatusBadGateway,
				ErrorType:  "api_error",
				Message:    "Failed to read upstream response",
			})
		}
		return nil, fmt.Errorf("read upstream body: %w", err)
	}
	usage, _ := extractOpenAIUsageFromJSONBytes(respBody)
	imageResults, err := extractOpenAIImagesFromChatCompletionsBody(respBody, parsed)
	if err != nil {
		writeOpenAIImagesUpstreamErrorResponse(c, &OpenAIImagesUpstreamError{
			StatusCode:        http.StatusBadGateway,
			ErrorType:         "upstream_error",
			Message:           err.Error(),
			UpstreamRequestID: strings.TrimSpace(resp.Header.Get("x-request-id")),
		})
		return nil, err
	}
	if !strings.EqualFold(strings.TrimSpace(parsed.ResponseFormat), "url") {
		imageResults, err = s.materializeOpenAIResponsesImageURLs(upstreamCtx, imageResults, upstreamReq.Header.Clone(), proxyURL, account.ID, account.Concurrency)
		if err != nil {
			writeOpenAIImagesUpstreamErrorResponse(c, &OpenAIImagesUpstreamError{
				StatusCode:        http.StatusBadGateway,
				ErrorType:         "upstream_error",
				Message:           "Failed to download upstream image result",
				UpstreamRequestID: strings.TrimSpace(resp.Header.Get("x-request-id")),
			})
			return nil, err
		}
	}
	imageResults = s.applyOpenAIResponses4KEnhancement(upstreamCtx, c, imageResults, parsed)
	responseBody, err := buildOpenAIImagesAPIResponse(imageResults, time.Now().Unix(), nil, openAIResponsesImageResult{
		Model: requestModel,
		Size:  parsed.Size,
	}, parsed.ResponseFormat)
	if err != nil {
		return nil, err
	}
	if s.responseHeaderFilter != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
	}
	c.Data(http.StatusOK, "application/json", responseBody)

	return &OpenAIForwardResult{
		RequestID:        resp.Header.Get("x-request-id"),
		Usage:            usage,
		Model:            requestModel,
		UpstreamModel:    upstreamModel,
		Stream:           false,
		ResponseHeaders:  resp.Header.Clone(),
		Duration:         time.Since(startTime),
		ImageCount:       len(imageResults),
		ImageSize:        parsed.SizeTier,
		ImageInputSize:   parsed.Size,
		ImageOutputSizes: collectOpenAIResponseImageOutputSizesFromJSONBytes(responseBody),
	}, nil
}

func buildOpenAIImagesChatCompletionsBody(parsed *OpenAIImagesRequest, upstreamModel string) ([]byte, error) {
	content := strings.TrimSpace(parsed.Prompt)
	if content == "" {
		return nil, fmt.Errorf("prompt is required")
	}
	var b strings.Builder
	b.WriteString(content)
	if parsed.IsEdits() {
		b.WriteString("\n\nUse the attached reference image(s) as visual input for this image edit request.")
		if parsed.HasMask {
			b.WriteString(" If a mask image is attached, treat it as the editable region mask.")
		}
	}
	b.WriteString("\n\nReturn exactly one generated image as either an image URL, a data:image/*;base64 URL, or JSON with url/image_url/b64_json. Do not include unrelated text.")
	if strings.TrimSpace(parsed.Size) != "" {
		b.WriteString("\nRequested image size: ")
		b.WriteString(strings.TrimSpace(parsed.Size))
	}
	messageContent, err := buildOpenAIImagesChatCompletionsMessageContent(parsed, b.String())
	if err != nil {
		return nil, err
	}
	payload := map[string]any{
		"model": upstreamModel,
		"messages": []map[string]any{{
			"role":    "user",
			"content": messageContent,
		}},
		"stream": false,
	}
	if imageConfig := openAIImagesChatCompletionsImageConfig(parsed); len(imageConfig) > 0 {
		payload["generationConfig"] = map[string]any{
			"imageConfig": imageConfig,
		}
	}
	return json.Marshal(payload)
}

func openAIImagesChatCompletionsImageConfig(parsed *OpenAIImagesRequest) map[string]any {
	if parsed == nil {
		return nil
	}
	imageSize := strings.TrimSpace(parsed.SizeTier)
	if imageSize == "" {
		imageSize = normalizeOpenAIImageSizeTier(parsed.Size)
	}
	imageConfig := map[string]any{}
	if imageSize != "" {
		imageConfig["imageSize"] = imageSize
	}
	if aspectRatio := openAIImagesAspectRatioFromSize(parsed.Size); aspectRatio != "" {
		imageConfig["aspectRatio"] = aspectRatio
	}
	if len(imageConfig) == 0 {
		return nil
	}
	return imageConfig
}

func openAIImagesAspectRatioFromSize(size string) string {
	width, height, ok := parseImageBillingDimensions(size)
	if !ok {
		return ""
	}
	divisor := gcdInt(width, height)
	if divisor <= 0 {
		return ""
	}
	w := width / divisor
	h := height / divisor
	if w <= 0 || h <= 0 {
		return ""
	}
	return strconv.Itoa(w) + ":" + strconv.Itoa(h)
}

func gcdInt(a, b int) int {
	if a < 0 {
		a = -a
	}
	if b < 0 {
		b = -b
	}
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

func buildOpenAIImagesChatCompletionsMessageContent(parsed *OpenAIImagesRequest, text string) (any, error) {
	if parsed == nil || !parsed.IsEdits() {
		return text, nil
	}
	imageURLs := make([]string, 0, len(parsed.InputImageURLs)+len(parsed.Uploads)+1)
	for _, imageURL := range parsed.InputImageURLs {
		if trimmed := strings.TrimSpace(imageURL); trimmed != "" {
			imageURLs = append(imageURLs, trimmed)
		}
	}
	for _, upload := range parsed.Uploads {
		dataURL, err := openAIImageUploadToDataURL(upload)
		if err != nil {
			return nil, err
		}
		imageURLs = append(imageURLs, dataURL)
	}
	if maskURL := strings.TrimSpace(parsed.MaskImageURL); maskURL != "" {
		imageURLs = append(imageURLs, maskURL)
	}
	if parsed.MaskUpload != nil {
		dataURL, err := openAIImageUploadToDataURL(*parsed.MaskUpload)
		if err != nil {
			return nil, err
		}
		imageURLs = append(imageURLs, dataURL)
	}
	if len(imageURLs) == 0 {
		return nil, &OpenAIImagesInputError{Err: fmt.Errorf("image input is required")}
	}

	parts := make([]map[string]any, 0, len(imageURLs)+1)
	parts = append(parts, map[string]any{
		"type": "text",
		"text": text,
	})
	for _, imageURL := range imageURLs {
		parts = append(parts, map[string]any{
			"type": "image_url",
			"image_url": map[string]string{
				"url": imageURL,
			},
		})
	}
	return parts, nil
}

func extractOpenAIImagesFromChatCompletionsBody(body []byte, parsed *OpenAIImagesRequest) ([]openAIResponsesImageResult, error) {
	content := strings.TrimSpace(gjson.GetBytes(body, "choices.0.message.content").String())
	if content == "" {
		content = strings.TrimSpace(gjson.GetBytes(body, "message.content").String())
	}
	if content == "" {
		result := openAIImageResultFromChatMessageImages(body, parsed)
		if strings.TrimSpace(result.Result) != "" || strings.TrimSpace(result.URL) != "" {
			return []openAIResponsesImageResult{result}, nil
		}
		return nil, fmt.Errorf("upstream chat_completions response did not include image content")
	}
	result := openAIImageResultFromChatContent(content, parsed)
	if strings.TrimSpace(result.Result) == "" && strings.TrimSpace(result.URL) == "" {
		return nil, fmt.Errorf("upstream chat_completions response did not include an image URL or base64 image")
	}
	return []openAIResponsesImageResult{result}, nil
}

func openAIImageResultFromChatMessageImages(body []byte, parsed *OpenAIImagesRequest) openAIResponsesImageResult {
	for _, path := range []string{
		"choices.0.message.images.0.image_url.url",
		"choices.0.message.images.0.image_url",
		"choices.0.message.images.0.url",
		"message.images.0.image_url.url",
		"message.images.0.image_url",
		"message.images.0.url",
	} {
		imageURL := strings.TrimSpace(gjson.GetBytes(body, path).String())
		if imageURL == "" {
			continue
		}
		if b64, mimeType := openAIImageBase64FromDataURL(imageURL); b64 != "" {
			return openAIResponsesImageResult{Result: b64, MimeType: mimeType, OutputFormat: outputFormatFromMimeType(mimeType), RevisedPrompt: revisedPromptForChatImages(parsed)}
		}
		return openAIResponsesImageResult{URL: imageURL, RevisedPrompt: revisedPromptForChatImages(parsed)}
	}
	return openAIResponsesImageResult{}
}

func openAIImageResultFromChatContent(content string, parsed *OpenAIImagesRequest) openAIResponsesImageResult {
	if b64, mimeType := openAIImageBase64FromDataURL(content); b64 != "" {
		return openAIResponsesImageResult{Result: b64, MimeType: mimeType, OutputFormat: outputFormatFromMimeType(mimeType), RevisedPrompt: revisedPromptForChatImages(parsed)}
	}
	if gjson.Valid(content) {
		if b64 := normalizeOpenAIImageBase64(firstGJSONString(content, "b64_json", "image.b64_json")); b64 != "" {
			return openAIResponsesImageResult{Result: b64, RevisedPrompt: revisedPromptForChatImages(parsed)}
		}
		if url := firstGJSONString(content, "url", "image_url", "download_url", "image.url", "image.image_url"); url != "" {
			if b64, mimeType := openAIImageBase64FromDataURL(url); b64 != "" {
				return openAIResponsesImageResult{Result: b64, MimeType: mimeType, OutputFormat: outputFormatFromMimeType(mimeType), RevisedPrompt: revisedPromptForChatImages(parsed)}
			}
			return openAIResponsesImageResult{URL: url, RevisedPrompt: revisedPromptForChatImages(parsed)}
		}
	}
	if dataURL := openAIImageDataURLRegexp.FindString(content); dataURL != "" {
		if b64, mimeType := openAIImageBase64FromDataURL(dataURL); b64 != "" {
			return openAIResponsesImageResult{Result: b64, MimeType: mimeType, OutputFormat: outputFormatFromMimeType(mimeType), RevisedPrompt: revisedPromptForChatImages(parsed)}
		}
	}
	if url := openAIImageURLRegexp.FindString(content); url != "" {
		return openAIResponsesImageResult{URL: strings.TrimRight(url, ".,;"), RevisedPrompt: revisedPromptForChatImages(parsed)}
	}
	return openAIResponsesImageResult{}
}

func firstGJSONString(jsonText string, paths ...string) string {
	for _, path := range paths {
		if value := strings.TrimSpace(gjson.Get(jsonText, path).String()); value != "" {
			return value
		}
	}
	return ""
}

func outputFormatFromMimeType(mimeType string) string {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	if strings.Contains(mimeType, "jpeg") || strings.Contains(mimeType, "jpg") {
		return "jpeg"
	}
	if strings.Contains(mimeType, "webp") {
		return "webp"
	}
	return "png"
}

func revisedPromptForChatImages(parsed *OpenAIImagesRequest) string {
	if parsed == nil {
		return ""
	}
	return strings.TrimSpace(parsed.Prompt)
}
