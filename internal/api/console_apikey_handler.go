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
	"kyc-service/pkg/crypto"
	"kyc-service/pkg/logger"
	"kyc-service/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

type ConsoleCreateAPIKeyRequest struct {
	Name   string   `json:"name" binding:"required,min=1,max=100"`
	Scopes []string `json:"scopes" binding:"required,min=1"`
}

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

func (h *ConsoleHandler) generateAPIKeySecret() string {
	// 生成32字节的随机数据
	bytes := make([]byte, 32)
	rand.Read(bytes)

	// 编码为base64
	encoded := base64.URLEncoding.EncodeToString(bytes)

	// 添加前缀并返回
	return "sk_live_" + strings.ToLower(encoded)
}
