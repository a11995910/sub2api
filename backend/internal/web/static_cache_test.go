//go:build unit

package web

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsFingerprintedEmbeddedAssetPath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		path string
		want bool
	}{
		{name: "fingerprinted_js", path: "assets/index-AbCd1234.js", want: true},
		{name: "fingerprinted_css", path: "assets/app-a1B2c3D4.css", want: true},
		{name: "fingerprinted_url_safe_hash", path: "assets/app-aB1-2_Cd.css", want: true},
		{name: "nested_fingerprinted_asset", path: "assets/vendor/chunk-AbCd1234.js", want: true},
		{name: "leading_slash_fingerprinted_asset", path: "/assets/index-AbCd1234.js", want: true},
		{name: "unhashed_asset", path: "assets/index.js", want: false},
		{name: "short_suffix", path: "assets/index-abc123.js", want: false},
		{name: "logo", path: "logo.png", want: false},
		{name: "favicon", path: "favicon.ico", want: false},
		{name: "fingerprint_outside_assets", path: "downloads/index-AbCd1234.js", want: false},
		{name: "index_html", path: "index.html", want: false},
		{name: "spa_route", path: "dashboard", want: false},
		{name: "assets_prefix_only", path: "assets", want: false},
		{name: "similar_name", path: "assets-backup/x.js", want: false},
		{name: "empty", path: "", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, isFingerprintedEmbeddedAssetPath(tc.path))
		})
	}
}

func TestApplyStaticAssetCacheHeaders(t *testing.T) {
	t.Parallel()

	t.Run("sets_immutable_cache_for_fingerprinted_asset", func(t *testing.T) {
		t.Parallel()
		header := make(http.Header)
		applyStaticAssetCacheHeaders(header, "assets/index-AbCd1234.js")
		assert.Equal(t, staticAssetsCacheControl, header.Get("Cache-Control"))
	})

	for _, path := range []string{"assets/index.js", "logo.png", "favicon.ico", "index.html"} {
		path := path
		t.Run("skips_"+path, func(t *testing.T) {
			t.Parallel()
			header := make(http.Header)
			applyStaticAssetCacheHeaders(header, path)
			assert.Empty(t, header.Get("Cache-Control"))
		})
	}

	t.Run("nil_header_is_noop", func(t *testing.T) {
		t.Parallel()
		assert.NotPanics(t, func() {
			applyStaticAssetCacheHeaders(nil, "assets/index-AbCd1234.js")
		})
	})
}

func TestApplyEmbeddedStaticHeaders(t *testing.T) {
	t.Parallel()

	t.Run("sets_utf8_content_type_for_markdown", func(t *testing.T) {
		t.Parallel()
		header := make(http.Header)
		applyEmbeddedStaticHeaders(header, "developer-docs.md")
		assert.Equal(t, markdownContentType, header.Get("Content-Type"))
	})

	t.Run("matches_markdown_extension_case_insensitively", func(t *testing.T) {
		t.Parallel()
		header := make(http.Header)
		applyEmbeddedStaticHeaders(header, "GUIDE.MD")
		assert.Equal(t, markdownContentType, header.Get("Content-Type"))
	})

	t.Run("preserves_other_content_type_inference", func(t *testing.T) {
		t.Parallel()
		header := make(http.Header)
		applyEmbeddedStaticHeaders(header, "logo.png")
		assert.Empty(t, header.Get("Content-Type"))
	})

	t.Run("keeps_immutable_cache_for_fingerprinted_assets", func(t *testing.T) {
		t.Parallel()
		header := make(http.Header)
		applyEmbeddedStaticHeaders(header, "assets/index-AbCd1234.js")
		assert.Equal(t, staticAssetsCacheControl, header.Get("Cache-Control"))
	})

	t.Run("nil_header_is_noop", func(t *testing.T) {
		t.Parallel()
		assert.NotPanics(t, func() {
			applyEmbeddedStaticHeaders(nil, "developer-docs.md")
		})
	})
}

func TestRuntimeDownloadHeaders(t *testing.T) {
	t.Parallel()

	t.Run("recognizes_only_download_children", func(t *testing.T) {
		t.Parallel()
		assert.True(t, isRuntimeDownloadPath("downloads/clients/codex/Codex-Windows-x64.msix"))
		assert.True(t, isRuntimeDownloadPath("/downloads/clients/SHA256SUMS.txt"))
		assert.False(t, isRuntimeDownloadPath("downloads"))
		assert.False(t, isRuntimeDownloadPath("download/client.exe"))
		assert.False(t, isRuntimeDownloadPath("assets/downloads/client.exe"))
	})

	t.Run("forces_attachment_and_short_cache", func(t *testing.T) {
		t.Parallel()
		header := make(http.Header)
		applyRuntimeDownloadHeaders(header, "downloads/clients/cc-switch/CC-Switch-macOS.dmg")

		assert.Equal(t, runtimeDownloadCacheControl, header.Get("Cache-Control"))
		assert.True(t, strings.HasPrefix(header.Get("Content-Disposition"), "attachment;"))
		assert.Contains(t, header.Get("Content-Disposition"), "CC-Switch-macOS.dmg")
	})

	t.Run("ignores_non_download_paths", func(t *testing.T) {
		t.Parallel()
		header := make(http.Header)
		applyRuntimeDownloadHeaders(header, "assets/index-AbCd1234.js")
		assert.Empty(t, header.Get("Content-Disposition"))
		assert.Empty(t, header.Get("Cache-Control"))
	})
}
