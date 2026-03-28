package api

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"kyc-service/internal/middleware"
	"kyc-service/internal/models"
	"kyc-service/pkg/logger"
	"kyc-service/pkg/metrics"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type ConsoleUpdateUserRequest struct {
	FullName  string `json:"name,omitempty"`
	AvatarURL string `json:"avatar,omitempty"`
}

type ActiveSession struct {
	ID        string    `json:"id"`
	Device    string    `json:"device"`
	OS        string    `json:"os"`
	Browser   string    `json:"browser"`
	IP        string    `json:"ip"`
	Location  string    `json:"location"`
	LastSeen  time.Time `json:"last_seen"`
	IsCurrent bool      `json:"is_current"`
}

type UpdateUserPasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password" binding:"required,min=6"`
}

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
		IsPlatformAdmin: user.IsPlatformAdmin,
	}

	// 记录业务操作成功
	metrics.RecordBusinessOperation(c.Request.Context(), "get_user_profile", true, time.Since(start), "")

	JSONSuccess(c, response)
}

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

	// 记录审计日志
	auditLog := &models.AuditLog{
		UserID:    userID,
		OrgID:     user.OrgID,
		Action:    "update_user_profile",
		Resource:  "user",
		Details:   datatypes.JSON(fmt.Sprintf(`{"updates": "%+v"}`, updates)),
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
		Status:    "success",
	}
	if err := h.recordAuditLog(auditLog); err != nil {
		logger.GetLogger().WithError(err).Error("记录审计日志失败")
	}

	// 记录业务操作成功
	middleware.RecordBusinessOperation("update_user_profile", true, time.Since(start), "")

	JSONSuccess(c, gin.H{
		"success": true,
		"message": "资料更新成功",
	})
}

func (h *ConsoleHandler) UpdateUserPassword(c *gin.Context) {
	start := time.Now()

	userClaims, exists := c.Get("user")
	if !exists {
		JSONError(c, CodeUnauthorized, "未授权访问")
		return
	}
	claims := userClaims.(jwt.MapClaims)
	userID := claims["user_id"].(string)

	var req UpdateUserPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		JSONError(c, CodeInvalidParameter, "参数验证失败")
		return
	}

	var user models.User
	if err := h.service.DB.First(&user, "id = ?", userID).Error; err != nil {
		JSONError(c, CodeNotFound, "用户不存在")
		return
	}

	// 如果用户有密码，且传了旧密码，则验证
	// 如果用户没有密码（比如纯通过 Google 注册），则允许旧密码为空
	if user.Password != "" {
		if req.CurrentPassword == "" {
			JSONError(c, CodeInvalidParameter, "当前密码不能为空")
			return
		}
		if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.CurrentPassword)); err != nil {
			JSONError(c, CodeUnauthorized, "当前密码错误")
			return
		}
	}

	// 生成新密码哈希
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		logger.GetLogger().WithError(err).Error("密码哈希失败")
		JSONError(c, CodeInternalError, "系统错误")
		return
	}

	// 更新密码
	if err := h.service.DB.Model(&user).Update("password", string(hashedPassword)).Error; err != nil {
		logger.GetLogger().WithError(err).Error("更新密码失败")
		JSONError(c, CodeDatabaseError, "更新失败")
		return
	}

	// 记录审计日志
	auditLog := &models.AuditLog{
		UserID:    userID,
		OrgID:     user.OrgID,
		Action:    "update_password",
		Resource:  "user",
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
		Status:    "success",
	}
	_ = h.recordAuditLog(auditLog)

	middleware.RecordBusinessOperation("update_password", true, time.Since(start), "")

	JSONSuccess(c, gin.H{
		"success": true,
		"message": "密码更新成功",
	})
}

