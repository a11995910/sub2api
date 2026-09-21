package service

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type codexTicketSettingRepo struct {
	*codexPolicyMigrationRepoStub
	err error
}

func (r *codexTicketSettingRepo) GetValue(ctx context.Context, key string) (string, error) {
	if r.err != nil {
		return "", r.err
	}
	return r.codexPolicyMigrationRepoStub.GetValue(ctx, key)
}

func (r *codexTicketSettingRepo) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	if r.err != nil {
		return nil, r.err
	}
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			values[key] = value
		}
	}
	return values, nil
}

func TestCodexTicketHarvestSourceRuntimeSettingsAndHotReload(t *testing.T) {
	repo := &codexTicketSettingRepo{codexPolicyMigrationRepoStub: &codexPolicyMigrationRepoStub{values: map[string]string{}}}
	settings := NewSettingService(repo, &config.Config{})
	fallback := normalizeOpenAICodexTicketHarvestSource(OpenAICodexTicketHarvestSource{ProxyURL: "http://fallback.example:8080"})
	require.Equal(t, fallback, settings.GetOpenAICodexTicketHarvestSource(context.Background(), fallback))
	repo.values[SettingKeyOpenAICodexTicketHarvestProxyMode] = "extract"
	repo.values[SettingKeyOpenAICodexTicketHarvestExtractURL] = "https://supplier.example/first-secret"
	repo.values[SettingKeyOpenAICodexTicketHarvestExtractProtocol] = "socks5h"
	settings.InvalidateOpenAICodexTicketHarvestSourceCache()
	source := settings.GetOpenAICodexTicketHarvestSource(context.Background(), fallback)
	require.Equal(t, "extract", source.Mode)
	require.Equal(t, "https://supplier.example/first-secret", source.ExtractURL)
	require.Equal(t, "socks5h", source.ExtractProtocol)
	repo.values[SettingKeyOpenAICodexTicketHarvestExtractURL] = "https://supplier.example/second-secret"
	settings.openAICodexTicketHarvestSourceCache.Store(&cachedOpenAICodexTicketHarvestSource{value: source})
	source = settings.GetOpenAICodexTicketHarvestSource(context.Background(), fallback)
	require.Equal(t, "https://supplier.example/second-secret", source.ExtractURL)
	repo.err = errors.New("数据库暂不可用")
	settings.openAICodexTicketHarvestSourceCache.Store(&cachedOpenAICodexTicketHarvestSource{value: source})
	require.Equal(t, source, settings.GetOpenAICodexTicketHarvestSource(context.Background(), fallback), "数据库短暂失败保留完整已知配置")
}

type codexTicketHarvestSourceBlockingRepo struct {
	*codexTicketSettingRepo
	started chan struct{}
	release chan struct{}
	reads   atomic.Int32
}

func (r *codexTicketHarvestSourceBlockingRepo) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	values, err := r.codexTicketSettingRepo.GetMultiple(ctx, keys)
	if r.reads.Add(1) == 1 {
		close(r.started)
		select {
		case <-r.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return values, err
}

func TestCodexTicketHarvestSourceInvalidationDiscardsInflightOldRead(t *testing.T) {
	repo := &codexTicketHarvestSourceBlockingRepo{
		codexTicketSettingRepo: &codexTicketSettingRepo{codexPolicyMigrationRepoStub: &codexPolicyMigrationRepoStub{values: map[string]string{
			SettingKeyOpenAICodexTicketHarvestProxyMode:  "extract",
			SettingKeyOpenAICodexTicketHarvestExtractURL: "https://supplier.example/old-secret",
		}}},
		started: make(chan struct{}), release: make(chan struct{}),
	}
	settings := NewSettingService(repo, &config.Config{})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	first := make(chan OpenAICodexTicketHarvestSource, 1)
	go func() { first <- settings.GetOpenAICodexTicketHarvestSource(ctx, OpenAICodexTicketHarvestSource{}) }()
	select {
	case <-repo.started:
	case <-ctx.Done():
		t.Fatal("旧配置查询未启动")
	}
	repo.values[SettingKeyOpenAICodexTicketHarvestExtractURL] = "https://supplier.example/new-secret"
	settings.InvalidateOpenAICodexTicketHarvestSourceCache()
	current := settings.GetOpenAICodexTicketHarvestSource(ctx, OpenAICodexTicketHarvestSource{})
	require.Equal(t, "https://supplier.example/new-secret", current.ExtractURL)
	close(repo.release)
	select {
	case value := <-first:
		require.Equal(t, current, value, "在途旧查询完成后必须重取新来源")
	case <-ctx.Done():
		t.Fatal("旧配置查询未结束")
	}
	require.Equal(t, current, settings.GetOpenAICodexTicketHarvestSource(ctx, OpenAICodexTicketHarvestSource{}), "旧查询不得覆盖已失效缓存")
	require.EqualValues(t, 2, repo.reads.Load())
}

func TestCodexTicketEnabledRuntimeSettingOverridesYaml(t *testing.T) {
	repo := &codexTicketSettingRepo{codexPolicyMigrationRepoStub: &codexPolicyMigrationRepoStub{values: map[string]string{}}}
	settings := NewSettingService(repo, &config.Config{})
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: false, FailClosed: true}, nil)
	svc.settingService = settings
	account := ticketTestAccount(41)
	svc.storeOpenAICodexTicket(context.Background(), account, &openAICodexTicket{ProxyURL: "http://192.0.2.10:8080",
		AccountID:  41,
		Model:      "gpt-6-astra",
		State:      fakeCodexTicketState(292),
		Length:     292,
		CapturedAt: time.Now(),
		ExpiresAt:  time.Now().Add(time.Hour),
	})

	h := http.Header{}
	h.Set(openAICodexTurnStateHeader, "client-state")
	require.False(t, svc.openAICodexTicketEnabled())
	require.NoError(t, svc.applyOpenAICodexTicket(context.Background(), account, "gpt-6-astra", h))
	require.Equal(t, "client-state", h.Get(openAICodexTurnStateHeader))

	repo.values[SettingKeyOpenAICodexTicketEnabled] = "true"
	settings.InvalidateOpenAICodexTicketEnabledCache()
	require.True(t, svc.openAICodexTicketEnabled())
	h = http.Header{}
	h.Set(openAICodexTurnStateHeader, "client-state")
	require.NoError(t, svc.applyOpenAICodexTicket(context.Background(), account, "gpt-6-astra", h))
	require.Equal(t, fakeCodexTicketState(292), h.Get(openAICodexTurnStateHeader))

	repo.values[SettingKeyOpenAICodexTicketEnabled] = "false"
	settings.InvalidateOpenAICodexTicketEnabledCache()
	require.False(t, svc.openAICodexTicketEnabled())
	h = http.Header{}
	h.Set(openAICodexTurnStateHeader, "client-state")
	require.NoError(t, svc.applyOpenAICodexTicket(context.Background(), account, "gpt-6-astra", h))
	require.Equal(t, "client-state", h.Get(openAICodexTurnStateHeader))
}

