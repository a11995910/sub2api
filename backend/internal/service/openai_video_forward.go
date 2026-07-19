package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
	"github.com/gin-gonic/gin"
)

const openAIVideoProtocolCacheTTL = 24 * time.Hour

func (s *OpenAIGatewayService) ForwardOpenAIVideoCreate(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	defaultMappedModel string,
) (*OpenAIForwardResult, error) {
	if account == nil || account.Platform != PlatformOpenAI || account.Type != AccountTypeAPIKey {
		return nil, fmt.Errorf("openai video requires an OpenAI API key account")
	}
	requestedModel := strings.TrimSpace(firstGJSONVideoString(body, "model"))
	upstreamModel := resolveOpenAIForwardModel(account, requestedModel, defaultMappedModel)
	normalizedBody, requestInfo, err := NormalizeOpenAIVideoCreateBody(body, upstreamModel)
	if err != nil {
		return nil, err
	}

	protocol := s.getCachedOpenAIVideoProtocol(ctx, account.ID, upstreamModel)
	if protocol == OpenAIVideoProtocolChatCompletions {
		return s.forwardOpenAIVideoViaChatCompletions(ctx, c, account, requestInfo, requestedModel, upstreamModel)
	}

	result, endpointUnsupported, err := s.forwardOpenAIVideoCreateTask(
		ctx, c, account, normalizedBody, requestInfo, requestedModel, upstreamModel,
	)
	if !endpointUnsupported {
		return result, err
	}
	s.setCachedOpenAIVideoProtocol(ctx, account.ID, upstreamModel, OpenAIVideoProtocolChatCompletions)
	return s.forwardOpenAIVideoViaChatCompletions(ctx, c, account, requestInfo, requestedModel, upstreamModel)
}

func (s *OpenAIGatewayService) ForwardOpenAIVideoStatus(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	taskID string,
) (*OpenAIForwardResult, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, fmt.Errorf("video task_id is required")
	}
	resp, startTime, err := s.sendOpenAIVideoLookup(ctx, c, account, "/"+url.PathEscape(taskID), "application/json", false)
	if err != nil {
		return nil, err
	}
	defer func(body io.ReadCloser) { _ = body.Close() }(resp.Body)
	body, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		message := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(body)))
		if message == "" {
			message = fmt.Sprintf("video upstream returned status %d", resp.StatusCode)
		}
		writeChatCompletionsError(c, resp.StatusCode, "upstream_error", message)
		return nil, errors.New(message)
	}
	videoResult, err := ParseOpenAIVideoResult(body)
	if err != nil {
		return nil, err
	}
	if videoResult.TaskID == "" {
		videoResult.TaskID = taskID
	}
	response := map[string]any{
		"id":       videoResult.TaskID,
		"task_id":  videoResult.TaskID,
		"object":   "video",
		"model":    videoResult.Model,
		"status":   videoResult.Status,
		"progress": videoResult.Progress,
	}
	if videoResult.Status == "completed" {
		response["url"] = grokMediaContentProxyURL(c, videoResult.TaskID)
	}
	if videoResult.Status == "failed" && videoResult.ErrorMessage != "" {
		response["error"] = map[string]string{
			"type":    "generation_failed",
			"message": videoResult.ErrorMessage,
		}
	}
	normalized, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("encode video status response: %w", err)
	}
	writeGrokMediaResponse(c, resp, normalized, s.responseHeaderFilter)
	return &OpenAIForwardResult{
		RequestID:        firstNonEmpty(resp.Header.Get("x-request-id"), videoResult.TaskID),
		ResponseID:       videoResult.TaskID,
		Model:            videoResult.Model,
		UpstreamEndpoint: "/v1/videos/{task_id}",
		ResponseHeaders:  resp.Header.Clone(),
		Duration:         time.Since(startTime),
	}, nil
}

