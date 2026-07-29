//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestVideoTestTaskContentStorePublishesAndDeletesMP4Atomically(t *testing.T) {
	store := NewVideoTestTaskContentStore(VideoTestTaskContentStoreConfig{
		Directory: t.TempDir(), MaxBytes: 1 << 20,
	})
	now := time.Date(2026, 7, 29, 2, 0, 0, 0, time.UTC)
	mp4 := videoTestTaskMP4Fixture()

	writer, err := store.Begin(context.Background(), "local-task-1")
	require.NoError(t, err)
	t.Cleanup(writer.Abort)
	_, err = writer.Write(mp4)
	require.NoError(t, err)
	stored, err := writer.Commit(now)
	require.NoError(t, err)
	require.Equal(t, int64(len(mp4)), stored.Size)

	resolved, err := store.Resolve("local-task-1", now.Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, stored.Path, resolved.Path)

	require.NoError(t, store.Delete("local-task-1"))
	_, err = store.Resolve("local-task-1", now.Add(time.Hour))
	require.ErrorIs(t, err, ErrVideoTestTaskContentNotFound)
}

func TestVideoTestTaskContentStoreRejectsInvalidOrOversizedContent(t *testing.T) {
	store := NewVideoTestTaskContentStore(VideoTestTaskContentStoreConfig{
		Directory: t.TempDir(), MaxBytes: 16,
	})

	writer, err := store.Begin(context.Background(), "oversized")
	require.NoError(t, err)
	t.Cleanup(writer.Abort)
	_, err = writer.Write(videoTestTaskMP4Fixture())
	require.ErrorIs(t, err, ErrVideoTestTaskContentInvalid)
	_, err = writer.Commit(time.Now().UTC())
	require.ErrorIs(t, err, ErrVideoTestTaskContentInvalid)

	invalidWriter, err := store.Begin(context.Background(), "invalid")
	require.NoError(t, err)
	t.Cleanup(invalidWriter.Abort)
	_, err = invalidWriter.Write([]byte("not a video"))
	require.NoError(t, err)
	_, err = invalidWriter.Commit(time.Now().UTC())
	require.ErrorIs(t, err, ErrVideoTestTaskContentInvalid)
}

func TestVideoTestTaskContentStoreCleanupRemovesExpiredFiles(t *testing.T) {
	store := NewVideoTestTaskContentStore(VideoTestTaskContentStoreConfig{
		Directory: t.TempDir(), MaxBytes: 1 << 20,
	})
	now := time.Date(2026, 7, 29, 2, 0, 0, 0, time.UTC)
	writer, err := store.Begin(context.Background(), "expired")
	require.NoError(t, err)
	t.Cleanup(writer.Abort)
	_, err = writer.Write(videoTestTaskMP4Fixture())
	require.NoError(t, err)
	_, err = writer.Commit(now.Add(-31 * 24 * time.Hour))
	require.NoError(t, err)

	deleted, err := store.Cleanup(now)
	require.NoError(t, err)
	require.Equal(t, 1, deleted)
	_, err = store.Resolve("expired", now)
	require.True(t, errors.Is(err, ErrVideoTestTaskContentNotFound))
}

func videoTestTaskMP4Fixture() []byte {
	return []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'm', 'p', '4', '2', 0, 0, 2, 0, 'm', 'p', '4', '2', 'i', 's', 'o', 'm'}
}
