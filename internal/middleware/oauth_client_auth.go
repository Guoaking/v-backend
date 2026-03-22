package middleware

import (
	"context"
	"strings"
	"time"

	"kyc-service/internal/models"
	"kyc-service/internal/service"
	"kyc-service/pkg/crypto"
	"kyc-service/pkg/logger"
	"kyc-service/pkg/response"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

func OAuth2ClientAuth(svc *service.KYCService) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.Next()
			return
		}
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.Next()
			return
		}
		tokenString := parts[1]

		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return []byte(svc.Config.Security.JWTSecret), nil
		})
		if err != nil || !token.Valid {
			c.Next()
			return
		}
		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			c.Next()
			return
		}
		if exp, ok := claims["exp"].(float64); ok {
			if time.Now().Unix() > int64(exp) {
				c.Next()
				return
			}
		}

		clientID, _ := claims["client_id"].(string)
		scopeStr, _ := claims["scope"].(string)
		claimOrgID, _ := claims["org_id"].(string)
		if clientID == "" {
			c.Next()
			return
		}

		var client models.OAuthClient
		if err := svc.DB.Where("id = ? AND status = ?", clientID, "active").First(&client).Error; err != nil {
			c.Next()
			return
		}

		// 组织ID注入与校验：优先使用令牌声明，其次使用客户端绑定；若请求头带组织则需匹配
		org := claimOrgID
		if org == "" {
			org = client.OrgID
		}
		headerOrg := c.Request.Header.Get("X-Organization-ID")
		if headerOrg != "" && org != "" && headerOrg != org {
			response.JSONError(c, response.CodeForbidden, "organization mismatch")
			c.Abort()
			return
		}
		if org != "" {
			c.Set("orgID", org)
		}
		if scopeStr != "" {
			c.Set("scopes", scopeStr)
		}
		c.Next()
	}
}

// APIOrOAuthAuth 允许使用 OAuth2 客户端凭证 或 API Key 进行认证
func APIOrOAuthAuth(svc *service.KYCService) gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		if auth == "" {
			response.JSONError(c, response.CodeUnauthorized, "Missing authorization header")
			c.Abort()
			return
		}
		parts := strings.Split(auth, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			response.JSONError(c, response.CodeUnauthorized, "Invalid authorization header format")
			c.Abort()
			return
		}
		credential := parts[1]

		// 尝试作为OAuth2访问令牌解析
		if tok, err := jwt.Parse(credential, func(token *jwt.Token) (interface{}, error) { return []byte(svc.Config.Security.JWTSecret), nil }); err == nil && tok.Valid {
			if claims, ok := tok.Claims.(jwt.MapClaims); ok {
				// 过期校验
				if v, ok := claims["exp"].(float64); ok && time.Now().Unix() > int64(v) {
					response.JSONError(c, response.CodeUnauthorized, "token expired")
					c.Abort()
					return
				}
				clientID, _ := claims["client_id"].(string)
				scopeStr, _ := claims["scope"].(string)
				claimOrgID, _ := claims["org_id"].(string)
				if clientID == "" {
					response.JSONError(c, response.CodeUnauthorized, "invalid token claims")
					c.Abort()
					return
				}
				var client models.OAuthClient
				if err := svc.DB.Where("id = ? AND status = ?", clientID, "active").First(&client).Error; err != nil {
					response.JSONError(c, response.CodeUnauthorized, "invalid client")
					c.Abort()
					return
				}
				org := claimOrgID
				if org == "" {
					org = client.OrgID
				}
				hdrOrg := c.Request.Header.Get("X-Organization-ID")
				if hdrOrg != "" && org != "" && hdrOrg != org {
					response.JSONError(c, response.CodeForbidden, "organization mismatch")
					c.Abort()
					return
				}
				if org != "" {
					c.Set("orgID", org)
				}
				if scopeStr != "" {
					c.Set("scopes", scopeStr)
				}
				if clientID != "" {
					c.Set("oauthClientID", clientID)
				}
				if client.OwnerID != "" {
					c.Set("clientOwnerID", client.OwnerID)
				}
				if len(client.IPWhitelist) > 0 {
					ip := c.ClientIP()
					if !isIPAllowed(ip, client.IPWhitelist) {
						response.JSONError(c, response.CodeForbidden, "IP not allowed")
						c.Abort()
						return
					}
				}
				limit := client.RateLimitPerSec
				if limit <= 0 {
					pol := svc.GetOrgPolicy(org)
					if pol.MaxRatePerSec > 0 {
						limit = pol.MaxRatePerSec
					}
				}
				if limit > 0 && svc.Redis != nil {
					ctx := context.Background()
					key := "rate_limit:oauth_client:" + clientID
					cur, err := svc.Redis.Get(ctx, key).Int()
					if err != nil && err.Error() != "redis: nil" {
						response.JSONError(c, response.CodeInternalError, "rate limit error")
						c.Abort()
						return
					}
					if cur >= limit {
						response.JSONError(c, response.CodeTooManyRequests, "too many requests")
						c.Abort()
						return
					}
					pipe := svc.Redis.Pipeline()
					pipe.Incr(ctx, key)
					pipe.Expire(ctx, key, time.Second)
					_, _ = pipe.Exec(ctx)
				}
				c.Next()
				return
			}
		}

		// 回退：按API Key处理
		hash, _ := crypto.HashString(credential)
		key, err := svc.GetAPIKeyBySecretHash(hash)
		if err != nil {
			logger.GetLogger().WithError(err).Error("API key查询失败")
			response.JSONError(c, response.CodeUnauthorized, "Invalid API key")
			c.Abort()
			return
		}
		if key.Status != "active" {
			response.JSONError(c, response.CodeUnauthorized, "API key is revoked")
			c.Abort()
			return
		}
		// IP白名单
		if len(key.IPWhitelist) > 0 {
			clientIP := c.ClientIP()
			if !isIPAllowed(clientIP, key.IPWhitelist) {
				response.JSONError(c, response.CodeForbidden, "IP not allowed")
				c.Abort()
				return
			}
		}
		// 注入上下文
		c.Set("userID", key.UserID)
		c.Set("orgID", key.OrgID)
		c.Set("apiKeyID", key.ID)
		c.Set("apiKeyOwnerID", key.UserID)
		c.Set("scopes", key.Scopes)
		c.Next()
	}
}

// 简单的 IP 校验，可在此扩展为子网或正则支持
func isIPAllowed(ip string, allowed []string) bool {
	for _, a := range allowed {
		if a == ip || a == "*" || a == "0.0.0.0/0" {
			return true
		}
	}
	return false
}
