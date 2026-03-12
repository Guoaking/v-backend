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
	"path/filepath"
	"strings"
	"time"

	"kyc-service/internal/config"
	"kyc-service/pkg/logger"
	"kyc-service/pkg/metrics"

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
func (t *ThirdPartyService) CallOCRService(ctx context.Context, imageData []byte) (*OCRServiceResponse, error) {
	start := time.Now()
	status := "success"
	httpCode := ""
	defer func() {
		metrics.RecordThirdPartyRequestWithOp(ctx, "ocr", "recognize", status, httpCode, time.Since(start))
	}()

	url := t.config.ThirdParty.OCRService.URL + "/api/v1/ocr/idcard"

	// 创建multipart请求
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// 添加图片文件，设置正确的Content-Type
	hdr := make(textproto.MIMEHeader)
	hdr.Set("Content-Disposition", `form-data; name="image"; filename="idcard.jpg"`)
	ct := detectContentTypeFromBytes(imageData, "image/jpeg")
	hdr.Set("Content-Type", ct)
	part, err := writer.CreatePart(hdr)
	if err != nil {
		return nil, fmt.Errorf("创建表单文件失败: %w", err)
	}
	if _, err := io.Copy(part, bytes.NewReader(imageData)); err != nil {
		return nil, fmt.Errorf("复制图片数据失败: %w", err)
	}

	// 添加其他字段
	if err := writer.WriteField("language", "auto"); err != nil {
		return nil, fmt.Errorf("添加语言字段失败: %w", err)
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
	req.Header.Set("Authorization", "Bearer "+t.config.ThirdParty.OCRService.APIKey)
	req.Header.Set("X-Request-ID", fmt.Sprintf("%v", ctx.Value("request_id")))

	// 发送请求
	resp, err := t.client.Do(req)
	if err != nil {
		status = "failed"
		httpCode = "client_error"
		metrics.RecordThirdPartyError(ctx, "ocr", "recognize", metrics.ResultHTTPClientError)
		return nil, fmt.Errorf("OCR服务请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	// 解析响应
	var result OCRServiceResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	// 检查响应状态
	httpCode = fmt.Sprintf("%d", resp.StatusCode)
	if resp.StatusCode != http.StatusOK {
		status = "failed"
		metrics.RecordThirdPartyError(ctx, "ocr", "recognize", fmt.Sprintf("http_%d", resp.StatusCode))
		return nil, fmt.Errorf("OCR服务返回错误: %d - %s", resp.StatusCode, result.Message)
	}

	if result.Code != 0 {
		status = "failed"
		metrics.RecordThirdPartyError(ctx, "ocr", "recognize", metrics.ResultBusinessFailed)
		return nil, fmt.Errorf("ocr task error: %s", result.Message)
	}

	logger.GetLogger().WithFields(logrus.Fields{
		"service": "ocr",
		"code":    result.Code,
		"id_card": logger.DesensitizeIDCard(result.Data.IDCard),
		"name":    logger.DesensitizeName(result.Data.Name),
	}).Info("OCR识别成功")

	return &result, nil
}

// CallFaceService 调用人脸识别服务
func (t *ThirdPartyService) CallFaceService(ctx context.Context, image1Data, image2Data []byte) (*FaceServiceResponse, error) {
	start := time.Now()
	status := "success"
	httpCode := ""
	defer func() {
		metrics.RecordThirdPartyRequestWithOp(ctx, "face", "verify", status, httpCode, time.Since(start))
	}()

	url := t.config.ThirdParty.FaceService.URL + "/api/v1/face/compare"

	// 创建multipart请求
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// 添加第一张图片
	hdr1 := make(textproto.MIMEHeader)
	hdr1.Set("Content-Disposition", `form-data; name="image1"; filename="image1.jpg"`)
	ct1 := detectContentTypeFromBytes(image1Data, "image/jpeg")
	hdr1.Set("Content-Type", ct1)
	part1, err := writer.CreatePart(hdr1)
	if err != nil {
		return nil, fmt.Errorf("创建第一张图片表单失败: %w", err)
	}
	if _, err := io.Copy(part1, bytes.NewReader(image1Data)); err != nil {
		return nil, fmt.Errorf("复制第一张图片数据失败: %w", err)
	}

	// 添加第二张图片
	hdr2 := make(textproto.MIMEHeader)
	hdr2.Set("Content-Disposition", `form-data; name="image2"; filename="image2.jpg"`)
	ct2 := detectContentTypeFromBytes(image2Data, "image/jpeg")
	hdr2.Set("Content-Type", ct2)
	part2, err := writer.CreatePart(hdr2)
	if err != nil {
		return nil, fmt.Errorf("创建第二张图片表单失败: %w", err)
	}
	if _, err := io.Copy(part2, bytes.NewReader(image2Data)); err != nil {
		return nil, fmt.Errorf("复制第二张图片数据失败: %w", err)
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
	req.Header.Set("Authorization", "Bearer "+t.config.ThirdParty.FaceService.APIKey)
	req.Header.Set("X-Request-ID", fmt.Sprintf("%v", ctx.Value("request_id")))

	// 发送请求
	resp, err := t.client.Do(req)
	if err != nil {
		status = "failed"
		httpCode = "client_error"
		metrics.RecordThirdPartyError(ctx, "face", "verify", metrics.ResultHTTPClientError)
		return nil, fmt.Errorf("人脸识别服务请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	// 解析响应
	var result FaceServiceResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	// 检查响应状态
	httpCode = fmt.Sprintf("%d", resp.StatusCode)
	if resp.StatusCode != http.StatusOK {
		status = "failed"
		metrics.RecordThirdPartyError(ctx, "face", "verify", fmt.Sprintf("http_%d", resp.StatusCode))
		return nil, fmt.Errorf("人脸识别服务返回错误: %d - %s", resp.StatusCode, result.Message)
	}

	if result.Code != 0 {
		status = "failed"
		metrics.RecordThirdPartyError(ctx, "face", "verify", metrics.ResultBusinessFailed)
		return nil, fmt.Errorf("人脸识别失败: %s", result.Message)
	}

	logger.GetLogger().WithFields(logrus.Fields{
		"service":  "face",
		"code":     result.Code,
		"score":    result.Data.Score,
		"match":    result.Data.Match,
		"duration": time.Since(start).Milliseconds(),
	}).Info("人脸识别成功")

	return &result, nil
}

// CallFaceSearch 调用人脸搜索服务
func (t *ThirdPartyService) CallFaceSearch(ctx context.Context, reader io.Reader, filename string) (*FaceSearchResponse, error) {
	start := time.Now()
	status := "success"
	var httpCode string
	defer func() {
		metrics.RecordThirdPartyRequestWithOp(ctx, "face", "search", status, httpCode, time.Since(start))
	}()

	base := t.config.ThirdParty.FaceService.URL
	url := base
	if !strings.Contains(strings.ToLower(base), "vrlfacesearch") {
		url = strings.TrimRight(base, "/") + "/vrlFaceSearch"
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	hdr := make(textproto.MIMEHeader)
	hdr.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, "picture", filename))
	hdr.Set("Content-Type", contentTypeFromFilename(filename, "image/jpeg"))
	part, err := writer.CreatePart(hdr)
	if err != nil {
		return nil, fmt.Errorf("创建图片表单失败: %w", err)
	}
	if _, err := io.Copy(part, reader); err != nil {
		return nil, fmt.Errorf("复制图片数据失败: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("关闭表单写入器失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, body)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+t.config.ThirdParty.FaceService.APIKey)
	req.Header.Set("X-Request-ID", fmt.Sprintf("%v", ctx.Value("request_id")))

	resp, err := t.client.Do(req)
	if err != nil {
		httpCode = "client_error"
		status = "failed"
		metrics.RecordThirdPartyError(ctx, "face", "search", metrics.ResultHTTPClientError)
		return nil, fmt.Errorf("人脸搜索服务请求失败: %w", err)
	}
	defer resp.Body.Close()
	httpCode = fmt.Sprintf("%d", resp.StatusCode)

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	var out FaceSearchResponse
	if v, ok := raw["code"].(float64); ok {
		out.Code = int(v)
	}
	out.Msg = fmt.Sprintf("%v", raw["msg"])
	out.Filename = fmt.Sprintf("%v", raw["filename"])
	srch, _ := raw["searching_results"].(map[string]interface{})
	list, _ := srch["searched_similar_pictures"].([]interface{})

	for _, p := range list {
		m := p.(map[string]interface{})
		pic := ""
		if v, ok := m["picture"]; ok {
			pic = fmt.Sprintf("%v", v)
		} else if v, ok := m["pciture"]; ok {
			pic = fmt.Sprintf("%v", v)
		}
		conf := 0.0
		if c, ok := m["confidence"].(float64); ok {
			conf = c
		}
		id := fmt.Sprintf("%v", m["id"]) // third-party id; will be remapped in service
		out.SearchingResults.SearchedSimilarPictures = append(out.SearchingResults.SearchedSimilarPictures, struct {
			ID         string  `json:"id"`
			Confidence float64 `json:"confidence"`
			Picture    string  `json:"picture,omitempty"`
		}{ID: id, Confidence: conf, Picture: pic})
	}

	if v, ok := srch["has_similar_picture"].(float64); ok {
		out.SearchingResults.HasSimilarPicture = int(v)
	}

	// remove duplicate DB writes; mapping handled in service layer

	if out.Code != 0 {
		status = "failed"
		metrics.RecordThirdPartyError(ctx, "face", "search", metrics.ResultBusinessFailed)
		return nil, fmt.Errorf("face search failed: code=%d msg=%s", out.Code, out.Msg)
	}
	return &out, nil
}

// CallFaceDetect 调用人脸检测服务
func (t *ThirdPartyService) CallFaceDetect(ctx context.Context, reader io.Reader, filename string) (*FaceDetectResponse, error) {
	start := time.Now()
	status := "success"
	var httpCode string
	defer func() {
		metrics.RecordThirdPartyRequestWithOp(ctx, "face", "detect", status, httpCode, time.Since(start))
	}()

	base := t.config.ThirdParty.FaceService.URL
	url := base
	if !strings.Contains(strings.ToLower(base), "vrlfacedetection") {
		url = strings.TrimRight(base, "/") + "/vrlFaceDetection"
	}
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	hdr := make(textproto.MIMEHeader)
	hdr.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, "picture", filename))
	hdr.Set("Content-Type", contentTypeFromFilename(filename, "image/jpeg"))
	part, err := writer.CreatePart(hdr)
	if err != nil {
		return nil, fmt.Errorf("创建图片表单失败: %w", err)
	}
	if _, err := io.Copy(part, reader); err != nil {
		return nil, fmt.Errorf("复制图片数据失败: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("关闭表单写入器失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, body)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+t.config.ThirdParty.FaceService.APIKey)
	req.Header.Set("X-Request-ID", fmt.Sprintf("%v", ctx.Value("request_id")))
	resp, err := t.client.Do(req)
	if err != nil {
		httpCode = "client_error"
		status = "failed"
		metrics.RecordThirdPartyError(ctx, "face", "detect", metrics.ResultHTTPClientError)
		return nil, fmt.Errorf("人脸检测服务请求失败: %w", err)
	}
	defer resp.Body.Close()
	httpCode = fmt.Sprintf("%d", resp.StatusCode)
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}
	var out FaceDetectResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK || out.Code != 0 {
		status = "failed"
		metrics.RecordThirdPartyError(ctx, "face", "detect", metrics.ResultBusinessFailed)
		return nil, fmt.Errorf("face detect failed: code=%d msg=%s", out.Code, out.Msg)
	}
	return &out, nil
}

// CallFaceCompare 调用人脸比对服务
func (t *ThirdPartyService) CallFaceCompare(ctx context.Context, src io.Reader, srcName string, dst io.Reader, dstName string) (*FaceCompareResponse, error) {
	start := time.Now()
	status := "success"
	var httpCode string
	defer func() {
		metrics.RecordThirdPartyRequestWithOp(ctx, "face", "compare", status, httpCode, time.Since(start))
	}()

	base := t.config.ThirdParty.FaceService.URL
	url := base
	if !strings.Contains(strings.ToLower(base), "vrlfacecomparison") {
		url = strings.TrimRight(base, "/") + "/vrlFaceComparison"
	}
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	hdr1 := make(textproto.MIMEHeader)
	hdr1.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, "picture1", srcName))
	hdr1.Set("Content-Type", contentTypeFromFilename(srcName, "image/jpeg"))
	p1, err := writer.CreatePart(hdr1)
	if err != nil {
		return nil, fmt.Errorf("创建第一张图片表单失败: %w", err)
	}
	if _, err := io.Copy(p1, src); err != nil {
		return nil, fmt.Errorf("复制第一张图片失败: %w", err)
	}
	hdr2 := make(textproto.MIMEHeader)
	hdr2.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, "picture2", dstName))
	hdr2.Set("Content-Type", contentTypeFromFilename(dstName, "image/jpeg"))
	p2, err := writer.CreatePart(hdr2)
	if err != nil {
		return nil, fmt.Errorf("创建第二张图片表单失败: %w", err)
	}
	if _, err := io.Copy(p2, dst); err != nil {
		return nil, fmt.Errorf("复制第二张图片失败: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("关闭表单写入器失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, body)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+t.config.ThirdParty.FaceService.APIKey)
	req.Header.Set("X-Request-ID", fmt.Sprintf("%v", ctx.Value("request_id")))
	resp, err := t.client.Do(req)
	if err != nil {
		httpCode = "client_error"
		status = "failed"
		metrics.RecordThirdPartyError(ctx, "face", "compare", metrics.ResultHTTPClientError)
		return nil, fmt.Errorf("人脸比对服务请求失败: %w", err)
	}
	defer resp.Body.Close()
	httpCode = fmt.Sprintf("%d", resp.StatusCode)
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}
	var out FaceCompareResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK || out.Code != 0 {
		status = "failed"
		metrics.RecordThirdPartyError(ctx, "face", "compare", metrics.ResultBusinessFailed)
		return nil, fmt.Errorf("face compare failed: code=%d msg=%s", out.Code, out.Msg)
	}
	return &out, nil
}

// CallLivenessSilent 调用静态活体服务
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

	if t.config.Storage.Mode == "remote" && pictureURL != "" {
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


// CallLivenessVideo 调用动态活体服务 (Video Silent Liveness)
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

	if t.config.Storage.Mode == "remote" && videoURL != "" {
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

// CallLivenessService 调用活体检测服务
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

// mockActionLivenessCallback simulates the async callback from third-party service
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
