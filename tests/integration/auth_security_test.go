package integration

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

// TestRBACVerticalEscalation 验证垂直越权 (Viewer 角色尝试管理操作)
func (s *AuthSecuritySuite) TestRBACVerticalEscalation() {
	// 1. 获取一个普通组织
	var orgID string
	s.Ctx.App.DB.Raw("SELECT id FROM organizations WHERE name = 'E2E Test Org' LIMIT 1").Scan(&orgID)
	require.NotEmpty(s.T(), orgID)

	// 2. 创建一个 viewer 角色用户
	viewerEmail := "viewer_rbac@test.com"
	viewerID := "11111111-1111-1111-1111-111111111111"
	s.Ctx.App.DB.Exec(`INSERT INTO users (id, email, name, role, org_id, org_role, current_org_id, status) 
		VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (id) DO NOTHING`,
		viewerID, viewerEmail, "Viewer User", "user", orgID, "viewer", orgID, "active")

	s.Ctx.App.DB.Exec(`INSERT INTO organization_members (id, organization_id, user_id, role, status) 
		VALUES (?, ?, ?, ?, ?) ON CONFLICT (id) DO NOTHING`,
		"22222222-2222-2222-2222-222222222222", orgID, viewerID, "viewer", "active")

	// 3. 尝试调用只有 admin/owner 能调用的接口：邀请新成员
	reqBody := map[string]interface{}{
		"email": "hacker_invite@test.com",
		"role":  "admin",
	}

	var errResp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}

	// 期望返回 403 Forbidden
	resp := s.Ctx.NewRequest(s.T(), "POST", "/api/v1/orgs/invitations").
		AsSpecificUser(viewerID, orgID).
		WithJSON(reqBody).
		ExpectStatus(403)

	err := json.Unmarshal(resp.Body.Bytes(), &errResp)
	require.NoError(s.T(), err)
	require.Contains(s.T(), errResp.Message, "Forbidden")

	s.T().Log("✅ 垂直越权 (Viewer -> Admin Action) 拦截验证通过")

	// 清理数据
	s.Ctx.App.DB.Exec("DELETE FROM organization_members WHERE user_id = ?", viewerID)
	s.Ctx.App.DB.Exec("DELETE FROM users WHERE id = ?", viewerID)
}

// TestRBACHorizontalEscalation 验证横向越权 (User A 尝试访问 User B 的组织数据)
func (s *AuthSecuritySuite) TestRBACHorizontalEscalation() {
	// 1. 找到当前测试默认组织 (Org A)
	var orgA string
	s.Ctx.App.DB.Raw("SELECT id FROM organizations WHERE name = 'E2E Test Org' LIMIT 1").Scan(&orgA)

	// 2. 创建一个完全不相干的组织 (Org B) 和用户 (User B)
	orgB := "33333333-3333-3333-3333-333333333333"
	userB_ID := "44444444-4444-4444-4444-444444444444"
	s.Ctx.App.DB.Exec(`INSERT INTO organizations (id, name, plan_id, status) VALUES (?, ?, ?, ?) ON CONFLICT (id) DO NOTHING`, orgB, "Hacker Org", "starter", "active")
	s.Ctx.App.DB.Exec(`INSERT INTO users (id, email, name, role, org_id, org_role, current_org_id, status) 
		VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (id) DO NOTHING`,
		userB_ID, "hacker_horizontal@test.com", "Hacker", "user", orgB, "owner", orgB, "active")

	// 3. 尝试使用 User B (Org B) 的身份去拉取 Org A 的用量数据
	// 注意：/api/v1/orgs/:org_id/usage/summary 这个接口必须检查用户是否属于目标 org_id
	var errResp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}

	resp := s.Ctx.NewRequest(s.T(), "GET", "/api/v1/orgs/"+orgA+"/usage/summary").
		AsSpecificUser(userB_ID, orgB).
		ExpectStatus(403) // 应该是 403 或者是 404

	err := json.Unmarshal(resp.Body.Bytes(), &errResp)
	require.NoError(s.T(), err)
	// 根据你的实现，可能是 403 Forbidden 或者是通过中间件直接拦截
	require.Contains(s.T(), errResp.Message, "Forbidden")

	s.T().Log("✅ 横向越权 (Cross-Org Access) 拦截验证通过")

	// 清理数据
	s.Ctx.App.DB.Exec("DELETE FROM users WHERE id = ?", userB_ID)
	s.Ctx.App.DB.Exec("DELETE FROM organizations WHERE id = ?", orgB)
}
