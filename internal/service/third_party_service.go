package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
func (t *ThirdPartyService) CallLivenessSilent(ctx context.Context, picturePath, language string) (*LivenessSilentResponse, error) {
	start := time.Now()
	status := "success"
	var httpCode string
	defer func() {
		metrics.RecordThirdPartyRequestWithOp(ctx, "liveness", "silent", status, httpCode, time.Since(start))
	}()
	url := t.config.ThirdParty.LivenessSlient.URL
	payload := map[string]string{"picture_path": picturePath, "language": language}
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

// CallLivenessVideo 调用动态活体服务
func (t *ThirdPartyService) CallLivenessVideo(ctx context.Context, videoPath, language string) (*LivenessVideoResponse, error) {
	start := time.Now()
	status := "success"
	var httpCode string
	defer func() {
		metrics.RecordThirdPartyRequestWithOp(ctx, "liveness", "video", status, httpCode, time.Since(start))
	}()
	url := t.config.ThirdParty.LivenessVideo.URL
	payload := map[string]string{"video_path": videoPath, "language": language}
	b, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(b))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.config.ThirdParty.LivenessVideo.APIKey)
	req.Header.Set("X-Request-ID", fmt.Sprintf("%v", ctx.Value("request_id")))
	resp, err := t.client.Do(req)
	if err != nil {
		httpCode = "client_error"
		status = "failed"
		metrics.RecordThirdPartyError(ctx, "liveness", "video", metrics.ResultHTTPClientError)
		return nil, fmt.Errorf("动态活体服务请求失败: %w", err)
	}
	defer resp.Body.Close()
	httpCode = fmt.Sprintf("%d", resp.StatusCode)
	rb, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}
	var out LivenessVideoResponse
	if err := json.Unmarshal(rb, &out); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK || out.Code != 0 {
		status = "failed"
		metrics.RecordThirdPartyError(ctx, "liveness", "video", metrics.ResultBusinessFailed)
		return nil, fmt.Errorf("动态活体失败: code=%d msg=%s", out.Code, out.Msg)
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
