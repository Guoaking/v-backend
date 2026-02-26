package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
)

func forbiddenJSON(c *gin.Context, err string) {
	c.JSON(403, gin.H{
		"code":       1002,
		"message":    "权限不足",
		"error":      err,
		"timestamp":  time.Now().UnixMilli(),
		"request_id": c.GetString("request_id"),
		"path":       c.Request.URL.Path,
		"method":     c.Request.Method,
	})
}

// RequireRole 要求特定角色权限的中间件
func RequireRole(requiredRole string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userRole, exists := c.Get("userRole")
		if !exists {
			forbiddenJSON(c, "Missing userRole")
			c.Abort()
			return
		}
		if userRole != requiredRole {
			forbiddenJSON(c, "Role not allowed")
			c.Abort()
			return
		}
		c.Next()
	}
}

// RequireOrgRole 要求组织角色权限的中间件
func RequireOrgRole(requiredOrgRole string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userOrgRole, exists := c.Get("userOrgRole")
		if !exists {
			forbiddenJSON(c, "Missing userOrgRole")
			c.Abort()
			return
		}
		if userOrgRole != requiredOrgRole {
			forbiddenJSON(c, "Org role not allowed")
			c.Abort()
			return
		}
		c.Next()
	}
}

// RequireRoleOrOrgRole 要求角色或组织角色权限的中间件
func RequireRoleOrOrgRole(requiredRole string, requiredOrgRole string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userRole, roleExists := c.Get("userRole")
		userOrgRole, orgRoleExists := c.Get("userOrgRole")

		hasPermission := false
		if roleExists && userRole == requiredRole {
			hasPermission = true
		}
		if orgRoleExists && userOrgRole == requiredOrgRole {
			hasPermission = true
		}
		if !hasPermission {
			forbiddenJSON(c, "Role or org role not allowed")
			c.Abort()
			return
		}
		c.Next()
	}
}

// RequirePermission 使用组织角色计算权限并校验
func RequirePermission(perm string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetBool("isPlatformAdmin") {
			c.Next()
			return
		}
		v, exists := c.Get("permissions")
		if !exists {
			forbiddenJSON(c, "Missing permissions in context")
			c.Abort()
			return
		}
		perms, _ := v.([]string)
		allowed := false
		for _, p := range perms {
			if p == perm || p == "*" {
				allowed = true
				break
			}
		}
		if !allowed {
			forbiddenJSON(c, "Permission denied: "+perm)
			c.Abort()
			return
		}
		c.Next()
	}
}

func RequireAnyPermission(perms []string) gin.HandlerFunc {
    return func(c *gin.Context) {
        if c.GetBool("isPlatformAdmin") {
            c.Next()
            return
        }
        role := c.GetString("orgRole")
        if role == "owner" || role == "admin" {
            c.Next()
            return
        }
        v, exists := c.Get("permissions")
        if !exists {
            forbiddenJSON(c, "Missing permissions in context")
            c.Abort()
            return
        }
        ps, _ := v.([]string)
        allowed := false
        for _, p := range ps {
            for _, need := range perms {
                if p == need || p == "*" {
                    allowed = true
                    break
                }
            }
            if allowed { break }
        }
        if !allowed {
            forbiddenJSON(c, "Permission denied")
            c.Abort()
            return
        }
        c.Next()
    }
}

// RequirePlatformAdmin 仅平台管理员可访问
func RequirePlatformAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !c.GetBool("isPlatformAdmin") {
			forbiddenJSON(c, "Platform admin required")
			c.Abort()
			return
		}
		c.Next()
	}
}
