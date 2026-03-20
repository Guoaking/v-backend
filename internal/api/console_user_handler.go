package api

import (
	"fmt"
	"time"

	"kyc-service/internal/middleware"
	"kyc-service/internal/models"
	"kyc-service/pkg/logger"
	"kyc-service/pkg/metrics"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type ConsoleUpdateUserRequest struct {
	FullName  string `json:"name,omitempty"`
	AvatarURL string `json:"avatar,omitempty"`
	Company   string `json:"company,omitempty"`
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
		Company:         org.Name,
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
