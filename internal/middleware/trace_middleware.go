package middleware

import (
	"fmt"

	"kyc-service/pkg/tracing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// TraceMiddleware 链路追踪中间件
func TraceMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 生成请求ID
		requestID := uuid.New().String()

		// 开始span
		ctx, span := tracing.StartSpan(c.Request.Context(), fmt.Sprintf("%s %s", c.Request.Method, c.Request.URL.Path))
		defer span.End()

		// 获取trace和span ID
		spanContext := span.SpanContext()
		traceID := spanContext.TraceID().String()
		spanID := spanContext.SpanID().String()

		// 设置到context和响应头
		c.Set("request_id", requestID)
		c.Set("trace_id", traceID)
		c.Set("span_id", spanID)

		c.Writer.Header().Set("X-Trace-ID", traceID)
		c.Writer.Header().Set("X-Span-ID", spanID)
		c.Writer.Header().Set("X-Request-ID", requestID)

		// 设置span属性
		span.SetAttributes(
			attribute.String("http.method", c.Request.Method),
			attribute.String("http.url", c.Request.URL.String()),
			attribute.String("http.host", c.Request.Host),
			attribute.String("http.user_agent", c.Request.UserAgent()),
			attribute.String("http.client_ip", c.ClientIP()),
			attribute.String("request_id", requestID),
		)

		// 记录请求开始
		//logger.GetLogger().WithFields(map[string]interface{}{
		//	"request_id": requestID,
		//	"trace_id":   traceID,
		//	"span_id":    spanID,
		//	"method":     c.Request.Method,
		//	"path":       c.Request.URL.Path,
		//	"client_ip":  c.ClientIP(),
		//}).Info("[TRACE] 请求开始")

		// 继续处理请求
		c.Request = c.Request.WithContext(ctx)
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

		// 记录请求结束
		status := c.Writer.Status()
		if status >= 400 {
			//logger.GetLogger().WithFields(map[string]interface{}{
			//	"request_id": requestID,
			//	"trace_id":   traceID,
			//	"span_id":    spanID,
			//	"status":     status,
			//}).Error("[TRACE] 请求失败")
		} else {

			//logger.GetLogger().WithFields(map[string]interface{}{
			//	"request_id": requestID,
			//	"trace_id":   traceID,
			//	"span_id":    spanID,
			//	"status":     status,
			//}).Info("[TRACE] 请求成功")
		}
	}
}
