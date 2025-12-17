package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"kyc-service/pkg/logger"
	"kyc-service/pkg/metrics"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// TokenGenerator JWT令牌生成器接口
type TokenGenerator interface {
	GenerateToken(ctx context.Context, params TokenParams) (*JWTTokenResponse, error)
	ValidateToken(ctx context.Context, token string) (*TokenClaims, error)
	RefreshToken(ctx context.Context, refreshToken string) (*JWTTokenResponse, error)
}

// TokenParams JWT生成参数
type TokenParams struct {
	Issuer     string                 `json:"issuer" binding:"required"`
	Subject    string                 `json:"subject" binding:"required"`
	Audience   []string               `json:"audience"`
	Expiration time.Duration          `json:"expiration"`
	NotBefore  time.Time              `json:"not_before"`
	CustomClaims map[string]interface{} `json:"custom_claims"`
	Algorithm  string                 `json:"algorithm"`
	Secret     string                 `json:"secret"`
}

// JWTTokenResponse JWT令牌响应
type JWTTokenResponse struct {
	AccessToken  string    `json:"access_token"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int64     `json:"expires_in"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	Scope        string    `json:"scope,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// TokenClaims JWT声明
type TokenClaims struct {
	Issuer    string    `json:"iss"`
	Subject   string    `json:"sub"`
	Audience  []string  `json:"aud"`
	ExpiresAt time.Time `json:"exp"`
	NotBefore time.Time `json:"nbf"`
	IssuedAt  time.Time `json:"iat"`
	JWTID     string    `json:"jti"`
	CustomClaims map[string]interface{} `json:"custom,omitempty"`
}

// JWTTokenGenerator JWT令牌生成器实现
type JWTTokenGenerator struct {
	defaultSecret string
	defaultAlg    string
	issuer        string
}

// NewJWTTokenGenerator 创建JWT令牌生成器
func NewJWTTokenGenerator(defaultSecret, issuer string) *JWTTokenGenerator {
	return &JWTTokenGenerator{
		defaultSecret: defaultSecret,
		defaultAlg:    "HS256",
		issuer:        issuer,
	}
}

// GenerateTokenHandler JWT令牌生成接口处理器
func (g *JWTTokenGenerator) GenerateTokenHandler(c *gin.Context) {
	ctx := c.Request.Context()
	requestID := c.GetString("request_id")
	
	// 创建span用于链路追踪
	tracer := otel.Tracer("jwt-generator")
	ctx, span := tracer.Start(ctx, "GenerateToken", trace.WithAttributes(
		attribute.String("request.id", requestID),
		attribute.String("operation", "jwt_generation"),
	))
	defer span.End()
	
	// 记录请求开始
	logger.GetLogger().WithFields(map[string]interface{}{
		"request_id": requestID,
		"operation":  "jwt_generation",
		"client_ip":  c.ClientIP(),
		"user_agent": c.Request.UserAgent(),
	}).Info("JWT令牌生成请求开始")
	
	// 记录指标
	startTime := time.Now()
	metrics.RecordAuthRequest("jwt_generation", true)
	
	var params TokenParams
	if err := c.ShouldBindJSON(&params); err != nil {
		span.RecordError(err)
		span.SetAttributes(attribute.String("error.type", "validation_error"))
		
		metrics.RecordAuthRequest("jwt_generation", false)
		logger.GetLogger().WithError(err).WithField("request_id", requestID).Warn("JWT参数验证失败")
		
		JSONError(c, http.StatusBadRequest, "参数验证失败")
		return
	}
	
	// 验证参数
	if err := g.validateParams(&params); err != nil {
		span.RecordError(err)
		span.SetAttributes(attribute.String("error.type", "validation_error"))
		
		metrics.RecordAuthRequest("jwt_generation", false)
		logger.GetLogger().WithError(err).WithField("request_id", requestID).Warn("JWT参数验证失败")
		
		JSONError(c, http.StatusBadRequest, err.Error())
		return
	}
	
	// 生成令牌
	tokenResponse, err := g.GenerateToken(ctx, params)
	if err != nil {
		span.RecordError(err)
		span.SetAttributes(attribute.String("error.type", "generation_error"))
		
		metrics.RecordAuthRequest("jwt_generation", false)
		logger.GetLogger().WithError(err).WithField("request_id", requestID).Error("JWT令牌生成失败")
		
		JSONError(c, http.StatusInternalServerError, "令牌生成失败")
		return
	}
	
	// 记录成功指标
	duration := time.Since(startTime)
	metrics.RecordHTTPRequest(ctx, "POST", "/api/v1/token/generate", "200", duration)
	
	// 记录成功日志
	logger.GetLogger().WithFields(map[string]interface{}{
		"request_id":    requestID,
		"operation":     "jwt_generation",
		"issuer":        params.Issuer,
		"subject":       params.Subject,
		"algorithm":     params.Algorithm,
		"expires_in":    tokenResponse.ExpiresIn,
		"duration_ms":   duration.Milliseconds(),
	}).Info("JWT令牌生成成功")
	
	// 设置span属性
	span.SetAttributes(
		attribute.String("jwt.issuer", params.Issuer),
		attribute.String("jwt.subject", params.Subject),
		attribute.String("jwt.algorithm", params.Algorithm),
		attribute.Int64("jwt.expires_in", int64(tokenResponse.ExpiresIn)),
		attribute.String("jwt.token_id", tokenResponse.AccessToken[:20]+"..."),
	)
	
	JSONSuccess(c, tokenResponse)
}

// GenerateToken 生成JWT令牌
func (g *JWTTokenGenerator) GenerateToken(ctx context.Context, params TokenParams) (*JWTTokenResponse, error) {
	tracer := otel.Tracer("jwt-generator")
	_, span := tracer.Start(ctx, "GenerateTokenInternal", trace.WithAttributes(
		attribute.String("jwt.issuer", params.Issuer),
		attribute.String("jwt.subject", params.Subject),
	))
	defer span.End()
	
	// 获取当前时间
	now := time.Now()
	
	// 设置默认参数
	if params.Algorithm == "" {
		params.Algorithm = g.defaultAlg
	}
	if params.Secret == "" {
		params.Secret = g.defaultSecret
	}
	if params.Expiration == 0 {
		params.Expiration = 24 * time.Hour
	}
	// 设置默认NotBefore为当前时间
	if params.NotBefore.IsZero() {
		params.NotBefore = now
	}
	
	// 生成JWT ID
	jwtID := fmt.Sprintf("jwt_%s_%d", params.Issuer, now.Unix())
	
	// 创建自定义声明映射
	customClaims := make(jwt.MapClaims)
	for k, v := range params.CustomClaims {
		customClaims[k] = v
	}
	customClaims["iss"] = params.Issuer
	customClaims["sub"] = params.Subject
	customClaims["aud"] = params.Audience
	customClaims["exp"] = now.Add(params.Expiration).Unix()
	customClaims["nbf"] = params.NotBefore.Unix()
	customClaims["iat"] = now.Unix()
	customClaims["jti"] = jwtID
	
	// 创建令牌
	token := jwt.NewWithClaims(jwt.GetSigningMethod(params.Algorithm), customClaims)
	
	// 签名令牌
	tokenString, err := token.SignedString([]byte(params.Secret))
	if err != nil {
		span.RecordError(err)
		logger.GetLogger().WithError(err).Error("JWT签名失败")
		return nil, fmt.Errorf("JWT签名失败: %w", err)
	}
	
	// 生成刷新令牌
	refreshToken := fmt.Sprintf("refresh_%s_%d", jwtID, time.Now().UnixNano())
	
	// 记录指标
	metrics.RecordJWTGeneration(params.Issuer, params.Algorithm, true)
	
	return &JWTTokenResponse{
		AccessToken:  tokenString,
		TokenType:    "Bearer",
		ExpiresIn:    int64(params.Expiration.Seconds()),
		RefreshToken: refreshToken,
		Scope:        "kyc:read kyc:write",
		CreatedAt:    now,
	}, nil
}

// ValidateToken 验证JWT令牌
func (g *JWTTokenGenerator) ValidateToken(ctx context.Context, tokenString string) (*TokenClaims, error) {
	tracer := otel.Tracer("jwt-generator")
	_, span := tracer.Start(ctx, "ValidateToken")
	defer span.End()
	
	// 解析令牌
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		// 验证签名方法
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("不支持的签名方法: %v", token.Header["alg"])
		}
		return []byte(g.defaultSecret), nil
	})
	
	if err != nil {
		span.RecordError(err)
		metrics.RecordJWTValidation(false)
		return nil, fmt.Errorf("令牌验证失败: %w", err)
	}
	
	// 验证令牌有效性
	if !token.Valid {
		span.SetAttributes(attribute.String("validation.result", "invalid"))
		metrics.RecordJWTValidation(false)
		return nil, fmt.Errorf("令牌无效")
	}
	
	// 提取声明
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		span.SetAttributes(attribute.String("validation.result", "claims_error"))
		metrics.RecordJWTValidation(false)
		return nil, fmt.Errorf("无法提取令牌声明")
	}
	
	// 构建响应
	tokenClaims := &TokenClaims{
		CustomClaims: make(map[string]interface{}),
	}
	
	// 提取标准声明
	if iss, ok := claims["iss"].(string); ok {
		tokenClaims.Issuer = iss
	}
	if sub, ok := claims["sub"].(string); ok {
		tokenClaims.Subject = sub
	}
	if aud, ok := claims["aud"].([]interface{}); ok {
		for _, a := range aud {
			if str, ok := a.(string); ok {
				tokenClaims.Audience = append(tokenClaims.Audience, str)
			}
		}
	}
	if exp, ok := claims["exp"].(float64); ok {
		tokenClaims.ExpiresAt = time.Unix(int64(exp), 0)
	}
	if nbf, ok := claims["nbf"].(float64); ok {
		tokenClaims.NotBefore = time.Unix(int64(nbf), 0)
	}
	if iat, ok := claims["iat"].(float64); ok {
		tokenClaims.IssuedAt = time.Unix(int64(iat), 0)
	}
	if jti, ok := claims["jti"].(string); ok {
		tokenClaims.JWTID = jti
	}
	
	// 提取自定义声明
	for k, v := range claims {
		if k != "iss" && k != "sub" && k != "aud" && k != "exp" && k != "nbf" && k != "iat" && k != "jti" {
			tokenClaims.CustomClaims[k] = v
		}
	}
	
	// 记录指标
	metrics.RecordJWTValidation(true)
	span.SetAttributes(attribute.String("validation.result", "success"))
	
	return tokenClaims, nil
}

// RefreshToken 刷新JWT令牌
func (g *JWTTokenGenerator) RefreshToken(ctx context.Context, refreshToken string) (*JWTTokenResponse, error) {
	tracer := otel.Tracer("jwt-generator")
	_, span := tracer.Start(ctx, "RefreshToken")
	defer span.End()
	
	// 验证刷新令牌格式
	if len(refreshToken) < 10 {
		return nil, fmt.Errorf("无效的刷新令牌格式")
	}
	
	// 这里应该验证刷新令牌的有效性
	// 实际实现中需要查询数据库或缓存验证
	
	span.SetAttributes(attribute.String("refresh_token_id", refreshToken[:20]))
	
	// 生成新的访问令牌
	params := TokenParams{
		Issuer:     "system",
		Subject:    "refreshed_user",
		Expiration: 24 * time.Hour,
		Algorithm:  g.defaultAlg,
		Secret:     g.defaultSecret,
	}
	
	return g.GenerateToken(ctx, params)
}

// validateParams 验证参数
func (g *JWTTokenGenerator) validateParams(params *TokenParams) error {
	if params.Issuer == "" {
		return fmt.Errorf("发行者(issuer)不能为空")
	}
	if params.Subject == "" {
		return fmt.Errorf("主题(subject)不能为空")
	}
	if params.Expiration <= 0 {
		return fmt.Errorf("过期时间(expiration)必须大于0")
	}
	if params.Expiration > 8760*time.Hour { // 1年
		return fmt.Errorf("过期时间不能超过1年")
	}
	if params.Algorithm != "" && params.Algorithm != "HS256" && params.Algorithm != "HS384" && params.Algorithm != "HS512" {
		return fmt.Errorf("不支持的算法: %s", params.Algorithm)
	}
	return nil
}