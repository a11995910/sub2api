//go:build unit

package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type videoProtocolGatewayCacheStub struct {
	videoBindingCacheStub
	protocols map[string]OpenAIVideoProtocol
	setCalls  int
	bindErr   error
}

func (s *videoProtocolGatewayCacheStub) SetSessionAccountID(
	ctx context.Context,
	groupID int64,
	sessionHash string,
	accountID int64,
	ttl time.Duration,
) error {
	if s.bindErr != nil {
		return s.bindErr
	}
	return s.videoBindingCacheStub.SetSessionAccountID(ctx, groupID, sessionHash, accountID, ttl)
}

func (s *videoProtocolGatewayCacheStub) protocolKey(accountID int64, model string) string {
	return strconv.FormatInt(accountID, 10) + ":" + model
}

func (s *videoProtocolGatewayCacheStub) GetOpenAIVideoProtocol(_ context.Context, accountID int64, model string) (OpenAIVideoProtocol, error) {
	protocol, ok := s.protocols[s.protocolKey(accountID, model)]
	if !ok {
		return "", redis.Nil
	}
	return protocol, nil
}

func (s *videoProtocolGatewayCacheStub) SetOpenAIVideoProtocol(_ context.Context, accountID int64, model string, protocol OpenAIVideoProtocol, _ time.Duration) error {
	if s.protocols == nil {
		s.protocols = make(map[string]OpenAIVideoProtocol)
	}
	s.protocols[s.protocolKey(accountID, model)] = protocol
	s.setCalls++
	return nil
}

func (s *videoProtocolGatewayCacheStub) DeleteOpenAIVideoProtocol(_ context.Context, accountID int64, model string) error {
	delete(s.protocols, s.protocolKey(accountID, model))
	return nil
}

func openAIVideoForwardTestContext(body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, recorder
}

func openAIVideoForwardTestAccount() *Account {
	return &Account{
		ID:          88,
		Name:        "video-upstream",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "sk-video-test",
			"base_url": "http://video-upstream.example/v1",
			"model_mapping": map[string]any{
				"dreamina-seedance-2-0-ep": "jing-video-2-pro",
			},
		},
	}
}

func TestForwardOpenAIVideoCreateUsesVideosEndpointAndMappedModel(t *testing.T) {
	body := []byte(`{"model":"dreamina-seedance-2-0-ep","prompt":"雨夜城市","resolution":"720p","duration":5}`)
	c, recorder := openAIVideoForwardTestContext(body)
	cache := &videoProtocolGatewayCacheStub{}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewBufferString(`{"id":"task-1","object":"video","model":"jing-video-2-pro","status":"queued","progress":0}`)),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), cache: cache, httpUpstream: upstream}
	account := openAIVideoForwardTestAccount()

	result, err := svc.ForwardOpenAIVideoCreate(context.Background(), c, account, body, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "task-1", result.ResponseID)
	require.Equal(t, "dreamina-seedance-2-0-ep", result.Model)
	require.Equal(t, "dreamina-seedance-2-0-ep", result.BillingModel)
	require.Equal(t, "jing-video-2-pro", result.UpstreamModel)
	require.Equal(t, 1, result.VideoCount)
	require.Equal(t, "720p", result.VideoResolution)
	require.Equal(t, 5, result.VideoDurationSeconds)
	require.Equal(t, "/v1/videos", result.UpstreamEndpoint)

	require.Equal(t, "http://video-upstream.example/v1/videos", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer sk-video-test", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, "jing-video-2-pro", gjson.GetBytes(upstream.lastBody, "model").String())
	require.Equal(t, "5", gjson.GetBytes(upstream.lastBody, "seconds").String())
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "task-1", gjson.Get(recorder.Body.String(), "task_id").String())
	require.Equal(t, "dreamina-seedance-2-0-ep", gjson.Get(recorder.Body.String(), "model").String())
	require.Equal(t, OpenAIVideoProtocolVideos, cache.protocols[cache.protocolKey(account.ID, "jing-video-2-pro")])
}

func TestForwardOpenAIVideoCreateBindsTaskBeforeReturning(t *testing.T) {
	body := []byte(`{"model":"dreamina-seedance-2-0-ep","prompt":"雨夜城市","duration":5}`)
	c, _ := openAIVideoForwardTestContext(body)
	SetOpenAIVideoContext(c, OpenAIVideoContext{
		Model:    "dreamina-seedance-2-0-ep",
		UserID:   10,
		APIKeyID: 20,
		GroupID:  7,
		BindTask: true,
	})
	cache := &videoProtocolGatewayCacheStub{}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewBufferString(`{"id":"task-bound","status":"queued"}`)),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), cache: cache, httpUpstream: upstream}

	_, err := svc.ForwardOpenAIVideoCreate(context.Background(), c, openAIVideoForwardTestAccount(), body, "")
	require.NoError(t, err)
	groupID := int64(7)
	accountID, err := svc.ResolveVideoTaskAccount(context.Background(), &groupID, "task-bound", 10, 20)
	require.NoError(t, err)
	require.Equal(t, int64(88), accountID)
}

