package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestExcelBPSSettingsEndpointsRoundTrip(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{"site_name": "保留站点"})
	call := func(method string, body any, handler gin.HandlerFunc) *httptest.ResponseRecorder {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(method, "/", bytes.NewReader(raw))
		c.Request.Header.Set("Content-Type", "application/json")
		handler(c)
		return rec
	}
	rec := call(http.MethodGet, nil, h.GetExcelBPS)
	require.Equal(t, 200, rec.Code)
	require.Contains(t, rec.Body.String(), `"excel_bps_enabled":false`)
	rec = call(http.MethodPut, map[string]any{"excel_bps_enabled": true}, h.SaveExcelBPS)
	require.Equal(t, 200, rec.Code, rec.Body.String())
	require.Equal(t, "true", repo.values[service.SettingKeyExcelBPSEnabled])
	require.Equal(t, "保留站点", repo.values["site_name"])
	defaults := service.DefaultExcelBPSDefaults()
	defaults.AllModels = true
	rec = call(http.MethodPut, map[string]any{"excel_bps": defaults}, h.SaveExcelBPSDefaults)
	require.Equal(t, 200, rec.Code, rec.Body.String())
	rec = call(http.MethodGet, nil, h.GetExcelBPSDefaults)
	require.Equal(t, 200, rec.Code)
	require.Contains(t, rec.Body.String(), `"all_models":true`)
	rec = call(http.MethodPut, map[string]any{"excel_bps_enabled": "true"}, h.SaveExcelBPS)
	require.Equal(t, 400, rec.Code)
	require.Equal(t, "true", repo.values[service.SettingKeyExcelBPSEnabled])
}
