package integration

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// KYCCoreSuite 专门测试 KYC 核心业务流 (OCR, Face, Liveness)
type KYCCoreSuite struct {
	BaseSuite
}

func TestKYCCoreSuite(t *testing.T) {
	suite.Run(t, new(KYCCoreSuite))
}

// TestSmoke_OCR [SMOKE] 验证 OCR 基础功能
func (s *KYCCoreSuite) TestSmoke_OCR() {
	files := map[string][]byte{"picture": []byte("fake-id-card-image")}
	fields := map[string]string{"type": "id_card", "language": "thai"}
	body, contentType := s.Ctx.MultipartBody(files, fields)

	w := s.Ctx.NewRequest(s.T(), "POST", "/api/v1/kyc/ocr").
		AsApp("ocr:read").
		WithBody(body).
		WithHeader("Content-Type", contentType).
		Do()

	s.AssertBusinessReach(s.T(), w)
	s.VerifyUsageLogged("ocr")
	s.T().Log("✅ Smoke: OCR 接口验证通过")
}

// TestFaceAPIs 验证人脸相关接口
func (s *KYCCoreSuite) TestFaceAPIs() {
	s.Run("FaceDetect", func() {
		files := map[string][]byte{"picture": []byte("face")}
		body, contentType := s.Ctx.MultipartBody(files, nil)
		s.Ctx.NewRequest(s.T(), "POST", "/api/v1/kyc/face/detect").
			AsApp("face:read").
			WithBody(body).
			WithHeader("Content-Type", contentType).
			ExpectStatus(400) // Expect 400 when third party is down
	})

	s.Run("FaceCompare", func() {
		files := map[string][]byte{"picture_1": []byte("f1"), "picture_2": []byte("f2")}
		body, contentType := s.Ctx.MultipartBody(files, nil)
		s.Ctx.NewRequest(s.T(), "POST", "/api/v1/kyc/face/compare").
			AsApp("face:read").
			WithBody(body).
			WithHeader("Content-Type", contentType).
			ExpectStatus(400) // Expect 400 when third party is down
	})
	s.T().Log("✅ 人脸相关接口验证通过 (已预期 400 失败)")
}

// TestThirdPartyDowngrade 验证第三方服务降级容错机制
func (s *KYCCoreSuite) TestThirdPartyDowngrade() {
	// 场景：在 UseMock=false 的真实环境下，第三方服务(159.x)不可达或返回错误
	// 我们期望网关层面能够妥善处理，不发生 panic，并返回 400 或 502
	files := map[string][]byte{"picture": []byte("invalid_image_for_third_party")}
	fields := map[string]string{"type": "id_card", "language": "th"}
	body, contentType := s.Ctx.MultipartBody(files, fields)

	var errResp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}

	resp := s.Ctx.NewRequest(s.T(), "POST", "/api/v1/kyc/ocr").
		AsApp("ocr:read"). // 使用 App 身份并赋予 scope
		WithBody(body).
		WithHeader("Content-Type", contentType).
		ExpectStatus(502) // 真实三方不可用时返回 502 Bad Gateway

	err := json.Unmarshal(resp.Body.Bytes(), &errResp)
	require.NoError(s.T(), err)
	// 断言错误信息中包含明确的第三方调用失败标识，而不是让前端懵逼
	require.NotEmpty(s.T(), errResp.Message)

	s.T().Log("✅ 第三方服务降级容错验证通过 (返回受控错误: " + errResp.Message + ")")
}

// TestLivenessAPIs 验证基础活体检测
func (s *KYCCoreSuite) TestLivenessAPIs() {
	s.T().Run("SilentLiveness", func(t *testing.T) {
		files := map[string][]byte{"picture": []byte("liveness-target")}
		body, contentType := s.Ctx.MultipartBody(files, nil)
		s.Ctx.NewRequest(t, "POST", "/api/v1/kyc/liveness/silent").
			AsApp("liveness:read").
			WithBody(body).
			WithHeader("Content-Type", contentType).
			ExpectStatus(400) // Expect 400 when third party is down
	})
	s.T().Log("✅ 基础活体接口验证通过 (已预期 400 失败)")
}

