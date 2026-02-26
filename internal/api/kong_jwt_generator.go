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

// KongJWTGenerator Kong JWT令牌生成器
type KongJWTGenerator struct {
	defaultSecret string
	defaultAlg    string
	issuer        string
}

// NewKongJWTGenerator 创建Kong兼容的JWT生成器
func NewKongJWTGenerator(defaultSecret, issuer string) *KongJWTGenerator {
	return &KongJWTGenerator{
		defaultSecret: defaultSecret,
		defaultAlg:    "HS256",
		issuer:        issuer,
	}
}

// KongJWTRequest Kong JWT生成请求
type KongJWTRequest struct {
	ConsumerID   string                 `json:"consumer_id"`   // JWT消费者ID
	Key          string                 `json:"key"`           // JWT key (iss)，如果不指定则使用consumer_id
	Secret       string                 `json:"secret"`        // JWT secret，如果不指定则使用默认密钥
	Algorithm    string                 `json:"algorithm"`     // 算法，默认HS256
	Expiration   int64                  `json:"expiration"`    // 过期时间（秒），默认3600
	CustomClaims map[string]interface{} `json:"custom_claims"` // 自定义声明
}

// KongJWTResponse Kong JWT生成响应
type KongJWTResponse struct {
	Token     string    `json:"token"`
	TokenType string    `json:"token_type"`
	ExpiresIn int64     `json:"expires_in"`
	Key       string    `json:"key"`
	Algorithm string    `json:"algorithm"`
	CreatedAt time.Time `json:"created_at"`
}

// KongJWTClaims Kong JWT声明结构
type KongJWTClaims struct {
	ConsumerID string                 `json:"consumer_id"`
	Key        string                 `json:"key"`
	IssuedAt   int64                  `json:"iat"`
	Expiration int64                  `json:"exp"`
	NotBefore  int64                  `json:"nbf"`
	Custom     map[string]interface{} `json:"custom,omitempty"`
}

// GenerateKongJWTHandler Kong JWT生成处理器
func (g *KongJWTGenerator) GenerateKongJWTHandler(c *gin.Context) {
	ctx := c.Request.Context()
	requestID := c.GetString("request_id")

	// 创建span用于链路追踪
	tracer := otel.Tracer("kong-jwt-generator")
	ctx, span := tracer.Start(ctx, "GenerateKongJWT", trace.WithAttributes(
		attribute.String("request.id", requestID),
		attribute.String("operation", "kong_jwt_generation"),
	))
	defer span.End()

	// 记录请求开始
	logger.GetLogger().WithFields(map[string]interface{}{
		"request_id": requestID,
		"operation":  "kong_jwt_generation",
		"client_ip":  c.ClientIP(),
		"user_agent": c.Request.UserAgent(),
	}).Info("Kong JWT令牌生成请求开始")

	// 记录指标
	startTime := time.Now()

	var req KongJWTRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		span.RecordError(err)
		span.SetAttributes(attribute.String("error.type", "validation_error"))

		logger.GetLogger().WithError(err).WithField("request_id", requestID).Warn("Kong JWT参数验证失败")

		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "参数验证失败",
			"message": err.Error(),
		})
		return
	}

	// 设置默认值和验证必填字段
	if req.ConsumerID == "" && req.Key == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "参数验证失败",
			"message": "consumer_id或key必须至少提供一个",
		})
		return
	}

	if req.Key == "" {
		req.Key = req.ConsumerID // 使用consumer_id作为默认key
	}
	if req.ConsumerID == "" {
		req.ConsumerID = req.Key // 使用key作为默认consumer_id
	}
	if req.Secret == "" {
		req.Secret = g.defaultSecret
	}
	if req.Algorithm == "" {
		req.Algorithm = g.defaultAlg
	}
	if req.Expiration == 0 {
		req.Expiration = 3600 // 默认1小时
	}

	// 验证参数
	if err := g.validateKongJWTRequest(&req); err != nil {
		span.RecordError(err)
		span.SetAttributes(attribute.String("error.type", "validation_error"))

		logger.GetLogger().WithError(err).WithField("request_id", requestID).Warn("Kong JWT参数验证失败")

		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	// 生成JWT令牌
	token, err := g.generateKongJWTToken(ctx, &req)
	if err != nil {
		span.RecordError(err)
		span.SetAttributes(attribute.String("error.type", "generation_error"))

		logger.GetLogger().WithError(err).WithField("request_id", requestID).Error("Kong JWT令牌生成失败")

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "令牌生成失败",
		})
		return
	}

	// 记录成功指标
	duration := time.Since(startTime)
	metrics.RecordJWTGeneration(req.Key, req.Algorithm, true)
	metrics.RecordJWTGenerationDuration(req.Key, req.Algorithm, duration)

	// 记录成功日志
	logger.GetLogger().WithFields(map[string]interface{}{
		"request_id":  requestID,
		"operation":   "kong_jwt_generation",
		"consumer_id": req.ConsumerID,
		"key":         req.Key,
		"algorithm":   req.Algorithm,
		"expires_in":  req.Expiration,
		"duration_ms": duration.Milliseconds(),
	}).Info("Kong JWT令牌生成成功")

	// 设置span属性
	span.SetAttributes(
		attribute.String("jwt.consumer_id", req.ConsumerID),
		attribute.String("jwt.key", req.Key),
		attribute.String("jwt.algorithm", req.Algorithm),
		attribute.Int64("jwt.expires_in", req.Expiration),
	)

	c.JSON(http.StatusOK, KongJWTResponse{
		Token:     token,
		TokenType: "Bearer",
		ExpiresIn: req.Expiration,
		Key:       req.Key,
		Algorithm: req.Algorithm,
		CreatedAt: time.Now(),
	})
}

