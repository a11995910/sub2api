//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestOpenAITransportErrorCooldownValidation(t *testing.T) {
	for _, value := range []any{nil, 1, int64(30), float64(120), json.Number("600"), json.Number("6e2"), 86400} {
		require.NoError(t, ValidateOpenAITransportErrorCooldownExtra(map[string]any{
			OpenAITransportErrorCooldownSecondsExtraKey: value,
		}))
	}
	require.NoError(t, ValidateOpenAITransportErrorCooldownExtra(nil))
	for _, value := range []any{0, -1, 86401, 1.5, "30", true, []int{30}, math.NaN(), math.Inf(1), math.Inf(-1), int64(math.MaxInt64), json.Number("1e999"), json.Number("1.5")} {
		extra := map[string]any{OpenAITransportErrorCooldownSecondsExtraKey: value}
		require.Equal(t, http.StatusBadRequest, infraerrors.Code(ValidateOpenAITransportErrorCooldownExtra(extra)))
		// 历史异常数据不能导致零冷却或溢出。
		require.Equal(t, 10*time.Minute, (&Account{Platform: PlatformOpenAI, Extra: extra}).OpenAITransportErrorCooldown())
	}
	var account *Account
	require.Equal(t, 10*time.Minute, account.OpenAITransportErrorCooldown())
	require.Equal(t, 10*time.Minute, (&Account{Platform: PlatformAnthropic, Extra: map[string]any{
		OpenAITransportErrorCooldownSecondsExtraKey: 30,
	}}).OpenAITransportErrorCooldown())
}

func TestHandleOpenAIUpstreamTransportError_PerAccountCooldown(t *testing.T) {
	repo := &openaiTransportAccountRepoStub{}
	svc := &OpenAIGatewayService{accountRepo: repo}
	cases := []struct {
		extra map[string]any
		want  time.Duration
	}{
		{extra: nil, want: 600 * time.Second},
		{extra: map[string]any{OpenAITransportErrorCooldownSecondsExtraKey: nil}, want: 600 * time.Second},
		{extra: map[string]any{OpenAITransportErrorCooldownSecondsExtraKey: float64(30)}, want: 30 * time.Second},
		{extra: map[string]any{OpenAITransportErrorCooldownSecondsExtraKey: 120}, want: 120 * time.Second},
		{extra: map[string]any{OpenAITransportErrorCooldownSecondsExtraKey: 86400}, want: 24 * time.Hour},
	}
	for i, tc := range cases {
		account := &Account{ID: int64(i + 1), Platform: PlatformOpenAI, Extra: tc.extra}
		c, _ := newOpenAITransportErrTestContext()
		before := time.Now()
		err := svc.handleOpenAIUpstreamTransportError(context.Background(), c, account, errors.New("connection refused"), false)
		after := time.Now()
		var failover *UpstreamFailoverError
		require.ErrorAs(t, err, &failover)
		require.Len(t, repo.tempUnschedCalls, i+1)
		call := repo.tempUnschedCalls[i]
		require.Equal(t, account.ID, call.accountID)
		require.False(t, call.until.Before(before.Add(tc.want)))
		require.False(t, call.until.After(after.Add(tc.want)))
		memoryUntil, ok := svc.openaiAccountRuntimeBlockUntil.Load(account.ID)
		require.True(t, ok)
		require.Equal(t, call.until, memoryUntil, "内存和数据库必须使用同一到期时间")
	}
}

func TestAdminServiceTransportCooldownLifecycle(t *testing.T) {
	ctx := context.Background()
	repo := &longContextBillingRepoStub{}
	svc := &adminServiceImpl{accountRepo: repo}
	account, err := svc.CreateAccount(ctx, &CreateAccountInput{
		Name: "单账号冷却测试", Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "test"}, SkipDefaultGroupBind: true,
		Extra: map[string]any{OpenAITransportErrorCooldownSecondsExtraKey: 30},
	})
	require.NoError(t, err)
	require.Equal(t, 30*time.Second, account.OpenAITransportErrorCooldown())
	until := time.Now().Add(30 * time.Second)
	account.TempUnschedulableUntil = &until
	account, err = svc.UpdateAccount(ctx, account.ID, &UpdateAccountInput{Extra: map[string]any{"unrelated": true}})
	require.NoError(t, err)
	require.Equal(t, 30*time.Second, account.OpenAITransportErrorCooldown(), "更新其他配置不能重置冷却")
	account, err = svc.UpdateAccount(ctx, account.ID, &UpdateAccountInput{Extra: map[string]any{
		OpenAITransportErrorCooldownSecondsExtraKey: 120,
	}})
	require.NoError(t, err)
	require.Equal(t, 120*time.Second, account.OpenAITransportErrorCooldown())
	require.Equal(t, until, *account.TempUnschedulableUntil, "修改配置不能改变现有冷却")
	account, err = svc.UpdateAccount(ctx, account.ID, &UpdateAccountInput{Extra: map[string]any{
		OpenAITransportErrorCooldownSecondsExtraKey: nil,
	}})
	require.NoError(t, err)
	require.Equal(t, 600*time.Second, account.OpenAITransportErrorCooldown())
	require.Equal(t, until, *account.TempUnschedulableUntil)
}

func TestAdminServiceTransportCooldownRejectsInvalidWrites(t *testing.T) {
	ctx := context.Background()
	invalid := map[string]any{OpenAITransportErrorCooldownSecondsExtraKey: 0}
	repo := &longContextBillingRepoStub{account: &Account{ID: 1, Platform: PlatformOpenAI, Name: "原名称"}}
	svc := &adminServiceImpl{accountRepo: repo}
	_, err := svc.CreateAccount(ctx, &CreateAccountInput{Platform: PlatformOpenAI, Extra: invalid})
	require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
	require.Nil(t, repo.createdAccount)
	_, err = svc.UpdateAccount(ctx, 1, &UpdateAccountInput{Name: "不应保存", Extra: invalid})
	require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
	require.Equal(t, "原名称", repo.account.Name)
	err = svc.UpdateAccountExtra(ctx, 1, invalid)
	require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
	require.Zero(t, repo.updateExtraCalls)
	_, err = svc.BulkUpdateAccounts(ctx, &BulkUpdateAccountsInput{AccountIDs: []int64{1}, Extra: invalid})
	require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
	require.Zero(t, repo.bulkUpdateCalls)
}