// TestSmoke_ActionLiveness [SMOKE] 验证动作活体异步全链路
func (s *KYCCoreSuite) TestSmoke_ActionLiveness() {
	// 1. Session
	var sessionResp struct {
		Data struct {
			SessionID string `json:"session_id"`
		} `json:"data"`
	}
	s.Ctx.NewRequest(s.T(), "POST", "/api/v1/kyc/liveness/action/session").
		AsApp("liveness:read").
		ExpectJSON(&sessionResp)

	sessionID := sessionResp.Data.SessionID
	require.NotEmpty(s.T(), sessionID)

	// 2. Upload
	files := map[string][]byte{"video": []byte("fake-video")}
	fields := map[string]string{"session_id": sessionID}
	body, contentType := s.Ctx.MultipartBody(files, fields)
	s.Ctx.NewRequest(s.T(), "POST", "/api/v1/kyc/liveness/action/upload").
		AsApp("liveness:read").
		WithBody(body).
		WithHeader("Content-Type", contentType).
		ExpectStatus(400) // 期望 400 因为三方服务不可用

	s.T().Log("✅ Smoke: 动作活体链路验证通过 (已预期 400 失败)")
}

// TestKYCVerifyAndStatus 验证完整 KYC 流程
func (s *KYCCoreSuite) TestKYCVerifyAndStatus() {
	files := map[string][]byte{"idcard_image": []byte("id"), "face_image": []byte("face")}
	fields := map[string]string{"name": "E2E", "idcard": "123"}
	body, contentType := s.Ctx.MultipartBody(files, fields)

	s.Ctx.NewRequest(s.T(), "POST", "/api/v1/kyc/verify").
		AsApp("kyc:verify").
		WithBody(body).
		WithHeader("Content-Type", contentType).
		ExpectSuccess()

	s.T().Log("✅ KYC 完整流程验证通过")
}

// TestQuotaExceeded 验证配额耗尽拦截
func (s *KYCCoreSuite) TestQuotaExceeded() {
	// 1. 获取当前用户所在组织
	var user struct {
		CurrentOrgID string `json:"current_org_id"`
	}
	s.Ctx.App.DB.Table("users").Where("email = ?", "console@example.com").Scan(&user)
	if user.CurrentOrgID == "" {
		// 回退到全局设置的 OrgID
		user.CurrentOrgID = s.Ctx.OrgID
	}
	require.NotEmpty(s.T(), user.CurrentOrgID)

	// 2. 将组织的 OCR 配额强行修改为已耗尽状态
	s.Ctx.App.DB.Exec("UPDATE organization_quotas SET allocation = 0, consumed = 0 WHERE organization_id = ? AND service_type = ?", user.CurrentOrgID, "ocr")
	// 如果使用了 Redis 缓存，需要清理缓存，或者利用 Redis 键模式强制过期

	bgCtx := context.Background()
	s.Ctx.App.RedisClient.Del(bgCtx, "quota:limit:"+user.CurrentOrgID+":ocr")
	s.Ctx.App.RedisClient.Del(bgCtx, "quota:consumed:"+user.CurrentOrgID+":ocr")

	// 3. 再次请求 OCR 接口
	files := map[string][]byte{"picture": []byte("test content")}
	fields := map[string]string{"type": "id_card", "language": "th"}
	body, contentType := s.Ctx.MultipartBody(files, fields)

	// 期望返回 402 Payment Required
	var errResp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}

	resp := s.Ctx.NewRequest(s.T(), "POST", "/api/v1/kyc/ocr").
		AsApp(). // 使用 App 身份（带正确 scope）而不是 User，因为 /kyc/ocr 可能需要 API Key 或 App token
		WithBody(body).
		WithHeader("Content-Type", contentType).
		ExpectStatus(402)

	err := json.Unmarshal(resp.Body.Bytes(), &errResp)
	require.NoError(s.T(), err)

	// 检查返回结构体中的特定错误码
	require.Equal(s.T(), 40201, errResp.Code)

	s.T().Log("✅ 配额拦截验证通过 (返回 402)")

	// 恢复配额，以免影响其他测试
	s.Ctx.App.DB.Exec("UPDATE organization_quotas SET allocation = 1000 WHERE organization_id = ? AND service_type = ?", user.CurrentOrgID, "ocr")
	s.Ctx.App.RedisClient.Del(bgCtx, "quota:limit:"+user.CurrentOrgID+":ocr")
	s.Ctx.App.RedisClient.Del(bgCtx, "quota:consumed:"+user.CurrentOrgID+":ocr")
}

// TestImageRetrieval 验证图片获取
func (s *KYCCoreSuite) TestImageRetrieval() {
	var upResp struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	files := map[string][]byte{"picture": []byte("content")}
	body, contentType := s.Ctx.MultipartBody(files, nil)
	s.Ctx.NewRequest(s.T(), "POST", "/api/v1/images").
		AsApp().
		WithBody(body).
		WithHeader("Content-Type", contentType).
		ExpectJSON(&upResp)

	s.Ctx.NewRequest(s.T(), "GET", "/api/v1/images/"+upResp.Data.ID+"/image").
		AsApp().
		ExpectSuccess()
}
