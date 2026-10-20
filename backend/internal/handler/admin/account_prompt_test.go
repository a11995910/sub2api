package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAccountPromptRejectsInvalidInputBeforeCallingService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := &AccountHandler{}
	router.POST("/accounts/:id/test", handler.Test)
	for _, body := range []string{
		`{"mode":"prompt","model_id":"gpt-6-astra","prompt":""}`,
		`{"mode":"prompt","model_id":"other","prompt":"题目"}`,
		`{"mode":"prompt","model_id":"gpt-6-astra","prompt":"题目","reasoning_effort":"invalid"}`,
		`{"mode":"prompt","model_id":"gpt-6-astra","prompt":"题目","image_data_url":"data:fixture"}`,
		`{"mode":"prompt","model_id":"gpt-6-astra","prompt":"题目",`,
	} {
		request := httptest.NewRequest(http.MethodPost, "/accounts/71/test", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusBadRequest, recorder.Code, body)
	}
}

func TestAccountPromptCapabilityRequiresNoAccountCall(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	handler := &AccountHandler{}
	handler.PromptTestCapabilities(c)
	require.Equal(t, http.StatusOK, c.Writer.Status())
	require.Contains(t, recorder.Body.String(), `"version":1`)
}
