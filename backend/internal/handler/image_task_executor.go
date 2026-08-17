package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *AsyncImageHandler) Execute(ctx context.Context, task *service.ImageTaskRecord) (int, json.RawMessage, error) {
	if h == nil || h.execute == nil || task == nil || h.loadAPIKey == nil {
		return http.StatusServiceUnavailable, nil, errors.New("image task executor is unavailable")
	}
	apiKey, err := h.loadAPIKey(ctx, task.APIKeyID)
	if err != nil {
		return http.StatusUnauthorized, nil, err
	}
	if apiKey == nil || apiKey.UserID != task.UserID || apiKey.User == nil || apiKey.Group == nil {
		return http.StatusUnauthorized, nil, errors.New("image task owner is unavailable")
	}
	if !apiKey.IsActive() || !apiKey.User.IsActive() || !apiKey.Group.IsActive() {
		return http.StatusForbidden, nil, errors.New("image task owner is inactive")
	}
	if task.Platform != "" && task.Platform != apiKey.Group.Platform {
		return http.StatusBadRequest, nil, errors.New("image task platform changed")
	}
	if !service.GroupAllowsImageGeneration(apiKey.Group) {
		return http.StatusForbidden, nil, errors.New(service.ImageGenerationPermissionMessage())
	}

	var subscription *service.UserSubscription
	if h.loadSubscription != nil && apiKey.Group.IsSubscriptionType() {
		groupID := apiKey.Group.ID
		subscription, err = h.loadSubscription(ctx, apiKey.UserID, groupID)
		if err != nil {
			return http.StatusForbidden, nil, err
		}
	}

	body := append([]byte(nil), task.RequestBody...)
	request := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	request = request.WithContext(ctx)
	request.Header.Set("Content-Type", task.ContentType)
	request.ContentLength = int64(len(body))
	recorder := httptest.NewRecorder()
	taskContext, _ := gin.CreateTestContext(recorder)
	taskContext.Request = request
	taskContext.Set(string(middleware2.ContextKeyAPIKey), apiKey)
	taskContext.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{
		UserID:      apiKey.User.ID,
		Concurrency: apiKey.User.Concurrency,
	})
	taskContext.Set(string(middleware2.ContextKeyUserRole), apiKey.User.Role)
	if subscription != nil {
		taskContext.Set(string(middleware2.ContextKeySubscription), subscription)
	}
	requestContext := context.WithValue(request.Context(), ctxkey.UserID, apiKey.User.ID)
	requestContext = context.WithValue(requestContext, ctxkey.Group, apiKey.Group)
	requestContext = service.WithAutoGroupFallbackState(requestContext, apiKey)
	taskContext.Request = request.WithContext(requestContext)
	taskContext.Set(securityAuditCompletedContextKey, true)

	h.execute(apiKey.Group.Platform, taskContext)
	result := bytes.TrimSpace(recorder.Body.Bytes())
	if result == nil {
		result = []byte{}
	}
	return recorder.Code, json.RawMessage(result), nil
}

var _ service.ImageTaskExecutor = (*AsyncImageHandler)(nil)
