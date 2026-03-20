package middleware

import (
	"encoding/json"
	"strings"

	"kyc-service/pkg/response"

	"github.com/gin-gonic/gin"
)

func RequireKeyScope(required string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var scopes []string
		raw := c.GetString("scopes")
		if raw != "" {
			// 兼容两种格式：JSON数组字符串 或 空格分隔字符串
			if err := json.Unmarshal([]byte(raw), &scopes); err != nil {
				scopes = strings.Fields(strings.TrimSpace(raw))
			}
		}
		has := false
		for _, s := range scopes {
			if s == required {
				has = true
				break
			}
		}
		if !has {
			response.JSONError(c, response.CodeForbidden, "API key lacks required scope")
			c.Abort()
			return
		}
		c.Next()
	}
}
