package middleware

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestIsAsyncImageTaskRead(t *testing.T) {
	require.True(t, isAsyncImageTaskRead(http.MethodGet, "/v1/images/tasks/imgtask_123"))
	require.True(t, isAsyncImageTaskRead(http.MethodGet, "/images/tasks/imgtask_123"))
	require.True(t, isAsyncImageTaskRead(http.MethodGet, "/v1/images/generations/imgtask_123"))
	require.True(t, isAsyncImageTaskRead(http.MethodGet, "/images/generations/imgtask_123"))
	require.False(t, isAsyncImageTaskRead(http.MethodPost, "/v1/images/tasks/imgtask_123"))
	require.False(t, isAsyncImageTaskRead(http.MethodGet, "/v1/images/generations"))
}

func TestIsAsyncImageTaskSubmitOnlyMatchesAsyncGeneration(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		body string
		want bool
	}{
		{name: "async generation", path: "/v1/images/generations", body: `{"async":true,"client_request_id":"request_1"}`, want: true},
		{name: "root async generation", path: "/images/generations", body: `{"async":true,"client_request_id":"request_2"}`, want: true},
		{name: "explicit sync", path: "/v1/images/generations", body: `{"async":false}`, want: false},
		{name: "legacy async path", path: "/v1/images/generations/async", body: `{"model":"gpt-image-2"}`, want: false},
		{name: "other endpoint", path: "/v1/images/edits", body: `{"async":true,"client_request_id":"request_1"}`, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = req
			controller := attachImageAdmissionController(c, 1024*1024)
			defer controller.releaseIfAttached()
			require.Equal(t, tc.want, isAsyncImageTaskSubmit(c))
			restored, err := io.ReadAll(c.Request.Body)
			require.NoError(t, err)
			require.Equal(t, tc.body, string(restored))
		})
	}
}

func attachImageAdmissionController(c *gin.Context, limit int64) *requestBodyAdmissionController {
	controller := &requestBodyAdmissionController{
		budget: NewBodyMemoryBudget(limit*2, 0, 1),
		limit:  limit,
		state:  requestBodyAdmissionPending,
	}
	c.Set(requestBodyAdmissionControllerKey, controller)
	return controller
}

type trackedImageProbeBody struct {
	*bytes.Reader
	closed bool
}

func (b *trackedImageProbeBody) Close() error {
	b.closed = true
	return nil
}

func TestIsAsyncImageTaskSubmitRecognizesLargeBodyAndCachesOriginal(t *testing.T) {
	payload := []byte(`{"async":true,"client_request_id":"request_large","model":"gpt-image-2","input":"`)
	payload = append(payload, bytes.Repeat([]byte("x"), 70*1024)...)
	payload = append(payload, `"}`...)
	original := &trackedImageProbeBody{Reader: bytes.NewReader(payload)}
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	req.Body = original
	req.ContentLength = int64(len(payload))
	req.Header.Set("Content-Type", "application/json")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req
	controller := attachImageAdmissionController(c, 1024*1024)
	defer controller.releaseIfAttached()

	require.True(t, isAsyncImageTaskSubmit(c))
	cached, ok := pkghttputil.CachedRequestBody(c.Request)
	require.True(t, ok)
	require.Equal(t, payload, cached)
	restored, err := io.ReadAll(c.Request.Body)
	require.NoError(t, err)
	require.Equal(t, payload, restored)
	require.NoError(t, c.Request.Body.Close())
	require.True(t, original.closed)
}

func TestIsAsyncImageTaskSubmitRecognizesChunkedBody(t *testing.T) {
	payload := []byte(`{"async":true,"client_request_id":"request_chunked","model":"gpt-image-2"}`)
	original := &trackedImageProbeBody{Reader: bytes.NewReader(payload)}
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	req.Body = original
	req.ContentLength = -1
	req.Header.Del("Content-Length")
	req.TransferEncoding = []string{"chunked"}
	req.Header.Set("Content-Type", "application/json")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req
	controller := attachImageAdmissionController(c, 1024*1024)
	defer controller.releaseIfAttached()

	require.True(t, isAsyncImageTaskSubmit(c))
	require.Zero(t, original.Len())
	restored, err := io.ReadAll(c.Request.Body)
	require.NoError(t, err)
	require.Equal(t, payload, restored)
	require.True(t, original.closed)
}

func TestIsAsyncImageTaskSubmitRecognizesCompressedBody(t *testing.T) {
	payload := []byte(`{"async":true,"client_request_id":"request_gzip","model":"gpt-image-2"}`)
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	_, err := gz.Write(payload)
	require.NoError(t, err)
	require.NoError(t, gz.Close())

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(compressed.Bytes()))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req
	controller := attachImageAdmissionController(c, 1024*1024)
	defer controller.releaseIfAttached()

	require.True(t, isAsyncImageTaskSubmit(c))
	cached, ok := pkghttputil.CachedRequestBody(c.Request)
	require.True(t, ok)
	require.Equal(t, payload, cached)
	require.Empty(t, c.Request.Header.Get("Content-Encoding"))
	restored, err := io.ReadAll(c.Request.Body)
	require.NoError(t, err)
	require.Equal(t, payload, restored)
}

type errorImageProbeBody struct {
	*bytes.Reader
	closed bool
}

func (b *errorImageProbeBody) Read(p []byte) (int, error) {
	if b.Len() == 0 {
		return 0, io.ErrUnexpectedEOF
	}
	n, _ := b.Reader.Read(p)
	return n, io.ErrUnexpectedEOF
}

func (b *errorImageProbeBody) Close() error {
	b.closed = true
	return nil
}

func TestIsAsyncImageTaskSubmitRestoresBodyAfterProbeReadError(t *testing.T) {
	payload := []byte(`{"async":true,"model":"gpt-image-2"}`)
	original := &errorImageProbeBody{Reader: bytes.NewReader(payload)}
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	req.Body = original
	req.ContentLength = int64(len(payload))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	controller := attachImageAdmissionController(c, 1024*1024)
	defer controller.releaseIfAttached()

	require.False(t, isAsyncImageTaskSubmit(c))
	require.True(t, c.IsAborted())
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "Failed to read request body")
	require.True(t, original.closed)
}

func TestIsAsyncImageTaskSubmitWithoutControllerDoesNotReadBody(t *testing.T) {
	payload := []byte(`{"async":true,"client_request_id":"request_1"}`)
	original := &trackedImageProbeBody{Reader: bytes.NewReader(payload)}
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	req.Body = original
	req.ContentLength = int64(len(payload))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req

	require.False(t, isAsyncImageTaskSubmit(c))
	require.Equal(t, len(payload), original.Len())
	require.False(t, c.IsAborted())
}

func TestIsAsyncImageTaskSubmitWithoutControllerDoesNotTrustCache(t *testing.T) {
	payload := []byte(`{"async":true,"client_request_id":"request_cached"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(payload))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = pkghttputil.WithCachedRequestBody(req, payload)

	require.False(t, isAsyncImageTaskSubmit(c))
	require.False(t, c.IsAborted())
	cached, ok := pkghttputil.CachedRequestBody(c.Request)
	require.True(t, ok)
	require.Equal(t, payload, cached)
}

func TestIsAsyncImageTaskSubmitHandlesNilURL(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{"async":true}`))
	req.URL = nil
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req

	require.False(t, isAsyncImageTaskSubmit(c))
}
