package handler

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type GeneratedImageHandler struct {
	store *service.GeneratedImageStore
}

func NewGeneratedImageHandler(store *service.GeneratedImageStore) *GeneratedImageHandler {
	return &GeneratedImageHandler{store: store}
}

// Get 提供 24 小时内生成图片的公开只读访问。
func (h *GeneratedImageHandler) Get(c *gin.Context) {
	if h == nil || h.store == nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	now := time.Now().UTC()
	image, err := h.store.Resolve(c.Param("filename"), now)
	if err != nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	file, err := os.Open(image.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	defer func() { _ = file.Close() }()
	remaining := int64(image.ExpiresAt.Sub(now).Seconds())
	if remaining < 0 {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	c.Header("Content-Type", image.MIMEType)
	c.Header("Cache-Control", fmt.Sprintf("private, max-age=%d", remaining))
	http.ServeContent(c.Writer, c.Request, image.Name, image.CreatedAt, file)
}
