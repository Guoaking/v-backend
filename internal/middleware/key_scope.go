package middleware

import (
    "encoding/json"
    "strings"
    "time"

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
			c.JSON(403, gin.H{
				"code":       1002,
				"message":    "权限不足",
				"error":      "API key lacks required scope",
				"timestamp":  time.Now().UnixMilli(),
				"request_id": c.GetString("request_id"),
				"path":       c.Request.URL.Path,
				"method":     c.Request.Method,
			})
			c.Abort()
			return
		}
		c.Next()
	}
}
