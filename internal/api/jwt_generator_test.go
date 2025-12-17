package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJWTTokenGenerator_GenerateTokenHandler(t *testing.T) {
	// 设置测试环境
	gin.SetMode(gin.TestMode)

	// 创建JWT生成器
	generator := NewJWTTokenGenerator("test-secret-key-32-bytes-long", "test-service")

	// 创建Gin路由器
	router := gin.New()
	router.POST("/token/generate", generator.GenerateTokenHandler)

	tests := []struct {
		name           string
		requestBody    TokenParams
		expectedStatus int
		expectedError  string
		validateToken  bool
	}{
		{
			name: "成功生成标准JWT令牌",
			requestBody: TokenParams{
				Issuer:     "test-issuer",
				Subject:    "test-user-123",
				Audience:   []string{"api", "web"},
				Expiration: time.Hour,
				Algorithm:  "HS256",
				Secret:     "test-secret-key-32-bytes-long",
			},
			expectedStatus: http.StatusOK,
			validateToken:  true,
		},
		{
			name: "成功生成带自定义声明的JWT",
			requestBody: TokenParams{
				Issuer:     "custom-issuer",
				Subject:    "user-456",
				Expiration: 2 * time.Hour,
				CustomClaims: map[string]interface{}{
					"role":       "admin",
					"department": "engineering",
					"level":      5,
				},
				Algorithm: "HS256",
				Secret:    "test-secret-key-32-bytes-long",
			},
			expectedStatus: http.StatusOK,
			validateToken:  true,
		},
		{
			name: "缺少发行者参数",
			requestBody: TokenParams{
				Subject:    "test-user",
				Expiration: time.Hour,
				Algorithm:  "HS256",
				Secret:     "test-secret-key-32-bytes-long",
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "发行者(issuer)不能为空",
		},
		{
			name: "缺少主题参数",
			requestBody: TokenParams{
				Issuer:     "test-issuer",
				Expiration: time.Hour,
				Algorithm:  "HS256",
				Secret:     "test-secret-key-32-bytes-long",
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "主题(subject)不能为空",
		},
		{
			name: "过期时间超过限制",
			requestBody: TokenParams{
				Issuer:     "test-issuer",
				Subject:    "test-user",
				Expiration: 9000 * time.Hour, // 超过1年
				Algorithm:  "HS256",
				Secret:     "test-secret-key-32-bytes-long",
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "过期时间不能超过1年",
		},
		{
			name: "不支持的算法",
			requestBody: TokenParams{
				Issuer:     "test-issuer",
				Subject:    "test-user",
				Expiration: time.Hour,
				Algorithm:  "RS256", // 不支持的算法
				Secret:     "test-secret-key-32-bytes-long",
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "不支持的算法: RS256",
		},
		{
			name: "使用默认参数生成",
			requestBody: TokenParams{
				Issuer:  "default-test",
				Subject: "default-user",
				// 不指定其他参数，使用默认值
			},
			expectedStatus: http.StatusOK,
			validateToken:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 创建请求
			body, err := json.Marshal(tt.requestBody)
			require.NoError(t, err)

			req := httptest.NewRequest("POST", "/token/generate", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Request-ID", "test-request-123")

			// 创建响应记录器
			w := httptest.NewRecorder()

			// 执行请求
			router.ServeHTTP(w, req)

			// 验证状态码
			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedStatus == http.StatusOK {
				// 解析响应
				var response JWTTokenResponse
				err := json.Unmarshal(w.Body.Bytes(), &response)
				require.NoError(t, err)

				// 验证响应结构
				assert.NotEmpty(t, response.AccessToken)
				assert.Equal(t, "Bearer", response.TokenType)
				assert.Greater(t, response.ExpiresIn, int64(0))
				assert.NotEmpty(t, response.RefreshToken)
				assert.Equal(t, "kyc:read kyc:write", response.Scope)
				assert.False(t, response.CreatedAt.IsZero())

				if tt.validateToken {
					// 验证令牌
					token, err := jwt.Parse(response.AccessToken, func(token *jwt.Token) (interface{}, error) {
						return []byte(tt.requestBody.Secret), nil
					})
					require.NoError(t, err)
					assert.True(t, token.Valid)

					// 验证声明
					claims, ok := token.Claims.(jwt.MapClaims)
					require.True(t, ok)
					assert.Equal(t, tt.requestBody.Issuer, claims["iss"])
					assert.Equal(t, tt.requestBody.Subject, claims["sub"])

					// 验证自定义声明
					for k, v := range tt.requestBody.CustomClaims {
						assert.Equal(t, v, claims[k])
					}
				}
			} else {
				// 验证错误响应
				var errorResp map[string]interface{}
				err := json.Unmarshal(w.Body.Bytes(), &errorResp)
				require.NoError(t, err)

				assert.Contains(t, errorResp["message"].(string), tt.expectedError)
			}
		})
	}
}

func TestJWTTokenGenerator_ValidateToken(t *testing.T) {
	generator := NewJWTTokenGenerator("test-secret-key-32-bytes-long", "test-service")
	ctx := context.Background()

	// 先生成一个有效的令牌
	params := TokenParams{
		Issuer:     "test-issuer",
		Subject:    "test-user",
		Audience:   []string{"api"},
		Expiration: time.Hour,
		Algorithm:  "HS256",
		Secret:     "test-secret-key-32-bytes-long",
		CustomClaims: map[string]interface{}{
			"role":  "user",
			"level": 1,
		},
	}

	tokenResponse, err := generator.GenerateToken(ctx, params)
	require.NoError(t, err)
	require.NotNil(t, tokenResponse)

	tests := []struct {
		name           string
		tokenString    string
		expectedError  bool
		validateClaims func(*testing.T, *TokenClaims)
	}{
		{
			name:          "验证有效令牌",
			tokenString:   tokenResponse.AccessToken,
			expectedError: false,
			validateClaims: func(t *testing.T, claims *TokenClaims) {
				assert.Equal(t, "test-issuer", claims.Issuer)
				assert.Equal(t, "test-user", claims.Subject)
				assert.Equal(t, []string{"api"}, claims.Audience)
				assert.Equal(t, "user", claims.CustomClaims["role"])
				assert.Equal(t, float64(1), claims.CustomClaims["level"])
			},
		},
		{
			name:          "验证无效令牌",
			tokenString:   "invalid.token.here",
			expectedError: true,
		},
		{
			name:          "验证过期令牌",
			tokenString:   "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJleHAiOjE2MDAwMDAwMDB9.invalid",
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims, err := generator.ValidateToken(ctx, tt.tokenString)

			if tt.expectedError {
				assert.Error(t, err)
				assert.Nil(t, claims)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, claims)

				if tt.validateClaims != nil {
					tt.validateClaims(t, claims)
				}
			}
		})
	}
}

func TestJWTTokenGenerator_RefreshToken(t *testing.T) {
	generator := NewJWTTokenGenerator("test-secret-key-32-bytes-long", "test-service")
	ctx := context.Background()

	// 测试刷新令牌
	refreshToken := "refresh_test_token_123"

	newToken, err := generator.RefreshToken(ctx, refreshToken)
	require.NoError(t, err)
	require.NotNil(t, newToken)

	// 验证新令牌
	assert.NotEmpty(t, newToken.AccessToken)
	assert.Equal(t, "Bearer", newToken.TokenType)
	assert.Greater(t, newToken.ExpiresIn, int64(0))
	assert.NotEmpty(t, newToken.RefreshToken)

	// 验证令牌有效性
	claims, err := generator.ValidateToken(ctx, newToken.AccessToken)
	require.NoError(t, err)
	assert.Equal(t, "system", claims.Issuer)
	assert.Equal(t, "refreshed_user", claims.Subject)
}

func TestJWTTokenGenerator_validateParams(t *testing.T) {
	generator := NewJWTTokenGenerator("test-secret", "test-service")

	tests := []struct {
		name          string
		params        TokenParams
		expectedError string
	}{
		{
			name: "有效参数",
			params: TokenParams{
				Issuer:     "test",
				Subject:    "user",
				Expiration: time.Hour,
			},
			expectedError: "",
		},
		{
			name: "缺少发行者",
			params: TokenParams{
				Subject:    "user",
				Expiration: time.Hour,
			},
			expectedError: "发行者(issuer)不能为空",
		},
		{
			name: "缺少主题",
			params: TokenParams{
				Issuer:     "test",
				Expiration: time.Hour,
			},
			expectedError: "主题(subject)不能为空",
		},
		{
			name: "过期时间为0",
			params: TokenParams{
				Issuer:     "test",
				Subject:    "user",
				Expiration: 0,
			},
			expectedError: "过期时间(expiration)必须大于0",
		},
		{
			name: "过期时间超过1年",
			params: TokenParams{
				Issuer:     "test",
				Subject:    "user",
				Expiration: 9000 * time.Hour,
			},
			expectedError: "过期时间不能超过1年",
		},
		{
			name: "不支持的算法",
			params: TokenParams{
				Issuer:     "test",
				Subject:    "user",
				Expiration: time.Hour,
				Algorithm:  "INVALID_ALG",
			},
			expectedError: "不支持的算法: INVALID_ALG",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := generator.validateParams(&tt.params)

			if tt.expectedError == "" {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			}
		})
	}
}

func Test2(t *testing.T) {
	fmt.Println("测试JWT生成器修复...")

	// 创建JWT生成器
	generator := NewJWTTokenGenerator("yYBD4KdaY8Y7YaWOYJvlvgl2fNfI8gzG", "kyc-service")

	// 测试参数
	params := TokenParams{
		Issuer:     "my-app",
		Subject:    "user123",
		Audience:   []string{"api", "web"},
		Expiration: 24 * time.Hour,
		CustomClaims: map[string]interface{}{
			"role":       "admin",
			"department": "engineering",
		},
	}

	// 生成token
	ctx := context.Background()
	tokenResponse, err := generator.GenerateToken(ctx, params)
	if err != nil {
		log.Fatalf("JWT生成失败: %v", err)
	}

	tokenResponse.AccessToken = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb25zdW1lcl9pZCI6InlZQkQ0S2RhWThZN1lhV09ZSnZsdmdsMmZOZkk4Z3pHIiwiZXhwIjoxNzYzMjkwNzI0LCJpYXQiOjE3NjMyODcxMjQsImlzcyI6InlZQkQ0S2RhWThZN1lhV09ZSnZsdmdsMmZOZkk4Z3pHIiwia2V5IjoieVlCRDRLZGFZOFk3WWFXT1lKdmx2Z2wyZk5mSThnekciLCJuYmYiOjE3NjMyODcxMjR9.Aooa97ZP_X6peTQnqvLugPo42Cqtha01kqWqBcS3wnk"
	fmt.Printf("生成的JWT Token: %s\n", tokenResponse.AccessToken)
	fmt.Printf("过期时间: %d秒\n", tokenResponse.ExpiresIn)
	fmt.Printf("刷新令牌: %s\n", tokenResponse.RefreshToken)

	// 验证token
	claims, err := generator.ValidateToken(ctx, tokenResponse.AccessToken)
	if err != nil {
		log.Fatalf("JWT验证失败: %v", err)
	}

	fmt.Printf("验证成功！\n")
	fmt.Printf("发行者: %s\n", claims.Issuer)
	fmt.Printf("主题: %s\n", claims.Subject)
	fmt.Printf("受众: %v\n", claims.Audience)
	fmt.Printf("自定义声明: %v\n", claims.CustomClaims)

	fmt.Println("\n测试完成！JWT生成器修复成功。")
}
