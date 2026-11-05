//go:build image_download_live

package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// 只读取专用测试已生成的图片，禁止在验收中再次提交生图。
type imageDownloadLiveUpstream struct {
	t          *testing.T
	imageURL   string
	downloaded []byte
}

func (u *imageDownloadLiveUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	if req.Method == http.MethodPost {
		body, err := json.Marshal(map[string]any{"data": []any{map[string]string{"url": u.imageURL}}})
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(body))}, err
	}
	require.Equal(u.t, http.MethodGet, req.Method)
	require.Empty(u.t, req.Header.Get("Authorization"))
	require.True(u.t, HTTPUpstreamPublicHostsOnly(req.Context()))
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, openAIImageMaxDownloadBytes+1))
	_ = resp.Body.Close()
	u.downloaded = body
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return resp, err
}

func (u *imageDownloadLiveUpstream) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, id, concurrency)
}

func TestOpenAIImagesDownloadLive(t *testing.T) {
	imageURL := os.Getenv("SUB2_IMAGE_DOWNLOAD_LIVE_URL")
	if imageURL == "" {
		t.Skip("未提供专用测试图片 URL")
	}
	u, err := url.Parse(imageURL)
	require.NoError(t, err)
	require.Equal(t, "https", u.Scheme)
	require.True(t, strings.HasSuffix(u.Hostname(), ".s3-accelerate.amazonaws.com"), "只允许本次专用测试的图片存储")
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name, format string
		backfill     bool
	}{
		{"线上现有开关路径", ImageResponseFormatB64JSON, true},
		{"通用下载转Base64", ImageResponseFormatB64JSON, false},
		{"通用下载转本站URL", ImageResponseFormatURL, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			upstream := &imageDownloadLiveUpstream{t: t, imageURL: imageURL}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream,
				generatedImageStore: NewGeneratedImageStore(GeneratedImageStoreConfig{Directory: t.TempDir()})}
			body := []byte(`{"model":"gpt-image-2.5-flare","prompt":"专用验收，不提交到真实生图接口"}`)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			require.NoError(t, ApplyDefaultOpenAIImagesResponseFormat(parsed, &Group{ImageResponseFormat: test.format}))
			account := b64BackfillAccount(test.backfill)

			result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")

			require.True(t, err == nil, "真实图片下载和转换必须成功；失败时不输出签名 URL")
			require.NotNil(t, result)
			require.Equal(t, http.StatusOK, rec.Code)
			require.Equal(t, 1, result.ImageCount)
			require.True(t, isBackfillImageContent(upstream.downloaded))
			if test.format == ImageResponseFormatB64JSON {
				require.Equal(t, base64.StdEncoding.EncodeToString(upstream.downloaded), gjson.Get(rec.Body.String(), "data.0.b64_json").String())
			} else {
				require.Regexp(t, `^/generated-images/[a-f0-9]{32}\.[a-z]+$`, gjson.Get(rec.Body.String(), "data.0.url").String())
			}
		})
	}
}
