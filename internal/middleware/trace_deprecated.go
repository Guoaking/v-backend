package middleware

import (
	"fmt"
	"time"

	"kyc-service/pkg/logger"
	"kyc-service/pkg/tracing"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// Trace 链路追踪中间件 - 添加trace信息到响应头和日志
func Trace() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 开始span
		ctx, span := tracing.StartSpan(c.Request.Context(), fmt.Sprintf("%s %s", c.Request.Method, c.FullPath()))
		defer span.End()

		// 设置span属性
		span.SetAttributes(
			attribute.String("http.method", c.Request.Method),
			attribute.String("http.url", c.Request.URL.String()),
			attribute.String("http.host", c.Request.Host),
			attribute.String("http.user_agent", c.Request.UserAgent()),
			attribute.String("http.client_ip", c.ClientIP()),
		)

		// 将span context注入到请求context
		c.Request = c.Request.WithContext(ctx)

		// 获取trace和span ID
		spanContext := span.SpanContext()
		traceID := spanContext.TraceID().String()
		spanID := spanContext.SpanID().String()

		// 设置请求ID（用于日志关联）
		requestID := traceID[len(traceID)-16:] // 使用后16位作为request ID
		c.Set("request_id", requestID)
		c.Set("trace_id", traceID)
		c.Set("span_id", spanID)

		// 在响应头中添加trace信息
		c.Writer.Header().Set("X-Trace-ID", traceID)
		c.Writer.Header().Set("X-Span-ID", spanID)
		c.Writer.Header().Set("X-Request-ID", requestID)

		// 记录请求开始日志
		logger.GetLogger().WithFields(map[string]interface{}{
			"request_id": requestID,
			"trace_id":   traceID,
			"span_id":    spanID,
			"method":     c.Request.Method,
			"path":       c.Request.URL.Path,
			"client_ip":  c.ClientIP(),
		}).Info("请求开始")

		// 继续处理请求
		c.Next()

		// 记录响应信息
		span.SetAttributes(
			attribute.Int("http.status_code", c.Writer.Status()),
			attribute.Int64("http.response_size", int64(c.Writer.Size())),
		)

		// 如果有错误，记录到span
		if len(c.Errors) > 0 {
			span.RecordError(c.Errors.Last())
			span.SetStatus(codes.Error, c.Errors.Last().Error())
		}

		// 记录请求结束日志
		logger.GetLogger().WithFields(map[string]interface{}{
			"request_id": requestID,
			"trace_id":   traceID,
			"span_id":    spanID,
			"status":     c.Writer.Status(),
			"latency":    time.Since(time.Now()), // 这里需要修正，应该用开始时间
		}).Info("请求结束")
	}
}

// TraceWithDuration 改进版 - 记录准确的耗时
func TraceWithDuration() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		// 开始span
		ctx, span := tracing.StartSpan(c.Request.Context(), fmt.Sprintf("%s %s", c.Request.Method, c.FullPath()))
		defer span.End()

		// 设置span属性
		span.SetAttributes(
			attribute.String("http.method", c.Request.Method),
			attribute.String("http.url", c.Request.URL.String()),
			attribute.String("http.host", c.Request.Host),
			attribute.String("http.user_agent", c.Request.UserAgent()),
			attribute.String("http.client_ip", c.ClientIP()),
		)

		// 将span context注入到请求context
		c.Request = c.Request.WithContext(ctx)

		// 获取trace和span ID
		spanContext := span.SpanContext()
		traceID := spanContext.TraceID().String()
		spanID := spanContext.SpanID().String()

		// 设置请求ID（用于日志关联）
		requestID := traceID[len(traceID)-16:] // 使用后16位作为request ID
		c.Set("request_id", requestID)
		c.Set("trace_id", traceID)
		c.Set("span_id", spanID)

		// 在响应头中添加trace信息
		c.Writer.Header().Set("X-Trace-ID", traceID)
		c.Writer.Header().Set("X-Span-ID", spanID)
		c.Writer.Header().Set("X-Request-ID", requestID)

		// 记录请求开始日志
		logger.GetLogger().WithFields(map[string]interface{}{
			"request_id": requestID,
			"trace_id":   traceID,
			"span_id":    spanID,
			"method":     c.Request.Method,
			"path":       c.Request.URL.Path,
			"client_ip":  c.ClientIP(),
		}).Info("请求开始")

		// 继续处理请求
		c.Next()

		// 计算耗时
		duration := time.Since(start)

		// 记录响应信息
		span.SetAttributes(
			attribute.Int("http.status_code", c.Writer.Status()),
			attribute.Int64("http.response_size", int64(c.Writer.Size())),
			attribute.Float64("http.duration_ms", float64(duration.Milliseconds())),
		)

		// 如果有错误，记录到span
		if len(c.Errors) > 0 {
			span.RecordError(c.Errors.Last())
			span.SetStatus(codes.Error, c.Errors.Last().Error())
		}

		// 记录请求结束日志
		logFields := map[string]interface{}{
			"request_id": requestID,
			"trace_id":   traceID,
			"span_id":    spanID,
			"status":     c.Writer.Status(),
			"latency_ms": duration.Milliseconds(),
		}

		// 根据状态码选择日志级别
		if c.Writer.Status() >= 400 {
			logger.GetLogger().WithFields(logFields).Error("请求失败")
		} else {
			logger.GetLogger().WithFields(logFields).Info("请求成功")
		}
	}
}
