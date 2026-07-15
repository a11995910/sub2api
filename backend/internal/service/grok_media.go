package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type GrokMediaEndpoint string

const (
	GrokMediaEndpointImagesGenerations GrokMediaEndpoint = "images_generations"
	GrokMediaEndpointImagesEdits       GrokMediaEndpoint = "images_edits"
	GrokMediaEndpointVideosGenerations GrokMediaEndpoint = "videos_generations"
	GrokMediaEndpointVideosEdits       GrokMediaEndpoint = "videos_edits"
	GrokMediaEndpointVideosExtensions  GrokMediaEndpoint = "videos_extensions"
	GrokMediaEndpointVideoStatus       GrokMediaEndpoint = "video_status"
	GrokMediaEndpointVideoContent      GrokMediaEndpoint = "video_content"
)

const (
	grokVideoContentMaxBytes            int64 = 128 << 20
	grokVideoInlineImageMaxDecodedBytes int64 = 1 << 20
)

func (e GrokMediaEndpoint) RequiresRequestBody() bool {
	return e != GrokMediaEndpointVideoStatus && e != GrokMediaEndpointVideoContent
}

func (e GrokMediaEndpoint) IsGenerationRequest() bool {
	switch e {
	case GrokMediaEndpointImagesGenerations, GrokMediaEndpointImagesEdits, GrokMediaEndpointVideosGenerations, GrokMediaEndpointVideosEdits, GrokMediaEndpointVideosExtensions:
		return true
	default:
		return false
	}
}

type GrokMediaRequestInfo struct {
	Model              string
	Prompt             string
	N                  int
	Size               string
	SizeTier           string
	Resolution         string
	DurationSeconds    int
	InputImageURLs     []string
	ReferenceImageURLs []string
	MaskImageURL       string
	Uploads            []OpenAIImagesUpload
	MaskUpload         *OpenAIImagesUpload
}

