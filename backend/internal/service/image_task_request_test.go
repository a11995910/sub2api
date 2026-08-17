package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseImageTaskRequestLeavesLegacySyncBodyUntouched(t *testing.T) {
	body := []byte(`{"prompt":"dog","model":"gpt-image-2"}`)

	parsed, err := ParseImageTaskRequest(body)

	require.NoError(t, err)
	require.False(t, parsed.Async)
	require.Equal(t, body, parsed.UpstreamBody)
	require.Empty(t, parsed.ClientRequestID)
	require.Empty(t, parsed.Fingerprint)
}

func TestParseImageTaskRequestCleansAsyncPayload(t *testing.T) {
	parsed, err := ParseImageTaskRequest([]byte(`{
		"async": true,
		"client_request_id": "request_7f98c6d2",
		"model": "gpt-image-2",
		"prompt": "dog",
		"size": "1:1"
	}`))

	require.NoError(t, err)
	require.True(t, parsed.Async)
	require.Equal(t, "request_7f98c6d2", parsed.ClientRequestID)
	require.JSONEq(t, `{"model":"gpt-image-2","prompt":"dog","size":"1024x1024"}`, string(parsed.UpstreamBody))
	require.Len(t, parsed.Fingerprint, 64)
}

func TestParseImageTaskRequestCleansExplicitSyncControlFields(t *testing.T) {
	parsed, err := ParseImageTaskRequest([]byte(`{
		"async": false,
		"client_request_id": "ignored_for_sync",
		"model": "gpt-image-2",
		"prompt": "dog"
	}`))

	require.NoError(t, err)
	require.False(t, parsed.Async)
	require.JSONEq(t, `{"model":"gpt-image-2","prompt":"dog"}`, string(parsed.UpstreamBody))
}

func TestParseImageTaskRequestUsesStableFingerprint(t *testing.T) {
	first, err := ParseImageTaskRequest([]byte(`{"async":true,"client_request_id":"same","prompt":"dog","model":"gpt-image-2"}`))
	require.NoError(t, err)
	second, err := ParseImageTaskRequest([]byte(`{"model":"gpt-image-2","prompt":"dog","client_request_id":"same","async":true}`))
	require.NoError(t, err)

	require.Equal(t, first.Fingerprint, second.Fingerprint)
	require.Equal(t, first.UpstreamBody, second.UpstreamBody)
}

func TestParseImageTaskRequestValidatesClientRequestID(t *testing.T) {
	for _, tc := range []struct {
		name string
		id   string
	}{
		{name: "empty", id: ""},
		{name: "too long", id: strings.Repeat("a", 65)},
		{name: "invalid characters", id: "canvas.request"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseImageTaskRequest([]byte(`{"async":true,"client_request_id":"` + tc.id + `","prompt":"dog"}`))
			require.ErrorIs(t, err, ErrInvalidImageTaskClientRequestID)
		})
	}
}

func TestParseImageTaskRequestLetsLegacyHandlerRejectMalformedJSON(t *testing.T) {
	body := []byte(`{"async":true`)

	parsed, err := ParseImageTaskRequest(body)

	require.NoError(t, err)
	require.False(t, parsed.Async)
	require.Equal(t, body, parsed.UpstreamBody)
}
