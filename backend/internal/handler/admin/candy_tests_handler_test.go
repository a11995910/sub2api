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
func (candyHandlerRepo) ListAccounts(context.Context) ([]service.CandyTestAccount, error) {
	return []service.CandyTestAccount{{ID: 18, Name: "保留正价分组的账号"}}, nil
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
	require.Contains(t, w.Body.String(), `"auto_group_excluded_account_ids":[]`)
	w = serveGroupTest(h.ListAccounts, http.MethodGet, "/accounts", "/accounts", "")
	require.Equal(t, http.StatusOK, w.Code)
	require.JSONEq(t, `{"code":0,"message":"success","data":[{"id":18,"name":"保留正价分组的账号"}]}`, w.Body.String())
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

type candyHandlerSettingsRepo struct {
	candyHandlerRepo
	input service.CandyTestSettingsInput
}

func (r *candyHandlerSettingsRepo) UpdateSettings(_ context.Context, input *service.CandyTestSettingsInput, _ time.Time) (*service.CandyTestSettings, error) {
	r.input = *input
	return &service.CandyTestSettings{CandyTestSettingsInput: *input}, nil
}

func TestCandyHandlerExcludedAccountsCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name  string
		field string
		want  []int64
	}{
		{name: "旧客户端省略字段", want: nil},
		{name: "空值保留名单", field: `,"auto_group_excluded_account_ids":null`, want: nil},
		{name: "空数组清空名单", field: `,"auto_group_excluded_account_ids":[]`, want: []int64{}},
		{name: "去重排序", field: `,"auto_group_excluded_account_ids":[9,2,9]`, want: []int64{2, 9}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &candyHandlerSettingsRepo{}
			h := NewCandyTestHandler(service.NewCandyTestService(repo, nil, nil))
			body := `{"prompt":"题目","expected_answer":"21","reasoning_effort":"high","interval_hours":24` + tc.field + `}`
			w := serveGroupTest(h.UpdateSettings, http.MethodPut, "/settings", "/settings", body)
			require.Equal(t, http.StatusOK, w.Code)
			require.Equal(t, tc.want, repo.input.AutoGroupExcludedAccountIDs)
		})
	}
	h := NewCandyTestHandler(service.NewCandyTestService(&candyHandlerSettingsRepo{}, nil, nil))
	for _, value := range []string{`[0]`, `[-1]`, `[1.5]`, `["1"]`} {
		body := `{"prompt":"题目","expected_answer":"21","reasoning_effort":"high","interval_hours":24,"auto_group_excluded_account_ids":` + value + `}`
		require.Equal(t, http.StatusBadRequest, serveGroupTest(h.UpdateSettings, http.MethodPut, "/settings", "/settings", body).Code)
	}
}
