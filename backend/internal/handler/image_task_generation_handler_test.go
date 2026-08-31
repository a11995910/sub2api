package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type asyncGenerationMemoryStore struct {
	mu    sync.Mutex
	tasks map[string]*service.ImageTaskRecord
	ready []string
}

func (s *asyncGenerationMemoryStore) Save(_ context.Context, task *service.ImageTaskRecord, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := *task
	s.tasks[task.ID] = &copy
	return nil
}

func (s *asyncGenerationMemoryStore) Get(_ context.Context, id string) (*service.ImageTaskRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task := s.tasks[id]
	if task == nil {
		return nil, service.ErrImageTaskNotFound
	}
	copy := *task
	return &copy, nil
}

func (s *asyncGenerationMemoryStore) CreateOrGet(_ context.Context, task *service.ImageTaskRecord, _ time.Duration) (*service.ImageTaskRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.tasks {
		if existing.UserID == task.UserID && existing.APIKeyID == task.APIKeyID && existing.ClientRequestID == task.ClientRequestID {
			if existing.RequestFingerprint != task.RequestFingerprint {
				return nil, false, service.ErrImageTaskIdempotencyConflict
			}
			copy := *existing
			return &copy, false, nil
		}
	}
	copy := *task
	s.tasks[task.ID] = &copy
	s.ready = append(s.ready, task.ID)
	return &copy, true, nil
}

func (s *asyncGenerationMemoryStore) Reserve(_ context.Context, token string, now time.Time) (*service.ImageTaskRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.ready) == 0 {
		return nil, service.ErrImageTaskQueueEmpty
	}
	id := s.ready[0]
	s.ready = s.ready[1:]
	task := s.tasks[id]
	task.Status = service.ImageTaskStatusRunning
	task.ExecutionToken = token
	task.UpdatedAt = now.Unix()
	copy := *task
	return &copy, nil
}

func (s *asyncGenerationMemoryStore) Heartbeat(context.Context, string, string, time.Time) error {
	return nil
}

func (s *asyncGenerationMemoryStore) SaveClaim(_ context.Context, task *service.ImageTaskRecord, token string, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.tasks[task.ID]
	if current == nil || current.ExecutionToken != token {
		return service.ErrImageTaskClaimLost
	}
	copy := *task
	copy.ExecutionToken = ""
	s.tasks[task.ID] = &copy
	return nil
}

func (s *asyncGenerationMemoryStore) RecoverStale(context.Context, time.Time, time.Time, time.Duration) (int, error) {
	return 0, nil
}

func newAsyncGenerationTestHandler() (*AsyncImageHandler, *asyncGenerationMemoryStore, *gin.Engine) {
	store := &asyncGenerationMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}
	tasks := service.NewImageTaskServiceWithUploader(store, nil, time.Hour, time.Minute)
	h := NewAsyncImageHandler(tasks, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		groupID := int64(3)
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
			ID:      9,
			UserID:  7,
			GroupID: &groupID,
			Group: &service.Group{
				ID:                   groupID,
				Platform:             service.PlatformOpenAI,
				AllowImageGeneration: true,
			},
		})
		c.Next()
	})
	return h, store, router
}

func TestAsyncImageHandlerDispatchGenerationsKeepsSyncMode(t *testing.T) {
	h, _, router := newAsyncGenerationTestHandler()
	var syncCalls int
	syncHandler := func(c *gin.Context) {
		syncCalls++
		c.JSON(http.StatusOK, gin.H{"legacy": true})
	}
	router.POST("/v1/images/generations", h.DispatchGenerations(syncHandler))

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{"model":"gpt-image-2","prompt":"dog"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, 1, syncCalls)
	require.JSONEq(t, `{"legacy":true}`, w.Body.String())
}

