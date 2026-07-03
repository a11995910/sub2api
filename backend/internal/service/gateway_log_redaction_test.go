package service

import (
	"net/http"
	"strings"
	"testing"
)

func TestBuildClaudeMimicDebugLineRedactsIdentifiers(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer secret-token")
	req.Header.Set("X-Claude-Code-Session-Id", "123e4567-e89b-12d3-a456-426614174000")
	req.Header.Set("x-client-request-id", "req-1234567890abcdef")
	req.Header.Set("User-Agent", "claude-cli/2.1.161 (external, cli)")

	body := []byte(`{
		"metadata":{"user_id":"clientid:accountuuid:sessionuuid:2.1.161"},
		"system":[{"type":"text","text":"You are Claude Code, Anthropic's official CLI for Claude"}]
	}`)

	line := buildClaudeMimicDebugLine(req, body, &Account{ID: 1, Name: "anthropic-oauth"}, "oauth", true)
	for _, leaked := range []string{
		"secret-token",
		"clientid:accountuuid:sessionuuid:2.1.161",
		"123e4567-e89b-12d3-a456-426614174000",
		"req-1234567890abcdef",
		"You are Claude Code",
	} {
		if strings.Contains(line, leaked) {
			t.Fatalf("debug line leaked %q: %s", leaked, line)
		}
	}
	if !strings.Contains(line, "Bearer [redacted]") {
		t.Fatalf("authorization was not redacted: %s", line)
	}
	if !strings.Contains(line, "meta.user_id=\"clie...") {
		t.Fatalf("metadata user id was not shortened: %s", line)
	}
}

func TestRedactGatewayBodyForLogRedactsKnownFingerprintFields(t *testing.T) {
	body := []byte(`{
		"metadata":{
			"user_id":"clientid:accountuuid:sessionuuid:2.1.161",
			"session_id":"session-secret",
			"client_id":"client-secret"
		},
		"prompt_cache_key":"cache-secret",
		"messages":[{"role":"user","content":"hello"}]
	}`)

	got := string(redactGatewayBodyForLog(body))
	for _, leaked := range []string{"clientid:accountuuid", "session-secret", "client-secret", "cache-secret"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("redacted body leaked %q: %s", leaked, got)
		}
	}
	if !strings.Contains(got, `"content":"hello"`) {
		t.Fatalf("redacted body should preserve non-sensitive content shape: %s", got)
	}
}
