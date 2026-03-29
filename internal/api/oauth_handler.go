package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"kyc-service/internal/models"
	"kyc-service/internal/service"
	"kyc-service/pkg/logger"
	"kyc-service/pkg/utils"
)

type OAuthHandler struct {
	service     *service.KYCService
	authHandler *ConsoleAuthHandler
}

func NewOAuthHandler(s *service.KYCService, auth *ConsoleAuthHandler) *OAuthHandler {
	return &OAuthHandler{
		service:     s,
		authHandler: auth,
	}
}

// GoogleLoginRedirect 重定向到 Google 授权页面
func (h *OAuthHandler) GoogleLoginRedirect(c *gin.Context) {
	clientID := h.service.Config.OAuth.Google.ClientID
	redirectURL := h.service.Config.OAuth.Google.RedirectURL

	if clientID == "" || redirectURL == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Google OAuth is not configured"})
		return
	}

	// 检查是否是为了“绑定”而发起的请求
	action := c.Query("action") // 比如 "bind"
	var userID string

	// 如果是绑定操作，需要从当前会话(JWT)中获取 user_id
	if action == "bind" {
		// 因为这个接口在 router.go 中是没有挂载 JWTAuth middleware 的 (属于 /auth 组)
		// 所以 c.Get("user_id") 会失败。我们需要手动解析 header 中的 Token
		tokenString := c.GetHeader("Authorization")
		if tokenString == "" {
			// 尝试从 URL 参数中获取 token (因为 window.location.href 不容易带 Header)
			tokenString = c.Query("token")
		} else {
			// Bearer token
			if len(tokenString) > 7 && strings.HasPrefix(tokenString, "Bearer ") {
				tokenString = tokenString[7:]
			}
		}

		if tokenString == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized for binding (missing token)"})
			return
		}

		// 解析 Token
		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return []byte(h.service.Config.Security.JWTSecret), nil
		})

		if err != nil || !token.Valid {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized for binding (invalid token)"})
			return
		}

		if claims, ok := token.Claims.(jwt.MapClaims); ok {
			if uid, ok := claims["user_id"].(string); ok {
				userID = uid
			} else {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized for binding (invalid user_id in token)"})
				return
			}
		} else {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized for binding (invalid claims format)"})
			return
		}
	}

	// 生成随机 state 防止 CSRF
	state := utils.GenerateID()

	// 将 state 存入 Redis，设置 5 分钟过期
	// 如果是绑定操作，把 user_id 也存进去，格式如 "google:bind:12345"
	stateVal := "google"
	if action == "bind" && userID != "" {
		stateVal = fmt.Sprintf("google:bind:%s", userID)
	}
	h.service.Redis.Set(c, fmt.Sprintf("oauth_state:%s", state), stateVal, 5*time.Minute)

	googleAuthURL := fmt.Sprintf(
		"https://accounts.google.com/o/oauth2/v2/auth?client_id=%s&redirect_uri=%s&response_type=code&scope=email profile openid&state=%s&prompt=select_account",
		url.QueryEscape(clientID),
		url.QueryEscape(redirectURL),
		url.QueryEscape(state),
	)

	c.Redirect(http.StatusTemporaryRedirect, googleAuthURL)
}