func (h *ConsoleHandler) GetActiveSessions(c *gin.Context) {
	userClaims, exists := c.Get("user")
	if !exists {
		JSONError(c, CodeUnauthorized, "未授权访问")
		return
	}
	claims := userClaims.(jwt.MapClaims)
	userID := claims["user_id"].(string)
	currentJti, _ := claims["jti"].(string)

	var sessions []ActiveSession
	// 使用 map 来根据设备指纹去重
	deviceMap := make(map[string]ActiveSession)

	if h.service.Redis != nil {
		ctx := context.Background()
		pattern := fmt.Sprintf("session:%s:*", userID)
		keys, err := h.service.Redis.Keys(ctx, pattern).Result()
		if err == nil {
			for _, key := range keys {
				val, err := h.service.Redis.Get(ctx, key).Result()
				if err == nil {
					var rawData map[string]interface{}
					if err := json.Unmarshal([]byte(val), &rawData); err == nil {
						jti := rawData["id"].(string)

						// 解析 User-Agent
						uaStr := rawData["user_agent"].(string)
						device := "Unknown Device"
						browser := "Unknown Browser"
						os := "Unknown OS"

						if strings.Contains(uaStr, "Mac OS X") {
							device = "MacBook"
							os = "macOS"
						} else if strings.Contains(uaStr, "Windows") {
							device = "PC"
							os = "Windows"
						} else if strings.Contains(uaStr, "iPhone") {
							device = "iPhone"
							os = "iOS"
						} else if strings.Contains(uaStr, "Linux") {
							device = "PC"
							os = "Linux"
						} else if strings.Contains(uaStr, "Android") {
							device = "Android"
							os = "Android"
						} else if strings.Contains(uaStr, "iPad") {
							device = "iPad"
							os = "iOS"
						}

						if strings.Contains(uaStr, "Edg") {
							browser = "Edge"
						} else if strings.Contains(uaStr, "Chrome") {
							browser = "Chrome"
						} else if strings.Contains(uaStr, "Firefox") {
							browser = "Firefox"
						} else if strings.Contains(uaStr, "Safari") {
							browser = "Safari"
						}

						lastSeenRaw := rawData["last_seen"].(float64)
						isCurrent := jti == currentJti

						fingerprint := ""
						if fp, ok := rawData["device_fingerprint"].(string); ok {
							fingerprint = fp
						} else {
							// 兼容老数据
							fingerprint = fmt.Sprintf("%x", md5.Sum([]byte(rawData["ip"].(string)+uaStr)))
						}

						session := ActiveSession{
							ID:        jti,
							Device:    device,
							OS:        os,
							Browser:   browser,
							IP:        rawData["ip"].(string),
							Location:  "Local Network", // 实际项目中可以通过 GeoIP 解析
							LastSeen:  time.Unix(int64(lastSeenRaw), 0),
							IsCurrent: isCurrent,
						}

						// 如果这个设备记录已经存在
						if existing, ok := deviceMap[fingerprint]; ok {
							// 如果当前遍历的记录是 "Current" 或者是更新的记录，则覆盖
							if isCurrent || (!existing.IsCurrent && session.LastSeen.After(existing.LastSeen)) {
								// 保留最新的，但如果要删除，可能需要同时删除旧的 Redis Key，
								// 这里只做展示层的合并去重
								deviceMap[fingerprint] = session
							}
						} else {
							deviceMap[fingerprint] = session
						}
					}
				}
			}
		}
	}

	for _, session := range deviceMap {
		sessions = append(sessions, session)
	}

	// 如果没有从 Redis 取到数据（比如没配置 Redis），降级使用当前请求的信息
	if len(sessions) == 0 {
		sessions = append(sessions, ActiveSession{
			ID:        currentJti,
			Device:    "Current Device",
			OS:        "Unknown",
			Browser:   "Unknown",
			IP:        c.ClientIP(),
			Location:  "Unknown",
			LastSeen:  time.Now(),
			IsCurrent: true,
		})
	}

	JSONSuccess(c, sessions)
}

