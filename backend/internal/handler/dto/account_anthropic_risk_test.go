package dto

import (
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestAccountFromServiceShallowAnthropicOAuthRisk(t *testing.T) {
	src := &service.Account{
		ID:       1,
		Platform: service.PlatformAnthropic,
		Type:     service.AccountTypeOAuth,
		Extra: map[string]any{
			"enable_tls_fingerprint":     true,
			"custom_base_url_enabled":    true,
			"tls_fingerprint_profile_id": float64(3),
		},
	}

	got := AccountFromServiceShallow(src)
	if got.AnthropicForwardingRisk == nil {
		t.Fatal("expected risk summary")
	}
	if got.AnthropicForwardingRisk.Level != "high" {
		t.Fatalf("unexpected risk level: %s", got.AnthropicForwardingRisk.Level)
	}
	assertReasonContains(t, got.AnthropicForwardingRisk.Reasons, "TLS 指纹伪装")
	assertReasonContains(t, got.AnthropicForwardingRisk.Reasons, "服务器默认出口")
	assertReasonContains(t, got.AnthropicForwardingRisk.Reasons, "自定义 Base URL")
}

func TestAccountFromServiceShallowAnthropicAPIKeyPassthroughRisk(t *testing.T) {
	src := &service.Account{
		ID:       2,
		Platform: service.PlatformAnthropic,
		Type:     service.AccountTypeAPIKey,
		Extra: map[string]any{
			"anthropic_passthrough": true,
		},
	}

	got := AccountFromServiceShallow(src)
	if got.AnthropicForwardingRisk == nil {
		t.Fatal("expected risk summary")
	}
	if got.AnthropicForwardingRisk.Level != "medium" {
		t.Fatalf("unexpected risk level: %s", got.AnthropicForwardingRisk.Level)
	}
	assertReasonContains(t, got.AnthropicForwardingRisk.Reasons, "API Key 自动透传")
}

func TestAccountFromServiceShallowNonAnthropicRiskOmitted(t *testing.T) {
	got := AccountFromServiceShallow(&service.Account{
		ID:       3,
		Platform: service.PlatformOpenAI,
		Type:     service.AccountTypeOAuth,
	})
	if got.AnthropicForwardingRisk != nil {
		t.Fatalf("unexpected risk summary: %+v", got.AnthropicForwardingRisk)
	}
}

func assertReasonContains(t *testing.T, reasons []string, want string) {
	t.Helper()
	for _, reason := range reasons {
		if strings.Contains(reason, want) {
			return
		}
	}
	t.Fatalf("expected reason containing %q in %#v", want, reasons)
}
