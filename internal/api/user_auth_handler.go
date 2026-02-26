package api

import (
	"time"

	"kyc-service/internal/models"
	"kyc-service/internal/service"
	"kyc-service/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// UserAuthHandler 用户认证处理器
type UserAuthHandler struct {
	service *service.KYCService
}

// NewUserAuthHandler 创建用户认证处理器
func NewUserAuthHandler(svc *service.KYCService) *UserAuthHandler {
	return &UserAuthHandler{service: svc}
}

// 已迁移至统一 BaseResponse 响应结构

// LoginRequest 登录请求
type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
}

// RegisterRequest 注册请求
type RegisterRequest struct {
	Name      string `json:"name" binding:"required"`
	Email     string `json:"email" binding:"required,email"`
	Password  string `json:"password" binding:"required,min=6"`
	Company   string `json:"company,omitempty"`
	AvatarURL string `json:"avatar_url,omitempty"` // 可选头像URL
}

// UpdatePasswordRequest 修改密码请求
type UpdatePasswordRequest struct {
	CurrentPassword string `json:"current_password" binding:"required,min=6"`
	NewPassword     string `json:"new_password" binding:"required,min=6"`
}

// AuthResponse 认证响应
type AuthResponse struct {
	Token string       `json:"token"`
	User  *UserProfile `json:"user"`
}

// UserProfile 用户档案
type UserProfile struct {
	ID        string                  `json:"id"`
	Email     string                  `json:"email"`
	Name      string                  `json:"name"`
	AvatarURL string                  `json:"avatar_url"`
	Role      string                  `json:"role"`
	APIKeys   []ConsoleAPIKeyResponse `json:"api_keys"`
}

// @Summary 修改用户密码
// @Description 修改当前用户的密码
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body UpdatePasswordRequest true "修改密码请求"
// @Success 200 {object} SuccessResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Router /api/v1/console/users/me/password [put]
func (h *UserAuthHandler) UpdatePassword(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		JSONError(c, CodeUnauthorized, "Unauthorized")
		return
	}

	var req UpdatePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		JSONError(c, CodeInvalidParameter, "Invalid request format")
		return
	}

	// 获取用户信息
	user, err := h.service.GetUserByID(userID.(string))
	if err != nil {
		JSONError(c, CodeNotFound, "User not found")
		return
	}

	// 验证当前密码
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.CurrentPassword)); err != nil {
		JSONError(c, CodeUnauthorized, "Current password is incorrect")
		return
	}

	// 哈希新密码
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		logger.GetLogger().WithError(err).Error("Failed to hash new password")
		JSONError(c, CodeInternalError, "Internal server error")
		return
	}

	// 更新密码
	if err := h.service.DB.Model(&user).Update("password", string(hashedPassword)).Error; err != nil {
		logger.GetLogger().WithError(err).Error("Failed to update password")
		JSONError(c, CodeDatabaseError, "Failed to update password")
		return
	}

	JSONSuccess(c, gin.H{"message": "Password updated successfully"})
}

// generateJWT 生成JWT令牌
func (h *UserAuthHandler) generateJWT(user *models.User) (string, error) {
	// 这里简化实现，实际应该使用配置的密钥
	secret := []byte("your-jwt-secret-key")

	claims := jwt.MapClaims{
		"user_id": user.ID,
		"email":   user.Email,
		"role":    user.Role,
		"org_id":  user.OrgID, // 添加org_id到JWT claims
		"exp":     time.Now().Add(time.Hour * 24).Unix(),
		"iat":     time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(secret)
}

// recordAuditLog 记录审计日志
func (h *UserAuthHandler) recordAuditLog(c *gin.Context, userID, action, status, message string) {
	// 延迟记录审计日志，避免阻塞主流程
	go func() {
		auditLog := &models.AuditLog{
			RequestID: c.GetString("requestID"),
			UserID:    userID,
			Action:    action,
			Resource:  "auth",
			IP:        c.ClientIP(),
			UserAgent: c.GetHeader("User-Agent"),
			Status:    status,
			Message:   message,
		}
		h.service.CreateAuditLog(auditLog)
	}()
}
