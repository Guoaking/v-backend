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
	"time"

	"kyc-service/pkg/logger"
	"kyc-service/pkg/metrics"

	"github.com/sirupsen/logrus"
)

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
