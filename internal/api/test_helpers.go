package api

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

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
