package admin

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type pelicanHandlerAccountRepo struct{ service.AccountRepository }

func (r *pelicanHandlerAccountRepo) GetByID(_ context.Context, id int64) (*service.Account, error) {
	return &service.Account{ID: id, Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "测试占位凭据"}}, nil
}

type pelicanHandlerUpstream struct {
	body      []byte
	accountID int64
}

func (u *pelicanHandlerUpstream) Do(req *http.Request, _ string, id int64, _ int) (*http.Response, error) {
	u.body, _ = io.ReadAll(req.Body)
	u.accountID = id
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"<svg></svg>\"}}\n\n" +
			"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n" +
			"data: {\"type\":\"message_stop\"}\n\n"))}, nil
}
func (u *pelicanHandlerUpstream) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, id, concurrency)
}

func TestAccountHandlerPelicanMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{"pelican", "default"} {
		t.Run(mode, func(t *testing.T) {
			upstream := &pelicanHandlerUpstream{}
			svc := service.NewAccountTestService(&pelicanHandlerAccountRepo{}, nil, nil, nil, nil, upstream, &config.Config{}, nil)
			h := &AccountHandler{accountTestService: svc}
			router := gin.New()
			router.POST("/accounts/:id/test", h.Test)
			request := httptest.NewRequest(http.MethodPost, "/accounts/42/test", strings.NewReader(`{"mode":"`+mode+`","model_id":"claude-sonnet-4-6","reasoning_effort":"high"}`))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			require.Equal(t, int64(42), upstream.accountID)
			require.Contains(t, recorder.Body.String(), `"success":true`)
			if mode == "pelican" {
				require.Contains(t, string(upstream.body), "鹈鹕骑自行车")
				require.Greater(t, gjson.GetBytes(upstream.body, "max_tokens").Int(), int64(1024))
				require.Equal(t, "high", gjson.GetBytes(upstream.body, "output_config.effort").String())
			} else {
				require.NotContains(t, string(upstream.body), "鹈鹕")
				require.Equal(t, int64(1024), gjson.GetBytes(upstream.body, "max_tokens").Int())
			}
		})
	}
}
