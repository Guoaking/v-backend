package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"time"

	"kyc-service/pkg/logger"
	"kyc-service/pkg/metrics"

	"github.com/gin-gonic/gin"
)

// BidirectionalAuth 双向鉴权中间件配置
type BidirectionalAuth struct {
	// Kong网关共享密钥
	KongSharedSecret string
	// 服务签名密钥
	ServiceSecretKey string
	// 服务名称
	ServiceName      string
	// 令牌过期时间
	TokenExpiration  time.Duration
}

// NewBidirectionalAuth 创建双向鉴权中间件
func NewBidirectionalAuth(kongSecret, serviceSecret, serviceName string) *BidirectionalAuth {
	return &BidirectionalAuth{
		KongSharedSecret: kongSecret,
		ServiceSecretKey:   serviceSecret,
		ServiceName:        serviceName,
		TokenExpiration:    24 * time.Hour,
	}
}

// KongAuthMiddleware 验证Kong网关身份（防止绕过）
func (b *BidirectionalAuth) KongAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 获取Kong网关签名
		kongSignature := c.GetHeader("X-Kong-Signature")
		kongTimestamp := c.GetHeader("X-Kong-Timestamp")
		kongService := c.GetHeader("X-Kong-Service")

		if kongSignature == "" || kongTimestamp == "" || kongService == "" {
			logger.GetLogger().Error("缺少Kong网关认证信息")
			metrics.RecordServiceAuthFailure()
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "缺少网关认证信息",
				"code":  "KONG_AUTH_MISSING",
			})
			c.Abort()
			return
		}

		// 验证时间戳（防止重放攻击）
		timestamp, err := time.Parse(time.RFC3339, kongTimestamp)
		if err != nil {
			logger.GetLogger().WithError(err).Error("Kong时间戳格式错误")
			metrics.RecordServiceAuthFailure()
			metrics.RecordTimestampExpired()
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "网关时间戳格式错误",
				"code":  "KONG_TIMESTAMP_INVALID",
			})
			c.Abort()
			return
		}

		// 检查时间戳是否在有效范围内（5分钟）
		if time.Since(timestamp) > 5*time.Minute || time.Since(timestamp) < -5*time.Minute {
			logger.GetLogger().Error("Kong时间戳超出有效范围")
			metrics.RecordServiceAuthFailure()
			metrics.RecordTimestampExpired()
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "网关时间戳超出有效范围",
				"code":  "KONG_TIMESTAMP_EXPIRED",
			})
			c.Abort()
			return
		}

		// 验证Kong签名
		if !b.verifyKongSignature(kongSignature, kongTimestamp, kongService, c.Request.URL.Path) {
			logger.GetLogger().Error("Kong网关签名验证失败")
			metrics.RecordServiceAuthFailure()
			metrics.RecordInvalidSignature()
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "网关签名验证失败",
				"code":  "KONG_SIGNATURE_INVALID",
			})
			c.Abort()
			return
		}

		// 验证通过，设置网关信息
		c.Set("kong_service", kongService)
		c.Set("kong_timestamp", kongTimestamp)
		c.Set("kong_verified", true)

		logger.GetLogger().WithFields(map[string]interface{}{
			"kong_service": kongService,
			"path":         c.Request.URL.Path,
		}).Info("Kong网关身份验证通过")

		c.Next()
	}
}

// ServiceAuthMiddleware 服务到网关的认证中间件
func (b *BidirectionalAuth) ServiceAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 生成服务签名
		serviceToken := b.generateServiceToken(c.Request.URL.Path, c.Request.Method)
		
		// 设置服务认证头
		c.Header("X-Service-Signature", serviceToken.Signature)
		c.Header("X-Service-Timestamp", serviceToken.Timestamp)
		c.Header("X-Service-Name", b.ServiceName)
		c.Header("X-Service-Nonce", serviceToken.Nonce)

		c.Next()
	}
}

// ServiceToken 服务认证令牌
type ServiceToken struct {
	Signature string `json:"signature"`
	Timestamp string `json:"timestamp"`
	Nonce     string `json:"nonce"`
}

// generateServiceToken 生成服务认证令牌
func (b *BidirectionalAuth) generateServiceToken(path, method string) *ServiceToken {
	timestamp := time.Now().Format(time.RFC3339)
	nonce := generateNonce()

	// 构建签名内容
	message := fmt.Sprintf("%s:%s:%s:%s:%s", b.ServiceName, path, method, timestamp, nonce)
	signature := b.generateHMACSignature(message, b.ServiceSecretKey)

	return &ServiceToken{
		Signature: signature,
		Timestamp: timestamp,
		Nonce:     nonce,
	}
}

// verifyKongSignature 验证Kong网关签名
func (b *BidirectionalAuth) verifyKongSignature(signature, timestamp, service, path string) bool {
	// 构建签名内容
	message := fmt.Sprintf("%s:%s:%s:%s", service, path, timestamp, b.KongSharedSecret)
	expectedSignature := b.generateHMACSignature(message, b.KongSharedSecret)

	// 使用常量时间比较防止时序攻击
	return hmac.Equal([]byte(signature), []byte(expectedSignature))
}

