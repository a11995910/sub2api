package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type rotationStatusConfigRepo struct {
	config *service.AccountTimeRotationConfig
	err    error
}

func (r *rotationStatusConfigRepo) Get(context.Context) (*service.AccountTimeRotationConfig, error) {
	return r.config, r.err
}

func (r *rotationStatusConfigRepo) Apply(_ context.Context, config *service.AccountTimeRotationConfig, _ time.Time) (*service.AccountTimeRotationConfig, error) {
	if config != nil {
		r.config = config
	}
	return r.config, r.err
}

type rotationStatusAccountRepo struct {
	service.AccountRepository
	accounts []*service.Account
	ids      []int64
	err      error
}

func (r *rotationStatusAccountRepo) GetByIDs(_ context.Context, ids []int64) ([]*service.Account, error) {
	r.ids = append([]int64(nil), ids...)
	return r.accounts, r.err
}

func newRotationStatusRouter(t *testing.T, repo *rotationStatusConfigRepo, accounts *rotationStatusAccountRepo, applied bool) (*gin.Engine, *service.AccountTimeRotationService) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	svc := service.NewAccountTimeRotationService(repo)
	if applied {
		_, err := svc.Save(context.Background(), repo.config)
		require.NoError(t, err)
	}
	h := NewAccountTimeRotationHandler(svc, accounts)
	router := gin.New()
	router.GET("/status", h.Status)
	return router, svc
}

func smartStatusConfig() *service.AccountTimeRotationConfig {
	config := service.DefaultAccountTimeRotationConfig()
	config.Mode = "smart"
	config.Enabled = true
	config.Revision = 7
	config.Smart.AccountIDs = []int64{1, 2, 3}
	return config
}

