package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// CreativeDrawingHandler 处理用户侧创意绘图持久任务接口。
type CreativeDrawingHandler struct {
	creativeDrawingService *service.CreativeDrawingService
}

func NewCreativeDrawingHandler(creativeDrawingService *service.CreativeDrawingService) *CreativeDrawingHandler {
	return &CreativeDrawingHandler{creativeDrawingService: creativeDrawingService}
}

// ListTasks 返回当前用户最近的创意绘图任务。
// GET /api/v1/creative-drawing/tasks
func (h *CreativeDrawingHandler) ListTasks(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	limit := 50
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	tasks, err := h.creativeDrawingService.ListTasks(c.Request.Context(), subject.UserID, limit)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, tasks)
}

// GetTask 返回当前用户指定创意绘图任务。
// GET /api/v1/creative-drawing/tasks/:id
func (h *CreativeDrawingHandler) GetTask(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	task, err := h.creativeDrawingService.GetTask(c.Request.Context(), subject.UserID, c.Param("id"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, task)
}

// CreateTask 创建创意绘图任务并交给后端异步执行。
// POST /api/v1/creative-drawing/tasks
func (h *CreativeDrawingHandler) CreateTask(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req service.CreativeDrawingCreateTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	task, err := h.creativeDrawingService.CreateTask(c.Request.Context(), subject.UserID, req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Accepted(c, task)
}

// GetPromptMarketLibrary 返回用户侧模板市场的原始模板数据。
// GET /api/v1/creative-drawing/prompt-market/libraries/:library/prompts
// GET /api/v1/creative-drawing/prompt-market/libraries/:library/prompts/:language
func (h *CreativeDrawingHandler) GetPromptMarketLibrary(c *gin.Context) {
	data, contentType, err := h.creativeDrawingService.FetchPromptMarketLibrary(
		c.Request.Context(),
		c.Param("library"),
		c.Param("language"),
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if strings.TrimSpace(contentType) == "" {
		contentType = "application/octet-stream"
	}
	c.Data(http.StatusOK, contentType, data)
}

// GetPromptMarketAsset 返回模板市场图片资源，避免用户侧页面直接暴露上游仓库地址。
// GET /api/v1/creative-drawing/prompt-market/assets/:library/*path
func (h *CreativeDrawingHandler) GetPromptMarketAsset(c *gin.Context) {
	asset, err := h.creativeDrawingService.FetchPromptMarketAsset(
		c.Request.Context(),
		c.Param("library"),
		c.Param("path"),
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	contentType := strings.TrimSpace(asset.ContentType)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	c.Header("Cache-Control", "public, max-age=3600")
	c.Data(http.StatusOK, contentType, asset.Body)
}
