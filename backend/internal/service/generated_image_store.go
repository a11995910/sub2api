package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	_ "golang.org/x/image/webp"
)

const (
	generatedImageRetention = 24 * time.Hour
	generatedImageMaxBytes  = 64 << 20
)

var (
	ErrGeneratedImageInvalid  = errors.New("generated image is invalid")
	ErrGeneratedImageNotFound = errors.New("generated image not found")
	generatedImageNamePattern = regexp.MustCompile(`^[a-f0-9]{32}\.(png|jpg|webp)$`)
)

type GeneratedImageStoreConfig struct {
	Directory    string
	PublicOrigin string
}

type GeneratedImage struct {
	Name      string
	Path      string
	MIMEType  string
	CreatedAt time.Time
	ExpiresAt time.Time
	PublicURL string
}

type GeneratedImageStore struct {
	directory    string
	publicOrigin string
}

func NewGeneratedImageStore(cfg GeneratedImageStoreConfig) *GeneratedImageStore {
	directory := strings.TrimSpace(cfg.Directory)
	if directory == "" {
		directory = filepath.Join("data", "generated-images")
	}
	if absolute, err := filepath.Abs(directory); err == nil {
		directory = absolute
	}
	return &GeneratedImageStore{
		directory:    filepath.Clean(directory),
		publicOrigin: generatedImagePublicOrigin(cfg.PublicOrigin),
	}
}

func (s *GeneratedImageStore) Save(ctx context.Context, data []byte, now time.Time) (GeneratedImage, error) {
	if s == nil {
		return GeneratedImage{}, fmt.Errorf("save generated image: %w", ErrGeneratedImageInvalid)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return GeneratedImage{}, err
	}
	if len(data) == 0 || len(data) > generatedImageMaxBytes {
		return GeneratedImage{}, ErrGeneratedImageInvalid
	}
	mimeType, extension, err := detectGeneratedImageType(data)
	if err != nil {
		return GeneratedImage{}, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if err := os.MkdirAll(s.directory, 0o750); err != nil {
		return GeneratedImage{}, fmt.Errorf("create generated image directory: %w", err)
	}

	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return GeneratedImage{}, fmt.Errorf("generate image name: %w", err)
	}
	name := hex.EncodeToString(random) + extension
	finalPath := filepath.Join(s.directory, name)
	temporary, err := os.CreateTemp(s.directory, ".generated-image-*")
	if err != nil {
		return GeneratedImage{}, fmt.Errorf("create generated image temp file: %w", err)
	}
	tempPath := temporary.Name()
	removeTemp := true
	defer func() {
		_ = temporary.Close()
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return GeneratedImage{}, fmt.Errorf("set generated image permissions: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		return GeneratedImage{}, fmt.Errorf("write generated image: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return GeneratedImage{}, fmt.Errorf("sync generated image: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return GeneratedImage{}, fmt.Errorf("close generated image: %w", err)
	}
	if err := os.Rename(tempPath, finalPath); err != nil {
		return GeneratedImage{}, fmt.Errorf("publish generated image: %w", err)
	}
	removeTemp = false
	if err := os.Chtimes(finalPath, now, now); err != nil {
		_ = os.Remove(finalPath)
		return GeneratedImage{}, fmt.Errorf("set generated image timestamp: %w", err)
	}

	return GeneratedImage{
		Name:      name,
		Path:      finalPath,
		MIMEType:  mimeType,
		CreatedAt: now,
		ExpiresAt: now.Add(generatedImageRetention),
		PublicURL: s.publicURL(name),
	}, nil
}

func (s *GeneratedImageStore) Resolve(name string, now time.Time) (GeneratedImage, error) {
	if s == nil || !generatedImageNamePattern.MatchString(name) {
		return GeneratedImage{}, ErrGeneratedImageNotFound
	}
	path := filepath.Join(s.directory, name)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return GeneratedImage{}, ErrGeneratedImageNotFound
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	createdAt := info.ModTime()
	if !now.Before(createdAt.Add(generatedImageRetention)) {
		return GeneratedImage{}, ErrGeneratedImageNotFound
	}
	mimeType := generatedImageMIMEFromName(name)
	if mimeType == "" {
		return GeneratedImage{}, ErrGeneratedImageNotFound
	}
	return GeneratedImage{
		Name:      name,
		Path:      path,
		MIMEType:  mimeType,
		CreatedAt: createdAt,
		ExpiresAt: createdAt.Add(generatedImageRetention),
		PublicURL: s.publicURL(name),
	}, nil
}

func (s *GeneratedImageStore) Cleanup(now time.Time) (int, error) {
	if s == nil {
		return 0, nil
	}
	entries, err := os.ReadDir(s.directory)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read generated image directory: %w", err)
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	deleted := 0
	var cleanupErr error
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || !generatedImageNamePattern.MatchString(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || now.Before(info.ModTime().Add(generatedImageRetention)) {
			continue
		}
		if err := os.Remove(filepath.Join(s.directory, entry.Name())); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
			continue
		}
		deleted++
	}
	return deleted, cleanupErr
}

func detectGeneratedImageType(data []byte) (string, string, error) {
	_, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", "", ErrGeneratedImageInvalid
	}
	switch strings.ToLower(format) {
	case "png":
		return "image/png", ".png", nil
	case "jpeg":
		return "image/jpeg", ".jpg", nil
	case "webp":
		return "image/webp", ".webp", nil
	default:
		return "", "", ErrGeneratedImageInvalid
	}
}

func generatedImageMIMEFromName(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png":
		return "image/png"
	case ".jpg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	default:
		return ""
	}
}

func generatedImagePublicOrigin(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

func (s *GeneratedImageStore) publicURL(name string) string {
	if s.publicOrigin == "" {
		return "/generated-images/" + name
	}
	return s.publicOrigin + "/generated-images/" + name
}

func (s *GeneratedImageStore) PublicURL(name, origin string) string {
	if normalized := generatedImagePublicOrigin(origin); normalized != "" {
		return normalized + "/generated-images/" + name
	}
	return s.publicURL(name)
}