func (r GrokMediaRequestInfo) ModerationBody() []byte {
	payload := map[string]any{}
	if prompt := strings.TrimSpace(r.Prompt); prompt != "" {
		payload["prompt"] = prompt
	}

	images := make([]map[string]string, 0, len(r.InputImageURLs)+len(r.ReferenceImageURLs)+len(r.Uploads)+1)
	for _, imageURL := range r.InputImageURLs {
		if imageURL = strings.TrimSpace(imageURL); imageURL != "" {
			images = append(images, map[string]string{"image_url": imageURL})
		}
	}
	for _, imageURL := range r.ReferenceImageURLs {
		if imageURL = strings.TrimSpace(imageURL); imageURL != "" {
			images = append(images, map[string]string{"image_url": imageURL})
		}
	}
	for _, upload := range r.Uploads {
		if dataURL := upload.ModerationDataURL(); dataURL != "" {
			images = append(images, map[string]string{"image_url": dataURL})
		}
	}
	if maskURL := strings.TrimSpace(r.MaskImageURL); maskURL != "" {
		images = append(images, map[string]string{"image_url": maskURL})
	}
	if r.MaskUpload != nil {
		if dataURL := r.MaskUpload.ModerationDataURL(); dataURL != "" {
			images = append(images, map[string]string{"image_url": dataURL})
		}
	}
	if len(images) > 0 {
		payload["images"] = images
	}
	if len(payload) == 0 {
		return nil
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	return body
}

func (e GrokMediaEndpoint) httpMethod() string {
	if e == GrokMediaEndpointVideoStatus || e == GrokMediaEndpointVideoContent {
		return http.MethodGet
	}
	return http.MethodPost
}

func ExtractGrokMediaModel(contentType string, body []byte) string {
	return ParseGrokMediaRequest(contentType, body).Model
}

func ParseGrokMediaRequest(contentType string, body []byte) GrokMediaRequestInfo {
	info := GrokMediaRequestInfo{N: 1}
	if gjson.ValidBytes(body) {
		parseGrokMediaJSONRequest(body, &info)
	} else {
		parseGrokMediaMultipartRequest(contentType, body, &info)
	}
	info.Model = strings.TrimSpace(info.Model)
	info.Prompt = strings.TrimSpace(info.Prompt)
	info.Size = strings.TrimSpace(info.Size)
	info.SizeTier = NormalizeImageBillingTierOrDefault(info.Size)
	info.Resolution = NormalizeVideoBillingResolutionOrDefault(info.Resolution)
	info.DurationSeconds = NormalizeVideoBillingDurationSecondsOrDefault(info.DurationSeconds)
	if info.N <= 0 {
		info.N = 1
	}
	return info
}

func parseGrokMediaJSONRequest(body []byte, info *GrokMediaRequestInfo) {
	if info == nil {
		return
	}
	info.Model = strings.TrimSpace(gjson.GetBytes(body, "model").String())
	info.Prompt = strings.TrimSpace(gjson.GetBytes(body, "prompt").String())
	info.Size = strings.TrimSpace(gjson.GetBytes(body, "size").String())
	info.Resolution = strings.TrimSpace(gjson.GetBytes(body, "resolution").String())
	if duration := gjson.GetBytes(body, "duration"); duration.Exists() && duration.Type == gjson.Number {
		info.DurationSeconds = int(duration.Int())
	}
	if n := gjson.GetBytes(body, "n"); n.Exists() && n.Type == gjson.Number {
		info.N = int(n.Int())
	}
	appendJSONImageURLs := func(value gjson.Result, target *[]string) {
		if !value.Exists() {
			return
		}
		appendURL := func(item gjson.Result) {
			for _, field := range []string{"url", "image_url"} {
				if imageURL := strings.TrimSpace(item.Get(field).String()); imageURL != "" {
					*target = append(*target, imageURL)
					return
				}
			}
			if item.Type == gjson.String {
				if imageURL := strings.TrimSpace(item.String()); imageURL != "" {
					*target = append(*target, imageURL)
				}
			}
		}
		switch {
		case value.IsArray():
			for _, item := range value.Array() {
				appendURL(item)
			}
		default:
			appendURL(value)
		}
	}
	appendJSONImageURLs(gjson.GetBytes(body, "image"), &info.InputImageURLs)
	appendJSONImageURLs(gjson.GetBytes(body, "images"), &info.InputImageURLs)
	appendJSONImageURLs(gjson.GetBytes(body, "reference_images"), &info.ReferenceImageURLs)
	info.MaskImageURL = firstNonEmpty(
		strings.TrimSpace(gjson.GetBytes(body, "mask.url").String()),
		strings.TrimSpace(gjson.GetBytes(body, "mask.image_url").String()),
	)
}

// GrokMediaRequestValidationError 表示无需请求上游即可确认的媒体参数错误。
type GrokMediaRequestValidationError struct {
	StatusCode int
	Message    string
}

func (e *GrokMediaRequestValidationError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// ValidateGrokMediaRequest 在账号调度和计费前校验视频图片模式与内联体积。
func ValidateGrokMediaRequest(endpoint GrokMediaEndpoint, info GrokMediaRequestInfo) *GrokMediaRequestValidationError {
	if endpoint != GrokMediaEndpointVideosGenerations {
		return nil
	}

	hasStartingImage := len(info.InputImageURLs) > 0 || len(info.Uploads) > 0
	hasReferenceImages := len(info.ReferenceImageURLs) > 0
	if hasStartingImage && hasReferenceImages {
		return &GrokMediaRequestValidationError{
			StatusCode: http.StatusBadRequest,
			Message:    "image and reference_images cannot be used together",
		}
	}

	model := strings.ToLower(strings.TrimSpace(info.Model))
	isVideo15 := strings.HasPrefix(model, "grok-imagine-video-1.5")
	isStandardVideo := strings.HasPrefix(model, "grok-imagine-video") && !isVideo15
	if hasStartingImage && isStandardVideo {
		return &GrokMediaRequestValidationError{
			StatusCode: http.StatusBadRequest,
			Message:    "grok-imagine-video does not support a starting image; use grok-imagine-video-1.5",
		}
	}
	if hasReferenceImages && isVideo15 {
		return &GrokMediaRequestValidationError{
			StatusCode: http.StatusBadRequest,
			Message:    "grok-imagine-video-1.5 does not support reference_images; use grok-imagine-video",
		}
	}

	for _, imageURL := range append(append([]string{}, info.InputImageURLs...), info.ReferenceImageURLs...) {
		if decodedSize, ok := inlineImageDecodedSize(imageURL); ok && decodedSize > grokVideoInlineImageMaxDecodedBytes {
			return &GrokMediaRequestValidationError{
				StatusCode: http.StatusRequestEntityTooLarge,
				Message:    "video reference image exceeds the 1 MB inline upload limit; compress the image before uploading",
			}
		}
	}
	return nil
}

func inlineImageDecodedSize(raw string) (int64, bool) {
	raw = strings.TrimSpace(raw)
	comma := strings.IndexByte(raw, ',')
	if comma <= 0 {
		return 0, false
	}
	metadata := strings.ToLower(raw[:comma])
	if !strings.HasPrefix(metadata, "data:image/") || !strings.Contains(metadata, ";base64") {
		return 0, false
	}
	encoded := strings.TrimSpace(raw[comma+1:])
	padding := int64(0)
	if strings.HasSuffix(encoded, "==") {
		padding = 2
	} else if strings.HasSuffix(encoded, "=") {
		padding = 1
	}
	decodedSize := int64(len(encoded))*3/4 - padding
	if decodedSize < 0 {
		decodedSize = 0
	}
	return decodedSize, true
}

func parseGrokMediaMultipartRequest(contentType string, body []byte, info *GrokMediaRequestInfo) {
	if info == nil {
		return
	}
	mediaType, params, err := mime.ParseMediaType(strings.TrimSpace(contentType))
	if err != nil || !strings.EqualFold(mediaType, "multipart/form-data") {
		return
	}
	boundary := strings.TrimSpace(params["boundary"])
	if boundary == "" {
		return
	}
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			return
		}
		if err != nil {
			return
		}
		name := strings.TrimSpace(part.FormName())
		if name == "" {
			_ = part.Close()
			continue
		}
		data, err := io.ReadAll(io.LimitReader(part, openAIImageMaxUploadPartSize))
		_ = part.Close()
		if err != nil {
			return
		}
		fileName := strings.TrimSpace(part.FileName())
		partContentType := strings.TrimSpace(part.Header.Get("Content-Type"))
		if fileName != "" {
			upload := OpenAIImagesUpload{
				FieldName:   name,
				FileName:    fileName,
				ContentType: partContentType,
				Data:        data,
			}
			if name == "mask" {
				info.MaskUpload = &upload
				continue
			}
			if name == "image" || strings.HasPrefix(name, "image[") {
				info.Uploads = append(info.Uploads, upload)
			}
			continue
		}

		value := strings.TrimSpace(string(data))
		switch name {
		case "model":
			info.Model = value
		case "prompt":
			info.Prompt = value
		case "size":
			info.Size = value
		case "resolution":
			info.Resolution = value
		case "duration":
			if duration, err := strconv.Atoi(value); err == nil {
				info.DurationSeconds = duration
			}
		case "n":
			if n, err := strconv.Atoi(value); err == nil {
				info.N = n
			}
		case "image", "image_url":
			if value != "" {
				info.InputImageURLs = append(info.InputImageURLs, value)
			}
		case "mask", "mask_image_url":
			info.MaskImageURL = value
		}
	}
}

