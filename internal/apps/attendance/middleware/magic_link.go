package middleware

import (
	"fmt"
	"net/http"
	"strings"

	"kyc-service/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const (
	AttendanceContextOrgID = "attendance_org_id"
)

// MagicLinkAuth 中间件：用于解析前端 H5 传来的专属打卡 Token
// 该 Token 由控制台老板生成，里面仅包含 org_id，不包含 user_id。
// 通过此中间件，H5 的所有请求都会被死死绑定在该 org_id 之下。
func MagicLinkAuth(jwtSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. 获取 Token
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "Missing Authorization header"})
			c.Abort()
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if !(len(parts) == 2 && parts[0] == "Bearer") {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "Invalid Authorization header format"})
			c.Abort()
			return
		}

		tokenString := parts[1]

		// 2. 解析 JWT
		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			// 确保签名方法正确
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return []byte(jwtSecret), nil
		})

		if err != nil || !token.Valid {
			logger.GetLogger().WithContext(c).Warnf("Invalid Magic Link Token: %v", err)
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "Invalid or expired magic link"})
			c.Abort()
			return
		}

		// 3. 提取 claims
		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "Invalid token claims"})
			c.Abort()
			return
		}

		// 4. 验证必须包含 org_id 且 scope 为 attendance
		orgID, hasOrg := claims["org_id"].(string)
		scope, hasScope := claims["scope"].(string)

		if !hasOrg || orgID == "" || !hasScope || scope != "attendance_magic_link" {
			c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "Invalid token scope for attendance"})
			c.Abort()
			return
		}

		// 5. 将 org_id 注入 Context，供下游所有的 Handler 使用
		c.Set(AttendanceContextOrgID, orgID)

		// 记录日志，方便追踪
		logger.GetLogger().WithContext(c).Infof("Attendance Request from Org: %s", orgID)

		c.Next()
	}
}