func TestRefreshOpenAICodexTickets_DisabledSkipsHarvest(t *testing.T) {
	upstream := &httpUpstreamRecorder{}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{
		Enabled:         false,
		HarvestProxyURL: "socks5h://proxy.example.com:1080",
	}, upstream)
	svc.accountRepo = &codexTicketRefreshRepo{accounts: []Account{*ticketTestAccount(41)}}
	svc.refreshOpenAICodexTickets(context.Background())
	require.Empty(t, upstream.requests)
}

func TestCodexTicketProxyRuntimeSettingAndFallback(t *testing.T) {
	repo := &codexTicketSettingRepo{codexPolicyMigrationRepoStub: &codexPolicyMigrationRepoStub{values: map[string]string{}}}
	settings := NewSettingService(repo, &config.Config{})
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{HarvestProxyURL: "http://fallback.example.com:8080"}, nil)
	svc.settingService = settings
	require.Equal(t, "http://fallback.example.com:8080", svc.openAICodexTicketHarvestProxyURL())
	repo.values[SettingKeyOpenAICodexTicketHarvestProxyURL] = "socks5h://user:secret@first.example.com:1080"
	settings.InvalidateOpenAICodexTicketHarvestProxyCache()
	require.Equal(t, repo.values[SettingKeyOpenAICodexTicketHarvestProxyURL], svc.openAICodexTicketHarvestProxyURL())
	repo.values[SettingKeyOpenAICodexTicketHarvestProxyURL] = "http://second.example.com:8080"
	settings.InvalidateOpenAICodexTicketHarvestProxyCache()
	require.Equal(t, "http://second.example.com:8080", svc.openAICodexTicketHarvestProxyURL())
	// Simulate another instance's settings write after the local cache expires.
	repo.values[SettingKeyOpenAICodexTicketHarvestProxyURL] = "https://third.example.com:443"
	settings.openAICodexTicketHarvestProxyCache.Store(&cachedOpenAICodexTicketHarvestProxy{value: "http://second.example.com:8080", expiresAt: time.Now().Add(-time.Second).UnixNano()})
	require.Equal(t, "https://third.example.com:443", svc.openAICodexTicketHarvestProxyURL())
	repo.err = errors.New("database unavailable")
	settings.openAICodexTicketHarvestProxyCache.Store(&cachedOpenAICodexTicketHarvestProxy{value: "https://third.example.com:443", expiresAt: 0})
	require.Equal(t, "https://third.example.com:443", svc.openAICodexTicketHarvestProxyURL())
}

func TestCodexTicketProxyMaskAndValidation(t *testing.T) {
	for _, raw := range []string{"http://user:secret@proxy.example.com:8080", "socks5h://user:secret@proxy.example.com:1080", "https://user:secret@[::1]:443"} {
		require.NoError(t, ValidateOpenAICodexTicketHarvestProxyURL(raw))
		masked := MaskProxyURL(raw)
		require.NotContains(t, masked, "secret")
		require.True(t, IsMaskedProxyURL(masked))
	}
	require.True(t, IsMaskedProxyURL(""))
	require.False(t, IsMaskedProxyURL("http://user:secret***suffix@proxy.example.com:8080"))
	for _, raw := range []string{"user:secret@host:1234", "http://user:secret@", "ftp://user:secret@host:1234", "http://user:secret@host:99999", "http://host:1234/?password=secret", "http://host:1234/#secret", "http://user:secret%zz@host:1234"} {
		err := ValidateOpenAICodexTicketHarvestProxyURL(raw)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "secret")
		require.Empty(t, MaskProxyURL(raw))
	}
}

func TestCodexTicketSettingsRefreshDoesNotMutateSharedConfig(t *testing.T) {
	cfg := &config.Config{}
	svc := NewSettingService(&codexTicketSettingRepo{codexPolicyMigrationRepoStub: &codexPolicyMigrationRepoStub{values: map[string]string{SettingKeyOpenAICodexTicketEnabled: "true"}}}, cfg)
	svc.refreshCachedSettings(&SystemSettings{OpenAICodexTicketEnabled: true})
	require.False(t, cfg.Gateway.OpenAICodexTicket.Enabled, "runtime settings must not write the shared immutable startup configuration")
	require.True(t, svc.GetOpenAICodexTicketEnabled(context.Background(), false))
}