func TestForwardOpenAIVideoCreateDoesNotDeliverTaskWhenBindingFails(t *testing.T) {
	body := []byte(`{"model":"dreamina-seedance-2-0-ep","prompt":"雨夜城市","duration":5}`)
	c, recorder := openAIVideoForwardTestContext(body)
	SetOpenAIVideoContext(c, OpenAIVideoContext{
		Model:    "dreamina-seedance-2-0-ep",
		UserID:   10,
		APIKeyID: 20,
		GroupID:  7,
		BindTask: true,
	})
	cache := &videoProtocolGatewayCacheStub{bindErr: errors.New("redis unavailable")}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewBufferString(`{"id":"task-unbound","status":"queued"}`)),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), cache: cache, httpUpstream: upstream}

	result, err := svc.ForwardOpenAIVideoCreate(context.Background(), c, openAIVideoForwardTestAccount(), body, "")
	require.ErrorContains(t, err, "bind video task")
	require.Nil(t, result)
	require.Equal(t, http.StatusBadGateway, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "task-unbound")
	require.Zero(t, cache.setCalls)
}

func TestForwardOpenAIVideoCreateFallsBackOnlyForUnsupportedEndpoint(t *testing.T) {
	body := []byte(`{"model":"dreamina-seedance-2-0-ep","prompt":"雨夜城市","resolution":"720p","duration":5}`)
	c, recorder := openAIVideoForwardTestContext(body)
	cache := &videoProtocolGatewayCacheStub{}
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		{
			StatusCode: http.StatusNotFound,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(`{"error":{"code":"not_found","message":"route not found"}}`)),
		},
		{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(`{"id":"chat-video-1","choices":[{"message":{"content":"https://cdn.test/result.mp4"}}]}`)),
		},
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), cache: cache, httpUpstream: upstream}

	result, err := svc.ForwardOpenAIVideoCreate(context.Background(), c, openAIVideoForwardTestAccount(), body, "")
	require.NoError(t, err)
	require.Equal(t, 2, len(upstream.requests))
	require.Equal(t, "/v1/videos", upstream.requests[0].URL.Path)
	require.Equal(t, "/v1/chat/completions", upstream.requests[1].URL.Path)
	require.Equal(t, OpenAIVideoProtocolChatCompletions, cache.protocols[cache.protocolKey(88, "jing-video-2-pro")])
	require.Equal(t, "completed", gjson.Get(recorder.Body.String(), "status").String())
	require.Equal(t, "https://cdn.test/result.mp4", gjson.Get(recorder.Body.String(), "url").String())
	require.Equal(t, "chat-video-1", result.ResponseID)
}

func TestForwardOpenAIVideoCreateDoesNotFallbackOnUpstreamFailure(t *testing.T) {
	body := []byte(`{"model":"dreamina-seedance-2-0-ep","prompt":"雨夜城市","resolution":"720p","duration":5}`)
	c, _ := openAIVideoForwardTestContext(body)
	cache := &videoProtocolGatewayCacheStub{}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusBadGateway,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewBufferString(`{"error":{"message":"temporarily unavailable"}}`)),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), cache: cache, httpUpstream: upstream}

	_, err := svc.ForwardOpenAIVideoCreate(context.Background(), c, openAIVideoForwardTestAccount(), body, "")
	require.Error(t, err)
	require.Len(t, upstream.requests, 1)
	require.Zero(t, cache.setCalls)
}

func TestForwardOpenAIVideoCreateCachesVideosOnBusinessValidationError(t *testing.T) {
	body := []byte(`{"model":"dreamina-seedance-2-0-ep","prompt":"雨夜城市","duration":5}`)
	c, _ := openAIVideoForwardTestContext(body)
	cache := &videoProtocolGatewayCacheStub{}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewBufferString(`{"code":"invalid_request","message":"unsupported resolution"}`)),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), cache: cache, httpUpstream: upstream}

	_, err := svc.ForwardOpenAIVideoCreate(context.Background(), c, openAIVideoForwardTestAccount(), body, "")
	require.Error(t, err)
	require.Len(t, upstream.requests, 1)
	require.Equal(t, OpenAIVideoProtocolVideos, cache.protocols[cache.protocolKey(88, "jing-video-2-pro")])
}

