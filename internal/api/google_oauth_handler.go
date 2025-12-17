package api

import (
    "encoding/json"
    "fmt"
    "io"
    "net/http"
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
    "golang.org/x/crypto/bcrypt"
    "gorm.io/gorm"
)

// GoogleOAuthHandler Google OAuth处理器
type GoogleOAuthHandler struct {
	service *service.KYCService
}

// NewGoogleOAuthHandler 创建Google OAuth处理器
func NewGoogleOAuthHandler(svc *service.KYCService) *GoogleOAuthHandler {
	return &GoogleOAuthHandler{service: svc}
}

// GoogleTokenRequest Google令牌请求
type GoogleTokenRequest struct {
	IDToken string `json:"id_token" binding:"required"`
}

// GoogleUserInfo Google用户信息
type GoogleUserInfo struct {
	Sub           string `json:"sub"`            // Google用户ID
	Email         string `json:"email"`          // 邮箱
	EmailVerified bool   `json:"email_verified"` // 邮箱是否验证
	Name          string `json:"name"`           // 全名
	GivenName     string `json:"given_name"`     // 名字
	FamilyName    string `json:"family_name"`    // 姓氏
	Picture       string `json:"picture"`        // 头像URL
}

// @Summary Google OAuth登录/注册
// @Description 使用Google ID Token进行登录或注册
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body GoogleTokenRequest true "Google ID Token"
// @Success 200 {object} SuccessResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Router /api/v1/auth/google [post]
func (h *GoogleOAuthHandler) GoogleLogin(c *gin.Context) {
	start := time.Now()

	var req GoogleTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
    metrics.RecordBusinessOperation(c.Request.Context(), "google_oauth", false, time.Since(start), "invalid_request")
		JSONError(c, CodeInvalidParameter, "参数验证失败")
		return
	}

	// 验证Google ID Token
	googleUser, err := h.verifyGoogleToken(req.IDToken)
	if err != nil {
		logger.GetLogger().WithError(err).Error("Google Token验证失败")
    metrics.RecordBusinessOperation(c.Request.Context(), "google_oauth", false, time.Since(start), "token_verification_failed")
		JSONError(c, CodeUnauthorized, "Google Token验证失败")
		return
	}

	// 检查邮箱是否已验证
	if !googleUser.EmailVerified {
    metrics.RecordBusinessOperation(c.Request.Context(), "google_oauth", false, time.Since(start), "email_not_verified")
		JSONError(c, CodeUnauthorized, "Google邮箱未验证")
		return
	}

	// 查找用户
	var user models.User
	err = h.service.DB.Where("email = ?", googleUser.Email).First(&user).Error

	if err == gorm.ErrRecordNotFound {
		// 用户不存在，自动注册
		user, err = h.createUserFromGoogle(googleUser)
		if err != nil {
			logger.GetLogger().WithError(err).Error("Google用户注册失败")
            metrics.RecordBusinessOperation(c.Request.Context(), "google_oauth", false, time.Since(start), "registration_failed")
			JSONError(c, CodeInternalError, "用户注册失败")
			return
		}
        metrics.RecordBusinessOperation(c.Request.Context(), "google_oauth", true, time.Since(start), "user_registered")
	} else if err != nil {
		logger.GetLogger().WithError(err).Error("查询用户失败")
        metrics.RecordBusinessOperation(c.Request.Context(), "google_oauth", false, time.Since(start), "database_error")
		JSONError(c, CodeDatabaseError, "系统错误")
		return
	} else {
		// 用户存在，检查状态
		if user.Status != "active" {
            metrics.RecordBusinessOperation(c.Request.Context(), "google_oauth", false, time.Since(start), "user_inactive")
			JSONError(c, CodeUnauthorized, "用户账户已禁用")
			return
		}
        metrics.RecordBusinessOperation(c.Request.Context(), "google_oauth", true, time.Since(start), "user_login")
	}

	// 获取组织信息
	var org models.Organization
	if err := h.service.DB.First(&org, "id = ?", user.OrgID).Error; err != nil {
		logger.GetLogger().WithError(err).Error("查询组织失败")
        metrics.RecordBusinessOperation(c.Request.Context(), "google_oauth", false, time.Since(start), "org_not_found")
		JSONError(c, CodeInternalError, "组织信息错误")
		return
	}

	// 生成JWT令牌
	accessToken, err := h.generateUserJWT(&user, &org)
	if err != nil {
		logger.GetLogger().WithError(err).Error("生成JWT失败")
        metrics.RecordBusinessOperation(c.Request.Context(), "google_oauth", false, time.Since(start), "jwt_generation_failed")
		JSONError(c, CodeInternalError, "令牌生成失败")
		return
	}

	// 更新最后登录时间
	now := time.Now()
	user.LastLoginAt = &now
	if err := h.service.DB.Save(&user).Error; err != nil {
		logger.GetLogger().WithError(err).Error("更新登录时间失败")
	}

	// 记录审计日志
	auditLog := &models.AuditLog{
		UserID:    user.ID,
		OrgID:     user.OrgID,
		Action:    "google_oauth_login",
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
		Status:    "success",
		Message:   fmt.Sprintf("Google OAuth login successful for: %s", user.Email),
	}
	if err := h.service.DB.Create(auditLog).Error; err != nil {
		logger.GetLogger().WithError(err).Error("记录审计日志失败")
	}

	// 记录业务操作成功
	middleware.RecordBusinessOperation("google_oauth", true, time.Since(start), "")

	// 返回用户信息
	JSONSuccess(c, ConsoleLoginResponse{
		AccessToken: accessToken,
		User: &ConsoleUserProfile{
			ID:        user.ID,
			Email:     user.Email,
			FullName:  user.Name,
			AvatarURL: user.AvatarURL,
			Company:   org.Name,
			Role:      user.Role,
			OrgRole:   user.OrgRole,
			OrgID:     user.OrgID,
			PlanID:    org.PlanID,
			Status:    user.Status,
		},
	})
}

