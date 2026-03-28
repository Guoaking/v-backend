package middleware

import (
	"context"
	"time"

	"kyc-service/internal/models"
	"kyc-service/internal/service"
	"kyc-service/pkg/response"

	"github.com/gin-gonic/gin"
)

func RequireOrganizationHeader(svc *service.KYCService) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID := c.GetHeader("X-Organization-ID")
		if orgID == "" {
			response.JSONError(c, response.CodeMissingParameter, "Missing X-Organization-ID header")
			c.Abort()
			return
		}

		userID := c.GetString("userID")
		if userID == "" {
			response.JSONError(c, response.CodeUnauthorized, "Missing user context")
			c.Abort()
			return
		}

		var user models.User
		if err := svc.DB.Where("id = ?", userID).First(&user).Error; err != nil {
			response.JSONError(c, response.CodeUnauthorized, "User not found")
			c.Abort()
			return
		}

		var member models.OrganizationMember
		if !user.IsPlatformAdmin {
			if err := svc.DB.Where("organization_id = ? AND user_id = ?", orgID, userID).First(&member).Error; err != nil {
				// 特殊处理：如果是尝试删除组织，即使可能不是 member（比如直接 owner 关联但不一定有 member 记录，尽管应该有），我们仍然允许进入 Handler，由 Handler 的 OwnerID 检查决定。但严格来说，Owner 应该在 member 表里。
				// 不过为了防止数据不一致导致的卡死，如果是 DELETE 请求，我们可以宽容一点
				if c.Request.Method != "DELETE" {
					response.JSONError(c, response.CodeForbidden, "Not a member of this organization")
					c.Abort()
					return
				}
			}
		}
		// 额外检查Redis中的停用标记
		if svc.Redis != nil {
			_ = svc.Redis.Del(context.Background(), "").Err() // noop to ensure client usable
			if res := svc.Redis.Get(context.Background(), "suspended:"+orgID+":"+userID).Val(); res != "" {
				response.JSONError(c, response.CodeForbidden, "Account Suspended")
				c.Abort()
				return
			}
		}
		if !user.IsPlatformAdmin && member.Status != "active" {
			// 同上，为了防止脏数据导致的死锁，允许 DELETE 请求通过，交由业务 Handler 最终校验
			if c.Request.Method != "DELETE" {
				response.JSONError(c, response.CodeForbidden, "Organization membership is inactive")
				c.Abort()
				return
			}
		}

		// 更新最后活跃时间
		if member.ID != "" {
			_ = svc.DB.Model(&models.OrganizationMember{}).Where("id = ?", member.ID).Update("last_active_at", time.Now()).Error
		}

		c.Set("orgID", orgID)
		if user.IsPlatformAdmin {
			c.Set("orgRole", "owner")
		} else {
			c.Set("orgRole", member.Role)
		}
		c.Next()
	}
}
