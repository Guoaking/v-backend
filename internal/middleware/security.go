package middleware

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"kyc-service/internal/models"
	"kyc-service/pkg/crypto"
	"kyc-service/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// SecurityMiddleware 安全中间件
type SecurityMiddleware struct {
	encryptor *crypto.Encryptor
}

func NewSecurityMiddleware(encryptionKey string) (*SecurityMiddleware, error) {
	encryptor, err := crypto.NewEncryptor(encryptionKey)
	if err != nil {
		return nil, err
	}

	return &SecurityMiddleware{
		encryptor: encryptor,
	}, nil
}

// DataMasking 数据脱敏中间件
func (s *SecurityMiddleware) DataMasking() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 生成请求ID用于追踪
		requestID := uuid.New().String()
		c.Set("request_id", requestID)

		// 继续处理请求
		c.Next()

		// 响应脱敏处理
		s.maskResponse(c)
	}
}

// AuditLogging 审计日志中间件
func (s *SecurityMiddleware) AuditLogging() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		// 记录请求前的状态
		requestID := c.GetString("request_id")
		if requestID == "" {
			requestID = uuid.New().String()
			c.Set("request_id", requestID)
		}

		// 获取用户信息
		userID, _ := c.Get("user_id")
		// clientID, _ := c.Get("client_id")  // 暂时不使用，避免编译错误

		// 脱敏处理请求参数
		maskedParams := s.maskRequestParams(c)

		// 记录敏感数据访问
		if isSensitiveEndpoint(c.Request.URL.Path) {
			userIDStr := fmt.Sprintf("%v", userID)
			RecordSensitiveDataAccess("audit_log", userIDStr, true, c.Request.URL.Path)
		}

		// 继续处理
		c.Next()

		// 记录审计日志
		duration := time.Since(start)
		statusCode := c.Writer.Status()

		auditLog := &models.AuditLog{
			RequestID: requestID,
			UserID:    fmt.Sprintf("%v", userID),
			Action:    fmt.Sprintf("%s %s", c.Request.Method, c.Request.URL.Path),
			Resource:  c.Request.URL.Path,
			IP:        c.ClientIP(),
			UserAgent: s.maskUserAgent(c.Request.UserAgent()),
			Status:    fmt.Sprintf("%d", statusCode),
			Message:   fmt.Sprintf("耗时: %v, 参数: %s", duration, maskedParams),
		}

		// 记录业务操作
		RecordBusinessOperation("audit_log", statusCode < 400, duration, "")

		// 异步保存审计日志
		go func() {
			if err := s.saveAuditLog(auditLog); err != nil {
				logger.GetLogger().WithError(err).Error("审计日志保存失败")
			}
		}()
	}
}

// PIIProtection PII保护中间件
func (s *SecurityMiddleware) PIIProtection() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 检查请求中是否包含敏感信息
		if err := s.checkSensitiveData(c); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请求包含非法敏感信息"})
			c.Abort()
			return
		}

		c.Next()
	}
}

// maskRequestParams 脱敏请求参数
func (s *SecurityMiddleware) maskRequestParams(c *gin.Context) string {
	params := make(map[string]interface{})

	// 处理查询参数
	for key, values := range c.Request.URL.Query() {
		if s.isSensitiveField(key) {
			params[key] = s.maskSensitiveValue(strings.Join(values, ","))
		} else {
			params[key] = values
		}
	}

	// 处理表单参数
	if c.Request.Method == "POST" || c.Request.Method == "PUT" {
		c.Request.ParseForm()
		for key, values := range c.Request.PostForm {
			if s.isSensitiveField(key) {
				params[key] = s.maskSensitiveValue(strings.Join(values, ","))
			} else {
				params[key] = values
			}
		}
	}

	// 转换为JSON字符串
	jsonData, _ := json.Marshal(params)
	return string(jsonData)
}

// maskResponse 脱敏响应数据
func (s *SecurityMiddleware) maskResponse(c *gin.Context) {
	// 这里可以实现响应数据的脱敏处理
	// 由于Gin的限制，需要在响应写入前进行拦截
	// 简化处理，实际应该使用自定义的ResponseWriter
}

// maskUserAgent 脱敏User-Agent
func (s *SecurityMiddleware) maskUserAgent(userAgent string) string {
	// 移除可能包含敏感信息的详细版本号
	if len(userAgent) > 100 {
		return userAgent[:100] + "..."
	}
	return userAgent
}

// isSensitiveField 判断是否为敏感字段
func (s *SecurityMiddleware) isSensitiveField(field string) bool {
	sensitiveFields := []string{
		"idcard", "id_card", "idnumber", "id_number",
		"phone", "mobile", "cellphone",
		"email", "mail",
		"password", "pwd", "pass",
		"bankcard", "bank_card", "creditcard", "credit_card",
		"passport", "driverlicense", "driver_license",
	}

	fieldLower := strings.ToLower(field)
	for _, sensitive := range sensitiveFields {
		if strings.Contains(fieldLower, sensitive) {
			return true
		}
	}
	return false
}

