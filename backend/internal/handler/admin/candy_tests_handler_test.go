package admin

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type candyHandlerRepo struct{ service.CandyTestRepository }

func (candyHandlerRepo) Expire(context.Context, time.Time) error { return nil }
func (candyHandlerRepo) GetSettings(context.Context) (*service.CandyTestSettings, error) {
	return &service.CandyTestSettings{CandyTestSettingsInput: service.DefaultCandyTestSettings()}, nil
}
func (candyHandlerRepo) Claim(context.Context, service.CandyTestStart, string, time.Time, time.Time, bool) (*service.CandyTestBatch, error) {
	return nil, service.ErrCandyTestRunning
}
func (candyHandlerRepo) ListResults(_ context.Context, offset, limit int) ([]*service.CandyTestResult, int64, error) {
	return []*service.CandyTestResult{{ID: 9, Status: "normal", Answer: "21"}}, 31, nil
}
func (candyHandlerRepo) GetResult(context.Context, int64) (*service.CandyTestResult, error) {
	return nil, nil
}

func TestCandyHandlerValidationPaginationAndConflict(t *testing.T) {
	h := NewCandyTestHandler(service.NewCandyTestService(candyHandlerRepo{}, nil, nil))
	w := serveGroupTest(h.GetSettings, http.MethodGet, "/settings", "/settings", "")
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"expected_answer":"21"`)
	require.Contains(t, w.Body.String(), `"schedule_enabled":false`)
	require.Contains(t, w.Body.String(), `"interval_hours":24`)
	for _, interval := range []string{"0", "-1", "169", "1.5"} {
		body := `{"prompt":"题目","expected_answer":"21","reasoning_effort":"high","interval_hours":` + interval + `}`
		require.Equal(t, http.StatusBadRequest, serveGroupTest(h.UpdateSettings, http.MethodPut, "/settings", "/settings", body).Code)
	}
	for _, body := range []string{`{`, `{}`, `{"all":true,"account_ids":[1]}`, `{"account_ids":[0]}`} {
		require.Equal(t, http.StatusBadRequest, serveGroupTest(h.Run, http.MethodPost, "/run", "/run", body).Code)
	}
	w = serveGroupTest(h.Run, http.MethodPost, "/run", "/run", `{"all":true}`)
	require.Equal(t, http.StatusConflict, w.Code)
	require.Contains(t, w.Body.String(), "CANDY_TEST_RUNNING")
	w = serveGroupTest(h.ListResults, http.MethodGet, "/results", "/results?page=2&page_size=10", "")
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"total":31`)
	require.Contains(t, w.Body.String(), `"page":2`)
	require.Equal(t, http.StatusNotFound, serveGroupTest(h.GetResult, http.MethodGet, "/results/:id", "/results/90", "").Code)
	require.Equal(t, http.StatusBadRequest, serveGroupTest(h.GetResult, http.MethodGet, "/results/:id", "/results/abc", "").Code)
	require.Equal(t, http.StatusBadRequest, serveGroupTest(h.Stop, http.MethodPost, "/stop", "/stop", `{"run_id":"bad"}`).Code)
}