// GoogleCallback 处理 Google 授权回调
func (h *OAuthHandler) GoogleCallback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")

	// 从配置中读取前端返回地址，如果未配置则降级到硬编码默认值
	frontendRedirectURL := h.service.Config.OAuth.Google.FrontendReturnURL
	if frontendRedirectURL == "" {
		frontendRedirectURL = "http://localhost:3000" // 由于我们已经移除了 HashRouter，登录成功后直接跳到 / (也就是 /console 或 /login 等路由处理的地方)，或者如果是回调带 token 的话直接跳转。
	}
	baseURL := strings.TrimRight(frontendRedirectURL, "/")

	if code == "" || state == "" {
		c.Redirect(http.StatusTemporaryRedirect, baseURL+"/login?error=invalid_callback")
		return
	}

	// 验证 state
	stateKey := fmt.Sprintf("oauth_state:%s", state)
	val, err := h.service.Redis.Get(c, stateKey).Result()
	if err != nil || !strings.HasPrefix(val, "google") {
		c.Redirect(http.StatusTemporaryRedirect, baseURL+"/login?error=invalid_state")
		return
	}
	h.service.Redis.Del(c, stateKey) // 验证通过后删除

	isBindAction := strings.HasPrefix(val, "google:bind:")
	var bindUserID string
	if isBindAction {
		parts := strings.Split(val, ":")
		if len(parts) == 3 {
			bindUserID = parts[2]
		}
	}

	// 1. 换取 Access Token
	tokenResp, err := h.exchangeGoogleToken(code)
	if err != nil {
		logger.GetLogger().Errorf("Failed to exchange google token: %v", err)
		c.Redirect(http.StatusTemporaryRedirect, baseURL+"/login?error=token_exchange_failed")
		return
	}

	// 2. 获取用户信息
	userInfo, err := h.getGoogleUserInfo(tokenResp.AccessToken)
	if err != nil {
		logger.GetLogger().Errorf("Failed to get google user info: %v", err)
		c.Redirect(http.StatusTemporaryRedirect, baseURL+"/login?error=get_user_info_failed")
		return
	}

	if userInfo.Email == "" {
		c.Redirect(http.StatusTemporaryRedirect, baseURL+"/login?error=email_not_found")
		return
	}

	// 3. 处理账号合并/创建
	user, err := h.handleOAuthUser(c, userInfo, bindUserID)
	if err != nil {
		logger.GetLogger().Errorf("Failed to handle oauth user: %v", err)

		// 判断是否是账号已被注销
		if err.Error() == "account_deleted" {
			c.Redirect(http.StatusTemporaryRedirect, baseURL+"/login?error=account_deleted")
			return
		}

		// 如果是绑定失败，应该跳回安全设置页并带上错误信息
		if bindUserID != "" {
			c.Redirect(http.StatusTemporaryRedirect, baseURL+"/account/security?error="+url.QueryEscape(err.Error()))
			return
		}
		c.Redirect(http.StatusTemporaryRedirect, baseURL+"/login?error=user_creation_failed")
		return
	}

	// 如果是单纯绑定操作，不需要重新签发 JWT，直接重定向回安全设置页
	if bindUserID != "" {
		c.Redirect(http.StatusTemporaryRedirect, baseURL+"/account/security?bind_success=true")
		return
	}

	// 获取用户所属组织以签发完整 JWT (如果用户有组织的话)
	var org *models.Organization
	if user.OrgID != "" {
		var tempOrg models.Organization
		// Ignore deleted organizations
		if err := h.service.DB.Where("status = ?", "active").First(&tempOrg, "id = ?", user.OrgID).Error; err == nil {
			org = &tempOrg
		} else {
			logger.GetLogger().Warnf("Failed to fetch user organization (might be deleted): %v", err)
			// Don't fail the login, just let org be nil so the user logs in with 0 orgs
		}
	}

	// 4. 签发 JWT
	userAgent := c.Request.UserAgent()
	ip := c.ClientIP()
	token, err := h.authHandler.generateUserJWT(user, org, userAgent, ip)
	if err != nil {
		logger.GetLogger().Errorf("Failed to generate JWT: %v", err)
		c.Redirect(http.StatusTemporaryRedirect, baseURL+"/login?error=jwt_generation_failed")
		return
	}

	// 5. 重定向回前端带上 Token
	// 因为现在前端已经使用了 BrowserRouter，不再有 #，我们可以直接将 token 放在 search params 里
	// 确保 frontendRedirectURL 末尾没有多余的斜杠
	finalRedirectURL := fmt.Sprintf("%s/login?token=%s", baseURL, token)
	c.Redirect(http.StatusTemporaryRedirect, finalRedirectURL)
}

type googleTokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
}

func (h *OAuthHandler) exchangeGoogleToken(code string) (*googleTokenResponse, error) {
	data := url.Values{}
	data.Set("client_id", h.service.Config.OAuth.Google.ClientID)
	data.Set("client_secret", h.service.Config.OAuth.Google.ClientSecret)
	data.Set("code", code)
	data.Set("grant_type", "authorization_code")
	data.Set("redirect_uri", h.service.Config.OAuth.Google.RedirectURL)

	req, err := http.NewRequest("POST", "https://oauth2.googleapis.com/token", strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google token exchange failed with status: %d", resp.StatusCode)
	}

	var tokenResp googleTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, err
	}

	return &tokenResp, nil
}

type googleUserInfo struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	VerifiedEmail bool   `json:"verified_email"`
	Name          string `json:"name"`
	GivenName     string `json:"given_name"`
	FamilyName    string `json:"family_name"`
	Picture       string `json:"picture"`
}

func (h *OAuthHandler) getGoogleUserInfo(accessToken string) (*googleUserInfo, error) {
	req, err := http.NewRequest("GET", "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google userinfo failed with status: %d", resp.StatusCode)
	}

	var userInfo googleUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		return nil, err
	}

	return &userInfo, nil
}

