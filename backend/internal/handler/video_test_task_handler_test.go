//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestVideoTestTaskHandlerRefreshUpdatesSuccessfulStatus(t *testing.T) {
	store := newHandlerVideoTaskStore(service.VideoTestTask{
		ID: "local-1", UserID: 7, APIKeyID: 11, AccountID: 17, Platform: service.PlatformOpenAI,
		UpstreamTaskID: "upstream-1", Status: service.VideoTestTaskStatusQueued,
	})
	progress := 35.0
	gateway := &videoTestTaskGatewayStub{statusResult: &service.OpenAIForwardResult{
		VideoStatus: "processing", VideoProgress: &progress, VideoResponseJSON: json.RawMessage(`{"status":"processing"}`),
	}}
	h := NewVideoTestTaskHandler(service.NewVideoTestTaskService(store), gateway, nil)
	c, recorder := newVideoTaskHandlerContext(http.MethodPost, "/api/v1/model-test/video-tasks/local-1/refresh", 7)
	c.Params = gin.Params{{Key: "id", Value: "local-1"}}

	h.Refresh(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, service.VideoTestTaskStatusInProgress, videoTaskJSONPathString(t, recorder.Body.Bytes(), "status"))
	require.Equal(t, 1, gateway.statusCalls)
}

func TestVideoTestTaskHandlerListReturnsOnlyCurrentUserTasks(t *testing.T) {
	store := newHandlerVideoTaskStore(
		service.VideoTestTask{ID: "mine", UserID: 7, Status: service.VideoTestTaskStatusQueued},
		service.VideoTestTask{ID: "other", UserID: 8, Status: service.VideoTestTaskStatusQueued},
	)
	h := NewVideoTestTaskHandler(service.NewVideoTestTaskService(store), &videoTestTaskGatewayStub{}, nil)
	c, recorder := newVideoTaskHandlerContext(http.MethodGet, "/api/v1/model-test/video-tasks?page=1&page_size=20", 7)

	h.List(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var page service.VideoTestTaskPage
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &page))
	require.Equal(t, int64(1), page.Total)
	require.Len(t, page.Items, 1)
	require.Equal(t, "mine", page.Items[0].ID)
}

func TestVideoTestTaskHandlerRefreshErrorKeepsTaskWaiting(t *testing.T) {
	store := newHandlerVideoTaskStore(service.VideoTestTask{
		ID: "local-1", UserID: 7, APIKeyID: 11, AccountID: 17, Platform: service.PlatformOpenAI,
		UpstreamTaskID: "upstream-1", Status: service.VideoTestTaskStatusInProgress,
	})
	gateway := &videoTestTaskGatewayStub{statusErr: errors.New("upstream timeout")}
	h := NewVideoTestTaskHandler(service.NewVideoTestTaskService(store), gateway, nil)
	c, recorder := newVideoTaskHandlerContext(http.MethodPost, "/api/v1/model-test/video-tasks/local-1/refresh", 7)
	c.Params = gin.Params{{Key: "id", Value: "local-1"}}

	h.Refresh(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, service.VideoTestTaskStatusInProgress, videoTaskJSONPathString(t, recorder.Body.Bytes(), "status"))
	require.Equal(t, "upstream timeout", videoTaskJSONPathString(t, recorder.Body.Bytes(), "last_poll_error"))
}

