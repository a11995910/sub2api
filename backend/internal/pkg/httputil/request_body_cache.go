package httputil

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"
	"sync"
)

// requestBodyCacheKey 保持私有，避免把 body 缓存变成跨包的上下文契约。
type requestBodyCacheKey struct{}

type requestBodyCache struct {
	mu   sync.RWMutex
	body []byte
}

// requestBodyDecompressedLimitKey 保存当前请求允许的解压后请求体上限。
// 网关准入中间件按路由写入，body 读取器据此覆盖全局解压上限。
type requestBodyDecompressedLimitKey struct{}

// RequestBodyMaterializationStage 表示请求体已经完成的物化阶段。
// 准入层据此把读取前的最坏预留收缩到实际仍需长期持有的大小。
type RequestBodyMaterializationStage uint8

const (
	RequestBodyMaterializedDecoded RequestBodyMaterializationStage = iota + 1
	RequestBodyMaterializedStable
)

type requestBodyMaterializationObserver func(RequestBodyMaterializationStage, int64)
type requestBodyMaterializationObserverKey struct{}

// WithRequestBodyMaterializationObserver 为请求安装物化阶段观察器。
// observer 只用于进程内资源记账，不得修改请求体。
func WithRequestBodyMaterializationObserver(
	req *http.Request,
	observer func(RequestBodyMaterializationStage, int64),
) *http.Request {
	if req == nil || observer == nil {
		return req
	}
	return req.WithContext(context.WithValue(
		req.Context(),
		requestBodyMaterializationObserverKey{},
		requestBodyMaterializationObserver(observer),
	))
}

func notifyRequestBodyMaterialized(req *http.Request, stage RequestBodyMaterializationStage, bodyBytes int64) {
	if req == nil || bodyBytes < 0 {
		return
	}
	observer, _ := req.Context().Value(requestBodyMaterializationObserverKey{}).(requestBodyMaterializationObserver)
	if observer != nil {
		observer(stage, bodyBytes)
	}
}

// NotifyRequestBodyStable 表示入口请求体已完成解压、规范化或最终媒体解析，
// 可以从读取阶段最坏预留切换到稳定阶段副本余量。后续转发仍可能创建副本，
// 因此该通知不表示请求体只剩一份，也不能在解析失败时调用。
func NotifyRequestBodyStable(req *http.Request, bodyBytes int64) {
	notifyRequestBodyMaterialized(req, RequestBodyMaterializedStable, bodyBytes)
}

// WithDecompressedBodyLimit 将请求级解压上限写入 request.Context。
// limit <= 0 表示不覆盖全局上限；函数返回的 request 可能是副本，调用方
// 必须接收返回值。
func WithDecompressedBodyLimit(req *http.Request, limit int64) *http.Request {
	if req == nil || limit <= 0 {
		return req
	}
	return req.WithContext(context.WithValue(req.Context(), requestBodyDecompressedLimitKey{}, limit))
}

// DecompressedBodyLimit 返回请求级解压上限；未设置时返回 0。
func DecompressedBodyLimit(req *http.Request) int64 {
	if req == nil {
		return 0
	}
	limit, _ := req.Context().Value(requestBodyDecompressedLimitKey{}).(int64)
	return limit
}

// WithCachedRequestBody 保存解码或改写后的 body，并重置请求 reader。
// 如果 req 尚未安装缓存，会返回带新 context 的请求，调用方必须接收返回值。
// 替换 reader 后旧 reader 不再由调用方使用，并会被关闭以释放连接和准入槽位。
func WithCachedRequestBody(req *http.Request, body []byte) *http.Request {
	if req == nil {
		return nil
	}
	if cached, ok := req.Context().Value(requestBodyCacheKey{}).(*requestBodyCache); ok && cached != nil {
		cached.mu.Lock()
		cached.body = body
		cached.mu.Unlock()
		ResetRequestBody(req, body)
		return req
	}
	cached := &requestBodyCache{body: body}
	cloned := req.WithContext(context.WithValue(req.Context(), requestBodyCacheKey{}, cached))
	ResetRequestBody(cloned, body)
	return cloned
}

// CachedRequestBody 返回 WithCachedRequestBody 保存的 body。返回的切片属于请求，
// 调用方必须按只读数据处理。
func CachedRequestBody(req *http.Request) ([]byte, bool) {
	if req == nil {
		return nil, false
	}
	cached, ok := req.Context().Value(requestBodyCacheKey{}).(*requestBodyCache)
	if !ok || cached == nil {
		return nil, false
	}
	cached.mu.RLock()
	body := cached.body
	cached.mu.RUnlock()
	return body, true
}

// ResetRequestBody 用 body 替换请求 reader，并更新解码后 body 的长度和编码头。
func ResetRequestBody(req *http.Request, body []byte) {
	if req == nil {
		return
	}
	oldBody := req.Body
	req.Body = io.NopCloser(bytes.NewReader(body))
	// 中间件改写或解码 body 后，保留重试和回退所需的可重放 reader。
	// 捕获的切片归请求所有，后续必须保持不可变。
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	req.ContentLength = int64(len(body))
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	req.Header.Del("Content-Encoding")
	req.Header.Set("Content-Length", strconv.FormatInt(int64(len(body)), 10))
	closeReplacedRequestBody(oldBody)
}

func closeReplacedRequestBody(oldBody io.ReadCloser) {
	if oldBody == nil {
		return
	}
	_ = oldBody.Close()
}
