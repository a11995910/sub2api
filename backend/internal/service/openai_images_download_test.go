package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIImagesDownloadsSignedURLWithoutUpstreamCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, format := range []string{ImageResponseFormatB64JSON, ImageResponseFormatURL} {
		t.Run(format, func(t *testing.T) {
			imageBytes := generatedImageTestPNG(t)
			upstream := &httpUpstreamRecorder{responses: []*http.Response{
				{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": {"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"data":[{"url":"https://assets.example.com/result.png?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Signature=test-signature"}]}`)),
				},
				b64BackfillImageResponse(http.StatusOK, "image/png", imageBytes),
			}}
			svc := &OpenAIGatewayService{
				cfg:                 &config.Config{},
				httpUpstream:        upstream,
				generatedImageStore: NewGeneratedImageStore(GeneratedImageStoreConfig{Directory: t.TempDir()}),
			}
			// 客户省略格式时，下载后的结果仍按分组默认值返回 Base64 或本站 URL。
			body := []byte(`{"model":"gpt-image-2.5-flare","prompt":"a blue circle"}`)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			require.NoError(t, ApplyDefaultOpenAIImagesResponseFormat(parsed, &Group{ImageResponseFormat: format}))
			account := &Account{ID: 6, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
				Credentials: map[string]any{"api_key": "test-api-key", "base_url": "https://relay.example.com/v1"}}

			result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")

			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, 1, result.ImageCount)
			require.Len(t, upstream.requests, 2, "下载结果不应重新提交生图")
			require.Equal(t, http.MethodPost, upstream.requests[0].Method)
			require.Equal(t, "Bearer test-api-key", upstream.requests[0].Header.Get("Authorization"))
			require.Equal(t, http.MethodGet, upstream.requests[1].Method)
			require.Empty(t, upstream.requests[1].Header.Get("Authorization"), "签名 URL 不能混用 Bearer 认证")
			require.Equal(t, "AWS4-HMAC-SHA256", upstream.requests[1].URL.Query().Get("X-Amz-Algorithm"))
			require.Equal(t, "test-signature", upstream.requests[1].URL.Query().Get("X-Amz-Signature"))
			require.Equal(t, http.StatusOK, rec.Code)
			if format == ImageResponseFormatB64JSON {
				require.Equal(t, base64.StdEncoding.EncodeToString(imageBytes), gjson.Get(rec.Body.String(), "data.0.b64_json").String())
			} else {
				require.Regexp(t, `^/generated-images/[a-f0-9]{32}\.png$`, gjson.Get(rec.Body.String(), "data.0.url").String())
				require.False(t, gjson.Get(rec.Body.String(), "data.0.b64_json").Exists())
			}
		})
	}
}

func TestOpenAIImagesDownloadRedirectsKeepCredentialsOnOriginalOrigin(t *testing.T) {
	baseURL, err := url.Parse("https://relay.example.com/v1/images/generations")
	require.NoError(t, err)
	imageBytes := generatedImageTestPNG(t)
	firstBody := &passthroughCloseTrackingReadCloser{Reader: strings.NewReader("")}
	secondBody := &passthroughCloseTrackingReadCloser{Reader: strings.NewReader("")}
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		{StatusCode: http.StatusFound, Header: http.Header{"Location": {"/download/protected"}}, Body: firstBody},
		{StatusCode: http.StatusTemporaryRedirect, Header: http.Header{"Location": {"https://assets.relay.example.com/result.png?X-Amz-Signature=test-signature"}}, Body: secondBody},
		b64BackfillImageResponse(http.StatusOK, "image/png", imageBytes),
	}}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	headers := http.Header{
		"Authorization": {"Bearer private-upstream-key"}, "Cookie": {"session=private-cookie"},
		"X-Api-Key": {"private-custom-key"}, "Content-Type": {"application/json"},
	}

	data, err := svc.downloadOpenAIImagesResponseURL(openAIImagesNonStreamingResponseOptions{
		ctx: context.Background(), baseURL: baseURL, requestHeaders: headers,
	}, "/download/start.png")

	require.NoError(t, err)
	require.Equal(t, imageBytes, data)
	require.True(t, firstBody.closed)
	require.True(t, secondBody.closed)
	require.Len(t, upstream.requests, 3)
	for _, req := range upstream.requests[:2] {
		require.Equal(t, "Bearer private-upstream-key", req.Header.Get("Authorization"))
		require.Equal(t, "session=private-cookie", req.Header.Get("Cookie"))
		require.Equal(t, "private-custom-key", req.Header.Get("X-Api-Key"))
		require.Empty(t, req.Header.Get("Content-Type"))
		require.True(t, HTTPUpstreamRedirectsDisabled(req.Context()))
	}
	assetReq := upstream.requests[2]
	require.Empty(t, assetReq.Header.Get("Authorization"))
	require.Empty(t, assetReq.Header.Get("Cookie"))
	require.Empty(t, assetReq.Header.Get("X-Api-Key"))
	require.True(t, HTTPUpstreamPublicHostsOnly(assetReq.Context()))
	require.Equal(t, "Bearer private-upstream-key", headers.Get("Authorization"), "不能修改生图请求头")
}

func TestOpenAIImagesDownloadRejectsUnsafeRedirects(t *testing.T) {
	baseURL, err := url.Parse("https://relay.example.com/v1/images/generations")
	require.NoError(t, err)
	for _, target := range []string{
		"http://127.0.0.1/private", "http://169.254.169.254/latest/meta-data/",
		"https://user:password@assets.example.com/result.png", "file:///etc/passwd", "",
	} {
		t.Run(target, func(t *testing.T) {
			body := &passthroughCloseTrackingReadCloser{Reader: strings.NewReader("")}
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusFound, Header: http.Header{"Location": {target}}, Body: body,
			}}
			svc := &OpenAIGatewayService{httpUpstream: upstream}

			_, err := svc.downloadOpenAIImagesResponseURL(openAIImagesNonStreamingResponseOptions{
				baseURL: baseURL,
			}, "/download/start.png")

			require.Error(t, err)
			require.Len(t, upstream.requests, 1)
			require.True(t, body.closed)
		})
	}
}

func TestOpenAIImagesDownloadDoesNotRestoreCredentialsAfterCrossOrigin(t *testing.T) {
	baseURL, err := url.Parse("https://relay.example.com/v1/images/generations")
	require.NoError(t, err)
	imageBytes := generatedImageTestPNG(t)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		{StatusCode: http.StatusFound, Header: http.Header{"Location": {"https://assets.example.com/result.png"}}, Body: io.NopCloser(strings.NewReader(""))},
		{StatusCode: http.StatusTemporaryRedirect, Header: http.Header{"Location": {"https://relay.example.com/download/result.png"}}, Body: io.NopCloser(strings.NewReader(""))},
		b64BackfillImageResponse(http.StatusOK, "image/png", imageBytes),
	}}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	headers := http.Header{"Authorization": {"Bearer test-key"}, "Cookie": {"session=test-cookie"}, "X-Api-Key": {"test-custom-key"}}

	data, err := svc.downloadOpenAIImagesResponseURL(openAIImagesNonStreamingResponseOptions{
		baseURL: baseURL, requestHeaders: headers,
	}, "/download/start.png")

	require.NoError(t, err)
	require.Equal(t, imageBytes, data)
	require.Len(t, upstream.requests, 3)
	require.Equal(t, "Bearer test-key", upstream.requests[0].Header.Get("Authorization"))
	for _, req := range upstream.requests[1:] {
		require.Empty(t, req.Header.Get("Authorization"), "跨域后返回原站也不能恢复凭据")
		require.Empty(t, req.Header.Get("Cookie"))
		require.Empty(t, req.Header.Get("X-Api-Key"))
		require.True(t, HTTPUpstreamPublicHostsOnly(req.Context()))
	}
}

func TestOpenAIImagesDownloadStopsRedirectLoop(t *testing.T) {
	baseURL, err := url.Parse("https://relay.example.com/v1/images/generations")
	require.NoError(t, err)
	upstream := &httpUpstreamRecorder{}
	bodies := make([]*passthroughCloseTrackingReadCloser, 6)
	for i := range bodies {
		bodies[i] = &passthroughCloseTrackingReadCloser{Reader: strings.NewReader("")}
		upstream.responses = append(upstream.responses, &http.Response{
			StatusCode: http.StatusFound, Header: http.Header{"Location": {"/download/loop.png"}}, Body: bodies[i],
		})
	}
	svc := &OpenAIGatewayService{httpUpstream: upstream}

	_, err = svc.downloadOpenAIImagesResponseURL(openAIImagesNonStreamingResponseOptions{
		baseURL: baseURL,
	}, "/download/loop.png")

	require.ErrorContains(t, err, "redirects too many times")
	require.Len(t, upstream.requests, 6, "初始请求加五次跳转后必须停止")
	for _, body := range bodies {
		require.True(t, body.closed, "所有跳转响应都必须关闭")
	}
}
