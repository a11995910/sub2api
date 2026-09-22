package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSettingsCodexTicketProxyWriteReadAndHotReload(t *testing.T) {
	key := service.SettingKeyOpenAICodexTicketHarvestProxyURL
	oldProxy := "http://user:old-secret@old.example.com:8080"
	newProxy := "socks5h://user:new-secret@new.example.com:1080"
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{key: oldProxy})
	require.Equal(t, oldProxy, h.settingService.GetOpenAICodexTicketHarvestProxyURL(context.Background()))
	rec := doUpdateSettings(t, h, map[string]any{key: newProxy}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, newProxy, repo.values[key])
	require.Equal(t, newProxy, h.settingService.GetOpenAICodexTicketHarvestProxyURL(context.Background()))
	require.NotContains(t, rec.Body.String(), "new-secret")
	require.Contains(t, rec.Body.String(), `"openai_codex_ticket_harvest_proxy_configured":true`)
	// 省略、空值和接口脱敏占位均保留真实凭据。
	for _, body := range []map[string]any{{"site_name": "updated"}, {key: ""}, {key: service.MaskProxyURL(newProxy)}} {
		rec = doUpdateSettings(t, h, body, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Equal(t, newProxy, repo.values[key])
	}
	get := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(get)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
	h.GetSettings(c)
	require.Equal(t, http.StatusOK, get.Code)
	require.NotContains(t, get.Body.String(), "new-secret")
	require.Contains(t, get.Body.String(), "new.example.com")
}

func TestSettingsCodexTicketRejectInvalidProxyWithoutLeakingPassword(t *testing.T) {
	key := service.SettingKeyOpenAICodexTicketHarvestProxyURL
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{key: "http://previous.example.com:8080"})
	rec := doUpdateSettings(t, h, map[string]any{key: "ftp://user:invalid-secret@proxy.example.com:21"}, nil)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.NotContains(t, rec.Body.String(), "invalid-secret")
	require.Equal(t, "http://previous.example.com:8080", repo.values[key])
}

func TestSettingsCodexTicketExtractWriteReadAndHotReload(t *testing.T) {
	key := service.SettingKeyOpenAICodexTicketHarvestExtractURL
	oldURL := "https://supplier.example/old-secret-path?key=old-secret-token"
	newURL := "http://supplier.example:8089/new-secret-path?key=new-secret-token&count=10&stype=json"
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{key: oldURL})
	fallback := service.OpenAICodexTicketHarvestSource{}
	require.Equal(t, oldURL, h.settingService.GetOpenAICodexTicketHarvestSource(context.Background(), fallback).ExtractURL)
	rec := doUpdateSettings(t, h, map[string]any{
		key: newURL,
		service.SettingKeyOpenAICodexTicketHarvestProxyMode:       "extract",
		service.SettingKeyOpenAICodexTicketHarvestExtractProtocol: "socks5h",
	}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, newURL, repo.values[key])
	source := h.settingService.GetOpenAICodexTicketHarvestSource(context.Background(), fallback)
	require.Equal(t, newURL, source.ExtractURL)
	require.Equal(t, "extract", source.Mode)
	require.Equal(t, "socks5h", source.ExtractProtocol)
	require.NotContains(t, rec.Body.String(), "new-secret")
	require.Contains(t, rec.Body.String(), `"openai_codex_ticket_harvest_extract_configured":true`)
	for _, body := range []map[string]any{{"site_name": "updated"}, {key: ""}, {key: service.MaskOpenAICodexTicketHarvestExtractURL(newURL)}} {
		rec = doUpdateSettings(t, h, body, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Equal(t, newURL, repo.values[key])
		require.Equal(t, "extract", repo.values[service.SettingKeyOpenAICodexTicketHarvestProxyMode])
		require.Equal(t, "socks5h", repo.values[service.SettingKeyOpenAICodexTicketHarvestExtractProtocol])
	}
	get := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(get)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
	h.GetSettings(c)
	require.Equal(t, http.StatusOK, get.Code)
	require.NotContains(t, get.Body.String(), "new-secret")
	require.Contains(t, get.Body.String(), "http://supplier.example:8089/••••")
}

func TestSettingsCodexTicketExtractRejectsInvalidWithoutLeakingURL(t *testing.T) {
	key := service.SettingKeyOpenAICodexTicketHarvestExtractURL
	for _, body := range []map[string]any{
		{key: "ftp://supplier.example/private-token"},
		{key: "http://127.0.0.1/private-token"},
		{key: "https://127.0.0.1/private-token"},
		{service.SettingKeyOpenAICodexTicketHarvestProxyMode: "invalid"},
		{service.SettingKeyOpenAICodexTicketHarvestProxyMode: "extract"},
		{service.SettingKeyOpenAICodexTicketHarvestExtractProtocol: "ftp"},
	} {
		h, repo := newStepUpSwitchTestHandler(t, nil)
		rec := doUpdateSettings(t, h, body, nil)
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		require.NotContains(t, rec.Body.String(), "private-token")
		require.Empty(t, repo.values[key])
	}
}

func TestSettingsCodexTicketPolicyDefaultsAndPartialUpdates(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, nil)
	ctx := context.Background()
	require.True(t, h.settingService.GetOpenAICodexTicketPolicy(ctx).ModelMismatchInvalidation)
	require.True(t, h.settingService.GetOpenAICodexTicketPolicy(ctx).UseHarvestProxy)
	keys := []string{service.SettingKeyOpenAICodexTicketModelMismatchInvalidation, service.SettingKeyOpenAICodexTicketUseHarvestProxy}
	for _, enabled := range []bool{false, true} {
		for _, key := range keys {
			rec := doUpdateSettings(t, h, map[string]any{key: enabled}, nil)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.Equal(t, enabled, repo.values[key] == "true")
		}
		policy := h.settingService.GetOpenAICodexTicketPolicy(ctx)
		require.Equal(t, enabled, policy.ModelMismatchInvalidation)
		require.Equal(t, enabled, policy.UseHarvestProxy)
		rec := doUpdateSettings(t, h, map[string]any{"site_name": "保留门票策略"}, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Equal(t, policy, h.settingService.GetOpenAICodexTicketPolicy(ctx))
		get := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(get)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
		h.GetSettings(c)
		require.Equal(t, http.StatusOK, get.Code)
		for _, key := range keys {
			require.Contains(t, get.Body.String(), `"`+key+`":`+repo.values[key])
			require.Contains(t, rec.Body.String(), `"`+key+`":`+repo.values[key])
		}
	}
}
