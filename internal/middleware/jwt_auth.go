package middleware

import (
	"strings"
	"time"

	"kyc-service/internal/models"
	"kyc-service/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// JWTAuth JWT认证中间件
func JWTAuth(service *service.KYCService) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 获取Authorization头
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(401, gin.H{
				"code":       1001,
				"message":    "未授权访问",
				"error":      "Missing authorization header",
				"timestamp":  time.Now().UnixMilli(),
				"request_id": c.GetString("request_id"),
				"path":       c.Request.URL.Path,
				"method":     c.Request.Method,
			})
			c.Abort()
			return
		}

		// 检查Bearer格式
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(401, gin.H{
				"code":       1001,
				"message":    "未授权访问",
				"error":      "Invalid authorization header format",
				"timestamp":  time.Now().UnixMilli(),
				"request_id": c.GetString("request_id"),
				"path":       c.Request.URL.Path,
				"method":     c.Request.Method,
			})
			c.Abort()
			return
		}

		tokenString := parts[1]

		// 解析JWT令牌
		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			// 验证签名方法
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			// 返回密钥
			return []byte(service.Config.Security.JWTSecret), nil
		})

		if err != nil || !token.Valid {
			c.JSON(401, gin.H{
				"code":       1001,
				"message":    "未授权访问",
				"error":      "Invalid or expired token",
				"timestamp":  time.Now().UnixMilli(),
				"request_id": c.GetString("request_id"),
				"path":       c.Request.URL.Path,
				"method":     c.Request.Method,
			})
			c.Abort()
			return
		}

		// 提取声明
		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			c.JSON(401, gin.H{
				"code":       1001,
				"message":    "未授权访问",
				"error":      "Invalid token claims",
				"timestamp":  time.Now().UnixMilli(),
				"request_id": c.GetString("request_id"),
				"path":       c.Request.URL.Path,
				"method":     c.Request.Method,
			})
			c.Abort()
			return
		}

		// 检查过期时间
		if exp, ok := claims["exp"].(float64); ok {
			if time.Now().Unix() > int64(exp) {
				c.JSON(401, gin.H{
					"code":       1001,
					"message":    "未授权访问",
					"error":      "Token has expired",
					"timestamp":  time.Now().UnixMilli(),
					"request_id": c.GetString("request_id"),
					"path":       c.Request.URL.Path,
					"method":     c.Request.Method,
				})
				c.Abort()
				return
			}
		}

		// 提取用户信息
		userID, ok := claims["user_id"].(string)
		if !ok {
			c.JSON(401, gin.H{
				"code":       1001,
				"message":    "未授权访问",
				"error":      "Invalid user ID in token",
				"timestamp":  time.Now().UnixMilli(),
				"request_id": c.GetString("request_id"),
				"path":       c.Request.URL.Path,
				"method":     c.Request.Method,
			})
			c.Abort()
			return
		}

		// 获取用户详细信息
		var user models.User
		if err := service.DB.Where("id = ? AND status = ?", userID, "active").First(&user).Error; err != nil {
			c.JSON(401, gin.H{
				"code":       1001,
				"message":    "未授权访问",
				"error":      "User not found or inactive",
				"timestamp":  time.Now().UnixMilli(),
				"request_id": c.GetString("request_id"),
				"path":       c.Request.URL.Path,
				"method":     c.Request.Method,
			})
			c.Abort()
			return
		}

		// 设置用户信息到上下文
		c.Set("user", claims)
		c.Set("userID", user.ID)
		c.Set("userEmail", user.Email)
		c.Set("userRole", user.Role)
		// 解析当前组织上下文：优先 CurrentOrgID，否则回退到用户OrgID
		currentOrgID := user.CurrentOrgID
		if currentOrgID == "" {
			currentOrgID = user.OrgID
		}
		c.Set("orgID", currentOrgID)
		// 从成员表解析组织角色
		var member models.OrganizationMember
		if currentOrgID != "" {
			_ = service.DB.Where("organization_id = ? AND user_id = ? AND status = ?", currentOrgID, user.ID, "active").First(&member).Error
		}
		orgRole := member.Role
		if orgRole == "" {
			orgRole = user.OrgRole
		}
		c.Set("orgRole", orgRole)
		c.Set("isPlatformAdmin", user.IsPlatformAdmin)
		if user.CurrentOrgID != "" {
			c.Set("currentOrgID", user.CurrentOrgID)
		}

		// 加载角色权限列表
		var permIDs []string
		type rp struct{ PermissionID string }
		var rows []rp
		if err := service.DB.Table("role_permissions").Select("permission_id").Where("role_id = ?", orgRole).Scan(&rows).Error; err == nil {
			for _, r := range rows {
				permIDs = append(permIDs, r.PermissionID)
			}
		}
		c.Set("permissions", permIDs)

		c.Next()
	}
}
