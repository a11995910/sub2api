package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAsyncImageHandlerExecuteRestoresAuthenticatedContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(3)
	h := &AsyncImageHandler{}
	h.loadAPIKey = func(context.Context, int64) (*service.APIKey, error) {
		return &service.APIKey{
			ID:      9,
			UserID:  7,
			Status:  service.StatusAPIKeyActive,
			GroupID: &groupID,
			User: &service.User{
				ID:          7,
				Status:      service.StatusActive,
				Role:        service.RoleUser,
				Concurrency: 2,
			},
			Group: &service.Group{
				ID:                   groupID,
				Status:               service.StatusActive,
				Platform:             service.PlatformOpenAI,
				AllowImageGeneration: true,
			},
		}, nil
	}
	h.execute = func(platform string, c *gin.Context) {
		require.Equal(t, service.PlatformOpenAI, platform)
		apiKey, ok := middleware2.GetAPIKeyFromContext(c)
		require.True(t, ok)
		require.Equal(t, int64(9), apiKey.ID)
		subject, ok := middleware2.GetAuthSubjectFromContext(c)
		require.True(t, ok)
		require.Equal(t, int64(7), subject.UserID)
		body, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"model":"gpt-image-2","prompt":"dog","size":"1024x1024"}`, string(body))
		require.NotContains(t, string(body), "client_request_id")
		c.JSON(http.StatusOK, gin.H{"data": []gin.H{{"url": "https://example.test/dog.png"}}})
	}
	task := &service.ImageTaskRecord{
		ID:          "imgtask_123",
		UserID:      7,
		APIKeyID:    9,
		Platform:    service.PlatformOpenAI,
		ContentType: "application/json",
		RequestBody: json.RawMessage(`{"model":"gpt-image-2","prompt":"dog","size":"1024x1024"}`),
	}

	statusCode, body, err := h.Execute(context.Background(), task)

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, statusCode)
	require.True(t, strings.Contains(string(body), "dog.png"))
}