func TestAsyncImageHandlerDispatchGenerationsReturns413ForActualOversize(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, path := range []string{"/v1/images/generations", "/images/generations"} {
		t.Run(path, func(t *testing.T) {
			h := &AsyncImageHandler{}
			router := gin.New()
			router.POST(path, middleware2.RequestBodyLimit(4), h.DispatchGenerations(func(c *gin.Context) {
				c.Status(http.StatusNoContent)
			}))

			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"gpt-image-2"}`))
			req.ContentLength = -1
			req.Header.Del("Content-Length")
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)

			require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
			require.Contains(t, recorder.Body.String(), "Request body too large")
		})
	}
}

func TestAsyncImageHandlerDispatchGenerationsIsIdempotent(t *testing.T) {
	h, store, router := newAsyncGenerationTestHandler()
	router.POST("/v1/images/generations", h.DispatchGenerations(func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"legacy": true})
	}))

	body := `{"async":true,"client_request_id":"request_123","model":"gpt-image-2","prompt":"dog","size":"1:1"}`
	firstReq := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(body))
	firstReq.Header.Set("Content-Type", "application/json")
	firstWriter := httptest.NewRecorder()
	router.ServeHTTP(firstWriter, firstReq)
	require.Equal(t, http.StatusAccepted, firstWriter.Code)
	var first map[string]any
	require.NoError(t, json.Unmarshal(firstWriter.Body.Bytes(), &first))
	require.Equal(t, service.ImageTaskStatusQueued, first["status"])

	replayReq := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(body))
	replayReq.Header.Set("Content-Type", "application/json")
	replayWriter := httptest.NewRecorder()
	router.ServeHTTP(replayWriter, replayReq)
	require.Equal(t, http.StatusAccepted, replayWriter.Code)
	var replay map[string]any
	require.NoError(t, json.Unmarshal(replayWriter.Body.Bytes(), &replay))
	require.Equal(t, first["id"], replay["id"])
	require.Len(t, store.ready, 1)

	conflictReq := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(strings.Replace(body, "dog", "cat", 1)))
	conflictReq.Header.Set("Content-Type", "application/json")
	conflictWriter := httptest.NewRecorder()
	router.ServeHTTP(conflictWriter, conflictReq)
	require.Equal(t, http.StatusConflict, conflictWriter.Code)
	require.Contains(t, conflictWriter.Body.String(), "IDEMPOTENCY_CONFLICT")
}

func TestAsyncImageHandlerDispatchGenerationsAcceptsWithoutObjectStorage(t *testing.T) {
	h, store, router := newAsyncGenerationTestHandler()
	h.tasks = service.NewImageTaskServiceWithResolver(store, func() (*service.ImageResultUploader, bool) {
		return nil, false
	}, time.Hour, time.Minute)
	router.POST("/v1/images/generations", h.DispatchGenerations(func(c *gin.Context) {
		c.Status(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{
		"async":true,
		"client_request_id":"request_local",
		"prompt":"dog",
		"response_format":"url"
	}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusAccepted, w.Code)
	require.Contains(t, w.Body.String(), `"status":"queued"`)
}

func TestAsyncImageHandlerDispatchGenerationsRejectsInvalidClientID(t *testing.T) {
	h, _, router := newAsyncGenerationTestHandler()
	router.POST("/v1/images/generations", h.DispatchGenerations(func(c *gin.Context) {
		c.Status(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{"async":true,"client_request_id":"bad.id","prompt":"dog"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.NotContains(t, w.Body.String(), "INTERNAL_ERROR")
}

func TestAsyncImageHandlerDispatchGenerationsRejectsBase64ResponseFormat(t *testing.T) {
	h, _, router := newAsyncGenerationTestHandler()
	router.POST("/v1/images/generations", h.DispatchGenerations(func(c *gin.Context) {
		c.Status(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{
		"async":true,
		"client_request_id":"request_base64",
		"prompt":"dog",
		"response_format":"b64_json"
	}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "ASYNC_RESPONSE_FORMAT_UNSUPPORTED")
}

func TestAsyncImageHandlerGetGenerationReturnsNewView(t *testing.T) {
	h, store, router := newAsyncGenerationTestHandler()
	store.tasks["imgtask_done"] = &service.ImageTaskRecord{
		ID:              "imgtask_done",
		UserID:          7,
		APIKeyID:        9,
		ClientRequestID: "request_done",
		Status:          service.ImageTaskStatusSucceeded,
		CreatedAt:       100,
		UpdatedAt:       110,
		Result:          json.RawMessage(`{"data":[{"url":"https://example.test/dog.png"}],"usage":{"total_tokens":3}}`),
	}
	router.GET("/v1/images/generations/:task_id", h.GetGeneration)

	req := httptest.NewRequest(http.MethodGet, "/v1/images/generations/imgtask_done", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"status":"succeeded"`)
	require.Contains(t, w.Body.String(), `"client_request_id":"request_done"`)
	require.Contains(t, w.Body.String(), "https://example.test/dog.png")
	require.Contains(t, w.Body.String(), `"total_tokens":3`)
}

var _ service.ImageTaskRepository = (*asyncGenerationMemoryStore)(nil)
