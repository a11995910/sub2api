package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractUpstreamErrorMessageSupportsTopLevelErrorString(t *testing.T) {
	body := []byte(`{"code":"Client specified an invalid argument","error":"Generated image rejected by content moderation.","usage":{"cost_in_usd_ticks":500000000}}`)

	require.Equal(t, "Generated image rejected by content moderation.", ExtractUpstreamErrorMessage(body))
}

func TestExtractUpstreamErrorMessageSupportsNestedJSONStringError(t *testing.T) {
	body := []byte(`{"error":{"message":"{\"error\":\"Generated image rejected by content moderation.\"}"}}`)

	require.Equal(t, "Generated image rejected by content moderation.", ExtractUpstreamErrorMessage(body))
}

func TestExtractUpstreamErrorMessageKeepsExistingErrorMessageShape(t *testing.T) {
	body := []byte(`{"type":"error","error":{"type":"invalid_request_error","message":"bad request"}}`)

	require.Equal(t, "bad request", ExtractUpstreamErrorMessage(body))
}
