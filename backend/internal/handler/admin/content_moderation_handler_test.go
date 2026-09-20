package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type contentModerationDetailRepository struct {
	service.ContentModerationRepository
	log      *service.ContentModerationLog
	err      error
	calledID int64
	calls    int
}

func (r *contentModerationDetailRepository) GetLog(_ context.Context, id int64) (*service.ContentModerationLog, error) {
	r.calledID = id
	r.calls++
	return r.log, r.err
}

func newContentModerationDetailRouter(repo *contentModerationDetailRepository) *gin.Engine {
	gin.SetMode(gin.TestMode)
	svc := service.NewContentModerationService(nil, repo, nil, nil, nil, nil, nil, nil)
	h := NewContentModerationHandler(svc)
	router := gin.New()
	router.GET("/logs/:id", h.GetLog)
	return router
}

func TestContentModerationGetLogReturnsStoredInputBeyondExcerpt(t *testing.T) {
	text := strings.Repeat("原始输入内容。", 80) + "最后一句：请解释上下文。"
	repo := &contentModerationDetailRepository{log: &service.ContentModerationLog{
		ID:           42,
		InputExcerpt: "原始输入内容。",
		InputContent: &service.ContentModerationLogInput{
			Text:          text,
			TextTruncated: false,
			TextRunes:     utf8.RuneCountInString(text),
			ImageCount:    2,
		},
	}}
	recorder := httptest.NewRecorder()
	newContentModerationDetailRouter(repo).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/logs/42", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, int64(42), repo.calledID)
	require.Equal(t, 1, repo.calls)
	var envelope struct {
		Code int                          `json:"code"`
		Data service.ContentModerationLog `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Zero(t, envelope.Code)
	require.Equal(t, int64(42), envelope.Data.ID)
	require.NotNil(t, envelope.Data.InputContent)
	require.Equal(t, text, envelope.Data.InputContent.Text, "详情必须返回已保存的原始输入，不能继续使用列表摘要")
	require.Equal(t, utf8.RuneCountInString(text), envelope.Data.InputContent.TextRunes)
	require.False(t, envelope.Data.InputContent.TextTruncated)
	require.Equal(t, 2, envelope.Data.InputContent.ImageCount)
}

func TestContentModerationGetLogRetainsTruncationMetadata(t *testing.T) {
	repo := &contentModerationDetailRepository{log: &service.ContentModerationLog{
		ID: 43,
		InputContent: &service.ContentModerationLogInput{
			Text:          "已保存的部分",
			TextTruncated: true,
			TextRunes:     200000,
			ImageCount:    0,
		},
	}}
	recorder := httptest.NewRecorder()
	newContentModerationDetailRouter(repo).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/logs/43", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	var envelope struct {
		Data struct {
			InputContent service.ContentModerationLogInput `json:"input_content"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.True(t, envelope.Data.InputContent.TextTruncated, "界面需要截断标志，避免把部分内容标为完整输入")
	require.Equal(t, 200000, envelope.Data.InputContent.TextRunes)
}

func TestContentModerationGetLogLegacyRecordDoesNotInventInput(t *testing.T) {
	repo := &contentModerationDetailRepository{log: &service.ContentModerationLog{
		ID:           44,
		InputExcerpt: "历史摘要",
	}}
	recorder := httptest.NewRecorder()
	newContentModerationDetailRouter(repo).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/logs/44", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	var envelope struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.NotContains(t, envelope.Data, "input_content", "未保存原始输入的历史记录不能伪装成完整详情")
	require.JSONEq(t, `"历史摘要"`, string(envelope.Data["input_excerpt"]))
}

func TestContentModerationGetLogRejectsInvalidIDWithoutQuerying(t *testing.T) {
	for _, id := range []string{"0", "-1", "abc", "1.5", "9223372036854775808"} {
		t.Run(id, func(t *testing.T) {
			repo := &contentModerationDetailRepository{}
			recorder := httptest.NewRecorder()
			newContentModerationDetailRouter(repo).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/logs/"+id, nil))

			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.Zero(t, repo.calls)
		})
	}
}

func TestContentModerationGetLogMapsRepositoryErrors(t *testing.T) {
	for _, tc := range []struct {
		name       string
		err        error
		wantStatus int
	}{
		{name: "记录不存在", err: service.ErrContentModerationLogNotFound, wantStatus: http.StatusNotFound},
		{name: "查询失败", err: errors.New("内部数据库查询失败"), wantStatus: http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &contentModerationDetailRepository{err: tc.err}
			recorder := httptest.NewRecorder()
			newContentModerationDetailRouter(repo).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/logs/42", nil))

			require.Equal(t, tc.wantStatus, recorder.Code)
			require.Equal(t, int64(42), repo.calledID)
			require.NotContains(t, recorder.Body.String(), "input_content")
			if tc.wantStatus == http.StatusInternalServerError {
				require.NotContains(t, recorder.Body.String(), tc.err.Error(), "内部数据库错误不得泄漏到管理接口响应")
			}
		})
	}
}
