package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"time"

	"kyc-service/pkg/logger"
	"kyc-service/pkg/metrics"

	"github.com/sirupsen/logrus"
)

func (t *ThirdPartyService) CallLivenessSilent(ctx context.Context, picturePath, pictureURL, language string) (*LivenessSilentResponse, error) {
	start := time.Now()
	status := "success"
	var httpCode string
	defer func() {
		metrics.RecordThirdPartyRequestWithOp(ctx, "liveness", "silent", status, httpCode, time.Since(start))
	}()
	url := t.config.ThirdParty.LivenessSlient.URL

	payload := make(map[string]string)
	payload["language"] = language

	if t.config.Storage.Upload.Mode == "remote" && pictureURL != "" {
		// Remote mode: send URL
		// Note: The key might need to be "picture_url" or remain "picture_path" depending on API spec.
		// For now, assuming the API is smart enough or uses a specific field for URL.
		// If the API strictly requires "picture_path" even for URLs, change key to "picture_path".
		payload["picture_url"] = pictureURL
	} else {
		// Local mode or fallback: send local path
		payload["picture_path"] = picturePath
	}

	b, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(b))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.config.ThirdParty.LivenessSlient.APIKey)
	req.Header.Set("X-Request-ID", fmt.Sprintf("%v", ctx.Value("request_id")))
	resp, err := t.client.Do(req)
	if err != nil {
		httpCode = "client_error"
		status = "failed"
		metrics.RecordThirdPartyError(ctx, "liveness", "silent", metrics.ResultHTTPClientError)
		return nil, fmt.Errorf("静态活体服务请求失败: %w", err)
	}
	defer resp.Body.Close()
	httpCode = fmt.Sprintf("%d", resp.StatusCode)
	rb, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}
	var out LivenessSilentResponse
	if err := json.Unmarshal(rb, &out); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK || out.Code != 0 {
		status = "failed"
		metrics.RecordThirdPartyError(ctx, "liveness", "silent", metrics.ResultBusinessFailed)
		return nil, fmt.Errorf("静态活体失败: code=%d msg=%s", out.Code, out.Msg)
	}
	return &out, nil
}