func (s *OpenAIGatewayService) ForwardOpenAIVideoContent(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	taskID string,
) (*OpenAIForwardResult, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, fmt.Errorf("video task_id is required")
	}
	resp, startTime, err := s.sendOpenAIVideoLookup(
		WithHTTPUpstreamRedirectsDisabled(ctx),
		c,
		account,
		"/"+url.PathEscape(taskID)+"/content",
		"video/mp4,video/*;q=0.9,application/octet-stream;q=0.8",
		true,
	)
	if err != nil {
		return nil, err
	}
	defer func(body io.ReadCloser) { _ = body.Close() }(resp.Body)
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		_ = resp.Body.Close()
		contentURL, resolveErr := s.resolveOpenAIVideoStatusContentURL(ctx, c, account, taskID)
		if resolveErr != nil {
			return nil, resolveErr
		}
		resp, err = s.sendOpenAIVideoPublicContent(ctx, c, account, contentURL)
		if err != nil {
			return nil, err
		}
		defer func(body io.ReadCloser) { _ = body.Close() }(resp.Body)
	}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return nil, fmt.Errorf("video content redirect is not allowed")
	}
	if resp.StatusCode >= 400 && resp.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		body, readErr := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
		if readErr != nil {
			return nil, readErr
		}
		message := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(body)))
		if message == "" {
			message = fmt.Sprintf("video upstream returned status %d", resp.StatusCode)
		}
		writeChatCompletionsError(c, resp.StatusCode, "upstream_error", message)
		return nil, errors.New(message)
	}
	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	mediaType, _, parseErr := mime.ParseMediaType(contentType)
	if parseErr != nil || (!strings.HasPrefix(strings.ToLower(mediaType), "video/") && mediaType != "application/octet-stream") {
		return nil, fmt.Errorf("video upstream returned unsupported content type %q", contentType)
	}
	maxBytes := resolveUpstreamResponseReadLimit(s.cfg)
	if resp.ContentLength > maxBytes {
		return nil, fmt.Errorf("video upstream content exceeds size limit")
	}
	if err := writeOpenAIVideoContentResponse(c, resp, maxBytes); err != nil {
		return nil, err
	}
	return &OpenAIForwardResult{
		RequestID:        resp.Header.Get("x-request-id"),
		ResponseID:       taskID,
		UpstreamEndpoint: "/v1/videos/{task_id}/content",
		ResponseHeaders:  resp.Header.Clone(),
		Duration:         time.Since(startTime),
	}, nil
}

func (s *OpenAIGatewayService) sendOpenAIVideoLookup(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	suffix string,
	accept string,
	forwardRange bool,
) (*http.Response, time.Time, error) {
	if account == nil || account.Platform != PlatformOpenAI || account.Type != AccountTypeAPIKey {
		return nil, time.Time{}, fmt.Errorf("openai video lookup requires an OpenAI API key account")
	}
	token := strings.TrimSpace(account.GetOpenAIApiKey())
	if token == "" {
		return nil, time.Time{}, fmt.Errorf("account %d missing api_key", account.ID)
	}
	targetURL, err := s.openAIVideoTargetURL(account, suffix)
	if err != nil {
		return nil, time.Time{}, err
	}
	upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
	defer releaseUpstreamCtx()
	req, err := http.NewRequestWithContext(upstreamCtx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("build video lookup request: %w", err)
	}
	req = req.WithContext(WithHTTPUpstreamProfile(req.Context(), HTTPUpstreamProfileOpenAI))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", accept)
	if forwardRange && c != nil {
		if rangeHeader := strings.TrimSpace(c.GetHeader("Range")); rangeHeader != "" {
			req.Header.Set("Range", rangeHeader)
		}
	}
	account.ApplyHeaderOverrides(req.Header)
	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	startTime := time.Now()
	resp, err := s.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(startTime).Milliseconds())
	if err != nil {
		return nil, time.Time{}, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	return resp, startTime, nil
}

