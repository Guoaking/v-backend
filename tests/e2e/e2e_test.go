package e2e

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// KYCFlowSuite 专门测试 KYC 核心业务流的套件
type KYCFlowSuite struct {
	BaseSuite
}

func TestKYCFlowSuite(t *testing.T) {
	suite.Run(t, new(KYCFlowSuite))
}

// TestOCR 验证 AsApp 身份调用 OCR 接口并进行全链路校验
func (s *KYCFlowSuite) TestOCR() {
	// 1. 准备上传数据
	files := map[string][]byte{
		"picture": []byte("fake-id-card-image"),
	}
	fields := map[string]string{
		"type":     "id_card",
		"language": "thai",
	}
	body, contentType := s.Ctx.MultipartBody(files, fields)

	// 2. 发送请求 (使用 AsApp 身份)
	w := s.Ctx.NewRequest(s.T(), "POST", "/api/v1/kyc/ocr").
		AsApp("ocr:read").
		WithBody(body).
		WithHeader("Content-Type", contentType).
		Do()

	// 3. 校验鉴权状态 (不应返回 401/403)
	require.NotEqual(s.T(), http.StatusUnauthorized, w.Code, "鉴权不应失败")
	require.NotEqual(s.T(), http.StatusForbidden, w.Code, "权限不应不足")

	// 4. 解析响应体，验证标准业务结构
	var resp struct {
		Code      int    `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(s.T(), err, "响应应为有效的 JSON")
	require.NotEmpty(s.T(), resp.RequestID, "响应应包含 RequestID")

	// 5. 验证计费副作用 (即使业务报错，计费日志也应产生)
	s.VerifyUsageLogged("ocr")

	s.T().Logf("✅ OCR 接口全链路校验通过. RequestID: %s, Status: %d", resp.RequestID, w.Code)
}

// TestUserProfile 验证 AsUser 身份查询资料
func (s *KYCFlowSuite) TestUserProfile() {
	s.Ctx.NewRequest(s.T(), "GET", "/api/v1/console/users/me").
		AsUser().
		ExpectSuccess()

	s.T().Log("✅ 用户资料查询 (AsUser) 验证通过")
}

// TestAdminStats 验证 AsAdmin 身份访问管理接口
func (s *KYCFlowSuite) TestAdminStats() {
	s.Ctx.NewRequest(s.T(), "GET", "/api/v1/admin/stats/overview").
		AsAdmin().
		ExpectSuccess()

	s.T().Log("✅ 管理员概览 (AsAdmin) 验证通过")
}

// TestSecurityBoundaries 验证权限边界 (反向测试)
func (s *KYCFlowSuite) TestSecurityBoundaries() {
	// 1. 普通用户试图访问管理员接口 -> 应被拒绝
	s.T().Log("测试场景: 普通用户访问管理员接口")
	s.Ctx.NewRequest(s.T(), "GET", "/api/v1/admin/stats/overview").
		AsUser().
		ExpectForbidden()

	// 2. 第三方 App 试图访问控制台接口 -> 应被拒绝
	s.T().Log("测试场景: 第三方 App 访问个人中心")
	s.Ctx.NewRequest(s.T(), "GET", "/api/v1/console/users/me").
		AsApp().
		ExpectUnauthorized() // AsApp 生成的是 Client Token，JWTAuth 识别不了 user_id

	s.T().Log("✅ 权限边界验证通过")
}

// TestPlaygroundAuth 验证 AsPlayground 身份 (STS 模式)
func (s *KYCFlowSuite) TestPlaygroundAuth() {
	s.Ctx.NewRequest(s.T(), "GET", "/api/v1/console/users/me").
		AsPlayground().
		ExpectSuccess()
	s.T().Log("✅ Playground STS 模式验证通过")
}