func (t *ThirdPartyService) CallLivenessVideo(ctx context.Context, videoPath, videoURL, language string) (*LivenessVideoResponse, error) {
	start := time.Now()
	status := "success"
	var httpCode string
	defer func() {
		metrics.RecordThirdPartyRequestWithOp(ctx, "liveness", "video", status, httpCode, time.Since(start))
	}()
	url := t.config.ThirdParty.LivenessVideo.URL

	payload := make(map[string]string)
	payload["language"] = language

	if t.config.Storage.Upload.Mode == "remote" && videoURL != "" {
		payload["video_url"] = videoURL
	} else {
		payload["video_path"] = videoPath
	}

	b, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(b))
	if err != nil {
		return nil, fmt.Errorf("create request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.config.ThirdParty.LivenessVideo.APIKey)
	req.Header.Set("X-Request-ID", fmt.Sprintf("%v", ctx.Value("request_id")))

	resp, err := t.client.Do(req)
	if err != nil {
		httpCode = "client_error"
		status = "failed"
		metrics.RecordThirdPartyError(ctx, "liveness", "video", metrics.ResultHTTPClientError)
		return nil, fmt.Errorf("liveness video request failed: %w", err)
	}
	defer resp.Body.Close()
	httpCode = fmt.Sprintf("%d", resp.StatusCode)

	rb, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response failed: %w", err)
	}

	var out LivenessVideoResponse
	if err := json.Unmarshal(rb, &out); err != nil {
		return nil, fmt.Errorf("parse response failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK || out.Code != 0 {
		status = "failed"
		metrics.RecordThirdPartyError(ctx, "liveness", "video", metrics.ResultBusinessFailed)
		return nil, fmt.Errorf("liveness video failed: code=%d msg=%s", out.Code, out.Msg)
	}

	return &out, nil
}

func (t *ThirdPartyService) CallLivenessService(ctx context.Context, action string, imageData []byte) (*LivenessServiceResponse, error) {
	start := time.Now()
	status := "success"
	httpCode := ""
	defer func() {
		metrics.RecordThirdPartyRequestWithOp(ctx, "liveness", action, status, httpCode, time.Since(start))
	}()

	url := t.config.ThirdParty.LivenessSlient.URL + "/api/v1/liveness/detect"

	// 创建multipart请求
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// 添加图片文件
	hdr := make(textproto.MIMEHeader)
	hdr.Set("Content-Disposition", `form-data; name="image"; filename="liveness.jpg"`)
	ct := detectContentTypeFromBytes(imageData, "image/jpeg")
	hdr.Set("Content-Type", ct)
	part, err := writer.CreatePart(hdr)
	if err != nil {
		return nil, fmt.Errorf("创建表单文件失败: %w", err)
	}

	if _, err := io.Copy(part, bytes.NewReader(imageData)); err != nil {
		return nil, fmt.Errorf("复制图片数据失败: %w", err)
	}

	// 添加动作类型
	if err := writer.WriteField("action", action); err != nil {
		return nil, fmt.Errorf("添加动作字段失败: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("关闭表单写入器失败: %w", err)
	}

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, "POST", url, body)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+t.config.ThirdParty.LivenessSlient.APIKey)
	req.Header.Set("X-Request-ID", fmt.Sprintf("%v", ctx.Value("request_id")))

	// 发送请求
	resp, err := t.client.Do(req)
	if err != nil {
		status = "failed"
		httpCode = "client_error"
		metrics.RecordThirdPartyError(ctx, "liveness", action, metrics.ResultHTTPClientError)
		return nil, fmt.Errorf("活体检测服务请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	// 解析响应
	var result LivenessServiceResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	// 检查响应状态
	httpCode = fmt.Sprintf("%d", resp.StatusCode)
	if resp.StatusCode != http.StatusOK {
		status = "failed"
		metrics.RecordThirdPartyError(ctx, "liveness", action, fmt.Sprintf("http_%d", resp.StatusCode))
		return nil, fmt.Errorf("活体检测服务返回错误: %d - %s", resp.StatusCode, result.Message)
	}

	if result.Code != 0 {
		status = "failed"
		metrics.RecordThirdPartyError(ctx, "liveness", action, metrics.ResultBusinessFailed)
		return nil, fmt.Errorf("活体检测失败: %s", result.Message)
	}

	logger.GetLogger().WithFields(logrus.Fields{
		"service": "liveness",
		"action":  action,
		"code":    result.Code,
		"score":   result.Data.Score,
		"pass":    result.Data.Pass,
	}).Info("活体检测成功")

	return &result, nil
}

func (t *ThirdPartyService) SubmitActionLivenessV2(ctx context.Context, req *ActionLivenessRequest) error {
	logger.GetLogger().Infof("SubmitActionLivenessV2 called. UseMock config: %v", t.config.UseMock)
	// Mock mode check
	if t.config.UseMock {
		go t.mockActionLivenessCallback(req)
		logger.GetLogger().Info("Mock mode enabled: SubmitActionLivenessV2 returning success immediately")
		return nil
	}

	start := time.Now()
	status := "success"
	var httpCode string
	defer func() {
		metrics.RecordThirdPartyRequestWithOp(ctx, "liveness", "action_submit_v2", status, httpCode, time.Since(start))
	}()
	url := t.config.ThirdParty.LivenessAction.SubmitURL

	b, _ := json.Marshal(req)
	r, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(b))
	if err != nil {
		return fmt.Errorf("create request failed: %w", err)
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+t.config.Security.ServiceSecretKey)
	r.Header.Set("X-Request-ID", fmt.Sprintf("%v", ctx.Value("request_id")))
	logger.GetLogger().Infof("SubmitActionLivenessV2 request payload: %s, url: %s, config: %v ", b, url, t.config.ThirdParty)

	resp, err := t.client.Do(r)
	if err != nil {
		httpCode = "client_error"
		status = "failed"
		metrics.RecordThirdPartyError(ctx, "liveness", "action_submit_v2", metrics.ResultHTTPClientError)
		return fmt.Errorf("submit action liveness v2 failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body for logging
	respBody, _ := io.ReadAll(resp.Body)
	// Restore body for subsequent reads if any (though here we just log it)
	// Actually we don't return the body, just check status code.

	logger.GetLogger().Infof("SubmitActionLivenessV2 response status: %d, body: %s", resp.StatusCode, string(respBody))

	httpCode = fmt.Sprintf("%d", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		status = "failed"
		metrics.RecordThirdPartyError(ctx, "liveness", "action_submit_v2", fmt.Sprintf("http_%d", resp.StatusCode))
		return fmt.Errorf("submit action liveness v2 returned %d", resp.StatusCode)
	}

	return nil
}

func (t *ThirdPartyService) mockActionLivenessCallback(req *ActionLivenessRequest) {
	// Simulate processing time
	time.Sleep(2 * time.Second)

	// Random outcome based on time seed
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	isSuccess := r.Float64() > 0.3 // 70% success rate

	var mockData *ActionLivenessData
	var code int
	var msg string

	if isSuccess {
		code = 0
		msg = "success"
		mockData = &ActionLivenessData{
			IsLiveness:         1,
			LivenessConfidence: 0.95 + r.Float64()*0.04, // 0.95-0.99
			IsFaceExist:        1,
			FaceInfo: &FaceInfo{
				Confidence:   0.98,
				QualityScore: 0.90,
			},
			ActionVerify: &ActionVerify{
				Passed:          true,
				RequiredActions: req.Actions,
				ActionDetails:   make([]*ActionDetail, 0),
			},
		}
		// All actions pass
		for _, action := range req.Actions {
			mockData.ActionVerify.ActionDetails = append(mockData.ActionVerify.ActionDetails, &ActionDetail{
				Action:     action,
				Passed:     true,
				Confidence: 0.90 + r.Float64()*0.09,
				Msg:        "动作通过",
			})
		}
	} else {
		// Failure scenario
		code = 0 // Business code 0 means processed, but result might be failed
		msg = "success"

		// Pick a random failure reason
		failType := r.Intn(3)

		mockData = &ActionLivenessData{
			IsLiveness:         0,                     // Failed
			LivenessConfidence: 0.4 + r.Float64()*0.3, // Low confidence
			IsFaceExist:        1,
			FaceInfo:           &FaceInfo{Confidence: 0.98, QualityScore: 0.90},
			ActionVerify: &ActionVerify{
				Passed:          false,
				RequiredActions: req.Actions,
				ActionDetails:   make([]*ActionDetail, 0),
			},
		}

		if failType == 0 {
			// Case 1: Liveness check failed (e.g. spoofing)
			mockData.IsLiveness = 0
			// Actions might still pass or not, but let's say they pass
			for _, action := range req.Actions {
				mockData.ActionVerify.ActionDetails = append(mockData.ActionVerify.ActionDetails, &ActionDetail{
					Action: action, Passed: true, Confidence: 0.9, Msg: "动作通过",
				})
			}
		} else {
			// Case 2: Specific action failed
			failedIdx := r.Intn(len(req.Actions))
			for i, action := range req.Actions {
				passed := true
				msg := "动作通过"
				conf := 0.9
				if i == failedIdx {
					passed = false
					msg = "动作幅度过小或未检测到"
					conf = 0.2
				}
				mockData.ActionVerify.ActionDetails = append(mockData.ActionVerify.ActionDetails, &ActionDetail{
					Action: action, Passed: passed, Confidence: conf, Msg: msg,
				})
			}
		}
	}

	callback := &ActionLivenessCallback{
		Code:      code,
		Msg:       msg,
		Data:      mockData,
		RequestID: req.RequestID,
		TaskID:    req.TaskID,
	}

	// Call back to our own server
	callbackBody, _ := json.Marshal(callback)

	// Calculate signature
	mac := hmac.New(sha256.New, []byte(t.config.Security.ServiceSecretKey))
	mac.Write(callbackBody)
	signature := hex.EncodeToString(mac.Sum(nil))

	client := &http.Client{Timeout: 5 * time.Second}
	// Use local address for mock callback
	targetURL := fmt.Sprintf("http://localhost:%d/api/v1/callbacks/liveness/action", t.config.Port)

	cbReq, err := http.NewRequest("POST", targetURL, bytes.NewBuffer(callbackBody))
	if err != nil {
		logger.GetLogger().Errorf("Failed to create mock callback request: %v", err)
		return
	}
	cbReq.Header.Set("Content-Type", "application/json")
	cbReq.Header.Set("X-ThirdParty-Signature", signature)

	logger.GetLogger().Infof("Sending mock callback to %s for task %s", targetURL, req.TaskID)
	resp, err := client.Do(cbReq)
	if err != nil {
		logger.GetLogger().Errorf("Failed to send mock callback: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		logger.GetLogger().Errorf("Mock callback failed with status: %d", resp.StatusCode)
	}
}