func TestVideoTestTaskHandlerRejectsOtherUsersTask(t *testing.T) {
	store := newHandlerVideoTaskStore(service.VideoTestTask{ID: "local-1", UserID: 8, Status: service.VideoTestTaskStatusQueued})
	h := NewVideoTestTaskHandler(service.NewVideoTestTaskService(store), &videoTestTaskGatewayStub{}, nil)
	c, recorder := newVideoTaskHandlerContext(http.MethodPost, "/api/v1/model-test/video-tasks/local-1/refresh", 7)
	c.Params = gin.Params{{Key: "id", Value: "local-1"}}

	h.Refresh(c)

	require.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestVideoTestTaskHandlerContentProxiesRangeOnlyAfterCompletion(t *testing.T) {
	store := newHandlerVideoTaskStore(service.VideoTestTask{
		ID: "local-1", UserID: 7, AccountID: 17, Platform: service.PlatformOpenAI,
		UpstreamTaskID: "upstream-1", Status: service.VideoTestTaskStatusCompleted,
	})
	gateway := &videoTestTaskGatewayStub{}
	h := NewVideoTestTaskHandler(service.NewVideoTestTaskService(store), gateway, nil)
	c, recorder := newVideoTaskHandlerContext(http.MethodGet, "/api/v1/model-test/video-tasks/local-1/content", 7)
	c.Params = gin.Params{{Key: "id", Value: "local-1"}}
	c.Request.Header.Set("Range", "bytes=0-3")

	h.Content(c)

	require.Equal(t, http.StatusPartialContent, recorder.Code)
	require.Equal(t, "test", recorder.Body.String())
	require.Equal(t, "bytes=0-3", gateway.contentRange)
}

func TestVideoTestTaskHandlerCachesCompletedVideoAndServesRangeLocally(t *testing.T) {
	store := newHandlerVideoTaskStore(service.VideoTestTask{
		ID: "local-1", UserID: 7, AccountID: 17, Platform: service.PlatformOpenAI,
		UpstreamTaskID: "upstream-1", Status: service.VideoTestTaskStatusCompleted,
	})
	contentStore := service.NewVideoTestTaskContentStore(service.VideoTestTaskContentStoreConfig{
		Directory: t.TempDir(), MaxBytes: 1 << 20,
	})
	mp4 := []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'm', 'p', '4', '2', 0, 0, 2, 0, 'm', 'p', '4', '2', 'i', 's', 'o', 'm'}
	gateway := &videoTestTaskGatewayStub{
		contentStatus: http.StatusOK,
		contentType:   "application/octet-stream",
		contentBody:   mp4,
	}
	h := NewVideoTestTaskHandler(service.NewVideoTestTaskService(store), gateway, contentStore)
	c, recorder := newVideoTaskHandlerContext(http.MethodGet, "/api/v1/model-test/video-tasks/local-1/content", 7)
	c.Params = gin.Params{{Key: "id", Value: "local-1"}}

	h.Content(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, mp4, recorder.Body.Bytes())
	require.Equal(t, 1, gateway.contentCalls)
	_, err := contentStore.Resolve("local-1", time.Now().UTC())
	require.NoError(t, err)

	gateway.resolveErr = errors.New("upstream account disabled")
	c, recorder = newVideoTaskHandlerContext(http.MethodGet, "/api/v1/model-test/video-tasks/local-1/content", 7)
	c.Params = gin.Params{{Key: "id", Value: "local-1"}}
	c.Request.Header.Set("Range", "bytes=4-7")

	h.Content(c)

	require.Equal(t, http.StatusPartialContent, recorder.Code)
	require.Equal(t, []byte("ftyp"), recorder.Body.Bytes())
	require.Equal(t, 1, gateway.contentCalls)
}

func TestVideoTestTaskHandlerDeleteIsOwnerScoped(t *testing.T) {
	store := newHandlerVideoTaskStore(service.VideoTestTask{ID: "local-1", UserID: 7, Status: service.VideoTestTaskStatusQueued})
	h := NewVideoTestTaskHandler(service.NewVideoTestTaskService(store), &videoTestTaskGatewayStub{}, nil)
	c, recorder := newVideoTaskHandlerContext(http.MethodDelete, "/api/v1/model-test/video-tasks/local-1", 7)
	c.Params = gin.Params{{Key: "id", Value: "local-1"}}

	h.Delete(c)

	require.Equal(t, http.StatusNoContent, recorder.Code)
	_, ok := store.tasks["local-1"]
	require.False(t, ok)
}

type videoTestTaskGatewayStub struct {
	statusResult  *service.OpenAIForwardResult
	statusErr     error
	statusCalls   int
	contentRange  string
	contentStatus int
	contentType   string
	contentBody   []byte
	contentCalls  int
	resolveErr    error
}

