package service

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"kyc-service/pkg/utils"
)

// SaveBase64ToLocal 截取 base64 的头部并保存为本地文件，返回相对路径
func SaveBase64ToLocal(base64Data, directory, prefix string) (string, error) {
	if base64Data == "" {
		return "", nil
	}

	// 1. 分离前缀 (e.g. data:image/jpeg;base64,.....)
	parts := strings.SplitN(base64Data, ",", 2)
	var rawBase64 string
	var ext string

	if len(parts) == 2 {
		rawBase64 = parts[1]
		// 尝试提取扩展名
		if strings.Contains(parts[0], "image/png") {
			ext = ".png"
		} else if strings.Contains(parts[0], "image/jpeg") || strings.Contains(parts[0], "image/jpg") {
			ext = ".jpg"
		} else {
			ext = ".jpg" // 默认 fallback
		}
	} else {
		rawBase64 = parts[0]
		ext = ".jpg"
	}

	// 2. Base64 解码
	decodedBytes, err := base64.StdEncoding.DecodeString(rawBase64)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64: %w", err)
	}

	// 3. 确保目录存在 (为了本地演示，存放在项目的 uploads 目录下)
	baseDir := filepath.Join(".", "uploads", directory)
	if err := os.MkdirAll(baseDir, os.ModePerm); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}

	// 4. 生成唯一文件名
	fileName := fmt.Sprintf("%s_%s%s", prefix, utils.GenerateID(), ext)
	fullPath := filepath.Join(baseDir, fileName)

	// 5. 写入文件
	if err := os.WriteFile(fullPath, decodedBytes, 0644); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	// 6. 返回相对路径供前端访问 (例如 /uploads/attendance/faces/xxx.jpg)
	relativePath := filepath.Join("/uploads", directory, fileName)
	// Ensure URL forward slash consistency
	relativePath = strings.ReplaceAll(relativePath, "\\", "/")
	return relativePath, nil
}

// ConvertLocalFileToMultipartHeader reads a local file and constructs a *multipart.FileHeader.
// This is useful for passing local images to internal services that expect multipart uploads.
func ConvertLocalFileToMultipartHeader(filePath string) (*multipart.FileHeader, error) {
	// Our paths are stored as "/uploads/..." but physically they are in "./uploads/..."
	physicalPath := "." + filePath

	fileBytes, err := os.ReadFile(physicalPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Create an in-memory multipart form
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Create a form file field named "file" (the name doesn't strictly matter for internal passthrough)
	part, err := writer.CreateFormFile("file", filepath.Base(physicalPath))
	if err != nil {
		return nil, fmt.Errorf("failed to create form file: %w", err)
	}

	if _, err := part.Write(fileBytes); err != nil {
		return nil, fmt.Errorf("failed to write to form file: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close writer: %w", err)
	}

	// Parse the multipart form to extract the FileHeader
	req, err := http.NewRequest("POST", "/", body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	if err := req.ParseMultipartForm(10 << 20); err != nil { // 10MB limit
		return nil, fmt.Errorf("failed to parse multipart form: %w", err)
	}

	headers := req.MultipartForm.File["file"]
	if len(headers) == 0 {
		return nil, fmt.Errorf("no file found in constructed multipart form")
	}

	return headers[0], nil
}
