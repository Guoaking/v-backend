package api

import (
	"fmt"
	"strings"
	"time"

	"kyc-service/internal/models"
	"kyc-service/internal/service"
	"kyc-service/pkg/logger"
	"kyc-service/pkg/utils"

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

// @Summary 用户登录
// @Description 用户使用邮箱和密码登录
// @Tags Auth
// @Accept json
// @Produce json
// @Param login body LoginRequest true "登录信息"
// @Success 200 {object} SuccessResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Router /api/v1/auth/login [post]
func (h *UserAuthHandler) Login(c *gin.Context) { //ignore_security_alert IDOR
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		JSONError(c, CodeInvalidParameter, "Invalid request format")
		return
	}

	// 查找用户
	user, err := h.service.GetUserByEmail(req.Email)
	if err != nil {
		JSONError(c, CodeUnauthorized, "Invalid email or password")
		return
	}

	// 验证密码
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		JSONError(c, CodeUnauthorized, "Invalid email or password")
		return
	}

	// 检查用户状态
	if user.Status != "active" {
		JSONError(c, CodeUnauthorized, "Account is suspended")
		return
	}

	// 生成JWT令牌
	token, err := h.generateJWT(user)
	if err != nil {
		logger.GetLogger().WithError(err).Error("Failed to generate JWT token")
		JSONError(c, CodeInternalError, "Internal server error")
		return
	}

	// 记录登录审计日志
	h.recordAuditLog(c, user.ID, "login", "success", "User logged in successfully")

	JSONSuccess(c, AuthResponse{
		Token: token,
		User: &UserProfile{
			ID:        user.ID,
			Email:     user.Email,
			Name:      user.Name,
			AvatarURL: user.AvatarURL,
			Role:      user.Role,
		},
	})
}

// @Summary 用户注册
// @Description 用户注册新账户
// @Tags Auth
// @Accept json
// @Produce json
// @Param register body RegisterRequest true "注册信息"
// @Success 200 {object} SuccessResponse
// @Failure 400 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Router /api/v1/auth/register [post]
func (h *UserAuthHandler) Register(c *gin.Context) { //ignore_security_alert IDOR
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		JSONError(c, CodeInvalidParameter, "Invalid request format")
		return
	}

	// 检查邮箱是否已存在
	existingUser, _ := h.service.GetUserByEmail(req.Email)
	if existingUser != nil {
		JSONError(c, CodeConflict, "Email already exists")
		return
	}

	// 哈希密码
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		logger.GetLogger().WithError(err).Error("Failed to hash password")
		JSONError(c, CodeInternalError, "Internal server error")
		return
	}

	// 生成默认头像URL（如果未提供）
	avatarURL := req.AvatarURL
	if avatarURL == "" {
		// 使用DiceBear API生成基于邮箱的默认头像
		avatarURL = fmt.Sprintf("https://api.dicebear.com/7.x/avataaars/svg?seed=%s", req.Email)
	}

	// 创建默认组织
	org := &models.Organization{
		ID:           utils.GenerateID(),
		Name:         req.Company,
		PlanID:       "starter",
		BillingEmail: req.Email,
		Status:       "active",
	}
	if err := h.service.CreateOrganization(org); err != nil {
		logger.GetLogger().WithError(err).Error("Failed to create organization")
		JSONError(c, CodeDatabaseError, "Failed to create organization")
		return
	}

	// 创建用户
	user := &models.User{
		ID:        utils.GenerateID(),
		Email:     req.Email,
		Password:  string(hashedPassword),
		Name:      req.Name,
		AvatarURL: avatarURL,
		Role:      "user",
		Status:    "active",
		OrgID:     org.ID, // 设置org_id
	}

	if err := h.service.CreateUser(user); err != nil {
		logger.GetLogger().WithError(err).Error("Failed to create user")
		JSONError(c, CodeDatabaseError, "Failed to create user")
		return
	}

	// 添加用户到组织
	member := &models.OrganizationMember{
		ID:             utils.GenerateID(),
		OrganizationID: org.ID,
		UserID:         user.ID,
		Role:           "owner",
		Status:         "active",
	}
	if err := h.service.CreateOrganizationMember(member); err != nil {
		logger.GetLogger().WithError(err).Error("Failed to add user to organization")
		// 不返回错误，继续处理
	}

	// 生成JWT令牌
	token, err := h.generateJWT(user)
	if err != nil {
		logger.GetLogger().WithError(err).Error("Failed to generate JWT token")
		JSONError(c, CodeInternalError, "Internal server error")
		return
	}

	// 记录注册审计日志
	h.recordAuditLog(c, user.ID, "register", "success", "User registered successfully")

	JSONSuccess(c, AuthResponse{
		Token: token,
		User: &UserProfile{
			ID:        user.ID,
			Email:     user.Email,
			Name:      user.Name,
			AvatarURL: user.AvatarURL,
			Role:      user.Role,
		},
	})
}

// @Summary 获取当前用户信息
// @Description 获取当前登录用户的信息
// @Tags Auth
// @Accept json
// @Produce json
// @Success 200 {object} SuccessResponse
// @Failure 401 {object} ErrorResponse
// @Router /api/v1/auth/me [get]
func (h *UserAuthHandler) GetCurrentUser(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		JSONError(c, CodeUnauthorized, "Unauthorized")
		return
	}

	user, err := h.service.GetUserByID(userID.(string))
	if err != nil {
		JSONError(c, CodeNotFound, "User not found")
		return
	}

	// 查询用户的API密钥
	var apiKeys []models.APIKey
	if err := h.service.DB.Where("user_id = ? AND status = ?", userID, "active").Order("created_at DESC").Find(&apiKeys).Error; err != nil {
		logger.GetLogger().WithError(err).Error("查询API密钥失败")
		apiKeys = []models.APIKey{}
	}

	// 构建API密钥响应
	apiKeyResponses := make([]ConsoleAPIKeyResponse, len(apiKeys))
	for i, key := range apiKeys {
		scopes := []string{"ocr:read"}
		if key.Scopes != "" {
			// 解析scopes JSON
			scopes = strings.Split(strings.Trim(key.Scopes, "[]\""), ",")
		}

		apiKeyResponses[i] = ConsoleAPIKeyResponse{
			ID:        key.ID,
			Name:      key.Name,
			Scopes:    scopes,
			Status:    key.Status,
			LastUsed:  key.LastUsedAt,
			CreatedAt: key.CreatedAt,
		}
	}

	JSONSuccess(c, &UserProfile{
		ID:        user.ID,
		Email:     user.Email,
		Name:      user.Name,
		AvatarURL: user.AvatarURL,
		Role:      user.Role,
		APIKeys:   apiKeyResponses,
	})
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

// @Summary 用户登出
// @Description 用户登出系统
// @Tags Auth
// @Accept json
// @Produce json
// @Success 200 {object} SuccessResponse
// @Failure 401 {object} ErrorResponse
// @Router /api/v1/auth/logout [post]
func (h *UserAuthHandler) Logout(c *gin.Context) {
	userID, exists := c.Get("userID")
	if exists {
		h.recordAuditLog(c, userID.(string), "logout", "success", "User logged out successfully")
	}

	JSONSuccess(c, map[string]string{"message": "Logged out successfully"})
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
