//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSafeHeaderValueForLogRedactsModelTestAuthorization(t *testing.T) {
	require.Equal(
		t,
		"Bearer [redacted]",
		safeHeaderValueForLog(ModelTestAuthorizationHeader, "Bearer panel-session-secret"),
	)
}