func readRotationStatus(t *testing.T, router *gin.Engine, query string) accountTimeRotationStatus {
	t.Helper()
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/status"+query, nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body struct {
		Data accountTimeRotationStatus `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body.Data
}

func TestAccountTimeRotationStatus_DoesNotRefreshUnconfirmedWorker(t *testing.T) {
	repo := &rotationStatusConfigRepo{config: smartStatusConfig()}
	accounts := &rotationStatusAccountRepo{}
	router, svc := newRotationStatusRouter(t, repo, accounts, false)
	status := readRotationStatus(t, router, "")
	require.Equal(t, "smart", status.Mode)
	require.True(t, status.Enabled)
	require.Equal(t, int64(7), status.Revision)
	require.False(t, status.Ready)
	require.Empty(t, status.Accounts)
	require.Nil(t, status.Period)
	require.Nil(t, accounts.ids)
	snapshot, _ := svc.Snapshot(time.Now())
	require.Nil(t, snapshot, "读取状态不能续期执行器健康状态")
}

func TestAccountTimeRotationStatus_AllAndGroupScope(t *testing.T) {
	repo := &rotationStatusConfigRepo{config: smartStatusConfig()}
	accounts := &rotationStatusAccountRepo{accounts: []*service.Account{
		{ID: 1, Name: "主账号", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, GroupIDs: []int64{73}},
		{ID: 2, Name: "停用账号", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: false, AccountGroups: []service.AccountGroup{{GroupID: 74}}},
	}}
	router, _ := newRotationStatusRouter(t, repo, accounts, true)
	status := readRotationStatus(t, router, "")
	require.True(t, status.Ready)
	require.Equal(t, "all", status.Scope)
	require.Equal(t, []int64{1, 2, 3}, accounts.ids)
	require.Nil(t, status.Period)
	require.Nil(t, status.NextRotationAt)
	require.NotNil(t, status.UpdatedAt)
	roles := map[int64]string{}
	for _, account := range status.Accounts {
		roles[account.AccountID] = account.Role
	}
	require.Equal(t, "unavailable", roles[2])
	require.Equal(t, "unavailable", roles[3], "缺失账号仍应可在全局状态中识别")

	status = readRotationStatus(t, router, "?group_id=74")
	require.Equal(t, "group", status.Scope)
	require.Len(t, status.Accounts, 1)
	require.Equal(t, int64(2), status.Accounts[0].AccountID)
	require.Equal(t, []int64{1, 2, 3}, repo.config.Smart.AccountIDs, "分组预览不能更改保存的账号池")
}

func TestAccountTimeRotationStatus_NewerSavedRevisionIsNotReady(t *testing.T) {
	repo := &rotationStatusConfigRepo{config: smartStatusConfig()}
	accounts := &rotationStatusAccountRepo{}
	router, _ := newRotationStatusRouter(t, repo, accounts, true)
	repo.config.Revision++
	status := readRotationStatus(t, router, "")
	require.False(t, status.Ready)
	require.Equal(t, int64(8), status.Revision)
	require.Empty(t, status.Accounts)
	require.Nil(t, accounts.ids)
}

func TestAccountTimeRotationStatus_DisabledAndManualHaveNoPlan(t *testing.T) {
	for _, mode := range []string{"smart", "manual"} {
		t.Run(mode, func(t *testing.T) {
			config := smartStatusConfig()
			config.Mode = mode
			config.Enabled = mode == "manual"
			repo := &rotationStatusConfigRepo{config: config}
			accounts := &rotationStatusAccountRepo{}
			router, _ := newRotationStatusRouter(t, repo, accounts, true)
			status := readRotationStatus(t, router, "")
			require.Empty(t, status.Accounts)
			require.Nil(t, status.Period)
			require.Nil(t, accounts.ids)
		})
	}
}

func TestAccountTimeRotationStatus_InvalidGroupAndRepositoryErrors(t *testing.T) {
	repo := &rotationStatusConfigRepo{config: smartStatusConfig()}
	accounts := &rotationStatusAccountRepo{}
	router, _ := newRotationStatusRouter(t, repo, accounts, true)
	for _, value := range []string{"", "0", "-1", "abc", "9223372036854775808"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/status?group_id="+value, nil))
		require.Equal(t, http.StatusBadRequest, w.Code, value)
	}
	accounts.err = errors.New("账号读取失败")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/status", nil))
	require.Equal(t, http.StatusInternalServerError, w.Code)
	accounts.err = nil
	repo.err = errors.New("配置读取失败")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/status", nil))
	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestAccountProtectionStatusManualAndGroupScope(t *testing.T) {
	config := service.DefaultAccountTimeRotationConfig()
	config.Enabled = true
	config.StrictRest = true
	config.Slots[0].AccountIDs = []int64{1, 2}
	repo := &rotationStatusConfigRepo{config: config}
	accounts := &rotationStatusAccountRepo{accounts: []*service.Account{
		{ID: 1, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, GroupIDs: []int64{73}},
		{ID: 2, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, GroupIDs: []int64{74}},
	}}
	router, _ := newRotationStatusRouter(t, repo, accounts, true)
	result := readRotationStatus(t, router, "")
	require.Len(t, result.Protection, 2)
	require.Empty(t, result.Accounts)
	result = readRotationStatus(t, router, "?group_id=73")
	require.Len(t, result.Protection, 1)
	require.EqualValues(t, 1, result.Protection[0].AccountID)
}

func TestAccountPacedStatusPreservesUnknownCountersAndGroupScope(t *testing.T) {
	cfg := &service.AccountTimeRotationConfig{Enabled: true, Mode: "paced", Revision: 9, Paced: &service.AccountPacedConfig{AccountIDs: []int64{1, 2}, Start: "08:30", End: "18:30", RPM: 40, SustainedRPM: 8, BurstCapacity: 40, MaxConcurrency: 12, MaxWaitSeconds: 15}}
	repo := &rotationStatusConfigRepo{config: cfg}
	accounts := &rotationStatusAccountRepo{accounts: []*service.Account{{ID: 1, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, GroupIDs: []int64{73}}, {ID: 2, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, GroupIDs: []int64{74}}}}
	router, _ := newRotationStatusRouter(t, repo, accounts, true)
	result := readRotationStatus(t, router, "")
	require.True(t, result.Ready)
	require.Equal(t, "paced", result.Mode)
	require.Empty(t, result.Accounts)
	require.Len(t, result.Protection, 2)
	require.Equal(t, "account_protection_unavailable", result.Protection[0].State)
	require.Nil(t, result.Protection[0].Used, "Redis未连接不能把未知用量当零")
	require.Equal(t, 8, result.Protection[0].Paced.SustainedRPM)
	require.Equal(t, 12, result.Protection[0].Paced.MaxConcurrency)
	group := readRotationStatus(t, router, "?group_id=74")
	require.Len(t, group.Protection, 1)
	require.EqualValues(t, 2, group.Protection[0].AccountID)
	require.Empty(t, readRotationStatus(t, router, "?group_id=75").Protection)
	repo.config.Revision++
	require.False(t, readRotationStatus(t, router, "").Ready)
}