func (s *OpenAIGatewayService) resolveOpenAIVideoStatusContentURL(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	taskID string,
) (string, error) {
	resp, _, err := s.sendOpenAIVideoLookup(
		WithHTTPUpstreamRedirectsDisabled(ctx),
		c,
		account,
		"/"+url.PathEscape(taskID),
		"application/json",
		false,
	)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return "", fmt.Errorf("video status redirect is not allowed")
	}
	body, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("video upstream returned status %d", resp.StatusCode)
	}
	result, err := ParseOpenAIVideoResult(body)
	if err != nil {
		return "", err
	}
	parsed, err := validateOpenAIVideoContentURL(ctx, result.VideoURL)
	if err != nil {
		return "", err
	}
	return parsed.String(), nil
}

func (s *OpenAIGatewayService) sendOpenAIVideoPublicContent(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	contentURL string,
) (*http.Response, error) {
	upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
	defer releaseUpstreamCtx()
	req, err := http.NewRequestWithContext(
		WithHTTPUpstreamRedirectsDisabled(upstreamCtx),
		http.MethodGet,
		contentURL,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("build video content request: %w", err)
	}
	req = req.WithContext(WithHTTPUpstreamProfile(req.Context(), HTTPUpstreamProfileOpenAI))
	req.Header.Set("Accept", "video/mp4,video/*;q=0.9,application/octet-stream;q=0.8")
	if c != nil {
		if rangeHeader := strings.TrimSpace(c.GetHeader("Range")); rangeHeader != "" {
			req.Header.Set("Range", rangeHeader)
		}
	}
	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	resp, err := s.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	return resp, nil
}