func TestForwardOpenAIVideoStatusNormalizesCompletedResponse(t *testing.T) {
	c, recorder := openAIVideoForwardTestContext(nil)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/task-1", nil)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(bytes.NewBufferString(`{
			"id":"task-1",
			"model":"jing-video-2-pro",
			"status":"done",
			"progress":"100%",
			"metadata":{"url":"https://signed.test/result.mp4?token=secret"}
		}`)),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

	result, err := svc.ForwardOpenAIVideoStatus(context.Background(), c, openAIVideoForwardTestAccount(), "task-1")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "http://video-upstream.example/v1/videos/task-1", upstream.lastReq.URL.String())
	require.Equal(t, "completed", gjson.Get(recorder.Body.String(), "status").String())
	require.Equal(t, "/v1/videos/task-1/content", gjson.Get(recorder.Body.String(), "url").String())
	require.NotContains(t, recorder.Body.String(), "signed.test")
}

func TestForwardOpenAIVideoContentProxiesVideoAndRange(t *testing.T) {
	c, recorder := openAIVideoForwardTestContext(nil)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/task-1/content", nil)
	c.Request.Header.Set("Range", "bytes=0-3")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode:    http.StatusPartialContent,
		Header:        http.Header{"Content-Type": []string{"video/mp4"}, "Content-Range": []string{"bytes 0-3/8"}},
		Body:          io.NopCloser(bytes.NewBufferString("test")),
		ContentLength: 4,
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

	result, err := svc.ForwardOpenAIVideoContent(context.Background(), c, openAIVideoForwardTestAccount(), "task-1")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "http://video-upstream.example/v1/videos/task-1/content", upstream.lastReq.URL.String())
	require.Equal(t, "bytes=0-3", upstream.lastReq.Header.Get("Range"))
	require.Equal(t, http.StatusPartialContent, recorder.Code)
	require.Equal(t, "video/mp4", recorder.Header().Get("Content-Type"))
	require.Equal(t, "test", recorder.Body.String())
}

func TestForwardOpenAIVideoContentSlicesRangeWhenUpstreamIgnoresIt(t *testing.T) {
	c, recorder := openAIVideoForwardTestContext(nil)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/task-1/content", nil)
	c.Request.Header.Set("Range", "bytes=2-5")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode:    http.StatusOK,
		Header:        http.Header{"Content-Type": []string{"video/mp4"}, "Accept-Ranges": []string{"bytes"}},
		Body:          io.NopCloser(bytes.NewBufferString("01234567")),
		ContentLength: 8,
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

	result, err := svc.ForwardOpenAIVideoContent(context.Background(), c, openAIVideoForwardTestAccount(), "task-1")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "bytes=2-5", upstream.lastReq.Header.Get("Range"))
	require.Equal(t, http.StatusPartialContent, recorder.Code)
	require.Equal(t, "bytes 2-5/8", recorder.Header().Get("Content-Range"))
	require.Equal(t, "4", recorder.Header().Get("Content-Length"))
	require.Equal(t, "2345", recorder.Body.String())
}

func TestForwardOpenAIVideoContentLeavesMultipleRangesUnchangedWhenUpstreamIgnoresThem(t *testing.T) {
	c, recorder := openAIVideoForwardTestContext(nil)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/task-1/content", nil)
	c.Request.Header.Set("Range", "bytes=0-1,4-5")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode:    http.StatusOK,
		Header:        http.Header{"Content-Type": []string{"video/mp4"}},
		Body:          io.NopCloser(bytes.NewBufferString("01234567")),
		ContentLength: 8,
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

	_, err := svc.ForwardOpenAIVideoContent(context.Background(), c, openAIVideoForwardTestAccount(), "task-1")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Empty(t, recorder.Header().Get("Content-Range"))
	require.Equal(t, "01234567", recorder.Body.String())
}

func TestForwardOpenAIVideoContentLeavesIfRangeRequestUnchangedWhenUpstreamIgnoresIt(t *testing.T) {
	c, recorder := openAIVideoForwardTestContext(nil)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/task-1/content", nil)
	c.Request.Header.Set("Range", "bytes=2-5")
	c.Request.Header.Set("If-Range", `"stale-etag"`)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode:    http.StatusOK,
		Header:        http.Header{"Content-Type": []string{"video/mp4"}},
		Body:          io.NopCloser(bytes.NewBufferString("01234567")),
		ContentLength: 8,
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

	_, err := svc.ForwardOpenAIVideoContent(context.Background(), c, openAIVideoForwardTestAccount(), "task-1")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Empty(t, recorder.Header().Get("Content-Range"))
	require.Equal(t, "01234567", recorder.Body.String())
}

func TestForwardOpenAIVideoContentFallsBackToValidatedStatusURL(t *testing.T) {
	c, recorder := openAIVideoForwardTestContext(nil)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/task-1/content", nil)
	c.Request.Header.Set("Range", "bytes=0-3")
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		{
			StatusCode: http.StatusNotFound,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(`{"error":{"message":"content route not found"}}`)),
		},
		{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(`{"id":"task-1","status":"done","metadata":{"url":"https://8.8.8.8/result.mp4?token=secret"}}`)),
		},
		{
			StatusCode:    http.StatusPartialContent,
			Header:        http.Header{"Content-Type": []string{"video/mp4"}, "Content-Range": []string{"bytes 0-3/8"}},
			Body:          io.NopCloser(bytes.NewBufferString("test")),
			ContentLength: 4,
		},
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

	_, err := svc.ForwardOpenAIVideoContent(context.Background(), c, openAIVideoForwardTestAccount(), "task-1")
	require.NoError(t, err)
	require.Equal(t, "test", recorder.Body.String())
	require.Len(t, upstream.requests, 3)
	require.Equal(t, "/v1/videos/task-1/content", upstream.requests[0].URL.Path)
	require.Equal(t, "/v1/videos/task-1", upstream.requests[1].URL.Path)
	require.Equal(t, "https://8.8.8.8/result.mp4?token=secret", upstream.requests[2].URL.String())
	require.Equal(t, "Bearer sk-video-test", upstream.requests[1].Header.Get("Authorization"))
	require.Empty(t, upstream.requests[2].Header.Get("Authorization"))
	require.Equal(t, "bytes=0-3", upstream.requests[2].Header.Get("Range"))
	require.True(t, HTTPUpstreamRedirectsDisabled(upstream.requests[2].Context()))
}

func TestForwardOpenAIVideoContentRejectsPrivateStatusURL(t *testing.T) {
	c, _ := openAIVideoForwardTestContext(nil)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/task-1/content", nil)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		{
			StatusCode: http.StatusNotFound,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(`{"error":{"message":"content route not found"}}`)),
		},
		{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(`{"id":"task-1","status":"done","metadata":{"url":"https://127.0.0.1/private.mp4"}}`)),
		},
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

	_, err := svc.ForwardOpenAIVideoContent(context.Background(), c, openAIVideoForwardTestAccount(), "task-1")
	require.ErrorContains(t, err, "public HTTPS")
	require.Len(t, upstream.requests, 2)
}

func TestForwardOpenAIVideoContentLimitsChunkedBody(t *testing.T) {
	c, recorder := openAIVideoForwardTestContext(nil)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/task-1/content", nil)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode:    http.StatusOK,
		Header:        http.Header{"Content-Type": []string{"video/mp4"}},
		Body:          io.NopCloser(bytes.NewBufferString("12345678")),
		ContentLength: -1,
	}}
	cfg := rawChatCompletionsTestConfig()
	cfg.Gateway.UpstreamResponseReadMaxBytes = 4
	svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}

	_, err := svc.ForwardOpenAIVideoContent(context.Background(), c, openAIVideoForwardTestAccount(), "task-1")
	require.ErrorIs(t, err, ErrUpstreamResponseBodyTooLarge)
	require.Equal(t, "1234", recorder.Body.String())
}

func TestValidateOpenAIVideoContentURLRejectsPrivateAndInsecureTargets(t *testing.T) {
	for _, rawURL := range []string{
		"http://8.8.8.8/video.mp4",
		"https://127.0.0.1/video.mp4",
		"https://169.254.169.254/latest/meta-data",
		"https://user:pass@8.8.8.8/video.mp4",
	} {
		t.Run(rawURL, func(t *testing.T) {
			_, err := validateOpenAIVideoContentURL(context.Background(), rawURL)
			require.ErrorContains(t, err, "public HTTPS")
		})
	}

	parsed, err := validateOpenAIVideoContentURL(context.Background(), "https://8.8.8.8/video.mp4")
	require.NoError(t, err)
	require.Equal(t, "https://8.8.8.8/video.mp4", parsed.String())
}

func TestForwardOpenAIVideoContentRejectsNonVideoResponse(t *testing.T) {
	c, _ := openAIVideoForwardTestContext(nil)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/task-1/content", nil)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/html"}},
		Body:       io.NopCloser(bytes.NewBufferString("not video")),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

	_, err := svc.ForwardOpenAIVideoContent(context.Background(), c, openAIVideoForwardTestAccount(), "task-1")
	require.ErrorContains(t, err, "unsupported content type")
}

func TestOpenAIVideoUsageClassificationDoesNotDependOnModelName(t *testing.T) {
	result := &OpenAIForwardResult{
		Model:         "future-motion-pro",
		BillingModel:  "future-motion-pro",
		UpstreamModel: "vendor-alias-2027",
		VideoCount:    1,
	}
	require.True(t, isVideoUsageResult(result, []string{"future-motion-pro"}))
}