// generateHMACSignature 生成HMAC签名
func (b *BidirectionalAuth) generateHMACSignature(message, key string) string {
	h := hmac.New(sha256.New, []byte(key))
	h.Write([]byte(message))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// generateNonce 生成随机nonce
func generateNonce() string {
	// 使用时间戳和随机数生成nonce
	timestamp := time.Now().UnixNano()
	return fmt.Sprintf("%d", timestamp)
}

// BidirectionalHealthCheck 双向健康检查
type BidirectionalHealthCheck struct {
	auth *BidirectionalAuth
}

// NewBidirectionalHealthCheck 创建双向健康检查
func NewBidirectionalHealthCheck(auth *BidirectionalAuth) *BidirectionalHealthCheck {
	return &BidirectionalHealthCheck{
		auth: auth,
	}
}

// HealthCheckHandler 健康检查处理器
func (h *BidirectionalHealthCheck) HealthCheckHandler(c *gin.Context) {
	// 验证请求是否来自Kong网关
	kongVerified := c.GetBool("kong_verified")
	if !kongVerified {
		c.JSON(http.StatusUnauthorized, gin.H{
			"status":  "error",
			"message": "健康检查请求必须通过Kong网关",
			"code":    "HEALTH_CHECK_UNAUTHORIZED",
		})
		return
	}

	// 返回服务状态
	healthInfo := gin.H{
		"status":       "healthy",
		"service":      h.auth.ServiceName,
		"timestamp":    time.Now().Format(time.RFC3339),
		"kong_verified": true,
		"version":      "1.0.0",
	}

	// 生成服务响应签名
	serviceToken := h.auth.generateServiceToken("/health", "GET")
	c.Header("X-Service-Signature", serviceToken.Signature)
	c.Header("X-Service-Timestamp", serviceToken.Timestamp)
	c.Header("X-Service-Name", h.auth.ServiceName)
	c.Header("X-Service-Nonce", serviceToken.Nonce)

	c.JSON(http.StatusOK, healthInfo)
}

// GatewayValidationMiddleware 网关验证中间件（用于验证响应来自合法服务）
func (b *BidirectionalAuth) GatewayValidationMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 获取服务响应头
		serviceSignature := c.GetHeader("X-Service-Signature")
		serviceTimestamp := c.GetHeader("X-Service-Timestamp")
		serviceName := c.GetHeader("X-Service-Name")
		serviceNonce := c.GetHeader("X-Service-Nonce")

		if serviceSignature == "" || serviceTimestamp == "" || serviceName == "" || serviceNonce == "" {
			logger.GetLogger().Error("缺少服务认证信息")
			c.JSON(http.StatusBadGateway, gin.H{
				"error": "缺少服务认证信息",
				"code":  "SERVICE_AUTH_MISSING",
			})
			c.Abort()
			return
		}

		// 验证服务签名
		message := fmt.Sprintf("%s:%s:%s:%s:%s", serviceName, c.Request.URL.Path, c.Request.Method, serviceTimestamp, serviceNonce)
		expectedSignature := b.generateHMACSignature(message, b.ServiceSecretKey)

		if !hmac.Equal([]byte(serviceSignature), []byte(expectedSignature)) {
			logger.GetLogger().Error("服务签名验证失败")
			c.JSON(http.StatusBadGateway, gin.H{
				"error": "服务签名验证失败",
				"code":  "SERVICE_SIGNATURE_INVALID",
			})
			c.Abort()
			return
		}

		logger.GetLogger().WithFields(map[string]interface{}{
			"service_name": serviceName,
			"path":         c.Request.URL.Path,
		}).Info("服务身份验证通过")

		c.Next()
	}
}

// BypassDetectionMiddleware 绕过检测中间件
func (b *BidirectionalAuth) BypassDetectionMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 检查是否缺少Kong认证头（可能的绕过尝试）
		kongSignature := c.GetHeader("X-Kong-Signature")
		
		// 检查是否直接访问服务（绕过Kong）
		if kongSignature == "" && c.Request.Header.Get("X-Forwarded-By") != "Kong" {
			logger.GetLogger().WithFields(map[string]interface{}{
				"ip":   c.ClientIP(),
				"path": c.Request.URL.Path,
				"user_agent": c.Request.UserAgent(),
			}).Warn("检测到可能的Kong网关绕过尝试")

			// 记录安全事件
			RecordSecurityEvent("KONG_BYPASS_ATTEMPT", c.ClientIP(), c.Request.URL.Path, "缺少Kong认证头")

			c.JSON(http.StatusForbidden, gin.H{
				"error": "访问被拒绝",
				"code":  "ACCESS_DENIED",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// RecordSecurityEvent 记录安全事件
func RecordSecurityEvent(eventType, clientIP, path, details string) {
	logger.GetLogger().WithFields(map[string]interface{}{
		"event_type": eventType,
		"client_ip":  clientIP,
		"path":       path,
		"details":    details,
		"timestamp":  time.Now().Format(time.RFC3339),
	}).Warn("安全事件记录")
	
	// 记录到Prometheus指标
	metrics.RecordSecurityEvent(eventType, clientIP, path)
	
	// 记录可疑IP访问
	if eventType == "KONG_BYPASS_ATTEMPT" || eventType == "SUSPICIOUS_IP_ACCESS" {
		metrics.RecordSuspiciousIPAccess(clientIP, path, details)
	}
}

