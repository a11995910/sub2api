package httputil

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"testing"
)

// countingBody 用于区分缓存命中和再次读取网络请求体。
type countingBody struct {
	reader *bytes.Reader
	reads  int
}

func (b *countingBody) Read(p []byte) (int, error) {
	b.reads++
	return b.reader.Read(p)
}

func (b *countingBody) Close() error { return nil }

type closeTrackingBody struct {
	*bytes.Reader
	closed int
}

func (b *closeTrackingBody) Close() error {
	b.closed++
	return nil
}

func TestCachedRequestBodyPreventsRepeatedUnderlyingReads(t *testing.T) {
	const payload = `{"model":"gpt-5.5","input":"hello"}`
	original := &countingBody{reader: bytes.NewReader([]byte(payload))}
	req, err := http.NewRequest(http.MethodPost, "/v1/responses", original)
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}

	body, err := ReadRequestBodyWithPrealloc(req)
	if err != nil {
		t.Fatalf("initial body read: %v", err)
	}
	if string(body) != payload {
		t.Fatalf("initial body mismatch: %q", body)
	}
	readsAfterInitialRead := original.reads
	if readsAfterInitialRead == 0 {
		t.Fatal("expected the uncached request body to be read")
	}

	// 首次安装缓存会返回带新 context 的请求；下游必须使用返回值。
	cachedReq := WithCachedRequestBody(req, body)
	if cachedReq == req {
		t.Fatal("expected cache installation to return a request with the cache context")
	}

	for i := 0; i < 2; i++ {
		got, readErr := ReadRequestBodyWithPrealloc(cachedReq)
		if readErr != nil {
			t.Fatalf("cached read %d: %v", i, readErr)
		}
		if string(got) != payload {
			t.Fatalf("cached body %d mismatch: %q", i, got)
		}
	}
	if original.reads != readsAfterInitialRead {
		t.Fatalf("cache hit reread underlying body: reads before=%d after=%d", readsAfterInitialRead, original.reads)
	}

	if _, ok := CachedRequestBody(req); ok {
		t.Fatal("original request unexpectedly acquired the cache context without assignment")
	}
	if got, ok := CachedRequestBody(cachedReq); !ok || string(got) != payload {
		t.Fatalf("cached body not retained: %q, %v", got, ok)
	}
}

func TestWithCachedRequestBodyUpdatesExistingCacheAndRequestHeaders(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader([]byte("compressed")))
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("Content-Length", "999")
	req.ContentLength = 999

	first := WithCachedRequestBody(req, []byte(`{"model":"old"}`))
	secondBody := []byte(`{"model":"new"}`)
	second := WithCachedRequestBody(first, secondBody)
	if second != first {
		t.Fatal("updating an existing cache should keep the request identity")
	}

	got, ok := CachedRequestBody(second)
	if !ok || string(got) != string(secondBody) {
		t.Fatalf("updated cache mismatch: %q, %v", got, ok)
	}
	readBody, err := ReadRequestBodyWithPrealloc(second)
	if err != nil {
		t.Fatalf("read updated cache: %v", err)
	}
	if string(readBody) != string(secondBody) {
		t.Fatalf("updated reader mismatch: %q", readBody)
	}
	if second.Header.Get("Content-Encoding") != "" {
		t.Fatalf("Content-Encoding should be cleared, got %q", second.Header.Get("Content-Encoding"))
	}
	wantLength := int64(len(secondBody))
	if second.ContentLength != wantLength {
		t.Fatalf("ContentLength mismatch: got %d want %d", second.ContentLength, wantLength)
	}
	if second.Header.Get("Content-Length") != strconv.FormatInt(wantLength, 10) {
		t.Fatalf("Content-Length header mismatch: got %q", second.Header.Get("Content-Length"))
	}
}

func TestWithCachedRequestBodyKeepsGetBodyAndClosesReplacedReader(t *testing.T) {
	original := &closeTrackingBody{Reader: bytes.NewReader([]byte("original"))}
	req, err := http.NewRequest(http.MethodPost, "/v1/responses", original)
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}
	firstBody := []byte(`{"model":"first"}`)
	cached := WithCachedRequestBody(req, firstBody)
	if original.closed != 1 {
		t.Fatalf("original body close count = %d, want 1", original.closed)
	}
	getBody, err := cached.GetBody()
	if err != nil {
		t.Fatalf("GetBody: %v", err)
	}
	got, err := io.ReadAll(getBody)
	if err != nil {
		t.Fatalf("read GetBody: %v", err)
	}
	if !bytes.Equal(got, firstBody) {
		t.Fatalf("GetBody returned %q, want %q", got, firstBody)
	}

	replacement := &closeTrackingBody{Reader: bytes.NewReader([]byte("replacement"))}
	cached.Body = replacement
	secondBody := []byte(`{"model":"second"}`)
	WithCachedRequestBody(cached, secondBody)
	if replacement.closed != 1 {
		t.Fatalf("replacement body close count = %d, want 1", replacement.closed)
	}
	getBody, err = cached.GetBody()
	if err != nil {
		t.Fatalf("GetBody after update: %v", err)
	}
	got, err = io.ReadAll(getBody)
	if err != nil {
		t.Fatalf("read updated GetBody: %v", err)
	}
	if !bytes.Equal(got, secondBody) {
		t.Fatalf("updated GetBody returned %q, want %q", got, secondBody)
	}
}

func TestReadLenientJSONRequestBodyUsesCachedNormalizedBody(t *testing.T) {
	// 原始 payload 含有宽松路径需要转义的控制字符。
	raw := []byte("{\"input\":\"hello\x00world\"}")
	req, err := http.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}
	normalized, err := NormalizeLenientJSONRequestBody(raw, 1024)
	if err != nil {
		t.Fatalf("normalize body: %v", err)
	}
	req = WithCachedRequestBody(req, normalized)

	got, err := ReadLenientJSONRequestBodyWithPrealloc(req, 1024)
	if err != nil {
		t.Fatalf("read cached normalized body: %v", err)
	}
	if !bytes.Equal(got, normalized) {
		t.Fatalf("cached normalized body mismatch: got %q want %q", got, normalized)
	}
	if bytes.Contains(got, []byte{0}) {
		t.Fatal("cached lenient body still contains a raw control byte")
	}
}

func TestRequestBodyCacheHelpersHandleNilRequest(t *testing.T) {
	if got := WithCachedRequestBody(nil, []byte("body")); got != nil {
		t.Fatalf("WithCachedRequestBody(nil) = %v, want nil", got)
	}
	if body, ok := CachedRequestBody(nil); ok || body != nil {
		t.Fatalf("CachedRequestBody(nil) = %q, %v", body, ok)
	}
	ResetRequestBody(nil, []byte("body"))
}
