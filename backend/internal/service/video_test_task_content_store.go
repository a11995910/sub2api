package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	defaultVideoTestTaskContentMaxBytes int64 = 512 << 20
	videoTestTaskContentRetention             = 30 * 24 * time.Hour
)

var (
	ErrVideoTestTaskContentNotFound = errors.New("video test task content not found")
	ErrVideoTestTaskContentInvalid  = errors.New("video test task content is invalid")
	videoTestTaskContentNamePattern = regexp.MustCompile(`^[a-f0-9]{64}\.mp4$`)
)

type VideoTestTaskContentStoreConfig struct {
	Directory string
	MaxBytes  int64
}

type VideoTestTaskContent struct {
	Path      string
	Size      int64
	CreatedAt time.Time
}

// VideoTestTaskContentStore 将模型测试台视频保存在持久化数据目录中。
// 文件名由内部任务 ID 哈希生成，避免上游任务 ID 或用户输入参与路径拼接。
type VideoTestTaskContentStore struct {
	directory string
	maxBytes  int64
}

func NewVideoTestTaskContentStore(cfg VideoTestTaskContentStoreConfig) *VideoTestTaskContentStore {
	directory := strings.TrimSpace(cfg.Directory)
	if directory == "" {
		directory = filepath.Join("data", "generated-videos")
	}
	if absolute, err := filepath.Abs(directory); err == nil {
		directory = absolute
	}
	maxBytes := cfg.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultVideoTestTaskContentMaxBytes
	}
	return &VideoTestTaskContentStore{directory: filepath.Clean(directory), maxBytes: maxBytes}
}

func (s *VideoTestTaskContentStore) Resolve(taskID string, now time.Time) (VideoTestTaskContent, error) {
	if s == nil || strings.TrimSpace(taskID) == "" {
		return VideoTestTaskContent{}, ErrVideoTestTaskContentNotFound
	}
	path := s.path(taskID)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return VideoTestTaskContent{}, ErrVideoTestTaskContentNotFound
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if info.Size() <= 0 || info.Size() > s.maxBytes || !now.Before(info.ModTime().Add(videoTestTaskContentRetention)) {
		return VideoTestTaskContent{}, ErrVideoTestTaskContentNotFound
	}
	if err := validateStoredVideoFile(path); err != nil {
		return VideoTestTaskContent{}, ErrVideoTestTaskContentNotFound
	}
	return VideoTestTaskContent{Path: path, Size: info.Size(), CreatedAt: info.ModTime()}, nil
}

func (s *VideoTestTaskContentStore) Begin(ctx context.Context, taskID string) (*VideoTestTaskContentWriter, error) {
	if s == nil || strings.TrimSpace(taskID) == "" {
		return nil, ErrVideoTestTaskContentInvalid
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(s.directory, 0o750); err != nil {
		return nil, fmt.Errorf("create video content directory: %w", err)
	}
	file, err := os.CreateTemp(s.directory, ".video-content-*")
	if err != nil {
		return nil, fmt.Errorf("create video content temp file: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return nil, fmt.Errorf("set video content permissions: %w", err)
	}
	return &VideoTestTaskContentWriter{
		store:     s,
		file:      file,
		tempPath:  file.Name(),
		finalPath: s.path(taskID),
	}, nil
}

func (s *VideoTestTaskContentStore) Delete(taskID string) error {
	if s == nil || strings.TrimSpace(taskID) == "" {
		return nil
	}
	err := os.Remove(s.path(taskID))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *VideoTestTaskContentStore) Cleanup(now time.Time) (int, error) {
	if s == nil {
		return 0, nil
	}
	entries, err := os.ReadDir(s.directory)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read video content directory: %w", err)
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	deleted := 0
	var cleanupErr error
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil || !info.Mode().IsRegular() {
			continue
		}
		name := entry.Name()
		expiredVideo := videoTestTaskContentNamePattern.MatchString(name) && !now.Before(info.ModTime().Add(videoTestTaskContentRetention))
		staleTemporary := strings.HasPrefix(name, ".video-content-") && !now.Before(info.ModTime().Add(time.Hour))
		if !expiredVideo && !staleTemporary {
			continue
		}
		if removeErr := os.Remove(filepath.Join(s.directory, name)); removeErr != nil {
			cleanupErr = errors.Join(cleanupErr, removeErr)
			continue
		}
		deleted++
	}
	return deleted, cleanupErr
}

func (s *VideoTestTaskContentStore) path(taskID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(taskID)))
	return filepath.Join(s.directory, hex.EncodeToString(sum[:])+".mp4")
}

type VideoTestTaskContentWriter struct {
	store     *VideoTestTaskContentStore
	file      *os.File
	tempPath  string
	finalPath string
	size      int64
	closed    bool
	writeErr  error
}

func (w *VideoTestTaskContentWriter) Write(data []byte) (int, error) {
	if w == nil || w.file == nil || w.closed {
		return 0, ErrVideoTestTaskContentInvalid
	}
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	if int64(len(data)) > w.store.maxBytes-w.size {
		w.writeErr = fmt.Errorf("%w: exceeds %d bytes", ErrVideoTestTaskContentInvalid, w.store.maxBytes)
		return 0, w.writeErr
	}
	n, err := w.file.Write(data)
	w.size += int64(n)
	if err != nil {
		w.writeErr = err
	}
	return n, err
}

func (w *VideoTestTaskContentWriter) Commit(now time.Time) (VideoTestTaskContent, error) {
	if w == nil || w.file == nil || w.closed || w.writeErr != nil || w.size <= 0 {
		if w != nil && w.writeErr != nil {
			return VideoTestTaskContent{}, w.writeErr
		}
		return VideoTestTaskContent{}, ErrVideoTestTaskContentInvalid
	}
	if err := w.file.Sync(); err != nil {
		return VideoTestTaskContent{}, fmt.Errorf("sync video content: %w", err)
	}
	if err := w.file.Close(); err != nil {
		return VideoTestTaskContent{}, fmt.Errorf("close video content: %w", err)
	}
	w.closed = true
	if err := validateStoredVideoFile(w.tempPath); err != nil {
		return VideoTestTaskContent{}, err
	}
	if err := os.Rename(w.tempPath, w.finalPath); err != nil {
		return VideoTestTaskContent{}, fmt.Errorf("publish video content: %w", err)
	}
	w.tempPath = ""
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if err := os.Chtimes(w.finalPath, now, now); err != nil {
		_ = os.Remove(w.finalPath)
		return VideoTestTaskContent{}, fmt.Errorf("set video content timestamp: %w", err)
	}
	return VideoTestTaskContent{Path: w.finalPath, Size: w.size, CreatedAt: now}, nil
}

func (w *VideoTestTaskContentWriter) Abort() {
	if w == nil {
		return
	}
	if w.file != nil && !w.closed {
		_ = w.file.Close()
		w.closed = true
	}
	if w.tempPath != "" {
		_ = os.Remove(w.tempPath)
		w.tempPath = ""
	}
}

func validateStoredVideoFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return ErrVideoTestTaskContentInvalid
	}
	defer func() { _ = file.Close() }()
	header := make([]byte, 512)
	n, err := io.ReadFull(file, header)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return ErrVideoTestTaskContentInvalid
	}
	if n < 12 || http.DetectContentType(header[:n]) != "video/mp4" {
		return ErrVideoTestTaskContentInvalid
	}
	return nil
}
