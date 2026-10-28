package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var zycaTrustedVideoPath = regexp.MustCompile(`^/temporary-media/([a-f0-9]{64})\.(mp4|mov|webm)$`)

var zycaMetadataHTTPClient = &http.Client{
	Timeout: 5 * time.Second,
	Transport: &http.Transport{
		DialContext: safeDialContext, DisableCompression: true,
		MaxIdleConns: 8, IdleConnTimeout: 30 * time.Second,
		TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 5 * time.Second,
		MaxResponseHeaderBytes: 16 << 10,
	},
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("可信媒体元数据接口不允许重定向")
	},
}

func zycaTrustedMetadataURL(rawURL string, trustedOrigins []string) (*url.URL, string, error) {
	target, err := url.Parse(rawURL)
	if err != nil || target.Scheme != "https" || target.User != nil || target.RawQuery != "" || target.ForceQuery ||
		target.Fragment != "" || target.RawPath != "" || target.Host == "" {
		return nil, "", errors.New("参考视频必须使用可信媒体服务的原始 HTTPS 地址")
	}
	matched := zycaTrustedVideoPath.FindStringSubmatch(target.Path)
	if len(matched) != 3 {
		return nil, "", errors.New("参考视频缺少可信内容标识")
	}
	trusted := false
	for _, rawOrigin := range trustedOrigins {
		origin, err := url.Parse(strings.TrimSpace(rawOrigin))
		if err == nil && origin.Scheme == "https" && origin.User == nil && origin.Host != "" &&
			(origin.Path == "" || origin.Path == "/") && origin.RawQuery == "" && !origin.ForceQuery && origin.Fragment == "" &&
			strings.EqualFold(origin.Host, target.Host) {
			trusted = true
			break
		}
	}
	if !trusted {
		return nil, "", errors.New("参考视频地址不在服务端可信媒体来源中")
	}
	target.Path += "/metadata"
	return target, matched[1], nil
}

func lookupZYCAReferenceMetadata(ctx context.Context, rawURL string, trustedOrigins []string, client *http.Client) (float64, string, error) {
	target, digest, err := zycaTrustedMetadataURL(rawURL, trustedOrigins)
	if err != nil {
		return 0, "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return 0, "", errors.New("无法构造媒体元数据请求")
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return 0, "", errors.New("无法读取可信媒体元数据")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.ContentLength > 16<<10 {
		return 0, "", errors.New("媒体元数据缺失或响应无效，请重新上传参考视频")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (16<<10)+1))
	if err != nil || len(body) > 16<<10 {
		return 0, "", errors.New("媒体元数据响应超限")
	}
	return parseZYCAReferenceMetadata(body, digest, target.Path)
}

func parseZYCAReferenceMetadata(body []byte, digest, path string) (float64, string, error) {
	var metadata struct {
		Version         int       `json:"version"`
		SHA256          string    `json:"sha256"`
		ByteSize        int64     `json:"byteSize"`
		MimeType        string    `json:"mimeType"`
		DurationSeconds float64   `json:"durationSeconds"`
		ExpiresAt       time.Time `json:"expiresAt"`
	}
	expectedMIME := "video/mp4"
	if strings.HasSuffix(path, ".mov/metadata") {
		expectedMIME = "video/quicktime"
	} else if strings.HasSuffix(path, ".webm/metadata") {
		expectedMIME = "video/webm"
	}
	if err := json.Unmarshal(body, &metadata); err != nil || metadata.Version != 1 || metadata.SHA256 != digest ||
		metadata.ByteSize <= 0 || metadata.MimeType != expectedMIME ||
		!validVideoStatsAmount(metadata.DurationSeconds) || metadata.DurationSeconds <= 0 || metadata.DurationSeconds > 3600 ||
		!metadata.ExpiresAt.After(time.Now().Add(5*time.Minute)) {
		return 0, "", errors.New("可信媒体元数据无效或即将过期，请重新上传参考视频")
	}
	return metadata.DurationSeconds, metadata.SHA256, nil
}
