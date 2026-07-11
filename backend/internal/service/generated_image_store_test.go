package service

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func generatedImageTestPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var out bytes.Buffer
	require.NoError(t, png.Encode(&out, img))
	return out.Bytes()
}

func TestGeneratedImageStoreSaveAndResolve(t *testing.T) {
	dir := t.TempDir()
	store := NewGeneratedImageStore(GeneratedImageStoreConfig{
		Directory:    dir,
		PublicOrigin: "https://api.example.com/v1",
	})
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)

	saved, err := store.Save(context.Background(), generatedImageTestPNG(t), now)

	require.NoError(t, err)
	require.Regexp(t, `^[a-f0-9]{32}\.png$`, saved.Name)
	require.Equal(t, "image/png", saved.MIMEType)
	require.Equal(t, "https://api.example.com/generated-images/"+saved.Name, saved.PublicURL)
	require.Equal(t, now.Add(24*time.Hour), saved.ExpiresAt)
	info, err := os.Stat(saved.Path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	resolved, err := store.Resolve(saved.Name, now.Add(23*time.Hour))
	require.NoError(t, err)
	require.Equal(t, saved.Path, resolved.Path)
	require.Equal(t, saved.MIMEType, resolved.MIMEType)
}

func TestGeneratedImageStoreRejectsInvalidAndExpiredImages(t *testing.T) {
	store := NewGeneratedImageStore(GeneratedImageStoreConfig{Directory: t.TempDir()})
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)

	_, err := store.Save(context.Background(), []byte("not-an-image"), now)
	require.ErrorIs(t, err, ErrGeneratedImageInvalid)

	saved, err := store.Save(context.Background(), generatedImageTestPNG(t), now)
	require.NoError(t, err)
	_, err = store.Resolve(saved.Name, now.Add(24*time.Hour))
	require.ErrorIs(t, err, ErrGeneratedImageNotFound)
	_, err = store.Resolve("../"+saved.Name, now)
	require.ErrorIs(t, err, ErrGeneratedImageNotFound)
}

func TestGeneratedImageStoreCleanupRemovesOnlyExpiredImages(t *testing.T) {
	store := NewGeneratedImageStore(GeneratedImageStoreConfig{Directory: t.TempDir()})
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	expired, err := store.Save(context.Background(), generatedImageTestPNG(t), now.Add(-25*time.Hour))
	require.NoError(t, err)
	active, err := store.Save(context.Background(), generatedImageTestPNG(t), now.Add(-time.Hour))
	require.NoError(t, err)

	deleted, err := store.Cleanup(now)

	require.NoError(t, err)
	require.Equal(t, 1, deleted)
	_, err = os.Stat(expired.Path)
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(active.Path)
	require.NoError(t, err)
}
