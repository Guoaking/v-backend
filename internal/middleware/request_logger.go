package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"time"

	"kyc-service/internal/models"
	"kyc-service/internal/service"
	"kyc-service/pkg/logger"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
)

// RequestLogger 请求日志记录中间件
func RequestLogger(service *service.KYCService) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 记录开始时间
		start := time.Now()

		// 获取请求路径
		path := c.Request.URL.Path

		// 跳过健康检查和metrics端点
		if strings.Contains(path, "/health") || strings.Contains(path, "/metrics") {
			c.Next()
			return
		}

		c.Next()

		// 计算延迟
		latency := time.Since(start)
		statusCode := c.Writer.Status()
		clientIP := c.ClientIP()
		method := c.Request.Method

		// 异步记录日志，避免阻塞响应
		go func() {
			// 获取用户信息（如果已认证）
			var userID *string
			var orgID string
			var apiKeyID *string
			var apiKeyOwnerID *string
			var apiKeyName string

			if uid, exists := c.Get("userID"); exists {
				if uidStr, ok := uid.(string); ok {
					userID = &uidStr
				}
			}
			if oid := c.GetString("orgID"); oid != "" {
				orgID = oid
			}

			if akid, exists := c.Get("apiKeyID"); exists {
				if akidStr, ok := akid.(string); ok {
					apiKeyID = &akidStr
				}
			}

			if owner, exists := c.Get("apiKeyOwnerID"); exists {
				if ownerStr, ok := owner.(string); ok {
					apiKeyOwnerID = &ownerStr
				}
			}

			// 获取Key名称快照（可选）
			if apiKeyID != nil {
				if key, err := service.GetAPIKeyByID(*apiKeyID); err == nil {
					apiKeyName = key.Name
					if apiKeyOwnerID == nil {
						id := key.UserID
						apiKeyOwnerID = &id
					}
					if orgID == "" {
						orgID = key.OrgID
					}
				}
			}

			// 准备请求和响应体（注意脱敏）
			var requestBody datatypes.JSON
			var responseBody datatypes.JSON

			// 只对特定路径记录请求体（生产环境需要更严格的脱敏）
			if shouldLogRequestBody(path) {
				if rb, exists := c.Get("request_body"); exists {
					if rbBytes, err := json.Marshal(rb); err == nil {
						requestBody = rbBytes
					}
				}
			}

			// 记录响应体（需要脱敏处理）
			if shouldLogResponseBody(path, statusCode) {
				if respData, exists := c.Get("response_data"); exists {
					if respBytes, err := json.Marshal(sanitizeResponse(respData)); err == nil {
						responseBody = respBytes
					}
				}
			}

			// 创建日志记录
			log := &models.APIRequestLog{
				OrgID:         orgID,
				UserID:        userID,
				APIKeyID:      apiKeyID,
				APIKeyName:    apiKeyName,
				APIKeyOwnerID: apiKeyOwnerID,
				Method:        method,
				Path:          path,
				StatusCode:    statusCode,
				LatencyMs:     int(latency.Milliseconds()),
				ClientIP:      clientIP,
				RequestBody:   requestBody,
				ResponseBody:  responseBody,
				CreatedAt:     time.Now(),
			}

			// 保存到数据库
			if err := service.DB.Create(log).Error; err != nil {
				logger.GetLogger().WithError(err).Error("Failed to save API request log")
			}

			// 记录到标准日志（简化版）
			logger.GetLogger().WithFields(map[string]interface{}{
				"method":     method,
				"path":       path,
				"status":     statusCode,
				"latency_ms": latency.Milliseconds(),
				"client_ip":  clientIP,
				"user_id":    userID,
				"api_key_id": apiKeyID,
			}).Info("API Request")
		}()
	}
}

// shouldLogRequestBody 判断是否应该记录请求体
func shouldLogRequestBody(path string) bool {
	// 只对非敏感接口记录请求体
	sensitivePaths := []string{
		"/kyc/ocr",
		"/kyc/face/verify",
		"/kyc/liveness/ws",
		"/kyc/verify",
		"/auth/login",
		"/auth/register",
	}

	for _, sensitive := range sensitivePaths {
		if strings.Contains(path, sensitive) {
			return true
		}
	}

	// 记录管理类接口的请求体
	if strings.Contains(path, "/admin/") || strings.Contains(path, "/console/") {
		return true
	}

	return false
}

