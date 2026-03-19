package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"kyc-service/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/trace"
)

// UnifiedContextMiddleware 统一上下文中间件
// 负责生成/提取 TraceID, RequestID 并注入 Context
func UnifiedContextMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. 生成或提取 RequestID
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
		}
		c.Header("X-Request-ID", requestID)

		// 2. 尝试从 OpenTelemetry 获取 TraceID/SpanID
		// OTel 中间件通常在 Router 最外层执行 (otelgin)，此时 Context 中应该已有 Span
		span := trace.SpanFromContext(c.Request.Context())
		var traceID, spanID string
		if span.SpanContext().IsValid() {
			traceID = span.SpanContext().TraceID().String()
			spanID = span.SpanContext().SpanID().String()
		} else {
			// 回退逻辑：如果 OTel 未启用或未采样，检查 Header 或生成新 ID
			traceID = c.GetHeader("X-Trace-ID")
			if traceID == "" {
				traceID = uuid.New().String()
			}
		}
		
		// 统一回写 Header，方便下游服务或客户端获取
		c.Header("X-Trace-ID", traceID)

		// 3. 注入 Context
		c.Set(models.ContextKeyRequestID, requestID)
		c.Set(models.ContextKeyTraceID, traceID)
		if spanID != "" {
			c.Set(models.ContextKeySpanID, spanID)
		}
		c.Set(models.ContextKeyClientIP, c.ClientIP())

		// 将 Context 传递给下游
		ctx := context.WithValue(c.Request.Context(), models.ContextKeyRequestID, requestID)
		ctx = context.WithValue(ctx, models.ContextKeyTraceID, traceID)
		if spanID != "" {
			ctx = context.WithValue(ctx, models.ContextKeySpanID, spanID)
		}
		c.Request = c.Request.WithContext(ctx)

		c.Next()
	}
}

// SystemLoggerMiddleware SRE 系统日志中间件
// 替代原有的 Logger()，只打印 JSON 到 Stdout，不写数据库
func SystemLoggerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		rawQuery := c.Request.URL.RawQuery

		// 记录 Request Body (可选，需注意性能和脱敏)
		// 暂时略过，避免读取 Body 导致流被消耗（需要 tee reader）

		c.Next()

		// 请求结束
		latency := time.Since(start)
		statusCode := c.Writer.Status()
		clientIP := c.ClientIP()
		method := c.Request.Method

		// 构建日志信封
		envelope := models.LogEnvelope{
			Timestamp: time.Now(),
			LogType:   models.LogTypeSRE,
			Level:     "INFO",
			Trace: models.TraceInfo{
				TraceID:   c.GetString(models.ContextKeyTraceID),
				RequestID: c.GetString(models.ContextKeyRequestID),
			},
			Identity: models.IdentityInfo{
				TenantID: c.GetString("orgID"), // 兼容现有 key
				UserID:   getString(c, "user_id"), // 兼容现有 key
				ClientIP: clientIP,
			},
			EventAction: "http_request",
			Payload: map[string]interface{}{
				"method":     method,
				"path":       path,
				"query":      rawQuery,
				"status":     statusCode,
				"latency_ms": latency.Milliseconds(),
				"user_agent": c.Request.UserAgent(),
				"error":      c.Errors.String(),
			},
		}

		if statusCode >= 500 {
			envelope.Level = "ERROR"
		} else if statusCode >= 400 {
			envelope.Level = "WARN"
		}

		// 输出到 Stdout (使用 logrus 格式化为 JSON)
		logJSON(envelope)
	}
}

// logJSON 辅助函数：将信封序列化并打印
func logJSON(envelope models.LogEnvelope) {
	// 这里直接使用 logrus 的 Print，假设 logrus 已配置为 JSONFormatter
	// 或者手动序列化以确保完全符合 LogEnvelope 结构
	// 为了确保完全控制结构，建议手动 Marshal
	data, err := json.Marshal(envelope)
	if err == nil {
		fmt.Println(string(data))
	} else {
		// Fallback
		logrus.WithFields(logrus.Fields{"err": err}).Error("Failed to marshal log envelope")
	}
}

// 辅助函数：安全获取 string 类型 context 值
func getString(c *gin.Context, key string) string {
	if v, exists := c.Get(key); exists {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// captureWriter 用于捕获响应体 (如果需要记录 SRE 日志中的响应内容)
type captureWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w captureWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}
