package middleware

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestResponseCompression_CompressesJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(ResponseCompression())
	r.GET("/api/data", func(c *gin.Context) {
		c.Data(http.StatusOK, "application/json; charset=utf-8", []byte(strings.Repeat(`{"ok":true}`, 200)))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding=%q, want gzip", got)
	}
	if !strings.Contains(w.Header().Get("Vary"), "Accept-Encoding") {
		t.Fatalf("Vary=%q, want Accept-Encoding", w.Header().Get("Vary"))
	}

	gr, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer func() { _ = gr.Close() }()
	body, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("read gzip body: %v", err)
	}
	if !strings.Contains(string(body), `"ok":true`) {
		t.Fatalf("unexpected body: %q", string(body[:min(len(body), 80)]))
	}
}

func TestResponseCompression_SkipsGatewayStreams(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(ResponseCompression())
	r.GET("/v1/chat/completions", func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		c.String(http.StatusOK, "data: ok\n\n")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding=%q, want empty", got)
	}
	if got := w.Body.String(); got != "data: ok\n\n" {
		t.Fatalf("body=%q", got)
	}
}

func TestResponseCompression_PreservesExplicitStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(ResponseCompression())
	r.POST("/api/data", func(c *gin.Context) {
		c.Status(http.StatusCreated)
		_, _ = c.Writer.Write([]byte(strings.Repeat(`{"created":true}`, 200)))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/data", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, want %d", w.Code, http.StatusCreated)
	}
	if got := w.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding=%q, want gzip", got)
	}
}

func TestResponseCompression_SkipsSmallContentLength(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(ResponseCompression())
	r.GET("/small", func(c *gin.Context) {
		body := "ok"
		c.Header("Content-Length", "2")
		c.String(http.StatusOK, body)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/small", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding=%q, want empty", got)
	}
	if got := w.Body.String(); got != "ok" {
		t.Fatalf("body=%q", got)
	}
}

func TestResponseCompression_PreservesStatusWithoutBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(ResponseCompression())
	r.DELETE("/empty", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/empty", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want %d", w.Code, http.StatusNoContent)
	}
	if got := w.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding=%q, want empty", got)
	}
}

func TestResponseCompression_RequiresAcceptEncoding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(ResponseCompression())
	r.GET("/html", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(strings.Repeat("<p>hello</p>", 200)))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/html", nil)
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding=%q, want empty", got)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
