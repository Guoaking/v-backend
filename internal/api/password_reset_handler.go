package api

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"kyc-service/internal/middleware"
	"kyc-service/internal/models"
	"kyc-service/internal/service"
	"kyc-service/pkg/logger"
	"kyc-service/pkg/metrics"
	"kyc-service/pkg/utils"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// PasswordResetHandler 密码重置处理器
type PasswordResetHandler struct {
	service *service.KYCService
}

// NewPasswordResetHandler 创建密码重置处理器
func NewPasswordResetHandler(svc *service.KYCService) *PasswordResetHandler {
	return &PasswordResetHandler{service: svc}
}

// PasswordResetRequest 密码重置请求
type PasswordResetRequest struct {
	Email string `json:"email" binding:"required,email"`
}

// PasswordResetResponse 密码重置响应
type PasswordResetResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// @Summary 请求密码重置
// @Description 发送密码重置邮件到用户邮箱
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body PasswordResetRequest true "密码重置请求"
// @Success 200 {object} SuccessResponse
// @Failure 400 {object} ErrorResponse
// @Router /api/v1/auth/password-reset/request [post]
func (h *PasswordResetHandler) RequestPasswordReset(c *gin.Context) {
	start := time.Now()

	var req PasswordResetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		metrics.RecordBusinessOperation(c.Request.Context(), "password_reset_request", false, time.Since(start), "invalid_request")
		JSONError(c, CodeInvalidParameter, "参数验证失败")
		return
	}

	// 查找用户
	var user models.User
	if err := h.service.DB.Where("email = ?", req.Email).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			// 用户不存在，但仍然返回成功，避免泄露用户信息
			metrics.RecordBusinessOperation(c.Request.Context(), "password_reset_request", true, time.Since(start), "user_not_found_but_hidden")
			JSONSuccess(c, PasswordResetResponse{
				Success: true,
				Message: "如果邮箱存在，重置链接已发送",
			})
			return
		}
		logger.GetLogger().WithError(err).Error("查询用户失败")
		metrics.RecordBusinessOperation(c.Request.Context(), "password_reset_request", false, time.Since(start), "database_error")
		JSONError(c, CodeDatabaseError, "系统错误")
		return
	}

	// 生成重置令牌
	token, err := h.generateResetToken()
	if err != nil {
		logger.GetLogger().WithError(err).Error("生成重置令牌失败")
		metrics.RecordBusinessOperation(c.Request.Context(), "password_reset_request", false, time.Since(start), "token_generation_failed")
		JSONError(c, CodeInternalError, "令牌生成失败")
		return
	}

	// 创建密码重置记录
	resetRecord := models.PasswordReset{
		ID:        utils.GenerateID(),
		UserID:    user.ID,
		Token:     token,
		ExpiresAt: time.Now().Add(15 * time.Minute),
		Status:    "pending",
	}

	if err := h.service.DB.Create(&resetRecord).Error; err != nil {
		logger.GetLogger().WithError(err).Error("创建密码重置记录失败")
		metrics.RecordBusinessOperation(c.Request.Context(), "password_reset_request", false, time.Since(start), "database_error")
		JSONError(c, CodeDatabaseError, "系统错误")
		return
	}

	// TODO: MVP阶段不实际发送邮件，仅返回成功消息
	// 在实际部署中，这里应该发送包含重置链接的邮件
	resetLink := fmt.Sprintf("https://verilocale.com/reset-password?token=%s", token)
	logger.GetLogger().Infof("密码重置链接已生成: %s (用户: %s)", resetLink, user.Email)

	// 记录审计日志
	auditLog := &models.AuditLog{
		UserID:    user.ID,
		Action:    "password_reset_requested",
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
		Status:    "success",
		Message:   fmt.Sprintf("Password reset requested for user: %s", user.Email),
	}
	if err := h.service.DB.Create(auditLog).Error; err != nil {
		logger.GetLogger().WithError(err).Error("记录审计日志失败")
	}

	// 记录业务操作成功
	metrics.RecordBusinessOperation(c.Request.Context(), "password_reset_request", true, time.Since(start), "")

	JSONSuccess(c, PasswordResetResponse{
		Success: true,
		Message: "重置链接已发送到您的邮箱",
	})
}

// generateResetToken 生成安全的重置令牌
func (h *PasswordResetHandler) generateResetToken() (string, error) {
	// 生成32字节的随机数据
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// @Summary 重置密码
// @Description 使用令牌重置用户密码
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body ResetPasswordConfirmRequest true "重置密码请求"
// @Success 200 {object} SuccessResponse
// @Failure 400 {object} ErrorResponse
// @Router /api/v1/auth/password-reset/confirm [post]
func (h *PasswordResetHandler) ConfirmPasswordReset(c *gin.Context) {
	start := time.Now()

	var req ResetPasswordConfirmRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		metrics.RecordBusinessOperation(c.Request.Context(), "password_reset_confirm", false, time.Since(start), "invalid_request")
		JSONError(c, CodeInvalidParameter, "参数验证失败")
		return
	}

	// 查找有效的重置令牌
	var resetRecord models.PasswordReset
	if err := h.service.DB.Where("token = ? AND status = ? AND expires_at > ?",
		req.Token, "pending", time.Now()).First(&resetRecord).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			metrics.RecordBusinessOperation(c.Request.Context(), "password_reset_confirm", false, time.Since(start), "invalid_or_expired_token")
			JSONError(c, CodeInvalidParameter, "令牌无效或已过期")
			return
		}
		logger.GetLogger().WithError(err).Error("查询重置记录失败")
		metrics.RecordBusinessOperation(c.Request.Context(), "password_reset_confirm", false, time.Since(start), "database_error")
		JSONError(c, CodeDatabaseError, "系统错误")
		return
	}

	// 更新用户密码
	if err := h.service.DB.Model(&models.User{}).Where("id = ?", resetRecord.UserID).
		Update("password", req.NewPassword).Error; err != nil {
		logger.GetLogger().WithError(err).Error("更新密码失败")
		metrics.RecordBusinessOperation(c.Request.Context(), "password_reset_confirm", false, time.Since(start), "database_error")
		JSONError(c, CodeDatabaseError, "密码更新失败")
		return
	}

	// 标记重置记录为已使用
	resetRecord.Status = "used"
	if err := h.service.DB.Save(&resetRecord).Error; err != nil {
		logger.GetLogger().WithError(err).Error("更新重置记录状态失败")
	}

	// 记录审计日志
	auditLog := &models.AuditLog{
		UserID:    resetRecord.UserID,
		Action:    "password_reset_completed",
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
		Status:    "success",
		Message:   "Password reset completed successfully",
	}
	if err := h.service.DB.Create(auditLog).Error; err != nil {
		logger.GetLogger().WithError(err).Error("记录审计日志失败")
	}

	// 记录业务操作成功
	middleware.RecordBusinessOperation("password_reset_confirm", true, time.Since(start), "")

	JSONSuccess(c, gin.H{
		"message": "密码重置成功",
	})
}

// ResetPasswordConfirmRequest 确认密码重置请求
type ResetPasswordConfirmRequest struct {
	Token       string `json:"token" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=6"`
}
