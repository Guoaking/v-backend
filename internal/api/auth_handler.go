package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"kyc-service/internal/middleware"
	"kyc-service/internal/models"
	"kyc-service/internal/service"
	"kyc-service/pkg/logger"
	"kyc-service/pkg/metrics"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

type AuthHandler struct {
	service *service.KYCService
}

func NewAuthHandler(svc *service.KYCService) *AuthHandler {
	return &AuthHandler{service: svc}
}

// TokenRequest 令牌请求
type TokenRequest struct {
	ClientID     string `json:"client_id" binding:"required"`
	ClientSecret string `json:"client_secret" binding:"required"`
	GrantType    string `json:"grant_type" binding:"required"`
	Scope        string `json:"scope"`
}

// TokenResponse 令牌响应
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope"`
}

// RefreshTokenRequest 刷新令牌请求
type RefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
	ClientID     string `json:"client_id" binding:"required"`
}

// GetToken Obtain access token
// @Summary Obtain access token
// @Description Use Client credentials to obtain access tokens
// @Tags Auth
// @Tags Public
// @Accept json
// @Produce json
// @Param request body TokenRequest true "Token request"
// @Success 200 {object} SuccessResponse
// @Router /auth/token [post]
func (h *AuthHandler) GetToken(c *gin.Context) {
	start := time.Now()

	var req TokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		metrics.RecordBusinessOperation(c.Request.Context(), "token_request", false, time.Since(start), "invalid_request")
		JSONError(c, CodeInvalidParameter, "Invalid request body")
		return
	}

	// 验证授权类型
	if req.GrantType != "client_credentials" {
		metrics.RecordBusinessOperation(c.Request.Context(), "token_request", false, time.Since(start), "unsupported_grant_type")
		JSONError(c, CodeInvalidParameter, "Unsupported grant type")
		return
	}
	// 验证客户端凭证
	var client models.OAuthClient
	if err := h.service.DB.Where("id = ? AND secret = ?", req.ClientID, req.ClientSecret).First(&client).Error; err != nil {
		metrics.RecordBusinessOperation(c.Request.Context(), "token_request", false, time.Since(start), "invalid_client")
		middleware.RecordAuthFailure("client_credentials", "invalid_client", c.ClientIP())
		JSONError(c, CodeUnauthorized, "Invalid client credentials")
		return
	}

	// 计算有效的scope：若请求为空，默认使用客户端预设；否则必须是预设的子集
	allowed := strings.Fields(strings.TrimSpace(client.Scopes))
	requested := strings.Fields(strings.TrimSpace(req.Scope))
	if len(requested) == 0 {
		requested = allowed
	} else {
		// 校验子集
		allowedSet := map[string]struct{}{}
		for _, s := range allowed {
			allowedSet[s] = struct{}{}
		}
		for _, s := range requested {
			if _, ok := allowedSet[s]; !ok {
				metrics.RecordBusinessOperation(c.Request.Context(), "token_request", false, time.Since(start), "invalid_scope")
				JSONError(c, CodeUnauthorized, "Requested scope is not allowed")
				return
			}
		}
	}
	scopeStr := strings.Join(requested, " ")

	if h.service.Redis != nil {
		key := "oauth:token:" + req.ClientID + ":" + scopeStr
		if val, err := h.service.Redis.Get(c.Request.Context(), key).Result(); err == nil && val != "" {
			var ct struct {
				AccessToken  string
				RefreshToken string
				ExpiresAt    int64
			}
			if json.Unmarshal([]byte(val), &ct) == nil && time.Now().Unix() < ct.ExpiresAt {
				remain := ct.ExpiresAt - time.Now().Unix()
				if remain > int64((5 * time.Minute).Seconds()) {
					middleware.RecordBusinessOperation("token_request", true, time.Since(start), "cache_hit_redis")
					JSONSuccess(c, TokenResponse{
						AccessToken:  ct.AccessToken,
						TokenType:    "Bearer",
						ExpiresIn:    int(remain),
						RefreshToken: ct.RefreshToken,
						Scope:        scopeStr,
					})
					return
				}
			}
		}
	}

	var existing models.OAuthToken
	if err := h.service.DB.Where("client_id = ? AND scopes = ? AND expires_at > ?", req.ClientID, scopeStr, time.Now()).Order("expires_at desc").First(&existing).Error; err == nil {
		remain := int(time.Until(existing.ExpiresAt).Seconds())
		if remain > int((5 * time.Minute).Seconds()) {
			middleware.RecordBusinessOperation("token_request", true, time.Since(start), "cache_hit_db")
			fmt.Printf("output: %v\n", "db query")
			if h.service.Redis != nil {
				key := "oauth:token:" + req.ClientID + ":" + scopeStr
				payload, _ := json.Marshal(map[string]interface{}{"AccessToken": existing.AccessToken, "RefreshToken": existing.RefreshToken, "ExpiresAt": existing.ExpiresAt.Unix()})
				_ = h.service.Redis.Set(c.Request.Context(), key, string(payload), time.Until(existing.ExpiresAt)).Err()
			}
			JSONSuccess(c, TokenResponse{
				AccessToken:  existing.AccessToken,
				TokenType:    "Bearer",
				ExpiresIn:    remain,
				RefreshToken: existing.RefreshToken,
				Scope:        scopeStr,
			})
			return
		}
	}

	ttl := 24 * time.Hour
	if client.TokenTTLSeconds > 0 {
		ttl = time.Duration(client.TokenTTLSeconds) * time.Second
	}
	accessToken, refreshToken, err := h.generateTokensWithTTL(req.ClientID, "", scopeStr, ttl)
	if err != nil {
		logger.GetLogger().WithError(err).Error("token generation failed")
		metrics.RecordBusinessOperation(c.Request.Context(), "token_request", false, time.Since(start), "token_generation_failed")
		JSONError(c, CodeInternalError, "Token generation failed")
		return
	}

	token := &models.OAuthToken{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ClientID:     req.ClientID,
		Scopes:       scopeStr,
		ExpiresAt:    time.Now().Add(ttl),
	}
	if err := h.service.DB.Create(token).Error; err != nil {
		logger.GetLogger().WithError(err).Error("save token failed")
		metrics.RecordBusinessOperation(c.Request.Context(), "token_request", false, time.Since(start), "database_error")
		JSONError(c, CodeDatabaseError, "Failed to save token")
		return
	}

	if h.service.Redis != nil {
		key := "oauth:token:" + req.ClientID + ":" + scopeStr
		payload, _ := json.Marshal(map[string]interface{}{"AccessToken": accessToken, "RefreshToken": refreshToken, "ExpiresAt": time.Now().Add(ttl).Unix()})
		_ = h.service.Redis.Set(c.Request.Context(), key, string(payload), ttl).Err()
	}

	metrics.RecordBusinessOperation(c.Request.Context(), "token_request", true, time.Since(start), "created_or_refreshed")
	JSONSuccess(c, TokenResponse{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    int(ttl.Seconds()),
		RefreshToken: refreshToken,
		Scope:        scopeStr,
	})
}

