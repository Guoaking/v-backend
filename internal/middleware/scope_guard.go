package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
)

func ScopePermission(perms []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		scope := strings.ToLower(strings.TrimSpace(c.Query("scope")))
		if scope == "" {
			scope = "org"
		}
		if scope == "personal" {
			c.Next()
			return
		}
		RequireAnyPermission(perms)(c)
	}
}

// UsageScopeGuard / BillingScopeGuard 已废弃，请在路由上直接组合：
// ScopePermission([]string{"org.usage.read","logs.read"}) 或 ScopePermission([]string{"org.billing.read","billing.read"})