func (h *ConsoleHandler) RevokeSession(c *gin.Context) {
	sessionID := c.Param("id")

	userClaims, exists := c.Get("user")
	if !exists {
		JSONError(c, CodeUnauthorized, "未授权访问")
		return
	}
	claims := userClaims.(jwt.MapClaims)
	userID := claims["user_id"].(string)

	if h.service.Redis != nil {
		ctx := context.Background()

		// 1. 从 Redis 的 Active Sessions 中删除该设备
		sessionKey := fmt.Sprintf("session:%s:%s", userID, sessionID)
		h.service.Redis.Del(ctx, sessionKey)

		// 2. 将该 JTI 加入黑名单 (Blocklist)，防止其继续使用未过期的 JWT 访问 API
		// 黑名单保留时间建议与 JWT 签发时的过期时间 (24h) 一致
		blocklistKey := fmt.Sprintf("blocklist:%s", sessionID)
		h.service.Redis.Set(ctx, blocklistKey, "revoked", 24*time.Hour)
	}

	logger.GetLogger().WithField("session_id", sessionID).Info("Session revoked and blocklisted")

	JSONSuccess(c, gin.H{
		"success": true,
		"message": "会话已下线",
	})
}

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
	// Only count active organizations, ignore deleted ones
	_ = h.service.DB.Table("organizations").Where("owner_id = ? AND status = ?", userID, "active").Count(&cnt).Error
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

// GetOAuthConnections 获取用户已绑定的第三方账号
func (h *ConsoleHandler) GetOAuthConnections(c *gin.Context) {
	userClaims, exists := c.Get("user")
	if !exists {
		JSONError(c, CodeUnauthorized, "Unauthorized")
		return
	}
	claims := userClaims.(jwt.MapClaims)
	userID := claims["user_id"].(string)

	var connections []models.UserOAuthConnection
	if err := h.service.DB.Where("user_id = ?", userID).Find(&connections).Error; err != nil {
		JSONError(c, CodeInternalError, "Failed to fetch connections")
		return
	}

	// 过滤敏感字段，只返回需要展示的信息
	var result []map[string]interface{}
	for _, conn := range connections {
		result = append(result, map[string]interface{}{
			"id":             conn.ID,
			"provider":       conn.Provider,
			"provider_email": conn.ProviderEmail,
			"created_at":     conn.CreatedAt,
		})
	}

	JSONSuccess(c, gin.H{"connections": result})
}

// UnbindOAuthConnection 解绑第三方账号
func (h *ConsoleHandler) UnbindOAuthConnection(c *gin.Context) {
	provider := c.Param("provider")
	userClaims, exists := c.Get("user")
	if !exists {
		JSONError(c, CodeUnauthorized, "Unauthorized")
		return
	}
	claims := userClaims.(jwt.MapClaims)
	userID := claims["user_id"].(string)

	// 安全检查 1: 检查用户是否有密码
	var user models.User
	if err := h.service.DB.First(&user, "id = ?", userID).Error; err != nil {
		JSONError(c, CodeInternalError, "User not found")
		return
	}

	// 安全检查 2: 获取当前所有的绑定记录
	var connections []models.UserOAuthConnection
	if err := h.service.DB.Where("user_id = ?", userID).Find(&connections).Error; err != nil {
		JSONError(c, CodeInternalError, "Failed to fetch connections")
		return
	}

	// 防锁死机制：如果没有密码，且只有这一个第三方绑定，拒绝解绑
	if user.Password == "" && len(connections) <= 1 {
		JSONError(c, CodeInvalidParameter, "Cannot unbind the only login method. Please set a password first.")
		return
	}

	// 执行解绑
	if err := h.service.DB.Where("user_id = ? AND provider = ?", userID, provider).Delete(&models.UserOAuthConnection{}).Error; err != nil {
		JSONError(c, CodeInternalError, "Failed to unbind connection")
		return
	}

	JSONSuccess(c, gin.H{
		"success": true,
		"message": fmt.Sprintf("Successfully unbound %s account", provider),
	})
}