func GrokMediaVideoRequestSessionHash(requestID string) string {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return ""
	}
	return "grok-video:" + DeriveSessionHashFromSeed(requestID)
}

func (s *OpenAIGatewayService) BindGrokMediaVideoRequestAccount(ctx context.Context, groupID *int64, requestID string, accountID int64) error {
	return s.BindStickySession(ctx, groupID, GrokMediaVideoRequestSessionHash(requestID), accountID)
}

func (s *OpenAIGatewayService) ForwardGrokMedia(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	endpoint GrokMediaEndpoint,
	requestID string,
	body []byte,
	contentType string,
) (*OpenAIForwardResult, error) {
	startTime := time.Now()
	if account == nil {
		return nil, fmt.Errorf("grok account is required")
	}
	if account.Platform != PlatformGrok {
		return nil, fmt.Errorf("account platform %s is not supported for grok media", account.Platform)
	}

	token, _, err := s.getRequestCredential(ctx, c, account)
	if err != nil {
		return nil, err
	}
	targetURL, err := buildGrokMediaURL(account, s.cfg, endpoint, requestID)
	if err != nil {
		return nil, err
	}

	body, contentType, err = prepareGrokMediaForwardBody(endpoint, body, contentType)
	if err != nil {
		return nil, err
	}
	body, contentType, err = normalizeGrokMediaForwardBody(endpoint, body, contentType)
	if err != nil {
		return nil, err
	}
	requestInfo := ParseGrokMediaRequest(contentType, body)
	body, contentType, err = sanitizeGrokMediaForwardBody(endpoint, body, contentType)
	if err != nil {
		return nil, err
	}

	var bodyReader io.Reader
	if endpoint.RequiresRequestBody() {
		bodyReader = bytes.NewReader(body)
	}
	upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
	defer releaseUpstreamCtx()
	upstreamReq, err := http.NewRequestWithContext(upstreamCtx, endpoint.httpMethod(), targetURL, bodyReader)
	if err != nil {
		return nil, err
	}
	upstreamReq.Header.Set("Authorization", "Bearer "+token)
	upstreamReq.Header.Set("Accept", "application/json")
	if account.IsGrokOAuth() {
		applyGrokCLIHeaders(upstreamReq.Header)
	}
	if endpoint.RequiresRequestBody() {
		contentType = strings.TrimSpace(contentType)
		if contentType == "" {
			contentType = "application/json"
		}
		upstreamReq.Header.Set("Content-Type", contentType)
	}

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	upstreamStart := time.Now()
	resp, err := s.httpUpstream.Do(upstreamReq, proxyURL, account.ID, account.Concurrency)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
	if err != nil {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	defer func() { _ = resp.Body.Close() }()

	requestIDHeader := firstNonEmpty(resp.Header.Get("x-request-id"), resp.Header.Get("xai-request-id"))
	requestModel := requestInfo.Model
	if resp.StatusCode >= 400 {
		return s.handleGrokMediaErrorResponse(ctx, resp, c, account, requestIDHeader, requestModel)
	}

	s.updateGrokUsageFromResponse(ctx, account, resp.Header, resp.StatusCode)
	respBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return nil, err
	}
	if endpoint == GrokMediaEndpointVideoContent {
		_ = resp.Body.Close()
		return s.forwardGrokVideoContent(upstreamCtx, c, account, proxyURL, respBody, startTime)
	}
	writeGrokMediaResponse(c, resp, respBody, s.responseHeaderFilter)
	usage := grokMediaUsageFromResponse(endpoint, requestInfo, respBody)
	return &OpenAIForwardResult{
		RequestID:            requestIDHeader,
		ResponseID:           usage.ResponseID,
		Usage:                usage.Usage,
		Model:                requestModel,
		BillingModel:         requestModel,
		UpstreamModel:        requestModel,
		ResponseHeaders:      resp.Header.Clone(),
		Duration:             time.Since(startTime),
		ImageCount:           usage.ImageCount,
		ImageSize:            usage.ImageSize,
		ImageInputSize:       usage.ImageInputSize,
		ImageOutputSizes:     usage.ImageOutputSizes,
		VideoCount:           usage.VideoCount,
		VideoResolution:      usage.VideoResolution,
		VideoDurationSeconds: usage.VideoDurationSeconds,
		VideoInputImageCount: usage.VideoInputImageCount,
	}, nil
}

