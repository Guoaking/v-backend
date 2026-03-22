package api

import (
	"context"
	"testing"
	"time"

	"kyc-service/internal/config"
	"kyc-service/internal/models"
	"kyc-service/internal/service"
	"kyc-service/internal/worker"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupTestService 初始化测试用的 KYCService 和 DB
func setupTestService(t *testing.T) (*service.KYCService, *gorm.DB) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	// 自动迁移模型
	err = db.AutoMigrate(
		&models.User{},
		&models.Organization{},
		&models.OrganizationMember{},
		&models.OrganizationInvitation{},
		&models.UsageLog{},
		&models.UsageMetricAgg{},
		&models.OAuthClient{},
	)
	require.NoError(t, err)

	svc := &service.KYCService{
		DB:        db,
		Config:    &config.Config{},
		LogWorker: &worker.DummyLogWorker{},
	}

	return svc, db
}

// setupRouter 初始化测试用的 Gin 路由
func setupRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	return r
}

// GenerateTestToken 生成用于测试的有效 JWT Token
// secret 应该与测试配置中的 JWTSecret 一致
func GenerateTestToken(t *testing.T, userID, orgID, role string, secret string) string {
	if secret == "" {
		secret = "your-secret-key-here-must-be-32-by" // 默认测试密钥
	}

	generator := NewJWTTokenGenerator(secret, "kyc-service-test")

	params := TokenParams{
		Issuer:     "kyc-service-test",
		Subject:    userID,
		Audience:   []string{"kyc-console"},
		Expiration: time.Hour,
		CustomClaims: map[string]interface{}{
			"user_id": userID,
			"org_id":  orgID, // 注意：中间件可能会根据 user 查找 org，但 token 中也可能包含
			"role":    role,
		},
		Secret: secret,
	}

	resp, err := generator.GenerateToken(context.Background(), params)
	require.NoError(t, err, "Failed to generate test token")

	return resp.AccessToken
}