// shouldLogResponseBody 判断是否应该记录响应体
func shouldLogResponseBody(path string, statusCode int) bool {
	// 只记录错误响应和管理类接口的响应
	if statusCode >= 400 {
		return true
	}

	if strings.Contains(path, "/admin/") || strings.Contains(path, "/console/") {
		return true
	}

	return false
}

// sanitizeResponse 对响应数据进行脱敏处理
func sanitizeResponse(data interface{}) interface{} {
	// 这里实现具体的脱敏逻辑
	// 例如：移除敏感字段、截断长文本等

	// 简单的脱敏示例：如果是map，移除敏感字段
	if m, ok := data.(map[string]interface{}); ok {
		sanitized := make(map[string]interface{})
		for k, v := range m {
			// 跳过敏感字段
			if isSensitiveField(k) {
				sanitized[k] = "[REDACTED]"
			} else {
				sanitized[k] = v
			}
		}
		return sanitized
	}

	return data
}

// isSensitiveField 判断是否为敏感字段
func isSensitiveField(field string) bool {
	sensitiveFields := []string{
		"password", "token", "secret", "key",
		"idcard", "id_card", "id_number",
		"phone", "mobile", "email",
		"image", "photo", "face_image", "idcard_image",
		"base64", "data",
	}

	fieldLower := strings.ToLower(field)
	for _, sensitive := range sensitiveFields {
		if strings.Contains(fieldLower, sensitive) {
			return true
		}
	}

	return false
}

// RequestBodyLogger 请求体记录中间件（需要在RequestLogger之前使用）
func RequestBodyLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		ct := c.Request.Header.Get("Content-Type")
		if shouldLogRequestBody(c.Request.URL.Path) {
			if strings.Contains(strings.ToLower(ct), "multipart/form-data") {
				c.Set("request_body", map[string]interface{}{"binary": true, "summary": "[Multipart Data]"})
			} else if c.Request.Body != nil {
				buf, _ := io.ReadAll(c.Request.Body)
				parsed := tryParseJSONWithSanitize(buf)
				c.Set("request_body", parsed)
				c.Request.Body = io.NopCloser(bytes.NewBuffer(buf))
			}
		}
		c.Next()
	}
}

// ResponseCapture 记录响应体中间件（用于辅助 RequestLogger）
func ResponseCapture() gin.HandlerFunc {
	return func(c *gin.Context) {
		var buf bytes.Buffer
		rw := &captureWriter{ResponseWriter: c.Writer, buf: &buf}
		c.Writer = rw
		c.Next()
		if shouldLogResponseBody(c.Request.URL.Path, c.Writer.Status()) {
			c.Set("response_data", tryParseJSONWithSanitize(buf.Bytes()))
		}
	}
}

type captureWriter struct {
	gin.ResponseWriter
	buf *bytes.Buffer
}

func (w *captureWriter) Write(b []byte) (int, error) {
	w.buf.Write(b)
	return w.ResponseWriter.Write(b)
}

func tryParseJSONWithSanitize(b []byte) interface{} {
	var v interface{}
	if len(b) == 0 {
		return map[string]interface{}{}
	}
	if len(b) > 2048 {
		b = b[:2048]
	}
	if json.Unmarshal(b, &v) == nil {
		return sanitizeJSONValue(v)
	}
	return string(b)
}

func sanitizeJSONValue(v interface{}) interface{} {
	switch vv := v.(type) {
	case map[string]interface{}:
		m := make(map[string]interface{}, len(vv))
		for k, val := range vv {
			m[k] = sanitizeJSONValue(val)
		}
		return m
	case []interface{}:
		arr := make([]interface{}, len(vv))
		for i := range vv {
			arr[i] = sanitizeJSONValue(vv[i])
		}
		return arr
	case string:
		s := strings.TrimSpace(vv)
		if len(s) > 1024 && isBase64Like(s) {
			return "[Binary Data]"
		}
		return vv
	default:
		return v
	}
}

func isBase64Like(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '+' || c == '/' || c == '=' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
