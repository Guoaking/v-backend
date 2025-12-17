package api

import (
	"kyc-service/internal/models"
	"kyc-service/internal/service"
	"kyc-service/pkg/crypto"
	"kyc-service/pkg/logger"
	"kyc-service/pkg/utils"
	"strings"

	"github.com/gin-gonic/gin"
)

// APIKeyHandler API密钥管理处理器
type APIKeyHandler struct {
	service *service.KYCService
}

// NewAPIKeyHandler 创建API密钥管理处理器
func NewAPIKeyHandler(svc *service.KYCService) *APIKeyHandler {
	return &APIKeyHandler{service: svc}
}

// CreateAPIKeyRequest 创建API密钥请求
type CreateAPIKeyRequest struct {
	Name        string   `json:"name" binding:"required"`
	Scopes      []string `json:"scopes" binding:"required"`
	IPWhitelist []string `json:"ip_whitelist,omitempty"` // IP白名单，CIDR格式
}

// APIKeyResponse API密钥响应
type APIKeyResponse struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Secret      string   `json:"secret,omitempty"` // 只在创建时返回
	Prefix      string   `json:"prefix,omitempty"`
	Scopes      []string `json:"scopes"`
	Status      string   `json:"status"`
	IPWhitelist []string `json:"ip_whitelist,omitempty"` // IP白名单
	CreatedAt   string   `json:"created_at"`
}

// @Summary 获取用户API密钥列表
// @Description 获取当前用户的所有API密钥
// @Tags Credentials
// @Accept json
// @Produce json
// @Success 200 {object} SuccessResponse
// @Failure 401 {object} ErrorResponse
// @Router /api/v1/keys [get]
func (h *APIKeyHandler) GetAPIKeys(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		JSONError(c, CodeUnauthorized, "Unauthorized")
		return
	}

	keys, err := h.service.GetAPIKeysByUserID(userID.(string))
	if err != nil {
		logger.GetLogger().WithError(err).Error("Failed to get API keys")
		JSONError(c, CodeDatabaseError, "Failed to get API keys")
		return
	}

	response := make([]APIKeyResponse, len(keys))
	for i, key := range keys {
		response[i] = APIKeyResponse{
			ID:          key.ID,
			Name:        key.Name,
			Prefix:      key.Prefix,
			Scopes:      utils.ParseJSONStringArray(key.Scopes),
			Status:      key.Status,
			IPWhitelist: key.IPWhitelist,
			CreatedAt:   key.CreatedAt.Format("2006-01-02 15:04:05"),
		}
	}

	JSONSuccess(c, response)
}

// @Summary 创建API密钥
// @Description 创建新的API密钥
// @Tags Credentials
// @Accept json
// @Produce json
// @Param key body CreateAPIKeyRequest true "API密钥信息"
// @Success 200 {object} SuccessResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Router /api/v1/keys [post]
func (h *APIKeyHandler) CreateAPIKey(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		JSONError(c, CodeUnauthorized, "Unauthorized")
		return
	}

	var req CreateAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		JSONError(c, CodeInvalidParameter, "Invalid request format")
		return
	}

	// 生成API密钥
	apiKey := utils.GenerateAPIKey()
	secret := utils.GenerateAPISecret()

	// 哈希密钥用于存储
	secretHash, _ := crypto.HashString(secret)

	// 计算前缀
	prefix := ""
	if idx := strings.Index(secret, "_"); idx != -1 {
		if j := strings.Index(secret[idx+1:], "_"); j != -1 {
			prefix = secret[:idx+1+j+1]
			if k := strings.LastIndex(prefix, "_"); k != -1 {
				prefix = prefix[:k]
			}
		}
	}

	key := &models.APIKey{
		ID:          apiKey,
		UserID:      userID.(string),
		Name:        req.Name,
		SecretHash:  secretHash,
		Prefix:      prefix,
		Scopes:      utils.ToJSONString(req.Scopes),
		Status:      "active",
		IPWhitelist: req.IPWhitelist,
	}

	if err := h.service.CreateAPIKey(key); err != nil {
		logger.GetLogger().WithError(err).Error("Failed to create API key")
		JSONError(c, CodeDatabaseError, "Failed to create API key")
		return
	}

	// 记录审计日志
	h.recordAuditLog(c, userID.(string), "create_api_key", "success", "API key created")

	JSONSuccess(c, APIKeyResponse{
		ID:          key.ID,
		Name:        key.Name,
		Secret:      secret,
		Prefix:      prefix,
		Scopes:      req.Scopes,
		Status:      key.Status,
		IPWhitelist: key.IPWhitelist,
		CreatedAt:   key.CreatedAt.Format("2006-01-02 15:04:05"),
	})
}

// @Summary 删除API密钥
// @Description 删除指定的API密钥
// @Tags Credentials
// @Accept json
// @Produce json
// @Param id path string true "API密钥ID"
// @Success 200 {object} SuccessResponse
// @Failure 401 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Router /api/v1/keys/{id} [delete]
func (h *APIKeyHandler) DeleteAPIKey(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		JSONError(c, CodeUnauthorized, "Unauthorized")
		return
	}

	keyID := c.Param("id")

	// 验证密钥属于当前用户
	key, err := h.service.GetAPIKeyByID(keyID)
	if err != nil || key.UserID != userID.(string) {
		JSONError(c, CodeNotFound, "API key not found")
		return
	}

	// 更新密钥状态为已撤销
	key.Status = "revoked"
	if err := h.service.UpdateAPIKey(key); err != nil {
		logger.GetLogger().WithError(err).Error("Failed to revoke API key")
		JSONError(c, CodeDatabaseError, "Failed to revoke API key")
		return
	}

	// 记录审计日志
	h.recordAuditLog(c, userID.(string), "revoke_api_key", "success", "API key revoked")

	JSONSuccess(c, map[string]string{"message": "API key revoked successfully"})
}

// recordAuditLog 记录审计日志
func (h *APIKeyHandler) recordAuditLog(c *gin.Context, userID, action, status, message string) {
	go func() {
		auditLog := &models.AuditLog{
			RequestID: c.GetString("requestID"),
			UserID:    userID,
			Action:    action,
			Resource:  "api_key",
			IP:        c.ClientIP(),
			UserAgent: c.GetHeader("User-Agent"),
			Status:    status,
			Message:   message,
		}
		h.service.CreateAuditLog(auditLog)
	}()
}
