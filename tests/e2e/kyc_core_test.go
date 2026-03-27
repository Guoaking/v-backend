package e2e

import (
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
	s.T().Run("FaceDetect", func(t *testing.T) {
		files := map[string][]byte{"picture": []byte("fake-face-image")}
		body, contentType := s.Ctx.MultipartBody(files, nil)
		s.Ctx.NewRequest(t, "POST", "/api/v1/kyc/face/detect").
			AsApp("face:read").
			WithBody(body).
			WithHeader("Content-Type", contentType).
			ExpectSuccess()
	})

	s.T().Run("FaceCompare", func(t *testing.T) {
		files := map[string][]byte{"source_image": []byte("f1"), "target_image": []byte("f2")}
		body, contentType := s.Ctx.MultipartBody(files, nil)
		s.Ctx.NewRequest(t, "POST", "/api/v1/kyc/face/compare").
			AsApp("face:read").
			WithBody(body).
			WithHeader("Content-Type", contentType).
			ExpectSuccess()
	})

	s.T().Log("✅ 人脸相关接口验证通过")
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
			ExpectSuccess()
	})
	s.T().Log("✅ 基础活体接口验证通过")
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
		ExpectSuccess()

	s.T().Log("✅ Smoke: 动作活体链路验证通过")
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
