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
	"strings"
	"time"

	"kyc-service/pkg/metrics"
)

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

func (t *ThirdPartyService) CallFaceIdMatch(ctx context.Context, src io.Reader, srcName string, dst io.Reader, dstName string) (*FaceCompareResponse, error) {
	start := time.Now()
	status := "success"
	var httpCode string
	defer func() {
		metrics.RecordThirdPartyRequestWithOp(ctx, "face", "id_match", status, httpCode, time.Since(start))
	}()

	base := t.config.ThirdParty.FaceService.URL
	url := base
	if !strings.Contains(strings.ToLower(base), "vrlfaceidcomparison") {
		url = strings.TrimRight(base, "/") + "/vrlFaceIdComparison"
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
		metrics.RecordThirdPartyError(ctx, "face", "id_match", metrics.ResultHTTPClientError)
		return nil, fmt.Errorf("人证比对服务请求失败: %w", err)
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
		metrics.RecordThirdPartyError(ctx, "face", "id_match", metrics.ResultBusinessFailed)
		return nil, fmt.Errorf("face id match failed: code=%d msg=%s, body=%s", out.Code, out.Msg, respBody)
	}
	return &out, nil
}

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