func (s *videoTestTaskGatewayStub) ResolveVideoTestTaskStoredAccount(context.Context, int64, string) (*service.Account, error) {
	if s.resolveErr != nil {
		return nil, s.resolveErr
	}
	return &service.Account{ID: 17, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey}, nil
}

func (s *videoTestTaskGatewayStub) ForwardOpenAIVideoStatus(_ context.Context, _ *gin.Context, _ *service.Account, _ string) (*service.OpenAIForwardResult, error) {
	s.statusCalls++
	return s.statusResult, s.statusErr
}

func (s *videoTestTaskGatewayStub) ForwardOpenAIVideoContent(_ context.Context, c *gin.Context, _ *service.Account, _ string) (*service.OpenAIForwardResult, error) {
	s.contentCalls++
	s.contentRange = c.GetHeader("Range")
	status := s.contentStatus
	if status == 0 {
		status = http.StatusPartialContent
	}
	body := s.contentBody
	if len(body) == 0 {
		body = []byte("test")
	}
	contentType := s.contentType
	if contentType == "" {
		contentType = "video/mp4"
	}
	c.Data(status, contentType, body)
	return &service.OpenAIForwardResult{}, nil
}

func (s *videoTestTaskGatewayStub) ForwardGrokMedia(context.Context, *gin.Context, *service.Account, service.GrokMediaEndpoint, string, []byte, string) (*service.OpenAIForwardResult, error) {
	return s.statusResult, s.statusErr
}

func newVideoTaskHandlerContext(method, path string, userID int64) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, path, nil)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: userID})
	return c, recorder
}

func videoTaskJSONPathString(t *testing.T, body []byte, key string) string {
	t.Helper()
	var payload map[string]any
	require.NoError(t, json.Unmarshal(body, &payload))
	value, _ := payload[key].(string)
	return value
}

type handlerVideoTaskStore struct {
	tasks map[string]service.VideoTestTask
}

func newHandlerVideoTaskStore(tasks ...service.VideoTestTask) *handlerVideoTaskStore {
	store := &handlerVideoTaskStore{tasks: make(map[string]service.VideoTestTask)}
	for _, task := range tasks {
		store.tasks[task.ID] = task
	}
	return store
}

func (s *handlerVideoTaskStore) Create(_ context.Context, task *service.VideoTestTask) error {
	s.tasks[task.ID] = *task
	return nil
}

func (s *handlerVideoTaskStore) GetByOwner(_ context.Context, userID int64, id string) (*service.VideoTestTask, error) {
	task, ok := s.tasks[id]
	if !ok || task.UserID != userID {
		return nil, service.ErrVideoTestTaskNotFound
	}
	return &task, nil
}

func (s *handlerVideoTaskStore) GetByUpstreamOwner(_ context.Context, userID, apiKeyID int64, upstreamTaskID string) (*service.VideoTestTask, error) {
	for _, task := range s.tasks {
		if task.UserID == userID && task.APIKeyID == apiKeyID && task.UpstreamTaskID == upstreamTaskID {
			copy := task
			return &copy, nil
		}
	}
	return nil, service.ErrVideoTestTaskNotFound
}

func (s *handlerVideoTaskStore) ListByUser(_ context.Context, userID int64, _, _ int) ([]service.VideoTestTask, int64, error) {
	items := make([]service.VideoTestTask, 0)
	for _, task := range s.tasks {
		if task.UserID == userID {
			items = append(items, task)
		}
	}
	return items, int64(len(items)), nil
}

func (s *handlerVideoTaskStore) UpdatePollResult(_ context.Context, task *service.VideoTestTask) error {
	s.tasks[task.ID] = *task
	return nil
}

func (s *handlerVideoTaskStore) DeleteByOwner(_ context.Context, userID int64, id string) error {
	task, ok := s.tasks[id]
	if !ok || task.UserID != userID {
		return service.ErrVideoTestTaskNotFound
	}
	delete(s.tasks, id)
	return nil
}

func (s *handlerVideoTaskStore) DeleteExpiredTerminal(context.Context, time.Time) (int64, error) {
	return 0, nil
}
