package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"kyc-service/internal/middleware"
	"kyc-service/internal/models"
	"kyc-service/internal/service"
	"kyc-service/pkg/crypto"
	"kyc-service/pkg/logger"
	"kyc-service/pkg/metrics"
	"kyc-service/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

// ConsoleHandler 控制台处理器
type ConsoleHandler struct {
	service *service.KYCService
}

// NewConsoleHandler 创建控制台处理器
func NewConsoleHandler(svc *service.KYCService) *ConsoleHandler {
	return &ConsoleHandler{service: svc}
}

// ConsoleCreateAPIKeyRequest 创建API密钥请求
type ConsoleCreateAPIKeyRequest struct {
	Name   string   `json:"name" binding:"required,min=1,max=100"`
	Scopes []string `json:"scopes" binding:"required,min=1"`
}

// ConsoleAPIKeyResponse API密钥响应
type ConsoleAPIKeyResponse struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Secret    string     `json:"secret,omitempty"` // 只在创建时返回
	Masked    string     `json:"masked_secret,omitempty"`
	Prefix    string     `json:"prefix,omitempty"`
	Scopes    []string   `json:"scopes"`
	Status    string     `json:"status"`
	LastUsed  *time.Time `json:"last_used_at,omitempty"`
	LastIP    string     `json:"last_ip,omitempty"`
	CreatedBy struct {
		UserID string `json:"user_id"`
		Name   string `json:"name"`
		Avatar string `json:"avatar"`
	} `json:"created_by,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	Stats     *struct {
		TotalRequests24h int        `json:"total_requests_24h"`
		SuccessRate24h   float64    `json:"success_rate_24h"`
		LastErrorMessage string     `json:"last_error_message,omitempty"`
		LastErrorAt      *time.Time `json:"last_error_at,omitempty"`
	} `json:"stats,omitempty"`
}

type UpdateKeyScopesRequest struct {
	Scopes []string `json:"scopes" binding:"required,min=1"`
}

// UserMeResponse 用户信息响应
type UserMeResponse struct {
	ID              string                  `json:"id"`
	Email           string                  `json:"email"`
	FullName        string                  `json:"full_name"`
	Name            string                  `json:"name"`
	AvatarURL       string                  `json:"avatar,omitempty"`
	Role            string                  `json:"role"`
	OrgRole         string                  `json:"org_role"`
	CurrentOrgID    string                  `json:"currentOrgId"`
	Permissions     []string                `json:"permissions"`
	Company         string                  `json:"company"`
	APIKeys         []ConsoleAPIKeyResponse `json:"apiKeys"`
	IsPlatformAdmin bool                    `json:"is_platform_admin"`
}

// ConsoleUpdateUserRequest 更新用户请求
type ConsoleUpdateUserRequest struct {
	FullName  string `json:"name,omitempty"`
	AvatarURL string `json:"avatar,omitempty"`
	Company   string `json:"company,omitempty"`
}

// GetCurrentUser 获取当前用户信息
// @Summary 获取当前用户信息
// @Description 获取当前登录用户的详细信息和API密钥列表
// @Tags Console
// @Accept json
// @Produce json
// @Success 200 {object} SuccessResponse
// @Failure 401 {object} ErrorResponse
// @Router /api/v1/users/me [get]
func (h *ConsoleHandler) GetCurrentUser(c *gin.Context) {
	start := time.Now()

	// 从JWT获取用户信息
	userClaims, exists := c.Get("user")
	if !exists {
		metrics.RecordBusinessOperation(c.Request.Context(), "get_user_profile", false, time.Since(start), "unauthorized")
		JSONError(c, CodeUnauthorized, "未授权访问")
		return
	}

	claims := userClaims.(jwt.MapClaims)
	userID := claims["user_id"].(string)

	// 查询用户信息
	var user models.User
	if err := h.service.DB.Preload("Organization").First(&user, "id = ?", userID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			metrics.RecordBusinessOperation(c.Request.Context(), "get_user_profile", false, time.Since(start), "user_not_found")
			JSONError(c, CodeNotFound, "用户不存在")
			return
		}
		logger.GetLogger().WithError(err).Error("查询用户失败")
		metrics.RecordBusinessOperation(c.Request.Context(), "get_user_profile", false, time.Since(start), "database_error")
		JSONError(c, CodeDatabaseError, "系统错误")
		return
	}

	currentOrg := user.OrgID
	if user.CurrentOrgID != "" {
		currentOrg = user.CurrentOrgID
	}
	// 从上下文读取权限列表（JWT 中间件已注入）
	var perms []string
	if v, ok := c.Get("permissions"); ok {
		perms, _ = v.([]string)
	}

	// 查询组织信息
	var org models.Organization
	if err := h.service.DB.First(&org, "id = ?", user.OrgID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			metrics.RecordBusinessOperation(c.Request.Context(), "get_org_info", false, time.Since(start), "org_not_found")
			JSONError(c, CodeNotFound, "组织不存在")
			return
		}
		logger.GetLogger().WithError(err).Error("查询组织失败")
		metrics.RecordBusinessOperation(c.Request.Context(), "get_org_info", false, time.Since(start), "database_error")
		JSONError(c, CodeDatabaseError, "系统错误")
		return
	}

	var apiKeys []models.APIKey
	if err := h.service.DB.Where("user_id = ? AND status = ?", userID, "active").Order("created_at DESC").Find(&apiKeys).Error; err != nil {
		logger.GetLogger().WithError(err).Error("查询API密钥失败")
		apiKeys = []models.APIKey{}
	}

	// 构建API密钥响应
	apiKeyResponses := make([]ConsoleAPIKeyResponse, len(apiKeys))
	for i, key := range apiKeys {
		scopes := []string{""}
		if key.Scopes != "" {
			// 解析scopes JSON
			scopes = strings.Split(strings.Trim(key.Scopes, "[]\""), ",")
		}

		apiKeyResponses[i] = ConsoleAPIKeyResponse{
			ID:        key.ID,
			Name:      key.Name,
			Secret:    key.SecretHash,
			Scopes:    scopes,
			Status:    key.Status,
			LastUsed:  key.LastUsedAt,
			CreatedAt: key.CreatedAt,
		}
	}

	response := UserMeResponse{
		ID:              user.ID,
		Email:           user.Email,
		FullName:        user.FullName,
		Name:            user.Name,
		AvatarURL:       user.AvatarURL,
		Role:            user.Role,
		OrgRole:         c.GetString("orgRole"),
		CurrentOrgID:    currentOrg,
		Permissions:     perms,
		Company:         org.Name,
		APIKeys:         apiKeyResponses,
		IsPlatformAdmin: user.IsPlatformAdmin,
	}

	// 记录业务操作成功
	metrics.RecordBusinessOperation(c.Request.Context(), "get_user_profile", true, time.Since(start), "")

	JSONSuccess(c, response)
}

// UpdateAPIKeyScopes 更新API Key的权限范围
func (h *ConsoleHandler) UpdateAPIKeyScopes(c *gin.Context) {
	// 权限由路由中间件校验 keys.write
	keyID := c.Param("id")
	if keyID == "" {
		JSONError(c, CodeInvalidParameter, "缺少Key ID")
		return
	}
	orgID := c.GetString("orgID")
	if orgID == "" {
		JSONError(c, CodeInvalidParameter, "组织信息错误")
		return
	}
	var req UpdateKeyScopesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		JSONError(c, CodeInvalidParameter, "参数验证失败")
		return
	}
	var key models.APIKey
	if err := h.service.DB.First(&key, "id = ?", keyID).Error; err != nil {
		JSONError(c, CodeNotFound, "API Key不存在")
		return
	}
	if key.OrgID != orgID {
		JSONError(c, CodeForbidden, "越权操作：Key不属于当前组织")
		return
	}
	// 去重并序列化为标准JSON
	uniq := make(map[string]struct{})
	cleaned := make([]string, 0, len(req.Scopes))
	for _, s := range req.Scopes {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := uniq[s]; !ok {
			uniq[s] = struct{}{}
			cleaned = append(cleaned, s)
		}
	}
	scopesBytes, _ := json.Marshal(cleaned)
	if err := h.service.DB.Model(&key).Update("scopes", string(scopesBytes)).Error; err != nil {
		JSONError(c, CodeDatabaseError, "更新失败")
		return
	}
	// 清理可能的缓存（最佳猜测键）
	if h.service.Redis != nil {
		ctx := context.Background()
		_ = h.service.Redis.Del(ctx, "api_key:id:"+key.ID).Err()
		if key.Prefix != "" {
			_ = h.service.Redis.Del(ctx, "api_key:prefix:"+key.Prefix).Err()
		}
		_ = h.service.Redis.Del(ctx, "api_key:scopes:"+key.ID).Err()
		_ = h.service.Redis.Del(ctx, "api_key:org:"+orgID+":id:"+key.ID).Err()
	}
	// 审计日志
	auditLog := &models.AuditLog{UserID: c.GetString("userID"), OrgID: orgID, Action: "key.update_scopes", IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), Status: "success", Message: fmt.Sprintf("Updated key %s scopes", key.ID)}
	h.recordAuditLog(auditLog)
	resp := ConsoleAPIKeyResponse{ID: key.ID, Name: key.Name, Prefix: key.Prefix, Scopes: cleaned, Status: key.Status, CreatedAt: key.CreatedAt}
	var creator models.User
	if err := h.service.DB.Select("id, full_name, avatar_url").Where("id = ?", key.CreatedByUserID).First(&creator).Error; err == nil {
		resp.CreatedBy.UserID = creator.ID
		resp.CreatedBy.Name = creator.FullName
		resp.CreatedBy.Avatar = creator.AvatarURL
	}
	JSONSuccess(c, resp)
}

// UpdateUserProfile 更新用户资料
// @Summary 更新用户资料
// @Description 更新当前用户的个人资料
// @Tags Console
// @Accept json
// @Produce json
// @Param request body ConsoleUpdateUserRequest true "更新用户资料请求"
// @Success 200 {object} SuccessResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Router /api/v1/users/me [put]
func (h *ConsoleHandler) UpdateUserProfile(c *gin.Context) {
	start := time.Now()

	// 获取用户信息
	userClaims, exists := c.Get("user")
	if !exists {
		middleware.RecordBusinessOperation("update_user_profile", false, time.Since(start), "unauthorized")
		JSONError(c, CodeUnauthorized, "未授权访问")
		return
	}

	claims := userClaims.(jwt.MapClaims)
	userID := claims["user_id"].(string)

	var req ConsoleUpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.RecordBusinessOperation("update_user_profile", false, time.Since(start), "invalid_request")
		JSONError(c, CodeInvalidParameter, "参数验证失败")
		return
	}

	// 查询用户信息
	var user models.User
	if err := h.service.DB.First(&user, "id = ?", userID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			middleware.RecordBusinessOperation("update_user_profile", false, time.Since(start), "user_not_found")
			JSONError(c, CodeNotFound, "用户不存在")
			return
		}
		logger.GetLogger().WithError(err).Error("查询用户失败")
		middleware.RecordBusinessOperation("update_user_profile", false, time.Since(start), "database_error")
		JSONError(c, CodeDatabaseError, "系统错误")
		return
	}

	// 更新用户信息
	updates := make(map[string]interface{})
	if req.FullName != "" {
		updates["full_name"] = req.FullName
		updates["name"] = req.FullName
	}
	if req.AvatarURL != "" {
		updates["avatar_url"] = req.AvatarURL
	}

	if len(updates) > 0 {
		if err := h.service.DB.Model(&user).Updates(updates).Error; err != nil {
			logger.GetLogger().WithError(err).Error("更新用户失败")
			middleware.RecordBusinessOperation("update_user_profile", false, time.Since(start), "update_failed")
			JSONError(c, CodeDatabaseError, "更新失败")
			return
		}
	}

	// 如果更新了公司名称，也更新组织名称
	if req.Company != "" && user.OrgID != "" {
		if err := h.service.DB.Model(&models.Organization{}).Where("id = ?", user.OrgID).Update("name", req.Company).Error; err != nil {
			logger.GetLogger().WithError(err).Error("更新组织名称失败")
			// 不返回错误，继续处理
		}
	}

	// 记录审计日志
	auditLog := &models.AuditLog{
		UserID:    userID,
		OrgID:     user.OrgID,
		Action:    "user_profile_updated",
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
		Status:    "success",
		Message:   fmt.Sprintf("User profile updated: %s", userID),
		Details:   fmt.Sprintf("Updates: %+v", updates),
	}
	h.recordAuditLog(auditLog)

	// 记录业务操作成功
	middleware.RecordBusinessOperation("update_user_profile", true, time.Since(start), "")

	JSONSuccess(c, gin.H{
		"success": true,
		"message": "资料更新成功",
	})
}

// CreateAPIKey 创建API密钥
// @Summary 创建API密钥
// @Description 创建新的API密钥
// @Tags Console
// @Accept json
// @Produce json
// @Param request body ConsoleCreateAPIKeyRequest true "创建API密钥请求"
// @Success 200 {object} SuccessResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Router /api/v1/keys [post]
func (h *ConsoleHandler) CreateAPIKey(c *gin.Context) {
	start := time.Now()

	// 获取用户信息
	userClaims, exists := c.Get("user")
	if !exists {
		middleware.RecordBusinessOperation("create_api_key", false, time.Since(start), "unauthorized")
		JSONError(c, CodeUnauthorized, "未授权访问")
		return
	}

	claims := userClaims.(jwt.MapClaims)
	userID := claims["user_id"].(string)
	orgID := claims["org_id"].(string)

	var req ConsoleCreateAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.RecordBusinessOperation("create_api_key", false, time.Since(start), "invalid_request")
		JSONError(c, CodeInvalidParameter, "参数验证失败")
		return
	}

	// 验证权限范围
	validScopes := map[string]bool{
		"ocr:read":      true,
		"liveness:read": true,
		"face:read":     true,
		"face:write":    true,
		"admin:read":    true,
		"admin:write":   true,
	}

	for _, scope := range req.Scopes {
		if !validScopes[scope] {
			middleware.RecordBusinessOperation("create_api_key", false, time.Since(start), "invalid_scope")
			JSONError(c, CodeInvalidParameter, fmt.Sprintf("无效的权限范围: %s", scope))
			return
		}
	}

	// 生成API密钥
	secretKey := h.generateAPIKeySecret()
	// 提取前缀
	prefix := ""
	if idx := strings.Index(secretKey, "_"); idx != -1 {
		// e.g. sk_live_xxx -> sk_live
		if j := strings.Index(secretKey[idx+1:], "_"); j != -1 {
			prefix = secretKey[:idx+1+j+1]
			// remove trailing underscore
			if k := strings.LastIndex(prefix, "_"); k != -1 {
				prefix = prefix[:k]
			}
		}
	}
	secretHash, err := crypto.HashString(secretKey)
	if err != nil {
		logger.GetLogger().WithError(err).Error("哈希API密钥失败")
		middleware.RecordBusinessOperation("create_api_key", false, time.Since(start), "hash_failed")
		JSONError(c, CodeInternalError, "密钥生成失败")
		return
	}

	// 创建API密钥
	encSecret := ""
	if h.service.Encryptor != nil {
		if es, err := h.service.Encryptor.Encrypt(secretKey); err == nil {
			encSecret = es
		} else {
			logger.GetLogger().WithError(err).Warn("加密API密钥失败，改为不保存明文密钥")
		}
	} else {
		logger.GetLogger().Warn("未配置加密密钥（EncryptionKey），不保存明文密钥副本")
	}
	apiKey := models.APIKey{
		ID:              utils.GenerateID(),
		UserID:          userID,
		OrgID:           orgID,
		Name:            req.Name,
		SecretHash:      secretHash,
		SecretEnc:       encSecret,
		Prefix:          prefix,
		Scopes:          func() string { b, _ := json.Marshal(req.Scopes); return string(b) }(),
		Status:          "active",
		CreatedByUserID: userID,
	}

	if err := h.service.DB.Create(&apiKey).Error; err != nil {
		logger.GetLogger().WithError(err).Error("创建API密钥失败")
		middleware.RecordBusinessOperation("create_api_key", false, time.Since(start), "database_error")
		JSONError(c, CodeDatabaseError, "密钥创建失败")
		return
	}

	// 记录审计日志
	auditLog := &models.AuditLog{
		UserID:    userID,
		OrgID:     orgID,
		Action:    "api_key_created",
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
		Status:    "success",
		Message:   fmt.Sprintf("API key created: %s with scopes: %v", req.Name, req.Scopes),
	}
	h.recordAuditLog(auditLog)

	// 记录业务操作成功
	middleware.RecordBusinessOperation("create_api_key", true, time.Since(start), "")

	// 仅创建时返回完整secret与masked
	masked := ""
	if len(secretKey) > 10 {
		masked = secretKey[:8] + "..." + secretKey[len(secretKey)-3:]
	}
	JSONSuccess(c, ConsoleAPIKeyResponse{
		ID:        apiKey.ID,
		Name:      apiKey.Name,
		Secret:    secretKey,
		Masked:    masked,
		Prefix:    prefix,
		Scopes:    req.Scopes,
		Status:    apiKey.Status,
		CreatedAt: apiKey.CreatedAt,
	})
}

// GetAPIKeySecret 返回明文密钥（加密存储，按权限解密）
func (h *ConsoleHandler) GetAPIKeySecret(c *gin.Context) {
	keyID := c.Param("id")
	if keyID == "" {
		JSONError(c, CodeInvalidParameter, "缺少Key ID")
		return
	}
	orgID := c.GetString("orgID")
	userID := c.GetString("userID")
	role := c.GetString("orgRole")
	var key models.APIKey
	if err := h.service.DB.First(&key, "id = ?", keyID).Error; err != nil {
		JSONError(c, CodeNotFound, "API Key不存在")
		return
	}
	if key.OrgID != orgID {
		JSONError(c, CodeForbidden, "越权操作")
		return
	}
	if role != "owner" && role != "admin" && key.CreatedByUserID != userID {
		JSONError(c, CodeForbidden, "权限不足")
		return
	}
	if key.SecretEnc == "" {
		JSONError(c, CodeNotFound, "密钥不可用")
		return
	}
	if h.service.Encryptor == nil {
		JSONError(c, CodeInternalError, "未配置加密密钥，无法取回明文")
		return
	}
	// 访问频率限制：默认为每分钟10次，playground上下文提高到每分钟100次
	maxPerMin := 10
	if c.Query("context") == "playground" || c.GetHeader("X-Playground") == "true" {
		maxPerMin = 100
	}
	if h.service.Redis != nil {
		ctx := context.Background()
		rlKey := fmt.Sprintf("rl:key_secret:%s:%s", userID, time.Now().Format("2006-01-02T15:04"))
		cur, _ := h.service.Redis.Incr(ctx, rlKey).Result()
		_ = h.service.Redis.Expire(ctx, rlKey, time.Minute).Err()
		if cur > int64(maxPerMin) {
			JSONError(c, CodeTooManyRequests, "请求过于频繁")
			return
		}
	}

	plain, err := h.service.Encryptor.Decrypt(key.SecretEnc)
	if err != nil {
		JSONError(c, CodeInternalError, "解密失败")
		return
	}
	masked := ""
	if len(plain) > 10 {
		masked = plain[:8] + "..." + plain[len(plain)-3:]
	}
	// 审计日志（支持 playground 上下文降噪）
	ctxTag := c.Query("context")
	msg := fmt.Sprintf("Show secret for %s", key.ID)
	if ctxTag == "playground" {
		msg = "Show secret (playground)"
	}
	audit := &models.AuditLog{UserID: userID, OrgID: orgID, Action: "key.show_secret", IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), Status: "success", Message: msg}
	_ = h.service.DB.Create(audit).Error
	JSONSuccess(c, gin.H{"id": key.ID, "name": key.Name, "prefix": key.Prefix, "secret": plain, "masked_secret": masked})
}

// ListAPIKeys 控制台获取API密钥列表（含统计）
func (h *ConsoleHandler) ListAPIKeys(c *gin.Context) {
	userClaims, exists := c.Get("user")
	if !exists {
		JSONError(c, CodeUnauthorized, "未授权访问")
		return
	}
	claims := userClaims.(jwt.MapClaims)
	userID := claims["user_id"].(string)
	orgID := c.GetString("orgID")
	role := c.GetString("orgRole")
	var keys []models.APIKey
	qb := h.service.DB.Model(&models.APIKey{}).Where("org_id = ? AND status <> ?", orgID, "revoked").Order("created_at DESC")
	if role != "owner" && role != "admin" {
		qb = qb.Where("created_by_user_id = ?", userID)
	}
	if err := qb.Find(&keys).Error; err != nil {
		JSONError(c, CodeDatabaseError, "查询失败")
		return
	}
	resp := make([]ConsoleAPIKeyResponse, len(keys))
	for i, k := range keys {
		resp[i] = ConsoleAPIKeyResponse{
			ID:        k.ID,
			Name:      k.Name,
			Prefix:    k.Prefix,
			Scopes:    utils.ParseJSONStringArray(k.Scopes),
			Status:    k.Status,
			LastUsed:  k.LastUsedAt,
			LastIP:    k.LastUsedIP,
			CreatedAt: k.CreatedAt,
			Stats: &struct {
				TotalRequests24h int        `json:"total_requests_24h"`
				SuccessRate24h   float64    `json:"success_rate_24h"`
				LastErrorMessage string     `json:"last_error_message,omitempty"`
				LastErrorAt      *time.Time `json:"last_error_at,omitempty"`
			}{TotalRequests24h: k.TotalRequests24h, SuccessRate24h: k.SuccessRate24h, LastErrorMessage: k.LastErrorMessage, LastErrorAt: k.LastErrorAt},
		}
		var creator models.User
		if err := h.service.DB.Select("id, full_name, avatar_url").Where("id = ?", k.CreatedByUserID).First(&creator).Error; err == nil {
			resp[i].CreatedBy.UserID = creator.ID
			resp[i].CreatedBy.Name = creator.FullName
			resp[i].CreatedBy.Avatar = creator.AvatarURL
		}
	}
	JSONSuccess(c, resp)
}

// RevokeAPIKey 撤销API密钥
// @Summary 撤销API密钥
// @Description 撤销指定的API密钥
// @Tags Console
// @Accept json
// @Produce json
// @Param id path string true "API密钥ID"
// @Success 200 {object} SuccessResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Router /api/v1/keys/{id} [delete]
func (h *ConsoleHandler) RevokeAPIKey(c *gin.Context) {
	start := time.Now()

	// 获取用户信息
	userClaims, exists := c.Get("user")
	if !exists {
		middleware.RecordBusinessOperation("revoke_api_key", false, time.Since(start), "unauthorized")
		JSONError(c, CodeUnauthorized, "未授权访问")
		return
	}

	claims := userClaims.(jwt.MapClaims)
	userID := claims["user_id"].(string)
	orgID := claims["org_id"].(string)

	keyID := c.Param("id")
	if keyID == "" {
		middleware.RecordBusinessOperation("revoke_api_key", false, time.Since(start), "invalid_id")
		JSONError(c, CodeInvalidParameter, "密钥ID不能为空")
		return
	}

	// 查找API密钥
	var apiKey models.APIKey
	if err := h.service.DB.Where("id = ? AND user_id = ? AND org_id = ?", keyID, userID, orgID).First(&apiKey).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			middleware.RecordBusinessOperation("revoke_api_key", false, time.Since(start), "key_not_found")
			JSONError(c, CodeNotFound, "API密钥不存在")
			return
		}
		logger.GetLogger().WithError(err).Error("查询API密钥失败")
		middleware.RecordBusinessOperation("revoke_api_key", false, time.Since(start), "database_error")
		JSONError(c, CodeDatabaseError, "系统错误")
		return
	}

	// 检查是否已经撤销
	if apiKey.Status == "revoked" {
		middleware.RecordBusinessOperation("revoke_api_key", false, time.Since(start), "already_revoked")
		JSONError(c, CodeConflict, "API密钥已撤销")
		return
	}

	// 撤销密钥
	apiKey.Status = "revoked"
	if err := h.service.DB.Save(&apiKey).Error; err != nil {
		logger.GetLogger().WithError(err).Error("撤销API密钥失败")
		middleware.RecordBusinessOperation("revoke_api_key", false, time.Since(start), "database_error")
		JSONError(c, CodeDatabaseError, "撤销失败")
		return
	}

	// 记录审计日志
	auditLog := &models.AuditLog{
		UserID:    userID,
		OrgID:     orgID,
		Action:    "api_key_revoked",
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
		Status:    "success",
		Message:   fmt.Sprintf("API key revoked: %s", apiKey.Name),
	}
	h.recordAuditLog(auditLog)

	// 记录业务操作成功
	middleware.RecordBusinessOperation("revoke_api_key", true, time.Since(start), "")

	JSONSuccess(c, gin.H{
		"message": "API密钥已撤销",
	})
}

// generateAPIKeySecret 生成API密钥
func (h *ConsoleHandler) generateAPIKeySecret() string {
	// 生成32字节的随机数据
	bytes := make([]byte, 32)
	rand.Read(bytes)

	// 编码为base64
	encoded := base64.URLEncoding.EncodeToString(bytes)

	// 添加前缀并返回
	return "sk_live_" + strings.ToLower(encoded)
}

// recordAuditLog 记录审计日志
func (h *ConsoleHandler) recordAuditLog(log *models.AuditLog) {
	if err := h.service.DB.Create(log).Error; err != nil {
		logger.GetLogger().WithError(err).Error("记录审计日志失败")
	}
}

// UsageItem 用量聚合项
type UsageItem struct {
	Date    string `json:"date"`
	Success int64  `json:"success"`
	Failed  int64  `json:"failed"`
}

// LogItem 日志项（不含敏感体）
type LogItem struct {
	ID           string `json:"id"`
	Method       string `json:"method"`
	Path         string `json:"path"`
	StatusCode   int    `json:"statusCode"`
	LatencyMs    int    `json:"latency"`
	ClientIP     string `json:"clientIp"`
	CreatedAt    string `json:"created_at"`
	TimeStamp    string `json:"timestamp"`
	RequestBody  string `json:"requestBody"`
	ResponseBody string `json:"responseBody"`
	KeyID        string `json:"key_id,omitempty"`
	KeyName      string `json:"key_name,omitempty"`
	KeyOwnerID   string `json:"keyOwner,omitempty"`
}

// GetUsage 聚合用量
func (h *ConsoleHandler) GetUsage(c *gin.Context) {
	orgID := c.GetString("orgID")
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")
	period := c.Query("period")
	if period != "" {
		now := time.Now()
		var dur time.Duration
		if period == "7d" {
			dur = 7 * 24 * time.Hour
		} else if period == "30d" {
			dur = 30 * 24 * time.Hour
		} else {
			dur = 7 * 24 * time.Hour
		}
		startDate = now.Add(-dur).Format("2006-01-02")
		endDate = now.Format("2006-01-02")
	}
	if startDate == "" || endDate == "" {
		JSONError(c, CodeInvalidParameter, "缺少日期范围")
		return
	}
	var rows []UsageItem
	// 按天聚合成功/失败
	if err := h.service.DB.Raw(`
        SELECT to_char(DATE(created_at), 'YYYY-MM-DD') AS date,
               SUM(CASE WHEN status_code < 400 THEN 1 ELSE 0 END) AS success,
               SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END) AS failed
        FROM api_request_logs
        WHERE org_id = ? AND created_at >= ?::date AND created_at <= ?::date + interval '1 day' - interval '1 second'
        GROUP BY DATE(created_at)
        ORDER BY DATE(created_at)
    `, orgID, startDate, endDate).Scan(&rows).Error; err != nil {
		JSONError(c, CodeDatabaseError, "查询失败")
		return
	}
	JSONSuccess(c, rows)
}

// GetLogs 分页查询日志
func (h *ConsoleHandler) GetLogs(c *gin.Context) {
	page := 1
	limit := 20
	if v := c.Query("page"); v != "" {
		fmt.Sscanf(v, "%d", &page)
	}
	if v := c.Query("limit"); v != "" {
		fmt.Sscanf(v, "%d", &limit)
	}
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit

	userID := c.GetString("userID")
	role := c.GetString("orgRole")
	orgID := c.GetString("orgID")
	var logs []models.APIRequestLog
	q := h.service.DB.Where("org_id = ?", orgID).Order("created_at DESC").Offset(offset).Limit(limit)
	// key_id 过滤与权限校验
	if kid := c.Query("key_id"); kid != "" {
		var key models.APIKey
		if err := h.service.DB.First(&key, "id = ?", kid).Error; err != nil {
			JSONError(c, CodeNotFound, "Key不存在")
			return
		}
		if role != "owner" && role != "admin" && key.CreatedByUserID != userID {
			JSONError(c, CodeForbidden, "无权查看该Key日志")
			return
		}
		q = q.Where("api_key_id = ?", kid)
	} else if role != "owner" && role != "admin" {
		// 非管理员仅查看自己Key的日志
		q = q.Where("api_key_owner_id = ?", userID)
	}
	if status := c.Query("status"); status != "" {
		if status == "success" {
			q = q.Where("status_code < 400")
		} else if status == "failed" {
			q = q.Where("status_code >= 400")
		}
	}
	if sc := c.Query("status_code"); sc != "" {
		if sc == "2xx" {
			q = q.Where("status_code BETWEEN 200 AND 299")
		} else if sc == "4xx" {
			q = q.Where("status_code BETWEEN 400 AND 499")
		} else if sc == "5xx" {
			q = q.Where("status_code BETWEEN 500 AND 599")
		} else {
			q = q.Where("status_code = ?", sc)
		}
	}
	if p := c.Query("path"); p != "" {
		q = q.Where("path LIKE ?", "%"+p+"%")
	}
	if m := c.Query("method"); m != "" {
		q = q.Where("method = ?", strings.ToUpper(m))
	}
	if sd := c.Query("start_date"); sd != "" {
		q = q.Where("created_at >= ?::date", sd)
	}
	if ed := c.Query("end_date"); ed != "" {
		q = q.Where("created_at < ?::date + interval '1 day'", ed)
	}
	if d := c.Query("date"); d != "" {
		q = q.Where("DATE(created_at) = ?", d)
	}
	if err := q.Find(&logs).Error; err != nil {
		JSONError(c, CodeDatabaseError, "查询失败")
		return
	}
	var total int64
	cq := h.service.DB.Model(&models.APIRequestLog{}).Where("org_id = ?", orgID)
	if kid := c.Query("key_id"); kid != "" {
		cq = cq.Where("api_key_id = ?", kid)
	} else if role != "owner" && role != "admin" {
		cq = cq.Where("api_key_owner_id = ?", userID)
	}
	if status := c.Query("status"); status != "" {
		if status == "success" {
			cq = cq.Where("status_code < 400")
		} else if status == "failed" {
			cq = cq.Where("status_code >= 400")
		}
	}
	if sc := c.Query("status_code"); sc != "" {
		if sc == "2xx" {
			cq = cq.Where("status_code BETWEEN 200 AND 299")
		} else if sc == "4xx" {
			cq = cq.Where("status_code BETWEEN 400 AND 499")
		} else if sc == "5xx" {
			cq = cq.Where("status_code BETWEEN 500 AND 599")
		} else {
			cq = cq.Where("status_code = ?", sc)
		}
	}
	if p := c.Query("path"); p != "" {
		cq = cq.Where("path LIKE ?", "%"+p+"%")
	}
	if m := c.Query("method"); m != "" {
		cq = cq.Where("method = ?", strings.ToUpper(m))
	}
	if sd := c.Query("start_date"); sd != "" {
		cq = cq.Where("created_at >= ?::date", sd)
	}
	if ed := c.Query("end_date"); ed != "" {
		cq = cq.Where("created_at < ?::date + interval '1 day'", ed)
	}
	if d := c.Query("date"); d != "" {
		cq = cq.Where("DATE(created_at) = ?", d)
	}
	_ = cq.Count(&total).Error
	items := make([]LogItem, len(logs))
	for i, lg := range logs {
		rb := summarizeJSON(string(lg.RequestBody))
		sb := summarizeJSON(string(lg.ResponseBody))
		items[i] = LogItem{
			ID:           lg.ID,
			Method:       lg.Method,
			Path:         lg.Path,
			StatusCode:   lg.StatusCode,
			LatencyMs:    lg.LatencyMs,
			ClientIP:     lg.ClientIP,
			CreatedAt:    utils.FormatTime(lg.CreatedAt),
			TimeStamp:    utils.FormatTimeUnix(lg.CreatedAt),
			RequestBody:  rb,
			ResponseBody: sb,
			KeyID: func() string {
				if lg.APIKeyID != nil {
					return *lg.APIKeyID
				}
				return ""
			}(),
			KeyName: lg.APIKeyName,
			KeyOwnerID: func() string {
				if lg.APIKeyOwnerID != nil {
					return *lg.APIKeyOwnerID
				}
				return ""
			}(),
		}
	}
	JSONSuccess(c, gin.H{"page": page, "limit": limit, "total": total, "items": items})
}

func summarizeJSON(s string) string {
	if len(s) == 0 {
		return ""
	}
	if len(s) > 256 {
		return s[:256]
	}
	return s
}

func (h *ConsoleHandler) GetUsageStats(c *gin.Context) {
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")
	if startDate == "" || endDate == "" {
		JSONError(c, CodeInvalidParameter, "缺少日期范围")
		return
	}
	type Daily struct {
		Date     string `json:"date"`
		Requests int64  `json:"requests"`
		Errors   int64  `json:"errors"`
	}
	var daily []Daily
	if err := h.service.DB.Raw(`
        SELECT to_char(DATE(created_at), 'YYYY-MM-DD') AS date,
               COUNT(*) AS requests,
               SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END) AS errors
        FROM api_request_logs
        WHERE org_id = ? AND created_at >= ?::date AND created_at <= ?::date + interval '1 day' - interval '1 second'
        GROUP BY DATE(created_at)
        ORDER BY DATE(created_at)
    `, c.GetString("orgID"), startDate, endDate).Scan(&daily).Error; err != nil {
		JSONError(c, CodeDatabaseError, "查询失败")
		return
	}
	type ErrItem struct {
		Code  int   `json:"code"`
		Count int64 `json:"count"`
	}
	var errorsBreakdown []ErrItem
	if err := h.service.DB.Raw(`
        SELECT status_code AS code, COUNT(*) AS count
        FROM api_request_logs
        WHERE org_id = ? AND created_at >= ?::date AND created_at <= ?::date + interval '1 day' - interval '1 second'
          AND status_code >= 400
        GROUP BY status_code
        ORDER BY status_code
    `, c.GetString("orgID"), startDate, endDate).Scan(&errorsBreakdown).Error; err != nil {
		JSONError(c, CodeDatabaseError, "查询失败")
		return
	}
	JSONSuccess(c, gin.H{"daily": daily, "errors_breakdown": errorsBreakdown})
}

// DeleteMe 删除当前账户（软删除）
func (h *ConsoleHandler) DeleteMe(c *gin.Context) {
	userClaims, exists := c.Get("user")
	if !exists {
		JSONError(c, CodeUnauthorized, "未授权访问")
		return
	}
	claims := userClaims.(jwt.MapClaims)
	userID := claims["user_id"].(string)
	var user models.User
	if err := h.service.DB.First(&user, "id = ?", userID).Error; err != nil {
		JSONError(c, CodeNotFound, "用户不存在")
		return
	}
	if user.Status != "active" {
		JSONError(c, CodeConflict, "当前状态不可注销")
		return
	}
	var cnt int64
	_ = h.service.DB.Table("organizations").Where("owner_id = ?", userID).Count(&cnt).Error
	if cnt > 0 {
		JSONError(c, CodeConflict, "You own organizations. Please transfer ownership or delete them first.")
		return
	}
	tx := h.service.DB.Begin()
	if err := tx.Model(&models.OrganizationMember{}).Where("user_id = ?", userID).Update("status", "suspended").Error; err != nil {
		tx.Rollback()
		JSONError(c, CodeDatabaseError, "更新成员状态失败")
		return
	}
	if err := tx.Model(&models.User{}).Where("id = ?", userID).Updates(map[string]interface{}{"status": "pending_deletion", "deleted_at": time.Now()}).Error; err != nil {
		tx.Rollback()
		JSONError(c, CodeDatabaseError, "更新用户失败")
		return
	}
	if err := tx.Commit().Error; err != nil {
		JSONError(c, CodeDatabaseError, "事务提交失败")
		return
	}
	JSONSuccess(c, gin.H{"deleted": true})
}

// GetNotifications 获取站内通知
func (h *ConsoleHandler) GetNotifications(c *gin.Context) {
	userID := c.GetString("userID")
	unreadOnly := c.Query("unread_only") == "true"
	// 分页参数
	page := 1
	limit := 20
	if v := c.Query("page"); v != "" {
		fmt.Sscanf(v, "%d", &page)
	}
	if v := c.Query("limit"); v != "" {
		fmt.Sscanf(v, "%d", &limit)
	}
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit

	var notifs []models.Notification
	qb := h.service.DB.Where("user_id = ?", userID)
	if unreadOnly {
		qb = qb.Where("is_read = ?", false)
	}
	if err := qb.Order("created_at DESC").Offset(offset).Limit(limit).Find(&notifs).Error; err != nil {
		JSONError(c, CodeDatabaseError, "查询失败")
		return
	}
	JSONSuccess(c, gin.H{"items": notifs, "page": page, "limit": limit})
}

// MarkNotificationRead 标记通知为已读
func (h *ConsoleHandler) MarkNotificationRead(c *gin.Context) {
	userID := c.GetString("userID")
	id := c.Param("id")
	if id == "" {
		JSONError(c, CodeInvalidParameter, "ID不能为空")
		return
	}
	if err := h.service.DB.Model(&models.Notification{}).Where("id = ? AND user_id = ?", id, userID).Update("is_read", true).Error; err != nil {
		JSONError(c, CodeDatabaseError, "更新失败")
		return
	}
	JSONSuccess(c, gin.H{"read": id})
}

type QuotaStatusItem struct {
	Limit     int     `json:"limit"`
	Used      int     `json:"used"`
	Remaining int     `json:"remaining"`
	ResetAt   *string `json:"reset_at"`
}

func (h *ConsoleHandler) GetQuotaStatus(c *gin.Context) {
	orgID := c.GetString("orgID")
	var quotas []models.OrganizationQuotas
	_ = h.service.DB.Where("organization_id = ?", orgID).Find(&quotas).Error
	m := map[string]QuotaStatusItem{}
	for _, q := range quotas {
		var resetStr *string
		if q.ResetAt != nil {
			s := q.ResetAt.UTC().Format("2006-01-02T15:04:05Z")
			resetStr = &s
		}
		m[q.ServiceType] = QuotaStatusItem{Limit: q.Allocation, Used: q.Consumed, Remaining: q.Allocation - q.Consumed, ResetAt: resetStr}
	}
	JSONSuccess(c, m)
}