func validateOpenAIVideoContentURL(ctx context.Context, rawURL string) (*url.URL, error) {
	validated, err := urlvalidator.ValidateHTTPSURL(rawURL, urlvalidator.ValidationOptions{})
	if err != nil {
		return nil, fmt.Errorf("video content URL must be a public HTTPS URL")
	}
	parsed, err := url.Parse(validated)
	if err != nil || parsed.User != nil || strings.TrimSpace(parsed.Hostname()) == "" {
		return nil, fmt.Errorf("video content URL must be a public HTTPS URL")
	}
	if err := urlvalidator.ValidateResolvedIP(parsed.Hostname()); err != nil {
		return nil, fmt.Errorf("video content URL must be a public HTTPS URL")
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
	return parsed, nil
}

func writeOpenAIVideoContentResponse(c *gin.Context, resp *http.Response, maxBytes int64) error {
	if c == nil || resp == nil || resp.Body == nil {
		return fmt.Errorf("video content response is incomplete")
	}
	if maxBytes <= 0 {
		maxBytes = defaultUpstreamResponseReadMaxBytes
	}
	if rangeHeader := strings.TrimSpace(c.GetHeader("Range")); resp.StatusCode == http.StatusOK &&
		isOpenAIVideoSingleRange(rangeHeader) &&
		strings.TrimSpace(c.GetHeader("If-Range")) == "" &&
		resp.ContentLength >= 0 {
		return writeOpenAIVideoLocalRange(c, resp, rangeHeader)
	}
	for _, name := range []string{
		"Content-Type",
		"Content-Length",
		"Content-Range",
		"Accept-Ranges",
		"Content-Disposition",
	} {
		if value := strings.TrimSpace(resp.Header.Get(name)); value != "" {
			c.Header(name, value)
		}
	}
	if strings.TrimSpace(c.Writer.Header().Get("Content-Length")) == "" && resp.ContentLength >= 0 {
		c.Header("Content-Length", fmt.Sprintf("%d", resp.ContentLength))
	}
	c.Status(resp.StatusCode)
	MarkResponseCommitted(c)
	limited := &io.LimitedReader{R: resp.Body, N: maxBytes}
	if _, err := io.Copy(c.Writer, limited); err != nil {
		return err
	}
	if limited.N == 0 {
		var probe [1]byte
		if n, err := resp.Body.Read(probe[:]); n > 0 {
			return fmt.Errorf("%w: limit=%d", ErrUpstreamResponseBodyTooLarge, maxBytes)
		} else if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
	}
	return nil
}

func isOpenAIVideoSingleRange(header string) bool {
	if !strings.HasPrefix(header, "bytes=") {
		return false
	}
	spec := strings.TrimSpace(strings.TrimPrefix(header, "bytes="))
	return spec != "" && !strings.Contains(spec, ",")
}

func writeOpenAIVideoLocalRange(c *gin.Context, resp *http.Response, rangeHeader string) error {
	start, end, err := parseOpenAIVideoSingleRange(rangeHeader, resp.ContentLength)
	if err != nil {
		c.Header("Content-Range", fmt.Sprintf("bytes */%d", resp.ContentLength))
		c.Header("Content-Length", "0")
		c.Status(http.StatusRequestedRangeNotSatisfiable)
		MarkResponseCommitted(c)
		return nil
	}
	if start > 0 {
		if _, err := io.CopyN(io.Discard, resp.Body, start); err != nil {
			return fmt.Errorf("skip video content to requested range: %w", err)
		}
	}
	length := end - start + 1
	for _, name := range []string{"Content-Type", "Content-Disposition"} {
		if value := strings.TrimSpace(resp.Header.Get(name)); value != "" {
			c.Header(name, value)
		}
	}
	c.Header("Accept-Ranges", "bytes")
	c.Header("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, resp.ContentLength))
	c.Header("Content-Length", strconv.FormatInt(length, 10))
	c.Status(http.StatusPartialContent)
	MarkResponseCommitted(c)
	if _, err := io.CopyN(c.Writer, resp.Body, length); err != nil {
		return fmt.Errorf("copy requested video content range: %w", err)
	}
	return nil
}

func parseOpenAIVideoSingleRange(header string, size int64) (int64, int64, error) {
	header = strings.TrimSpace(header)
	if size <= 0 || !strings.HasPrefix(header, "bytes=") {
		return 0, 0, fmt.Errorf("invalid video content range")
	}
	spec := strings.TrimSpace(strings.TrimPrefix(header, "bytes="))
	if spec == "" || strings.Contains(spec, ",") {
		return 0, 0, fmt.Errorf("only one video content range is supported")
	}
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid video content range")
	}
	if parts[0] == "" {
		suffixLength, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || suffixLength <= 0 {
			return 0, 0, fmt.Errorf("invalid video content suffix range")
		}
		if suffixLength > size {
			suffixLength = size
		}
		return size - suffixLength, size - 1, nil
	}

	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || start < 0 || start >= size {
		return 0, 0, fmt.Errorf("video content range start is unsatisfiable")
	}
	end := size - 1
	if parts[1] != "" {
		end, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil || end < start {
			return 0, 0, fmt.Errorf("invalid video content range end")
		}
		if end >= size {
			end = size - 1
		}
	}
	return start, end, nil
}

func (s *OpenAIGatewayService) forwardOpenAIVideoCreateTask(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	requestInfo OpenAIVideoRequest,
	requestedModel string,
	upstreamModel string,
) (*OpenAIForwardResult, bool, error) {
	token := strings.TrimSpace(account.GetOpenAIApiKey())
	if token == "" {
		return nil, false, fmt.Errorf("account %d missing api_key", account.ID)
	}
	targetURL, err := s.openAIVideoTargetURL(account, "")
	if err != nil {
		return nil, false, err
	}
	upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
	defer releaseUpstreamCtx()
	req, err := http.NewRequestWithContext(upstreamCtx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, false, fmt.Errorf("build video create request: %w", err)
	}
	req = req.WithContext(WithHTTPUpstreamProfile(req.Context(), HTTPUpstreamProfileOpenAI))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	account.ApplyHeaderOverrides(req.Header)

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	startTime := time.Now()
	upstreamStart := time.Now()
	resp, err := s.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
	if err != nil {
		return nil, false, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return nil, false, err
	}
	if resp.StatusCode >= 400 {
		if IsOpenAIVideoEndpointUnsupported(resp.StatusCode, respBody) {
			return nil, true, nil
		}
		if resp.StatusCode == http.StatusBadRequest {
			s.setCachedOpenAIVideoProtocol(ctx, account.ID, upstreamModel, OpenAIVideoProtocolVideos)
		}
		resp.Body = io.NopCloser(bytes.NewReader(respBody))
		upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
		if failoverErr := s.failoverOpenAIUpstreamHTTPError(ctx, c, account, resp, respBody, upstreamMsg, upstreamModel); failoverErr != nil {
			return nil, false, failoverErr
		}
		if upstreamMsg == "" {
			upstreamMsg = fmt.Sprintf("video upstream returned status %d", resp.StatusCode)
		}
		writeChatCompletionsError(c, resp.StatusCode, "upstream_error", upstreamMsg)
		return nil, false, errors.New(upstreamMsg)
	}

	videoResult, err := ParseOpenAIVideoResult(respBody)
	if err != nil {
		return nil, false, err
	}
	if strings.TrimSpace(videoResult.TaskID) == "" {
		writeChatCompletionsError(c, http.StatusBadGateway, "upstream_error", "Video upstream response did not include task_id")
		return nil, false, fmt.Errorf("video upstream response did not include task_id")
	}
	if videoResult.Status == "" {
		videoResult.Status = "queued"
	}
	if videoMeta, ok := openAIVideoContextFromGin(c); ok && videoMeta.BindTask {
		groupID := videoMeta.GroupID
		if err := s.BindVideoTaskAccount(ctx, &groupID, videoResult.TaskID, videoMeta.UserID, videoMeta.APIKeyID, account.ID); err != nil {
			writeChatCompletionsError(c, http.StatusBadGateway, "upstream_error", "Failed to bind video task")
			return nil, false, fmt.Errorf("bind video task: %w", err)
		}
	}
	normalizedResponse, err := json.Marshal(map[string]any{
		"id":       videoResult.TaskID,
		"task_id":  videoResult.TaskID,
		"object":   "video",
		"model":    requestedModel,
		"status":   videoResult.Status,
		"progress": videoResult.Progress,
	})
	if err != nil {
		return nil, false, fmt.Errorf("encode video create response: %w", err)
	}
	if s.responseHeaderFilter != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
	}
	c.Header("Content-Type", "application/json")
	c.Status(http.StatusOK)
	_, _ = c.Writer.Write(normalizedResponse)
	s.setCachedOpenAIVideoProtocol(ctx, account.ID, upstreamModel, OpenAIVideoProtocolVideos)
	SetActualOpenAIUpstreamEndpoint(c, "/v1/videos")
	return &OpenAIForwardResult{
		RequestID:            firstNonEmpty(resp.Header.Get("x-request-id"), videoResult.TaskID),
		ResponseID:           videoResult.TaskID,
		Model:                requestedModel,
		BillingModel:         requestedModel,
		UpstreamModel:        upstreamModel,
		UpstreamEndpoint:     "/v1/videos",
		ResponseHeaders:      resp.Header.Clone(),
		Duration:             time.Since(startTime),
		VideoCount:           1,
		VideoResolution:      requestInfo.Resolution,
		VideoDurationSeconds: requestInfo.DurationSeconds,
		VideoInputImageCount: len(requestInfo.ImageURLs),
	}, false, nil
}

