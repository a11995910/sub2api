package handler

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func generatedImageHandlerTestPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{G: 255, A: 255})
	var out bytes.Buffer
	require.NoError(t, png.Encode(&out, img))
	return out.Bytes()
}

func TestGeneratedImageHandlerGet(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := service.NewGeneratedImageStore(service.GeneratedImageStoreConfig{Directory: t.TempDir()})
	now := time.Now().UTC()
	saved, err := store.Save(context.Background(), generatedImageHandlerTestPNG(t), now)
	require.NoError(t, err)
	handler := NewGeneratedImageHandler(store)
	router := gin.New()
	router.GET("/generated-images/:filename", handler.Get)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/generated-images/"+saved.Name, nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "image/png", rec.Header().Get("Content-Type"))
	require.Contains(t, rec.Header().Get("Cache-Control"), "private, max-age=")
	require.NotEmpty(t, rec.Body.Bytes())
}

func TestGeneratedImageHandlerRejectsInvalidName(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewGeneratedImageHandler(service.NewGeneratedImageStore(service.GeneratedImageStoreConfig{Directory: t.TempDir()}))
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Params = gin.Params{{Key: "filename", Value: "..-secret.png"}}
	c.Request = httptest.NewRequest(http.MethodGet, "/generated-images/..-secret.png", nil)

	handler.Get(c)

	require.Equal(t, http.StatusNotFound, rec.Code)
}
