//go:build embed || unit

package web

import (
	"mime"
	"net/http"
	"path"
	"strings"
)

// Vite emits content-hashed filenames under assets/, so the backend can apply
// immutable caching without relying on a reverse proxy to classify paths.
const staticAssetsCacheControl = "public, max-age=31536000, immutable"
const runtimeDownloadCacheControl = "public, max-age=3600"

const markdownContentType = "text/markdown; charset=utf-8"

// applyEmbeddedStaticHeaders sets response metadata that cannot be inferred
// reliably by net/http for embedded public files.
func applyEmbeddedStaticHeaders(header http.Header, cleanPath string) {
	if header == nil {
		return
	}
	if strings.EqualFold(path.Ext(cleanPath), ".md") {
		header.Set("Content-Type", markdownContentType)
	}
	applyRuntimeDownloadHeaders(header, cleanPath)
	applyStaticAssetCacheHeaders(header, cleanPath)
}

func isRuntimeDownloadPath(cleanPath string) bool {
	normalized := strings.TrimPrefix(path.Clean("/"+cleanPath), "/")
	return strings.HasPrefix(normalized, "downloads/")
}

func applyRuntimeDownloadHeaders(header http.Header, cleanPath string) {
	if header == nil || !isRuntimeDownloadPath(cleanPath) {
		return
	}

	header.Set("Cache-Control", runtimeDownloadCacheControl)
	if disposition := mime.FormatMediaType("attachment", map[string]string{"filename": path.Base(cleanPath)}); disposition != "" {
		header.Set("Content-Disposition", disposition)
	}
}

// isFingerprintedEmbeddedAssetPath reports whether a cleaned URL path refers to
// a Vite asset whose filename contains the default eight-character build hash.
func isFingerprintedEmbeddedAssetPath(cleanPath string) bool {
	cleanPath = strings.TrimPrefix(cleanPath, "/")
	if !strings.HasPrefix(cleanPath, "assets/") {
		return false
	}

	filename := path.Base(cleanPath)
	extension := path.Ext(filename)
	stem := strings.TrimSuffix(filename, extension)
	const fingerprintLength = 8
	delimiterIndex := len(stem) - fingerprintLength - 1
	if extension == "" || delimiterIndex < 1 || stem[delimiterIndex] != '-' {
		return false
	}

	// Vite hashes use URL-safe characters and are stable for immutable caching.
	fingerprint := stem[delimiterIndex+1:]
	for _, char := range fingerprint {
		if (char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '_' || char == '-' {
			continue
		}
		return false
	}
	return true
}

// applyStaticAssetCacheHeaders sets Cache-Control for long-cacheable static paths.
// index.html / SPA routes must keep no-cache and are not handled here.
func applyStaticAssetCacheHeaders(header http.Header, cleanPath string) {
	if header == nil || !isFingerprintedEmbeddedAssetPath(cleanPath) {
		return
	}
	header.Set("Cache-Control", staticAssetsCacheControl)
}