// RefreshToken Refresh Access Token
// @Summary Refresh Access Token
// @Description Use the refresh token to obtain a new access token
// @Tags Auth
// @Tags Public
// @Accept json
// @Produce json
// @Param request body RefreshTokenRequest true "Refresh token request"
// @Success 200 {object} SuccessResponse
// @Router /auth/refresh [post]
func (h *AuthHandler) RefreshToken(c *gin.Context) { //ignore_security_alert IDOR
	var req RefreshTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		JSONError(c, CodeInvalidParameter, "Invalid request body")
		return
	}

	// 验证刷新令牌
	var token models.OAuthToken
	if err := h.service.DB.Where("refresh_token = ? AND client_id = ?", req.RefreshToken, req.ClientID).First(&token).Error; err != nil {
		JSONError(c, CodeUnauthorized, "刷新令牌无效")
		return
	}

	// 检查令牌是否过期
	if time.Now().After(token.ExpiresAt) {
		JSONError(c, CodeUnauthorized, "刷新令牌已过期")
		return
	}

	// 生成新的访问令牌
	var client models.OAuthClient
	_ = h.service.DB.Where("id = ?", token.ClientID).First(&client).Error
	ttl := 24 * time.Hour
	if client.TokenTTLSeconds > 0 {
		ttl = time.Duration(client.TokenTTLSeconds) * time.Second
	}
	accessToken, newRefreshToken, err := h.generateTokensWithTTL(req.ClientID, token.UserID, token.Scopes, ttl)
	if err != nil {
		logger.GetLogger().WithError(err).Error("生成令牌失败")
		JSONError(c, CodeInternalError, "生成令牌失败")
		return
	}

	// 更新令牌记录
	token.AccessToken = accessToken
	token.RefreshToken = newRefreshToken
	token.ExpiresAt = time.Now().Add(ttl)

	if err := h.service.DB.Save(&token).Error; err != nil {
		logger.GetLogger().WithError(err).Error("更新令牌失败")
		JSONError(c, CodeDatabaseError, "更新令牌失败")
		return
	}

	if h.service.Redis != nil {
		key := "oauth:token:" + token.ClientID + ":" + token.Scopes
		payload, _ := json.Marshal(map[string]interface{}{"AccessToken": accessToken, "RefreshToken": newRefreshToken, "ExpiresAt": token.ExpiresAt.Unix()})
		_ = h.service.Redis.Set(c.Request.Context(), key, string(payload), time.Until(token.ExpiresAt)).Err()
	}

	// 返回标准格式的成功响应
	JSONSuccess(c, TokenResponse{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    86400, // 24小时
		RefreshToken: newRefreshToken,
		Scope:        token.Scopes,
	})
}

