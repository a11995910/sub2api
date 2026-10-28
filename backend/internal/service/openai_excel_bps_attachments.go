package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
)

type excelBPSAttachmentError struct {
	status         int
	retryAfter     string
	upstreamStatus int
	stage, kind    string
	cause          error
}

func (e *excelBPSAttachmentError) Error() string {
	return fmt.Sprintf("excel BPS attachment failed: %s/%s (status %d)", e.stage, e.kind, e.status)
}

func (e *excelBPSAttachmentError) Unwrap() error { return e.cause }

// 仅保留允许的诊断项和上下文错误，不保存原始地址、正文、文件 ID 或含凭据的传输错误。
func excelBPSAttachmentFailure(stage, kind string, upstreamStatus int, cause error) *excelBPSAttachmentError {
	failure := &excelBPSAttachmentError{status: http.StatusBadGateway, stage: stage, kind: kind, upstreamStatus: upstreamStatus}
	if errors.Is(cause, context.Canceled) {
		failure.cause = context.Canceled
	} else if errors.Is(cause, context.DeadlineExceeded) {
		failure.cause = context.DeadlineExceeded
	}
	return failure
}

func (s *OpenAIGatewayService) uploadExcelBPSAttachment(ctx context.Context, account *Account, token, accountID, proxyURL string, img basispoints.InlineAttachment) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	ctx = WithHTTPUpstreamRedirectsDisabled(WithHTTPUpstreamProfile(ctx, HTTPUpstreamProfileOpenAI))
	reader, contentType, length, err := img.Multipart()
	if err != nil {
		return "", excelBPSAttachmentFailure("attachment_prepare", "multipart", 0, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, basispoints.AttachmentsURL, reader)
	if err != nil {
		return "", excelBPSAttachmentFailure("attachment_prepare", "request_construction", 0, err)
	}
	auth, err := newExcelBPSRequest(ctx, nil, token, accountID)
	if err != nil {
		return "", excelBPSAttachmentFailure("attachment_prepare", "authentication", 0, err)
	}
	req.Header = auth.Header
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "identity")
	req.ContentLength = length
	resp, err := s.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		return "", excelBPSAttachmentFailure("attachment_transport", "transport_error", 0, err)
	}
	if resp == nil || resp.Body == nil {
		return "", excelBPSAttachmentFailure("attachment_response", "missing_response", 0, nil)
	}
	// 发起生成前释放上传占用的上游连接槽。
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		status := resp.StatusCode
		if status < 400 || status > 599 {
			status = http.StatusBadGateway
		}
		failure := excelBPSAttachmentFailure("attachment_http", "upstream_http", resp.StatusCode, nil)
		failure.status, failure.retryAfter = status, resp.Header.Get("Retry-After")
		return "", failure
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (64<<10)+1))
	if err != nil {
		return "", excelBPSAttachmentFailure("attachment_response", "transport_error", resp.StatusCode, err)
	}
	if len(raw) > 64<<10 {
		return "", excelBPSAttachmentFailure("attachment_response", "response_too_large", resp.StatusCode, nil)
	}
	var result struct {
		OpenAIFileID string `json:"openai_file_id"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return "", excelBPSAttachmentFailure("attachment_response", "invalid_json", resp.StatusCode, nil)
	}
	if !basispoints.ValidAttachmentID(result.OpenAIFileID) {
		return "", excelBPSAttachmentFailure("attachment_response", "invalid_file_id", resp.StatusCode, nil)
	}
	return result.OpenAIFileID, nil
}
