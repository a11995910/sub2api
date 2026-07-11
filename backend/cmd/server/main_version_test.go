package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatVersionInfoIncludesExplicitVideoPricingCapability(t *testing.T) {
	info := formatVersionInfo("0.1.151", "7d5b9bc6bb6d", "2026-07-11T15:57:07+08:00")

	require.Equal(
		t,
		"Sub2API 0.1.151 (commit: 7d5b9bc6bb6d, built: 2026-07-11T15:57:07+08:00, capabilities: explicit_video_pricing_per_second)",
		info,
	)
}