// verifyGoogleToken 验证Google ID Token
func (h *GoogleOAuthHandler) verifyGoogleToken(idToken string) (*GoogleUserInfo, error) {
	// 调用Google Token验证API
	url := fmt.Sprintf("https://oauth2.googleapis.com/tokeninfo?id_token=%s", idToken)

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("请求Google API失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Google API返回错误状态: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %v", err)
	}

	var googleUser GoogleUserInfo
	if err := json.Unmarshal(body, &googleUser); err != nil {
		return nil, fmt.Errorf("解析响应失败: %v", err)
	}

	// 验证audience (客户端ID) - 在实际部署中应该验证
	// 这里简化处理，只验证基本的token有效性

	return &googleUser, nil
}

// createUserFromGoogle 从Google用户信息创建用户
func (h *GoogleOAuthHandler) createUserFromGoogle(googleUser *GoogleUserInfo) (models.User, error) {
	tx := h.service.DB.Begin()
	if tx.Error != nil {
		return models.User{}, fmt.Errorf("开始事务失败: %v", tx.Error)
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// 创建默认组织
	org := models.Organization{
		ID:           utils.GenerateID(),
		Name:         fmt.Sprintf("%s's Organization", googleUser.Name),
		PlanID:       "starter",
		BillingEmail: googleUser.Email,
		Status:       "active",
	}
	if err := tx.Create(&org).Error; err != nil {
		tx.Rollback()
		return models.User{}, fmt.Errorf("创建组织失败: %v", err)
	}

	// 创建用户
	user := models.User{
		ID:        utils.GenerateID(),
		Email:     googleUser.Email,
		Name:      googleUser.Name,
		FullName:  googleUser.Name,
		AvatarURL: googleUser.Picture,
		Role:      "user",
		OrgID:     org.ID,
		OrgRole:   "owner",
		Status:    "active",
	}

	// 生成随机密码（用户不会使用，因为使用OAuth登录）
	randomPassword := utils.GenerateID() + utils.GenerateID()
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(randomPassword), bcrypt.DefaultCost)
	if err != nil {
		tx.Rollback()
		return models.User{}, fmt.Errorf("密码加密失败: %v", err)
	}
	user.Password = string(hashedPassword)

	if err := tx.Create(&user).Error; err != nil {
		tx.Rollback()
		return models.User{}, fmt.Errorf("创建用户失败: %v", err)
	}

	// 添加用户到组织
	member := models.OrganizationMember{
		ID:             utils.GenerateID(),
		OrganizationID: org.ID,
		UserID:         user.ID,
		Role:           "owner",
		Status:         "active",
	}
	if err := tx.Create(&member).Error; err != nil {
		tx.Rollback()
		return models.User{}, fmt.Errorf("添加用户到组织失败: %v", err)
	}

	// 创建默认API密钥
	// 创建默认API密钥
	defaultSecret := "sk_live_" + utils.GenerateID()
	// 计算前缀
	prefix := ""
	if idx := strings.Index(defaultSecret, "_"); idx != -1 {
		if j := strings.Index(defaultSecret[idx+1:], "_"); j != -1 {
			prefix = defaultSecret[:idx+1+j+1]
			if k := strings.LastIndex(prefix, "_"); k != -1 {
				prefix = prefix[:k]
			}
		}
	}

	apiKey := models.APIKey{
		ID:         utils.GenerateID(),
		UserID:     user.ID,
		OrgID:      org.ID,
		Name:       "Default Key",
		SecretHash: "", // 将在下面设置
		Prefix:     prefix,
		Scopes:     `["ocr:read"]`,
		Status:     "active",
	}

	// 生成密钥哈希
	secretHash, err := crypto.HashString(defaultSecret)
	if err != nil {
		tx.Rollback()
		return models.User{}, fmt.Errorf("密钥哈希失败: %v", err)
	}
	apiKey.SecretHash = secretHash

	if err := tx.Create(&apiKey).Error; err != nil {
		tx.Rollback()
		return models.User{}, fmt.Errorf("创建默认API密钥失败: %v", err)
	}

	if err := tx.Commit().Error; err != nil {
		return models.User{}, fmt.Errorf("提交事务失败: %v", err)
	}

	return user, nil
}

// generateUserJWT 生成用户JWT令牌
func (h *GoogleOAuthHandler) generateUserJWT(user *models.User, org *models.Organization) (string, error) {
	claims := jwt.MapClaims{
		"user_id":  user.ID,
		"email":    user.Email,
		"role":     user.Role,
		"org_id":   user.OrgID,
		"org_role": user.OrgRole,
		"plan_id":  org.PlanID,
		"exp":      time.Now().Add(24 * time.Hour).Unix(),
		"iat":      time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(h.service.Config.Security.JWTSecret))
}
