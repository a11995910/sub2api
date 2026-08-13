package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type videoTaskReviewServiceStub struct {
	listFilter service.VideoTaskReviewFilter
	actionID   int64
	reason     string
	err        error
}

func (s *videoTaskReviewServiceStub) List(_ context.Context, filter service.VideoTaskReviewFilter) ([]service.VideoTaskReviewItem, int64, error) {
	s.listFilter = filter
	return []service.VideoTaskReviewItem{{Task: &service.VideoTaskBilling{ID: 9}, UserEmail: "user@example.com"}}, 1, s.err
}

func (s *videoTaskReviewServiceStub) ConfirmFailed(_ context.Context, id int64, reason string) error {
	s.actionID, s.reason = id, reason
	return s.err
}

func (s *videoTaskReviewServiceStub) ConfirmSucceeded(_ context.Context, id int64, reason string) error {
	s.actionID, s.reason = id, reason
	return s.err
}

func (s *videoTaskReviewServiceStub) Recheck(_ context.Context, id int64) error {
	s.actionID = id
	return s.err
}

func setupVideoTaskReviewRouter(svc videoTaskReviewHandlerService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := &VideoTaskReviewHandler{reviewService: svc}
	router.GET("/reviews", h.List)
	router.POST("/reviews/:id/recheck", h.Recheck)
	router.POST("/reviews/:id/confirm-failed", h.ConfirmFailed)
	router.POST("/reviews/:id/confirm-succeeded", h.ConfirmSucceeded)
	return router
}

func TestVideoTaskReviewHandlerListParsesUserAndSearchFilters(t *testing.T) {
	svc := &videoTaskReviewServiceStub{}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/reviews?user_id=7&search=user%40example.com&page=2&page_size=10", nil)

	setupVideoTaskReviewRouter(svc).ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, int64(7), svc.listFilter.UserID)
	require.Equal(t, "user@example.com", svc.listFilter.Search)
	require.Equal(t, 2, svc.listFilter.Page)
	require.Equal(t, 10, svc.listFilter.PageSize)
	require.Contains(t, recorder.Body.String(), `"user_email":"user@example.com"`)
	require.Contains(t, recorder.Body.String(), `"id":9`)
	require.NotContains(t, recorder.Body.String(), "UsageContextJSON")
	require.NotContains(t, recorder.Body.String(), "ResponseJSON")
}

func TestVideoTaskReviewHandlerConfirmFailedRequiresReason(t *testing.T) {
	svc := &videoTaskReviewServiceStub{}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/reviews/9/confirm-failed", strings.NewReader(`{"reason":""}`))
	request.Header.Set("Content-Type", "application/json")

	setupVideoTaskReviewRouter(svc).ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Zero(t, svc.actionID)
}

func TestVideoTaskReviewHandlerRecheckWithoutTaskIDIsBadRequest(t *testing.T) {
	svc := &videoTaskReviewServiceStub{err: service.ErrVideoTaskReviewCannotRecheck}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/reviews/9/recheck", nil)

	setupVideoTaskReviewRouter(svc).ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestVideoTaskReviewHandlerStateConflictIsConflict(t *testing.T) {
	svc := &videoTaskReviewServiceStub{err: service.ErrVideoTaskBillingInvalidState}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/reviews/9/confirm-succeeded", strings.NewReader(`{"reason":"上游账单已核对"}`))
	request.Header.Set("Content-Type", "application/json")

	setupVideoTaskReviewRouter(svc).ServeHTTP(recorder, request)

	require.Equal(t, http.StatusConflict, recorder.Code)
	require.True(t, errors.Is(svc.err, service.ErrVideoTaskBillingInvalidState))
}
