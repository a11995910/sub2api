package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// RequestBodyLimit 使用 MaxBytesReader 限制请求体大小。
func RequestBodyLimit(maxBytes int64, budgets ...*BodyMemoryBudget) gin.HandlerFunc {
	var budget *BodyMemoryBudget
	if len(budgets) > 0 {
		budget = budgets[0]
	}
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		if budget != nil {
			RequestBodyBudget(budget, maxBytes)(c)
			return
		}
		c.Next()
	}
}