func (s *OpenAIGatewayService) forwardOpenAIVideoViaChatCompletions(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	requestInfo OpenAIVideoRequest,
	requestedModel string,
	upstreamModel string,
) (*OpenAIForwardResult, error) {
	body, err := BuildOpenAIVideoChatRequest(requestInfo, upstreamModel)
	if err != nil {
		return nil, err
	}
	SetOpenAIVideoContext(c, OpenAIVideoContext{
		Model:               requestedModel,
		Resolution:          requestInfo.Resolution,
		DurationSeconds:     requestInfo.DurationSeconds,
		ReferenceImageCount: len(requestInfo.ImageURLs),
	})
	result, err := s.forwardAsRawChatCompletions(ctx, c, account, body, upstreamModel)
	if result != nil {
		result.Model = requestedModel
		result.BillingModel = requestedModel
		result.UpstreamModel = upstreamModel
		result.UpstreamEndpoint = "/v1/chat/completions"
	}
	return result, err
}

func BuildOpenAIVideoChatRequest(request OpenAIVideoRequest, upstreamModel string) ([]byte, error) {
	upstreamModel = strings.TrimSpace(upstreamModel)
	if upstreamModel == "" {
		return nil, fmt.Errorf("upstream model is required")
	}
	instruction := fmt.Sprintf("%s\n\n视频规格：%s，时长 %d 秒。", request.Prompt, request.Resolution, request.DurationSeconds)
	var content any = instruction
	if len(request.ImageURLs) > 0 {
		parts := []map[string]any{{"type": "text", "text": instruction}}
		for _, rawURL := range request.ImageURLs {
			parts = append(parts, map[string]any{
				"type":      "image_url",
				"image_url": map[string]string{"url": rawURL},
			})
		}
		content = parts
	}
	return json.Marshal(map[string]any{
		"model": upstreamModel,
		"messages": []map[string]any{{
			"role":    "user",
			"content": content,
		}},
		"stream": false,
	})
}

