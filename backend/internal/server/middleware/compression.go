package middleware

import (
	"compress/gzip"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

const minimumCompressionSize = 1024

// ResponseCompression 为浏览器页面和 JSON API 提供 gzip 压缩。
// 网关、SSE、WebSocket、Range、附件下载都保持原样，避免破坏流式响应和下载语义。
func ResponseCompression() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requestAcceptsGzip(c.Request) || shouldBypassCompressionPath(c.Request.URL.Path) {
			c.Next()
			return
		}

		writer := &compressionResponseWriter{
			ResponseWriter: c.Writer,
			request:        c.Request,
		}
		c.Writer = writer

		c.Next()

		if writer.gzipWriter != nil {
			_ = writer.gzipWriter.Close()
		} else if !writer.started && writer.status != 0 {
			writer.ResponseWriter.WriteHeader(writer.status)
		}
	}
}

type compressionResponseWriter struct {
	gin.ResponseWriter
	request    *http.Request
	started    bool
	compress   bool
	status     int
	gzipWriter *gzip.Writer
}

func (w *compressionResponseWriter) WriteHeader(statusCode int) {
	w.status = statusCode
}

func (w *compressionResponseWriter) WriteHeaderNow() {
	if w.started {
		return
	}
	status := w.status
	if status == 0 {
		status = w.Status()
		if status == 0 {
			status = http.StatusOK
		}
	}
	w.started = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *compressionResponseWriter) Write(data []byte) (int, error) {
	w.start(data)
	if w.compress {
		return w.gzipWriter.Write(data)
	}
	return w.ResponseWriter.Write(data)
}

func (w *compressionResponseWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

func (w *compressionResponseWriter) Flush() {
	if w.gzipWriter != nil {
		_ = w.gzipWriter.Flush()
	}
	w.ResponseWriter.Flush()
}

func (w *compressionResponseWriter) start(firstChunk []byte) {
	if w.started {
		return
	}
	w.started = true

	header := w.Header()
	status := w.status
	if status == 0 {
		status = w.Status()
		if status == 0 {
			status = http.StatusOK
		}
	}

	if !shouldCompressResponse(w.request, status, header, firstChunk) {
		w.ResponseWriter.WriteHeader(status)
		return
	}

	contentType := header.Get("Content-Type")
	if strings.TrimSpace(contentType) == "" && len(firstChunk) > 0 {
		header.Set("Content-Type", http.DetectContentType(firstChunk))
	}

	addVaryHeader(header, "Accept-Encoding")
	header.Set("Content-Encoding", "gzip")
	header.Del("Content-Length")

	gz, err := gzip.NewWriterLevel(w.ResponseWriter, gzip.BestSpeed)
	if err != nil {
		header.Del("Content-Encoding")
		return
	}

	w.compress = true
	w.gzipWriter = gz
	w.ResponseWriter.WriteHeader(status)
}

func requestAcceptsGzip(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.Method == http.MethodHead {
		return false
	}
	if r.Header.Get("Range") != "" {
		return false
	}
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return false
	}
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		if strings.TrimSpace(strings.Split(part, ";")[0]) == "gzip" {
			return true
		}
	}
	return false
}

func shouldBypassCompressionPath(path string) bool {
	return strings.HasPrefix(path, "/v1/") ||
		strings.HasPrefix(path, "/v1beta/") ||
		strings.HasPrefix(path, "/antigravity/") ||
		strings.HasPrefix(path, "/responses") ||
		strings.HasPrefix(path, "/backend-api/")
}

func shouldCompressResponse(r *http.Request, status int, header http.Header, firstChunk []byte) bool {
	if r == nil || header == nil {
		return false
	}
	if status < http.StatusOK || status == http.StatusNoContent || status == http.StatusNotModified {
		return false
	}
	if header.Get("Content-Encoding") != "" || header.Get("Content-Disposition") != "" {
		return false
	}
	if contentLength := strings.TrimSpace(header.Get("Content-Length")); contentLength != "" {
		if n, err := strconv.Atoi(contentLength); err == nil && n > 0 && n < minimumCompressionSize {
			return false
		}
	}

	contentType := header.Get("Content-Type")
	if strings.TrimSpace(contentType) == "" && len(firstChunk) > 0 {
		contentType = http.DetectContentType(firstChunk)
	}
	return isCompressibleContentType(contentType)
}

func isCompressibleContentType(contentType string) bool {
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if mediaType == "" || mediaType == "text/event-stream" {
		return false
	}
	if strings.HasPrefix(mediaType, "text/") {
		return true
	}
	switch mediaType {
	case "application/json",
		"application/javascript",
		"application/xml",
		"application/rss+xml",
		"application/manifest+json",
		"application/problem+json",
		"image/svg+xml":
		return true
	default:
		return strings.HasSuffix(mediaType, "+json") || strings.HasSuffix(mediaType, "+xml")
	}
}

func addVaryHeader(header http.Header, value string) {
	existing := header.Values("Vary")
	for _, current := range existing {
		for _, part := range strings.Split(current, ",") {
			if strings.EqualFold(strings.TrimSpace(part), value) {
				return
			}
		}
	}
	header.Add("Vary", value)
}