// generateKongJWTToken 生成Kong兼容的JWT令牌
func (g *KongJWTGenerator) generateKongJWTToken(ctx context.Context, req *KongJWTRequest) (string, error) {
	tracer := otel.Tracer("kong-jwt-generator")
	_, span := tracer.Start(ctx, "GenerateKongJWTToken", trace.WithAttributes(
		attribute.String("jwt.consumer_id", req.ConsumerID),
		attribute.String("jwt.key", req.Key),
	))
	defer span.End()

	now := time.Now()
	issuedAt := now.Unix()
	expiration := now.Add(time.Duration(req.Expiration) * time.Second).Unix()
	notBefore := issuedAt

	// 创建声明 - 添加Kong JWT插件需要的iss声明
	claims := jwt.MapClaims{
		"iss":         req.Key, // Kong JWT插件使用iss声明作为key查找凭据
		"consumer_id": req.ConsumerID,
		"key":         req.Key,
		"iat":         issuedAt,
		"exp":         expiration,
		"nbf":         notBefore,
	}

	// 添加自定义声明
	for k, v := range req.CustomClaims {
		claims[k] = v
	}

	// 创建令牌
	token := jwt.NewWithClaims(jwt.GetSigningMethod(req.Algorithm), claims)

	// 签名令牌
	tokenString, err := token.SignedString([]byte(req.Secret))
	if err != nil {
		span.RecordError(err)
		logger.GetLogger().WithError(err).Error("Kong JWT签名失败")
		return "", fmt.Errorf("JWT签名失败: %w", err)
	}

	return tokenString, nil
}

// validateKongJWTRequest 验证Kong JWT请求参数
func (g *KongJWTGenerator) validateKongJWTRequest(req *KongJWTRequest) error {
	if req.Expiration < 0 {
		return fmt.Errorf("expiration不能为负数")
	}
	if req.Expiration > 86400*365 { // 最大1年
		return fmt.Errorf("expiration不能超过1年")
	}
	if req.Algorithm != "" && req.Algorithm != "HS256" && req.Algorithm != "HS384" && req.Algorithm != "HS512" {
		return fmt.Errorf("不支持的算法: %s", req.Algorithm)
	}
	return nil
}

// GenerateKongCompatibleToken 生成与Kong JWT插件兼容的令牌
func (g *KongJWTGenerator) GenerateKongCompatibleToken(consumerID, key, secret string, expiration int64, customClaims map[string]interface{}) (string, error) {
	req := &KongJWTRequest{
		ConsumerID:   consumerID,
		Key:          key,
		Secret:       secret,
		Expiration:   expiration,
		CustomClaims: customClaims,
	}

	return g.generateKongJWTToken(context.Background(), req)
}

// CreateKongJWTForConsumer 为指定消费者创建Kong JWT令牌
func (g *KongJWTGenerator) CreateKongJWTForConsumer(consumerID string, expiration int64) (string, error) {
	return g.GenerateKongCompatibleToken(consumerID, consumerID, g.defaultSecret, expiration, nil)
}
