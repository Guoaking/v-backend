package middleware

import (
	"context"
	"fmt"
	"strings"
	"time"

	"kyc-service/pkg/logger"
	"kyc-service/pkg/metrics"

	"github.com/gin-gonic/gin"
)

// EnterpriseMetricsInstrumentation 企业级指标中间件（使用OpenTelemetry）
// 注意：此函数需要在main.go中初始化指标后才能正常工作
func EnterpriseMetricsInstrumentation() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		ctx := c.Request.Context()

		// 获取请求信息
		method := c.Request.Method
		endpoint := c.FullPath()
		clientIP := c.ClientIP()
		userAgent := c.Request.UserAgent()
		authorization := c.GetHeader("Authorization")

		// 设置基本信息到context
		c.Set("client_ip", clientIP)
		c.Set("user_agent", userAgent)
		c.Set("start_time", start)

		// 记录活跃请求开始
		metrics.RecordActiveRequest(ctx, "http", 1)

		// 继续处理请求
		c.Next()

		// 处理链路可能在下游中间件更新了 context（如 InjectOrgContext），此处重新获取
		ctx = c.Request.Context()

		// 记录响应信息
		statusCode := c.Writer.Status()
		statusClass := getStatusClass(statusCode)
		duration := time.Since(start)

		// 记录活跃请求结束
		metrics.RecordActiveRequest(ctx, "http", -1)

		// 记录HTTP请求指标
		metrics.RecordHTTPRequest(ctx, method, endpoint, fmt.Sprintf("%d", statusCode), duration)

		// 记录认证失败
		if statusCode == 401 {
			metrics.RecordAuthFailure(ctx, "token", "invalid_token", clientIP)
		}

		// 记录权限拒绝
		if statusCode == 403 {
			userID := getUserIDFromContext(c)
			metrics.RecordPermissionDenied(ctx, endpoint, method, userID, "insufficient_permissions")
		}

		// 记录敏感数据访问
		if isSensitiveEndpoint(endpoint) && authorization != "" {
			userID := getUserIDFromContext(c)
			metrics.RecordSensitiveDataAccess(ctx, "api_endpoint", userID, true, endpoint)
		}

		// 记录业务指标 - 根据endpoint类型记录不同的业务操作
		if strings.Contains(endpoint, "/kyc/") {
			if statusCode >= 200 && statusCode < 300 {
				metrics.RecordBusinessOperation(ctx, "kyc_request", true, duration, "")
			} else {
				metrics.RecordBusinessOperation(ctx, "kyc_request", false, duration, "http_error")
			}
		} else if strings.Contains(endpoint, "/auth/") {
			if statusCode >= 200 && statusCode < 300 {
				metrics.RecordBusinessOperation(ctx, "auth_request", true, duration, "")
			} else {
				metrics.RecordBusinessOperation(ctx, "auth_request", false, duration, "auth_error")
			}
		}

		// 记录日志
		if !strings.Contains(endpoint, "/metrics") {
			logger.GetLogger().WithFields(map[string]interface{}{
				"method":       method,
				"endpoint":     endpoint,
				"status":       statusCode,
				"duration_ms":  duration.Milliseconds(),
				"client_ip":    clientIP,
				"status_class": statusClass,
			}).Info("企业级HTTP请求完成")
		}

	}
}

// RecordBusinessOperation 记录业务操作（兼容函数）
func RecordBusinessOperation(operation string, success bool, duration time.Duration, errorType string) {
	ctx := context.Background()
	metrics.RecordBusinessOperation(ctx, operation, success, duration, errorType)
}

// RecordDependencyCall 记录依赖调用（兼容函数）
func RecordDependencyCall(service string, method string, success bool, duration time.Duration) {
	ctx := context.Background()
	metrics.RecordDependencyCall(ctx, service, method, success, duration)
}

// RecordSensitiveDataAccess 记录敏感数据访问（兼容函数）
func RecordSensitiveDataAccess(dataType string, userID string, authorized bool, endpoint string) {
	ctx := context.Background()
	metrics.RecordSensitiveDataAccess(ctx, dataType, userID, authorized, endpoint)
}

// RecordAuthFailure 记录认证失败（兼容函数）
func RecordAuthFailure(authType string, reason string, clientIP string) {
	ctx := context.Background()
	metrics.RecordAuthFailure(ctx, authType, reason, clientIP)
}

// RecordPermissionDenied 记录权限拒绝（兼容函数）
func RecordPermissionDenied(resource string, action string, userID string, reason string) {
	ctx := context.Background()
	metrics.RecordPermissionDenied(ctx, resource, action, userID, reason)
}

// 辅助函数
func getStatusClass(statusCode int) string {
	switch {
	case statusCode >= 200 && statusCode < 300:
		return "2xx"
	case statusCode >= 300 && statusCode < 400:
		return "3xx"
	case statusCode >= 400 && statusCode < 500:
		return "4xx"
	case statusCode >= 500:
		return "5xx"
	default:
		return "other"
	}
}

func getErrorType(statusCode int) string {
	switch {
	case statusCode >= 400 && statusCode < 500:
		return "client_error"
	case statusCode >= 500:
		return "server_error"
	default:
		return "other_error"
	}
}

func isSensitiveEndpoint(endpoint string) bool {
	sensitiveEndpoints := []string{
		"/api/v1/kyc/",
		"/api/v1/auth/",
		"/api/v1/clients/",
	}

	for _, sensitive := range sensitiveEndpoints {
		if strings.Contains(endpoint, sensitive) {
			return true
		}
	}
	return false
}

func getUserIDFromContext(c *gin.Context) string {
	if userID, exists := c.Get("user_id"); exists {
		if uid, ok := userID.(string); ok {
			return uid
		}
	}
	return "unknown"
}
