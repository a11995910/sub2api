package service

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
)

var errOpenAIUpstreamModelMismatch = errors.New("上游响应模型与请求模型不一致")

func (o *openAIHealthyTurnStateObserver) observeModel(payload []byte) bool {
	model := firstValidTrimmedGJSONString(payload, "response.model", "model")
	if model == "" || strings.TrimSpace(o.attempt.scope.model) == "" {
		return false
	}
	o.modelObserved = true
	if mismatch := upstreamModelMismatch(o.attempt.scope.model, model); mismatch != nil && *mismatch {
		o.modelMismatch, o.failed = true, true
		o.finish()
		return true
	}
	return false
}

func newOpenAIHealthyModelPreview(model string) *openAIHealthyTurnStateObserver {
	return &openAIHealthyTurnStateObserver{
		attempt: &openAIHealthyTurnStateAttempt{scope: openAIHealthyTurnStateScope{model: model}},
		preview: true,
	}
}

func (o *openAIHealthyTurnStateObserver) modelPreviewDone() bool {
	return o.modelObserved || o.healthy || o.terminal || o.failed
}

// 有界检查首个模型声明；未声明模型时最多等待首个有效输出或 1 MiB。
// 已检查字节原样交回转发层，后续声明仍由完整响应观察器校验。
func inspectOpenAIHealthyTurnStateHTTP(response *http.Response, model string) (bool, error) {
	preview := newOpenAIHealthyModelPreview(model)
	body := newOpenAIHealthyTurnStateBody(response, preview)
	var prefix bytes.Buffer
	buffer := make([]byte, 4096)
	for prefix.Len() < openAIHealthyTurnStateEventMaxBytes && !preview.modelPreviewDone() {
		n, err := body.Read(buffer[:min(len(buffer), openAIHealthyTurnStateEventMaxBytes-prefix.Len())])
		prefix.Write(buffer[:n])
		if err != nil {
			if err != io.EOF {
				return false, err
			}
			break
		}
	}
	response.Body = &openAIHealthyPrefixBody{Reader: io.MultiReader(bytes.NewReader(prefix.Bytes()), response.Body), Closer: response.Body}
	return preview.modelMismatch, nil
}

type openAIHealthyPrefixBody struct {
	io.Reader
	io.Closer
}

func (a *openAIHealthyTurnStateAttempt) rejectModelMismatch(headers http.Header, current string) {
	a.failed()
	for _, value := range []string{current, extractOpenAICodexTurnState(headers)} {
		a.cache.reject(a.scope, openAIHealthyTurnStateEntry{value: value})
	}
}

func openAIModelMismatchHTTPResponse(response *http.Response) *http.Response {
	if response.Body != nil {
		_ = response.Body.Close()
	}
	const body = `{"error":{"type":"upstream_error","code":"upstream_model_mismatch","message":"上游响应模型与请求模型不一致"}}`
	response.StatusCode = http.StatusBadGateway
	response.Status = "502 Bad Gateway"
	response.Header = response.Header.Clone()
	response.Header.Del(openAICodexTurnStateHeader)
	response.Header.Del("Content-Encoding")
	response.Header.Del("Content-Length")
	response.Header.Set("Content-Type", "application/json")
	response.ContentLength = int64(len(body))
	response.Body = io.NopCloser(strings.NewReader(body))
	return response
}