func (h *OAuthHandler) handleOAuthUser(ctx context.Context, info *googleUserInfo, bindUserID string) (*models.User, error) {
	var conn models.UserOAuthConnection
	var user models.User

	tx := h.service.DB.Begin()
	if tx.Error != nil {
		return nil, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// 0. 如果明确是绑定操作
	if bindUserID != "" {
		// 检查当前账号是否已经被其他人绑定了
		err := tx.Where("provider = ? AND provider_account_id = ?", "google", info.ID).First(&conn).Error
		if err == nil {
			if conn.UserID != bindUserID {
				tx.Rollback()
				return nil, fmt.Errorf("this google account is already bound to another user")
			}
			// 已经绑定过了，直接返回
			return nil, nil
		}

		// 检查该用户是否已经绑定过一个 google 账号
		var count int64
		if err := tx.Model(&models.UserOAuthConnection{}).Where("user_id = ? AND provider = ?", bindUserID, "google").Count(&count).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("failed to check existing bindings: %v", err)
		}
		if count > 0 {
			tx.Rollback()
			return nil, fmt.Errorf("this account is already bound to a google account")
		}

		// 执行绑定
		newConn := models.UserOAuthConnection{
			ID:                utils.GenerateID(),
			UserID:            bindUserID,
			Provider:          "google",
			ProviderAccountID: info.ID,
			ProviderEmail:     info.Email,
		}
		if err := tx.Create(&newConn).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("failed to bind oauth connection: %v", err)
		}
		tx.Commit()
		return nil, nil // 绑定操作不需要返回 User
	}

	// 1. 尝试通过 Provider Account ID 查找是否已经有绑定记录
	err := tx.Where("provider = ? AND provider_account_id = ?", "google", info.ID).First(&conn).Error
	if err == nil {
		// 找到了绑定记录，直接查出对应的 User
		// 【重要修复】：使用 Unscoped 检查该用户是否已被注销
		if err := tx.Unscoped().Where("id = ?", conn.UserID).First(&user).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("user linked to connection not found: %v", err)
		}

		if user.Status == "pending_deletion" || user.DeletedAt.Valid {
			tx.Rollback()
			return nil, fmt.Errorf("account_deleted")
		}

		// 更新最后登录时间
		now := time.Now()
		tx.Model(&user).Update("last_login_at", &now)
		tx.Commit()
		return &user, nil
	}

	// 2. 如果没找到绑定记录，说明这是此 Google 账号第一次登录
	// 尝试通过邮箱查找是否已经有 User (比如之前用密码注册过)
	// 【重要修复】：如果用户状态是 pending_deletion，我们不应该让他直接登录成功，
	// 要么拒绝登录，要么恢复账号。这里为了产品体验，我们选择“阻止登录并提示”。
	err = tx.Unscoped().Where("email = ?", info.Email).First(&user).Error

	if err == nil {
		if user.Status == "pending_deletion" || user.DeletedAt.Valid {
			tx.Rollback()
			return nil, fmt.Errorf("account_deleted")
		}

		// 找到了对应邮箱的 User，执行绑定逻辑 (创建一条 connection 记录)
		newConn := models.UserOAuthConnection{
			ID:                utils.GenerateID(),
			UserID:            user.ID,
			Provider:          "google",
			ProviderAccountID: info.ID,
			ProviderEmail:     info.Email,
		}
		if err := tx.Create(&newConn).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("failed to create oauth connection: %v", err)
		}

		// 可选：如果用户没有头像，更新头像
		if user.AvatarURL == "" && info.Picture != "" {
			tx.Model(&user).Update("avatar_url", info.Picture)
		}

		// 更新最后登录时间
		now := time.Now()
		tx.Model(&user).Update("last_login_at", &now)
		tx.Commit()
		return &user, nil
	}

	// 3. 用户不存在：创建新用户、默认组织，并创建绑定记录
	orgID := utils.GenerateID()
	userID := utils.GenerateID()

	// 优先使用给定的名字，如果为空则从邮箱截取
	displayName := info.Name
	if displayName == "" {
		parts := strings.Split(info.Email, "@")
		if len(parts) > 0 {
			displayName = parts[0]
		} else {
			displayName = "User"
		}
	}

	// 3.1 创建默认组织
	org := models.Organization{
		ID:           orgID,
		Name:         displayName + "'s Workspace",
		PlanID:       "starter",
		BillingEmail: info.Email,
		Status:       "active",
		OwnerID:      userID,
	}

	if err := tx.Create(&org).Error; err != nil {
		tx.Rollback()
		return nil, err
	}

	// 3.2 创建用户 (注意：不需要再传 Provider 字段了，它在 models 中已经被去掉了)
	now := time.Now()
	newUser := models.User{
		ID:              userID,
		Email:           info.Email,
		Name:            displayName,
		FullName:        displayName,
		AvatarURL:       info.Picture,
		Role:            "user",
		OrgID:           orgID,
		OrgRole:         "owner",
		CurrentOrgID:    orgID,
		LastActiveOrgID: orgID,
		Status:          "active",
		LastLoginAt:     &now,
	}

	if err := tx.Create(&newUser).Error; err != nil {
		tx.Rollback()
		return nil, err
	}

	// 3.3 创建 OAuth 绑定记录
	newConn := models.UserOAuthConnection{
		ID:                utils.GenerateID(),
		UserID:            userID,
		Provider:          "google",
		ProviderAccountID: info.ID,
		ProviderEmail:     info.Email,
	}
	if err := tx.Create(&newConn).Error; err != nil {
		tx.Rollback()
		return nil, err
	}

	// 3.4 创建组织成员关联
	member := models.OrganizationMember{
		ID:             utils.GenerateID(),
		OrganizationID: orgID,
		UserID:         userID,
		Role:           "owner",
		Status:         "active",
	}

	if err := tx.Create(&member).Error; err != nil {
		tx.Rollback()
		return nil, err
	}

	if err := tx.Commit().Error; err != nil {
		return nil, err
	}

	return &newUser, nil
}
