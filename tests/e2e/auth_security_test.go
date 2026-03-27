package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// AuthSecuritySuite 专门测试认证、授权与安全边界
type AuthSecuritySuite struct {
	BaseSuite
}

func TestAuthSecuritySuite(t *testing.T) {
	suite.Run(t, new(AuthSecuritySuite))
}

// TestSmoke_LoginRegister [SMOKE] 验证注册登录核心流程
func (s *AuthSecuritySuite) TestSmoke_LoginRegister() {
	email := fmt.Sprintf("e2e_%d@test.com", time.Now().UnixNano())
	// 1. Register
	regReq := map[string]string{
		"full_name": "Smoke User",
		"email":     email,
		"company":   "Smoke Company",
		"password":  "password123",
	}
	body, _ := json.Marshal(regReq)
	s.Ctx.NewRequest(s.T(), "POST", "/api/v1/auth/register").
		WithBody(bytes.NewReader(body)).
		WithHeader("Content-Type", "application/json").
		ExpectSuccess()

	// 2. Login
	loginReq := map[string]string{"email": email, "password": "password123"}
	lBody, _ := json.Marshal(loginReq)
	var loginResp struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	s.Ctx.NewRequest(s.T(), "POST", "/api/v1/auth/login").
		WithBody(bytes.NewReader(lBody)).
		WithHeader("Content-Type", "application/json").
		ExpectJSON(&loginResp)

	require.NotEmpty(s.T(), loginResp.Data.AccessToken)
	s.T().Log("✅ Smoke: 注册登录流程验证通过")
}

// TestSecurityBoundaries 验证权限隔离与越权拦截
func (s *AuthSecuritySuite) TestSecurityBoundaries() {
	// 1. User -> Admin API
	s.Ctx.NewRequest(s.T(), "GET", "/api/v1/admin/stats/overview").
		AsUser().
		ExpectForbidden()

	// 2. App -> Console API
	s.Ctx.NewRequest(s.T(), "GET", "/api/v1/console/users/me").
		AsApp().
		ExpectUnauthorized()

	s.T().Log("✅ 安全边界拦截验证通过")
}

// TestPlaygroundAuth 验证 STS 临时令牌
func (s *AuthSecuritySuite) TestPlaygroundAuth() {
	s.Ctx.NewRequest(s.T(), "GET", "/api/v1/console/users/me").
		AsPlayground().
		ExpectSuccess()
}

// TestMetaAPIs 验证基础元数据接口
func (s *AuthSecuritySuite) TestMetaAPIs() {
	s.Ctx.NewRequest(s.T(), "GET", "/api/v1/meta/permissions").ExpectSuccess()
	s.Ctx.NewRequest(s.T(), "GET", "/api/v1/meta/roles").ExpectSuccess()
}
