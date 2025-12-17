package middleware

import (
	"context"
	"time"

	"kyc-service/internal/models"
	"kyc-service/internal/service"

	"github.com/gin-gonic/gin"
)

func RequireOrganizationHeader(svc *service.KYCService) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID := c.GetHeader("X-Organization-ID")
		if orgID == "" {
			c.JSON(400, gin.H{
				"code":       1007,
				"message":    "缺少必要参数",
				"error":      "Missing X-Organization-ID header",
				"timestamp":  time.Now().UnixMilli(),
				"request_id": c.GetString("request_id"),
				"path":       c.Request.URL.Path,
				"method":     c.Request.Method,
			})
			c.Abort()
			return
		}

		userID := c.GetString("userID")
		if userID == "" {
			c.JSON(401, gin.H{
				"code":       1001,
				"message":    "未授权访问",
				"error":      "Missing user context",
				"timestamp":  time.Now().UnixMilli(),
				"request_id": c.GetString("request_id"),
				"path":       c.Request.URL.Path,
				"method":     c.Request.Method,
			})
			c.Abort()
			return
		}

		var user models.User
		if err := svc.DB.Where("id = ?", userID).First(&user).Error; err != nil {
			c.JSON(401, gin.H{
				"code":       1001,
				"message":    "未授权访问",
				"error":      "User not found",
				"timestamp":  time.Now().UnixMilli(),
				"request_id": c.GetString("request_id"),
				"path":       c.Request.URL.Path,
				"method":     c.Request.Method,
			})
			c.Abort()
			return
		}

		var member models.OrganizationMember
		if err := svc.DB.Where("organization_id = ? AND user_id = ?", orgID, userID).First(&member).Error; err != nil {
			c.JSON(403, gin.H{
				"code":       1002,
				"message":    "权限不足",
				"error":      "Not a member of this organization",
				"timestamp":  time.Now().UnixMilli(),
				"request_id": c.GetString("request_id"),
				"path":       c.Request.URL.Path,
				"method":     c.Request.Method,
			})
			c.Abort()
			return
		}
		// 额外检查Redis中的停用标记
		if svc.Redis != nil {
			_ = svc.Redis.Del(context.Background(), "").Err() // noop to ensure client usable
			if res := svc.Redis.Get(context.Background(), "suspended:"+orgID+":"+userID).Val(); res != "" {
				c.JSON(403, gin.H{"code": 1002, "message": "权限不足", "error": "Account Suspended", "timestamp": time.Now().UnixMilli(), "request_id": c.GetString("request_id"), "path": c.Request.URL.Path, "method": c.Request.Method})
				c.Abort()
				return
			}
		}
		if member.Status != "active" {
			c.JSON(403, gin.H{
				"code":       1002,
				"message":    "权限不足",
				"error":      "Account Suspended",
				"timestamp":  time.Now().UnixMilli(),
				"request_id": c.GetString("request_id"),
				"path":       c.Request.URL.Path,
				"method":     c.Request.Method,
			})
			c.Abort()
			return
		}

		// 更新最后活跃时间
		_ = svc.DB.Model(&models.OrganizationMember{}).Where("id = ?", member.ID).Update("last_active_at", time.Now()).Error

		c.Set("orgID", orgID)
		c.Set("orgRole", member.Role)
		c.Next()
	}
}
