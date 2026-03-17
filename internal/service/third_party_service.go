package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"kyc-service/internal/config"
	"kyc-service/pkg/logger"

	"github.com/sirupsen/logrus"
)

// ThirdPartyService 第三方服务集成
type ThirdPartyService struct {
	config *config.Config
	client *http.Client
}

func NewThirdPartyService(cfg *config.Config) *ThirdPartyService {
	return &ThirdPartyService{
		config: cfg,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func detectContentTypeFromBytes(b []byte, fallback string) string {
	if len(b) > 0 {
		// http.DetectContentType inspects at most the first 512 bytes
		n := 512
		if len(b) < n {
			n = len(b)
		}
		ct := http.DetectContentType(b[:n])
		if ct != "application/octet-stream" && ct != "text/plain; charset=utf-8" {
			return ct
		}
	}
	return fallback
}

func contentTypeFromFilename(name, fallback string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".bmp":
		return "image/bmp"
	case ".mp4":
		return "video/mp4"
	case ".mov":
		return "video/quicktime"
	}
	return fallback
}

// OCRServiceResponse OCR服务响应
type OCRServiceResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		IDCard   string `json:"id_card"`
		Name     string `json:"name"`
		Gender   string `json:"gender"`
		Ethnic   string `json:"ethnic"`
		Birthday string `json:"birthday"`
		Address  string `json:"address"`
		Agency   string `json:"agency"`
		Valid    string `json:"valid"`
	} `json:"data"`
}

// FaceServiceResponse 人脸识别服务响应
type FaceServiceResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Score     float64 `json:"score"`
		Threshold float64 `json:"threshold"`
		Match     bool    `json:"match"`
	} `json:"data"`
}

// LivenessServiceResponse 活体检测服务响应
type LivenessServiceResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Score   float64 `json:"score"`
		Pass    bool    `json:"pass"`
		Action  string  `json:"action"`
		Message string  `json:"message"`
	} `json:"data"`
}

// CallOCRService 调用OCR服务

// CallFaceSearch 调用人脸搜索服务

// CallFaceIdMatch 调用人证比对服务

// CallFaceDetect 调用人脸检测服务

// CallFaceCompare 调用人脸比对服务

// CallLivenessSilent 调用静态活体服务

// CallLivenessVideo 调用动态活体服务 (Video Silent Liveness)

// CallLivenessService 调用活体检测服务

// RetryWithBackoff 带退避重试的辅助函数
func (t *ThirdPartyService) RetryWithBackoff(ctx context.Context, fn func() error, maxRetries int) error {
	var err error
	for i := 0; i < maxRetries; i++ {
		if err = fn(); err == nil {
			return nil
		}

		// 计算退避时间
		backoff := time.Duration(i+1) * time.Second
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}

		logger.GetLogger().WithFields(logrus.Fields{
			"retry":       i + 1,
			"max_retries": maxRetries,
			"backoff":     backoff,
			"error":       err.Error(),
		}).Warn("第三方服务调用失败，重试中")

		select {
		case <-time.After(backoff):
			continue
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return fmt.Errorf("重试%d次后仍然失败: %w", maxRetries, err)
}

// ActionLivenessRequest 动作活体请求参数
type ActionLivenessRequest struct {
	RequestID       string           `json:"request_id"`
	TaskID          string           `json:"task_id"`
	VideoPath       string           `json:"video_path"`
	Actions         []string         `json:"actions"`
	ThresholdConfig *ThresholdConfig `json:"threshold_config"`
	ActionConfig    *ActionConfig    `json:"action_config"`
	CallBackURL     string           `json:"callback_url"`
}

type ThresholdConfig struct {
	LivenessThreshold float64 `json:"liveness_threshold"`
	ActionThreshold   float64 `json:"action_threshold"`
}

type ActionConfig struct {
	MaxVideoDuration int `json:"max_video_duration"`
	PerActionTimeout int `json:"per_action_timeout"`
}

// ActionLivenessCallback 动作活体回调响应
type ActionLivenessCallback struct {
	Code      int                 `json:"code"`
	Msg       string              `json:"msg"`
	Data      *ActionLivenessData `json:"data"`
	RequestID string              `json:"request_id"`
	TaskID    interface{}         `json:"task_id"`
}

type ActionLivenessData struct {
	IsLiveness         int           `json:"is_liveness"`
	LivenessConfidence float64       `json:"liveness_confidence"`
	IsFaceExist        int           `json:"is_face_exist"`
	FaceInfo           *FaceInfo     `json:"face_info"`
	ActionVerify       *ActionVerify `json:"action_verify"`
}

type FaceInfo struct {
	Confidence   float64 `json:"confidence"`
	QualityScore float64 `json:"quality_score"`
}

type ActionVerify struct {
	Passed          bool            `json:"passed"`
	RequiredActions []string        `json:"required_actions"`
	ActionDetails   []*ActionDetail `json:"action_details"`
}

type ActionDetail struct {
	Action     string  `json:"action"`
	Passed     bool    `json:"passed"`
	Confidence float64 `json:"confidence"`
	Msg        string  `json:"msg"`
}

// SubmitActionLivenessV2 提交动作活体检测任务 (新版)

// mockActionLivenessCallback simulates the async callback from third-party service

// VerifyCallbackSignature 验证回调签名
// signature: X-ThirdParty-Signature header value
// body: request body
func (t *ThirdPartyService) VerifyCallbackSignature(signature string, body []byte) bool {
	secret := t.config.Security.ServiceSecretKey // Use service secret key as callback secret
	if secret == "" {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expectedSignature := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(signature), []byte(expectedSignature))
}