// maskSensitiveValue 脱敏敏感值
func (s *SecurityMiddleware) maskSensitiveValue(value string) string {
	if value == "" {
		return ""
	}

	// 身份证号脱敏
	if len(value) == 18 && s.isIDCard(value) {
		return logger.DesensitizeIDCard(value)
	}

	// 手机号脱敏
	if len(value) == 11 && s.isPhoneNumber(value) {
		return logger.DesensitizePhone(value)
	}

	// 姓名脱敏
	if len(value) >= 2 && s.isChineseName(value) {
		return logger.DesensitizeName(value)
	}

	// 邮箱脱敏
	if strings.Contains(value, "@") {
		parts := strings.Split(value, "@")
		if len(parts) == 2 {
			local := parts[0]
			if len(local) > 3 {
				return local[:3] + "***@" + parts[1]
			}
			return "***@" + parts[1]
		}
	}

	// 通用脱敏：保留前2后2，中间用*代替
	if len(value) > 4 {
		return value[:2] + "***" + value[len(value)-2:]
	}

	return "***"
}

// isIDCard 判断是否为身份证号
func (s *SecurityMiddleware) isIDCard(value string) bool {
	if len(value) != 18 {
		return false
	}

	// 检查前17位是否为数字
	for i := 0; i < 17; i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}

	// 检查最后一位（可以是数字或X）
	lastChar := value[17]
	return (lastChar >= '0' && lastChar <= '9') || lastChar == 'X' || lastChar == 'x'
}

// isPhoneNumber 判断是否为手机号
func (s *SecurityMiddleware) isPhoneNumber(value string) bool {
	if len(value) != 11 {
		return false
	}

	// 检查是否都是数字
	for i := 0; i < 11; i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}

	// 检查是否为有效的手机号段（简化检查）
	return value[0] == '1' && (value[1] >= '3' && value[1] <= '9')
}

// isChineseName 判断是否为中文姓名
func (s *SecurityMiddleware) isChineseName(value string) bool {
	// 检查是否包含中文字符
	hasChinese := false
	for _, r := range value {
		if r >= 0x4E00 && r <= 0x9FFF {
			hasChinese = true
			break
		}
	}
	return hasChinese
}

// checkSensitiveData 检查敏感数据
func (s *SecurityMiddleware) checkSensitiveData(c *gin.Context) error {
	// 检查请求体中是否包含明文敏感信息
	// 这里可以实现更复杂的检测逻辑

	// 检查身份证号
	body, _ := c.GetRawData()
	if s.containsPlainIDCard(string(body)) {
		return fmt.Errorf("请求包含明文身份证号")
	}

	// 检查银行卡号
	if s.containsPlainBankCard(string(body)) {
		return fmt.Errorf("请求包含明文银行卡号")
	}

	return nil
}

// containsPlainIDCard 检查是否包含明文身份证号
func (s *SecurityMiddleware) containsPlainIDCard(text string) bool {
	// 简单的正则表达式匹配身份证号模式
	// 实际应该使用更复杂的正则表达式
	if len(text) < 18 {
		return false
	}

	// 查找连续的18位数字或17位数字+X
	for i := 0; i <= len(text)-18; i++ {
		segment := text[i : i+18]
		if s.isIDCard(segment) {
			return true
		}
	}

	return false
}

// containsPlainBankCard 检查是否包含明文银行卡号
func (s *SecurityMiddleware) containsPlainBankCard(text string) bool {
	// 银行卡号通常是16-19位数字
	// 简化检查，实际应该使用Luhn算法验证
	if len(text) < 16 {
		return false
	}

	// 查找连续的16-19位数字
	for i := 0; i <= len(text)-16; i++ {
		end := i + 16
		if end > len(text) {
			end = len(text)
		}

		segment := text[i:end]
		if len(segment) >= 16 && len(segment) <= 19 {
			allDigits := true
			for _, char := range segment {
				if char < '0' || char > '9' {
					allDigits = false
					break
				}
			}
			if allDigits {
				return true
			}
		}
	}

	return false
}

// saveAuditLog 保存审计日志
func (s *SecurityMiddleware) saveAuditLog(log *models.AuditLog) error {
	// 这里应该实现审计日志的保存逻辑
	// 可以保存到数据库、消息队列或专门的审计系统

	logger.GetLogger().WithFields(map[string]interface{}{
		"request_id": log.RequestID,
		"user_id":    log.UserID,
		"action":     log.Action,
		"resource":   log.Resource,
		"ip":         log.IP,
		"status":     log.Status,
		"message":    log.Message,
	}).Info("审计日志")

	return nil
}

// EncryptSensitiveData 加密敏感数据
func (s *SecurityMiddleware) EncryptSensitiveData(data string) (string, error) {
	return s.encryptor.Encrypt(data)
}

// DecryptSensitiveData 解密敏感数据
func (s *SecurityMiddleware) DecryptSensitiveData(encryptedData string) (string, error) {
	return s.encryptor.Decrypt(encryptedData)
}

// HashIDCard 身份证号哈希（用于索引）
func (s *SecurityMiddleware) HashIDCard(id string) string {
	return crypto.HashIDCard(id)
}