func (s *OpenAIGatewayService) forwardGrokVideoContent(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	proxyURL string,
	statusBody []byte,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	videoURL := extractGrokVideoContentURL(statusBody)
	if videoURL == "" {
		writeGrokMediaErrorResponse(c, http.StatusConflict, "video_not_ready", "Video content is not available yet")
		return nil, errors.New("grok video content URL is missing from status response")
	}
	if err := validateGrokVideoContentURL(videoURL); err != nil {
		writeGrokMediaErrorResponse(c, http.StatusBadGateway, "upstream_error", "Upstream returned an invalid video URL")
		return nil, fmt.Errorf("validate grok video content URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, videoURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "video/mp4,video/*;q=0.9")

	upstreamStart := time.Now()
	resp, err := s.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
	if err != nil {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		setOpsUpstreamError(c, resp.StatusCode, "video content download failed", "")
		writeGrokMediaErrorResponse(c, http.StatusBadGateway, "upstream_error", "Failed to download generated video")
		return nil, fmt.Errorf("grok video content returned status %d", resp.StatusCode)
	}
	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if !isAllowedGrokVideoContentType(contentType) {
		writeGrokMediaErrorResponse(c, http.StatusBadGateway, "upstream_error", "Upstream returned invalid video content")
		return nil, fmt.Errorf("unsupported grok video content type: %q", contentType)
	}
	if resp.ContentLength > grokVideoContentMaxBytes {
		writeGrokMediaErrorResponse(c, http.StatusBadGateway, "upstream_error", "Generated video is too large")
		return nil, fmt.Errorf("%w: limit=%d", ErrUpstreamResponseBodyTooLarge, grokVideoContentMaxBytes)
	}
	body, err := readUpstreamResponseBodyLimited(resp.Body, grokVideoContentMaxBytes)
	if err != nil {
		if errors.Is(err, ErrUpstreamResponseBodyTooLarge) {
			writeGrokMediaErrorResponse(c, http.StatusBadGateway, "upstream_error", "Generated video is too large")
		}
		return nil, err
	}

	c.Header("Cache-Control", "private, max-age=300")
	c.Header("Content-Disposition", `inline; filename="generated-video.mp4"`)
	c.Data(http.StatusOK, contentType, body)
	return &OpenAIForwardResult{
		Duration:        time.Since(startTime),
		ResponseHeaders: resp.Header.Clone(),
	}, nil
}

func extractGrokVideoContentURL(body []byte) string {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return ""
	}
	for _, field := range []string{"video.url", "video.video_url", "data.video.url", "data.url", "url", "video_url"} {
		if value := strings.TrimSpace(gjson.GetBytes(body, field).String()); value != "" {
			return value
		}
	}
	return ""
}

func validateGrokVideoContentURL(rawURL string) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return err
	}
	if parsed.Scheme != "https" || parsed.User != nil || !strings.EqualFold(parsed.Hostname(), "vidgen.x.ai") {
		return errors.New("video URL must use the allowed xAI HTTPS host")
	}
	if parsed.Port() != "" {
		return errors.New("video URL must not specify a custom port")
	}
	path := strings.ToLower(parsed.EscapedPath())
	if !strings.HasPrefix(path, "/xai-vidgen-bucket/") || !strings.HasSuffix(path, ".mp4") {
		return errors.New("video URL path is not allowed")
	}
	return nil
}

func isAllowedGrokVideoContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	return strings.EqualFold(mediaType, "video/mp4") || strings.EqualFold(mediaType, "application/octet-stream")
}

func prepareGrokMediaForwardBody(endpoint GrokMediaEndpoint, body []byte, contentType string) ([]byte, string, error) {
	if endpoint != GrokMediaEndpointImagesEdits || gjson.ValidBytes(body) {
		return body, contentType, nil
	}
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(contentType))
	if err != nil || !strings.EqualFold(mediaType, "multipart/form-data") {
		return body, contentType, nil
	}

	info := ParseGrokMediaRequest(contentType, body)
	payload := make(map[string]any)
	if info.Model != "" {
		payload["model"] = info.Model
	}
	if info.Prompt != "" {
		payload["prompt"] = info.Prompt
	}
	if info.N > 1 {
		payload["n"] = info.N
	}
	if info.Size != "" {
		payload["size"] = info.Size
	}

	images := make([]map[string]string, 0, len(info.InputImageURLs)+len(info.Uploads))
	for _, imageURL := range info.InputImageURLs {
		if imageURL = strings.TrimSpace(imageURL); imageURL != "" {
			images = append(images, map[string]string{"image_url": imageURL})
		}
	}
	for _, upload := range info.Uploads {
		dataURL, err := openAIImageUploadToDataURL(upload)
		if err != nil {
			return nil, "", err
		}
		images = append(images, map[string]string{"image_url": dataURL})
	}
	if len(images) > 0 {
		payload["image"] = images[0]
		if len(images) > 1 {
			payload["images"] = images
		}
	}

	maskImageURL := strings.TrimSpace(info.MaskImageURL)
	if info.MaskUpload != nil {
		dataURL, err := openAIImageUploadToDataURL(*info.MaskUpload)
		if err != nil {
			return nil, "", err
		}
		maskImageURL = dataURL
	}
	if maskImageURL != "" {
		payload["mask"] = map[string]string{"image_url": maskImageURL}
	}

	out, err := marshalOpenAIUpstreamJSON(payload)
	if err != nil {
		return nil, "", err
	}
	return out, "application/json", nil
}

func normalizeGrokMediaForwardBody(endpoint GrokMediaEndpoint, body []byte, contentType string) ([]byte, string, error) {
	if !endpoint.RequiresRequestBody() || !gjson.ValidBytes(body) {
		return body, contentType, nil
	}
	info := ParseGrokMediaRequest(contentType, body)
	out := body
	upstreamModel := normalizeGrokMediaModelForEndpoint(endpoint, info.Model, info.HasStartingImage())
	if upstreamModel != "" && upstreamModel != info.Model {
		var err error
		out, err = sjson.SetBytes(out, "model", upstreamModel)
		if err != nil {
			return nil, "", fmt.Errorf("rewrite grok media model: %w", err)
		}
	}

	if endpoint == GrokMediaEndpointVideosGenerations {
		if len(info.InputImageURLs) > 0 {
			var err error
			out, err = sjson.SetBytes(out, "image", map[string]string{"url": info.InputImageURLs[0]})
			if err != nil {
				return nil, "", fmt.Errorf("normalize grok video starting image: %w", err)
			}
			out, err = sjson.DeleteBytes(out, "images")
			if err != nil {
				return nil, "", fmt.Errorf("remove legacy grok video images field: %w", err)
			}
		}
		if len(info.ReferenceImageURLs) > 0 {
			references := make([]map[string]string, 0, len(info.ReferenceImageURLs))
			for _, imageURL := range info.ReferenceImageURLs {
				references = append(references, map[string]string{"url": imageURL})
			}
			var err error
			out, err = sjson.SetBytes(out, "reference_images", references)
			if err != nil {
				return nil, "", fmt.Errorf("normalize grok video reference images: %w", err)
			}
		}
	}
	return out, contentType, nil
}

func sanitizeGrokMediaForwardBody(endpoint GrokMediaEndpoint, body []byte, contentType string) ([]byte, string, error) {
	if !endpoint.RequiresRequestBody() || !gjson.ValidBytes(body) {
		return body, contentType, nil
	}
	switch endpoint {
	case GrokMediaEndpointImagesGenerations, GrokMediaEndpointImagesEdits:
		if !gjson.GetBytes(body, "size").Exists() {
			return body, contentType, nil
		}
		out, err := sjson.DeleteBytes(body, "size")
		if err != nil {
			return nil, "", fmt.Errorf("sanitize grok media size: %w", err)
		}
		return out, contentType, nil
	default:
		return body, contentType, nil
	}
}

func (r GrokMediaRequestInfo) HasStartingImage() bool {
	return len(r.InputImageURLs) > 0 || len(r.Uploads) > 0
}

func (r GrokMediaRequestInfo) HasInputImage() bool {
	return r.HasStartingImage() || len(r.ReferenceImageURLs) > 0
}

func normalizeGrokMediaModelForEndpoint(endpoint GrokMediaEndpoint, model string, hasInputImage bool) string {
	model = strings.TrimSpace(model)
	switch endpoint {
	case GrokMediaEndpointImagesGenerations, GrokMediaEndpointImagesEdits:
		if model == "grok-imagine" {
			return "grok-imagine-image-quality"
		}
	case GrokMediaEndpointVideosGenerations:
		if strings.HasPrefix(strings.ToLower(model), "grok-imagine-video-1.5") && !hasInputImage {
			return "grok-imagine-video"
		}
	}
	return model
}

type grokMediaUsageMetadata struct {
	ResponseID           string
	Usage                OpenAIUsage
	ImageCount           int
	ImageSize            string
	ImageInputSize       string
	ImageOutputSizes     []string
	VideoCount           int
	VideoResolution      string
	VideoDurationSeconds int
	VideoInputImageCount int
}

func grokMediaUsageFromResponse(endpoint GrokMediaEndpoint, requestInfo GrokMediaRequestInfo, responseBody []byte) grokMediaUsageMetadata {
	usage, _ := extractOpenAIUsageFromJSONBytes(responseBody)
	meta := grokMediaUsageMetadata{Usage: usage}
	switch endpoint {
	case GrokMediaEndpointImagesGenerations, GrokMediaEndpointImagesEdits:
		imageCount := countOpenAIResponseImageOutputsFromJSONBytes(responseBody)
		if imageCount <= 0 {
			imageCount = requestInfo.N
		}
		if imageCount <= 0 {
			imageCount = 1
		}
		meta.ImageCount = imageCount
		meta.ImageSize = requestInfo.SizeTier
		meta.ImageInputSize = requestInfo.Size
		meta.ImageOutputSizes = collectOpenAIResponseImageOutputSizesFromJSONBytes(responseBody)
	case GrokMediaEndpointVideosGenerations, GrokMediaEndpointVideosEdits, GrokMediaEndpointVideosExtensions:
		meta.ResponseID = extractGrokMediaVideoRequestID(responseBody)
		meta.VideoCount = 1
		meta.VideoResolution = requestInfo.Resolution
		meta.VideoDurationSeconds = requestInfo.DurationSeconds
		meta.VideoInputImageCount = len(requestInfo.InputImageURLs) + len(requestInfo.ReferenceImageURLs) + len(requestInfo.Uploads)
		// Keep the legacy media-unit counter populated for existing usage displays.
		meta.ImageCount = 1
	}
	return meta
}

func extractGrokMediaVideoRequestID(body []byte) string {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return ""
	}
	for _, path := range []string{"request_id", "id", "data.request_id", "data.id", "video.request_id", "video.id"} {
		if id := strings.TrimSpace(gjson.GetBytes(body, path).String()); id != "" {
			return id
		}
	}
	return ""
}

func (s *OpenAIGatewayService) handleGrokMediaErrorResponse(
	ctx context.Context,
	resp *http.Response,
	c *gin.Context,
	account *Account,
	requestIDHeader string,
	requestedModel string,
) (*OpenAIForwardResult, error) {
	body := s.readUpstreamErrorBody(resp)
	// Reconcile readiness before configurable passthrough branches can return;
	// otherwise a Grok 429 can remain schedulable.
	s.handleGrokAccountUpstreamError(ctx, account, resp.StatusCode, resp.Header, body)
	upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(body)))
	if upstreamMsg == "" {
		upstreamMsg = fmt.Sprintf("xAI upstream returned status %d", resp.StatusCode)
	}

	upstreamDetail := ""
	if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
		maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
		if maxBytes <= 0 {
			maxBytes = 2048
		}
		upstreamDetail = truncateString(string(body), maxBytes)
	}
	setOpsUpstreamError(c, resp.StatusCode, upstreamMsg, upstreamDetail)

	if status, errType, errMsg, matched := applyErrorPassthroughRule(
		c,
		account.Platform,
		resp.StatusCode,
		body,
		http.StatusBadGateway,
		"upstream_error",
		"Upstream request failed",
	); matched {
		MarkResponseCommitted(c)
		writeGrokMediaErrorResponse(c, status, errType, errMsg)
		return nil, fmt.Errorf("upstream error: %d (passthrough rule matched) message=%s", resp.StatusCode, upstreamMsg)
	}

	if !account.ShouldHandleErrorCode(resp.StatusCode) {
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform:           account.Platform,
			AccountID:          account.ID,
			AccountName:        account.Name,
			UpstreamStatusCode: resp.StatusCode,
			UpstreamRequestID:  requestIDHeader,
			Kind:               "http_error",
			Message:            upstreamMsg,
			Detail:             upstreamDetail,
		})
		MarkResponseCommitted(c)
		writeGrokMediaErrorResponse(c, http.StatusInternalServerError, "upstream_error", "Upstream gateway error")
		return nil, fmt.Errorf("upstream error: %d (not in custom error codes) message=%s", resp.StatusCode, upstreamMsg)
	}

	kind := "http_error"
	if s.shouldFailoverUpstreamError(resp.StatusCode) {
		kind = "failover"
	}
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		Platform:           account.Platform,
		AccountID:          account.ID,
		AccountName:        account.Name,
		UpstreamStatusCode: resp.StatusCode,
		UpstreamRequestID:  requestIDHeader,
		Kind:               kind,
		Message:            upstreamMsg,
		Detail:             upstreamDetail,
	})
	if kind == "failover" {
		return nil, &UpstreamFailoverError{
			StatusCode:             resp.StatusCode,
			ResponseBody:           body,
			ResponseHeaders:        resp.Header.Clone(),
			RetryableOnSameAccount: account.IsPoolMode() && account.IsPoolModeRetryableStatus(resp.StatusCode),
		}
	}

	MarkResponseCommitted(c)
	writeGrokMediaErrorResponse(c, resp.StatusCode, grokMediaErrorType(resp.StatusCode), upstreamMsg)
	return nil, fmt.Errorf("upstream error: %d %s", resp.StatusCode, upstreamMsg)
}

func grokMediaErrorType(statusCode int) string {
	switch statusCode {
	case http.StatusBadRequest:
		return "invalid_request_error"
	case http.StatusNotFound:
		return "not_found_error"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	default:
		return "upstream_error"
	}
}

func writeGrokMediaErrorResponse(c *gin.Context, statusCode int, errType, message string) {
	if c == nil || c.Writer == nil || c.Writer.Written() {
		return
	}
	c.JSON(statusCode, gin.H{
		"error": gin.H{
			"type":    strings.TrimSpace(errType),
			"message": strings.TrimSpace(message),
		},
	})
}

func writeGrokMediaResponse(c *gin.Context, resp *http.Response, body []byte, filter *responseheaders.CompiledHeaderFilter) {
	if c == nil || resp == nil {
		return
	}
	writeOpenAIPassthroughResponseHeaders(c.Writer.Header(), resp.Header, filter)
	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = "application/json"
	}
	c.Data(resp.StatusCode, contentType, body)
}
