package middleware

import (
	"net"
	"strings"
	"time"

	"kyc-service/internal/service"
	"kyc-service/pkg/crypto"
	"kyc-service/pkg/logger"

	"github.com/gin-gonic/gin"
)

// APIKeyAuth API密钥认证中间件
func APIKeyAuth(service *service.KYCService) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 获取Authorization头
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(401, gin.H{
				"success": false,
				"error":   "Missing authorization header",
				"code":    "AUTH_MISSING",
			})
			c.Abort()
			return
		}

		// 检查Bearer格式
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(401, gin.H{
				"success": false,
				"error":   "Invalid authorization header format",
				"code":    "AUTH_INVALID_FORMAT",
			})
			c.Abort()
			return
		}

		token := parts[1]

		// 验证API密钥（使用哈希匹配）
		hash, _ := crypto.HashString(token)
		key, err := service.GetAPIKeyBySecretHash(hash)

		if err != nil {
			logger.GetLogger().WithError(err).Error("API key查询失败")
			c.JSON(401, gin.H{
				"success": false,
				"error":   "Invalid API key",
				"code":    "AUTH_INVALID_KEY",
			})
			c.Abort()
			return
		}

		// 检查密钥状态
		switch key.Status {
		case "active":
			// ok
		case "rate_limited":
			c.JSON(429, gin.H{
				"success": false,
				"error":   "Rate limit exceeded",
				"code":    "RATE_LIMITED",
			})
			c.Abort()
			return
		case "quota_exceeded":
			c.JSON(429, gin.H{
				"success": false,
				"error":   "Quota exceeded",
				"code":    "QUOTA_EXCEEDED",
			})
			c.Abort()
			return
		default:
			// revoked or unknown
			c.JSON(401, gin.H{
				"success": false,
				"error":   "API key is revoked",
				"code":    "AUTH_REVOKED_KEY",
			})
			c.Abort()
			return
		}

		// IP白名单校验
		if len(key.IPWhitelist) > 0 {
			clientIP := c.ClientIP()
			if !isIPAllowed(clientIP, key.IPWhitelist) {
				c.JSON(403, gin.H{
					"success": false,
					"error":   "IP not allowed",
					"code":    "IP_NOT_ALLOWED",
					"details": gin.H{
						"client_ip":   clientIP,
						"allowed_ips": key.IPWhitelist,
					},
				})
				c.Abort()
				return
			}
		}

		// 更新最后使用时间与来源IP（异步）
		now := time.Now()
		clientIP := c.ClientIP()
		go func() {
			key.LastUsedAt = &now
			key.LastUsedIP = clientIP
			if err := service.UpdateAPIKey(key); err != nil {
				logger.GetLogger().WithError(err).Error("Failed to update API key last used metadata")
			}
		}()

		// 获取用户信息
		user, err := service.GetUserByID(key.UserID)
		if err != nil {
			c.JSON(401, gin.H{
				"success": false,
				"error":   "User not found",
				"code":    "AUTH_USER_NOT_FOUND",
			})
			c.Abort()
			return
		}

		// 获取用户组织信息（优先成员表，失败则回退到API Key的Org或用户的Org）
		var orgID string
		var orgRole string
		if member, err := service.GetOrganizationMemberByUserID(user.ID); err == nil {
			orgID = member.OrganizationID
			orgRole = member.Role
		} else {
			logger.GetLogger().WithError(err).Warn("用户组织成员未找到，使用回退策略")
			if key.OrgID != "" {
				orgID = key.OrgID
			} else if user.OrgID != "" {
				orgID = user.OrgID
			}
			if orgID == "" {
				c.JSON(401, gin.H{
					"success": false,
					"error":   "Organization not found",
					"code":    "AUTH_ORG_NOT_FOUND",
				})
				c.Abort()
				return
			}
			orgRole = user.OrgRole
			if orgRole == "" {
				orgRole = "viewer"
			}
		}

		// 设置用户信息到上下文
		c.Set("userID", user.ID)
		c.Set("userEmail", user.Email)
		c.Set("userRole", user.Role)
		c.Set("userOrgRole", orgRole)
		c.Set("orgID", orgID)
		c.Set("apiKeyID", key.ID)
		c.Set("apiKeyOwnerID", key.UserID)
		c.Set("scopes", key.Scopes)

		c.Next()
	}
}

// isIPAllowed 检查IP是否在白名单内
func isIPAllowed(clientIP string, whitelist []string) bool {
	// 解析客户端IP
	clientAddr := net.ParseIP(clientIP)
	if clientAddr == nil {
		logger.GetLogger().WithField("client_ip", clientIP).Error("无法解析客户端IP")
		return false
	}

	// 检查每个白名单条目
	for _, allowed := range whitelist {
		allowed = strings.TrimSpace(allowed)
		if allowed == "" {
			continue
		}

		// 检查是否是CIDR格式
		if strings.Contains(allowed, "/") {
			_, ipNet, err := net.ParseCIDR(allowed)
			if err != nil {
				logger.GetLogger().WithField("cidr", allowed).WithError(err).Error("无效的CIDR格式")
				continue
			}
			if ipNet.Contains(clientAddr) {
				return true
			}
		} else {
			// 单个IP地址
			allowedIP := net.ParseIP(allowed)
			if allowedIP != nil && allowedIP.Equal(clientAddr) {
				return true
			}
		}
	}

	return false
}
