//go:build unit

package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadVideoStorageFromEnv(t *testing.T) {
	resetViperWithJWTSecret(t)
	t.Setenv("VIDEO_STORAGE_PATH", "/app/data/test-videos")
	t.Setenv("VIDEO_STORAGE_MAX_BYTES", "67108864")

	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, "/app/data/test-videos", cfg.VideoStorage.StoragePath)
	require.Equal(t, int64(67108864), cfg.VideoStorage.MaxBytes)
}