// generateTokens 生成访问令牌和刷新令牌
func (h *AuthHandler) generateTokensWithTTL(clientID, userID, scope string, ttl time.Duration) (string, string, error) {
	// 生成访问令牌
	accessTokenClaims := jwt.MapClaims{
		"client_id": clientID,
		"user_id":   userID,
		"scope":     scope,
		"org_id": func() string {
			var oc models.OAuthClient
			if err := h.service.DB.Where("id = ?", clientID).First(&oc).Error; err == nil {
				return oc.OrgID
			}
			return ""
		}(),
		"exp": time.Now().Add(ttl).Unix(),
		"iat": time.Now().Unix(),
	}

	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessTokenClaims)
	accessTokenString, err := accessToken.SignedString([]byte(h.service.Config.Security.JWTSecret))
	if err != nil {
		return "", "", err
	}

	// 生成刷新令牌
	refreshTokenClaims := jwt.MapClaims{
		"client_id": clientID,
		"user_id":   userID,
		"exp":       time.Now().Add(7 * 24 * time.Hour).Unix(),
		"iat":       time.Now().Unix(),
	}

	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshTokenClaims)
	refreshTokenString, err := refreshToken.SignedString([]byte(h.service.Config.Security.JWTSecret))
	if err != nil {
		return "", "", err
	}

	return accessTokenString, refreshTokenString, nil
}

type RevokeRequest struct {
	Token         string `json:"token"`
	TokenTypeHint string `json:"token_type_hint"`
	ClientID      string `json:"client_id"`
}

func (h *AuthHandler) Revoke(c *gin.Context) {
	var req RevokeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		JSONError(c, CodeInvalidParameter, "参数绑定失败")
		return
	}
	var tok models.OAuthToken
	if req.TokenTypeHint == "refresh_token" {
		_ = h.service.DB.Where("refresh_token = ? AND client_id = ?", req.Token, req.ClientID).First(&tok).Error
	} else {
		_ = h.service.DB.Where("access_token = ? AND client_id = ?", req.Token, req.ClientID).First(&tok).Error
	}
	if tok.ID == 0 {
		JSONSuccess(c, gin.H{"revoked": false})
		return
	}
	if err := h.service.DB.Delete(&tok).Error; err != nil {
		JSONError(c, CodeDatabaseError, "撤销失败")
		return
	}
	if h.service.Redis != nil {
		key := "oauth:token:" + tok.ClientID + ":" + tok.Scopes
		_ = h.service.Redis.Del(c.Request.Context(), key).Err()
	}
	h.service.RecordAuditLog(c, "oauth.revoke", "oauth", req.ClientID, "success", "")
	JSONSuccess(c, gin.H{"revoked": true})
}

type IntrospectRequest struct {
	Token    string `json:"token"`
	ClientID string `json:"client_id"`
}

func (h *AuthHandler) Introspect(c *gin.Context) {
	var req IntrospectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		JSONError(c, CodeInvalidParameter, "参数绑定失败")
		return
	}
	var tok models.OAuthToken
	_ = h.service.DB.Where("access_token = ? AND client_id = ?", req.Token, req.ClientID).First(&tok).Error
	if tok.ID == 0 {
		_ = h.service.DB.Where("refresh_token = ? AND client_id = ?", req.Token, req.ClientID).First(&tok).Error
	}
	active := false
	scope := ""
	clientID := ""
	exp := int64(0)
	iat := int64(0)
	if tok.ID != 0 && time.Now().Before(tok.ExpiresAt) {
		active = true
		scope = tok.Scopes
		clientID = tok.ClientID
	}
	token, err := jwt.Parse(req.Token, func(token *jwt.Token) (interface{}, error) { return []byte(h.service.Config.Security.JWTSecret), nil })
	if err == nil && token.Valid {
		if claims, ok := token.Claims.(jwt.MapClaims); ok {
			if v, ok := claims["exp"].(float64); ok {
				exp = int64(v)
			}
			if v, ok := claims["iat"].(float64); ok {
				iat = int64(v)
			}
			if v, ok := claims["scope"].(string); ok && scope == "" {
				scope = v
			}
		}
	}
	JSONSuccess(c, gin.H{"active": active, "client_id": clientID, "scope": scope, "exp": exp, "iat": iat})
}