func (s *OpenAIGatewayService) openAIVideoTargetURL(account *Account, suffix string) (string, error) {
	baseURL := strings.TrimSpace(account.GetOpenAIBaseURL())
	if baseURL == "" {
		return "", fmt.Errorf("account %d missing base_url", account.ID)
	}
	validatedURL, err := s.validateUpstreamBaseURL(baseURL)
	if err != nil {
		return "", fmt.Errorf("invalid base_url: %w", err)
	}
	return buildOpenAIEndpointURL(validatedURL, "/v1/videos"+suffix), nil
}

func (s *OpenAIGatewayService) getCachedOpenAIVideoProtocol(ctx context.Context, accountID int64, mappedModel string) OpenAIVideoProtocol {
	cache, ok := s.cache.(OpenAIVideoProtocolCache)
	if !ok || cache == nil {
		return ""
	}
	protocol, err := cache.GetOpenAIVideoProtocol(ctx, accountID, mappedModel)
	if err != nil {
		return ""
	}
	return protocol
}

func (s *OpenAIGatewayService) setCachedOpenAIVideoProtocol(ctx context.Context, accountID int64, mappedModel string, protocol OpenAIVideoProtocol) {
	cache, ok := s.cache.(OpenAIVideoProtocolCache)
	if !ok || cache == nil {
		return
	}
	_ = cache.SetOpenAIVideoProtocol(ctx, accountID, mappedModel, protocol, openAIVideoProtocolCacheTTL)
}

func (s *OpenAIGatewayService) ResolveOpenAIVideoTaskAccount(
	ctx context.Context,
	groupID *int64,
	taskID string,
	userID, apiKeyID int64,
) (*Account, error) {
	accountID, err := s.ResolveVideoTaskAccount(ctx, groupID, taskID, userID, apiKeyID)
	if err != nil {
		return nil, err
	}
	var account *Account
	if s.schedulerSnapshot != nil {
		account, err = s.schedulerSnapshot.GetAccount(ctx, accountID)
	} else if s.accountRepo != nil {
		account, err = s.accountRepo.GetByID(ctx, accountID)
	} else {
		return nil, fmt.Errorf("video account repository is unavailable")
	}
	if err != nil {
		return nil, err
	}
	if account == nil || account.Platform != PlatformOpenAI || account.Type != AccountTypeAPIKey || !account.IsActive() {
		return nil, fmt.Errorf("bound video account is unavailable")
	}
	return account, nil
}
