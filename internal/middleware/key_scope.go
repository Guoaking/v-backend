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
			// HACK/FIX: Allow attendance_magic_link to bypass all specific KYC scopes
			// since the magic link token acts as an all-access pass for attendance-related KYC tasks
			if s == required || s == "attendance_magic_link" {
				has = true
				break
			}
		}
		if !has {
			response.JSONError(c, response.CodeForbidden, "Missing required scope")
			c.Abort()
			return
		}
		c.Next()
	}
}
