package middleware

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/sync/semaphore"
)

const bodyMemoryReservationMultiplier int64 = 2

var errBodyMemoryBudgetUnavailable = errors.New("request body memory budget unavailable")

// BodyMemoryBudget 限制处理中的请求体估算内存。
// 预留会一直持有到 c.Next() 返回，因为大 body 可能在等待上游、重试和流式
// 响应期间持续被引用。
type BodyMemoryBudget struct {
	semaphore *semaphore.Weighted
	capacity  int64
	wait      int
}

// NewBodyMemoryBudget 创建进程级请求体内存预算。
// waitSeconds <= 0 表示预算耗尽时立即拒绝。
func NewBodyMemoryBudget(capacityBytes int64, waitSeconds int) *BodyMemoryBudget {
	if capacityBytes <= 0 {
		return nil
	}
	if waitSeconds < 0 {
		waitSeconds = 0
	}
	return &BodyMemoryBudget{
		semaphore: semaphore.NewWeighted(capacityBytes),
		capacity:  capacityBytes,
		wait:      waitSeconds,
	}
}

func (b *BodyMemoryBudget) reservationBytes(contentLength, routeLimit int64) int64 {
	if b == nil || routeLimit <= 0 {
		return 0
	}
	bodyBytes := contentLength
	if bodyBytes < 0 {
		// 分块/流式请求在读取前没有可靠的大小，按路由上限预留，避免绕过进程预算。
		bodyBytes = routeLimit
	}
	if bodyBytes <= 0 {
		return 0
	}
	if bodyBytes > routeLimit {
		bodyBytes = routeLimit
	}
	if bodyBytes > b.capacity/bodyMemoryReservationMultiplier {
		return b.capacity + 1
	}
	return bodyBytes * bodyMemoryReservationMultiplier
}

func (b *BodyMemoryBudget) acquire(ctx context.Context, n int64) error {
	if b == nil || n <= 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if n > b.capacity {
		return errBodyMemoryBudgetUnavailable
	}
	if b.wait <= 0 {
		if b.semaphore.TryAcquire(n) {
			return nil
		}
		return errBodyMemoryBudgetUnavailable
	}
	waitCtx, cancel := context.WithTimeout(ctx, durationSeconds(b.wait))
	defer cancel()
	if err := b.semaphore.Acquire(waitCtx, n); err != nil {
		return errBodyMemoryBudgetUnavailable
	}
	return nil
}

func (b *BodyMemoryBudget) release(n int64) {
	if b != nil && n > 0 {
		b.semaphore.Release(n)
	}
}

// durationSeconds 将整数配置转换为超时时长，避免路由组装处暴露 time.Duration。
func durationSeconds(seconds int) time.Duration {
	return time.Duration(seconds) * time.Second
}

// RequestBodyBudget 返回一个在下游读取 body 前预留内存、处理完成后释放的中间件。
func RequestBodyBudget(budget *BodyMemoryBudget, routeLimit int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if budget == nil || c == nil || c.Request == nil || c.Request.Body == nil || c.Request.Body == http.NoBody {
			c.Next()
			return
		}

		reservation := budget.reservationBytes(c.Request.ContentLength, routeLimit)
		if reservation <= 0 {
			c.Next()
			return
		}
		if err := budget.acquire(c.Request.Context(), reservation); err != nil {
			MarkIngressRejected(c, IngressRejectBodyMemoryBudget)
			c.Header("Retry-After", strconv.Itoa(maxInt(1, budget.wait)))
			writeBodyBudgetError(c)
			return
		}
		defer budget.release(reservation)
		c.Next()
	}
}

func writeBodyBudgetError(c *gin.Context) {
	message := "Too many large requests are being processed; please retry later"
	path := strings.ToLower(c.Request.URL.Path)
	switch {
	case strings.HasPrefix(path, "/v1beta") || strings.HasPrefix(path, "/antigravity/v1beta"):
		// Gemini SDK 需要 Google JSON 错误封装。
		GoogleErrorWriter(c, http.StatusTooManyRequests, message)
	case strings.Contains(path, "/messages") || strings.HasPrefix(path, "/antigravity"):
		// Anthropic 客户端需要顶层 {type:error} 错误封装。
		AnthropicErrorWriter(c, http.StatusTooManyRequests, message)
	default:
		c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
			"error": gin.H{
				"type":    "rate_limit_error",
				"code":    "request_body_memory_budget_exhausted",
				"message": message,
			},
		})
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
