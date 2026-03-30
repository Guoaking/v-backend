package service

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"

	"kyc-service/pkg/utils"
)

// SaveMultipartToLocal reads a multipart.FileHeader and saves it to a local directory.
func SaveMultipartToLocal(fileHeader *multipart.FileHeader, dir, prefix string) (string, error) {
	file, err := fileHeader.Open()
	if err != nil {
		return "", fmt.Errorf("failed to open multipart file: %w", err)
	}
	defer file.Close()

	// Ensure the directory exists
	uploadDir := filepath.Join(".", "uploads", dir)
	if err := os.MkdirAll(uploadDir, os.ModePerm); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}

	// Generate a unique filename
	filename := fmt.Sprintf("%s_%s.jpg", prefix, utils.GenerateID())
	savePath := filepath.Join(uploadDir, filename)

	// Create the destination file
	dst, err := os.Create(savePath)
	if err != nil {
		return "", fmt.Errorf("failed to create destination file: %w", err)
	}
	defer dst.Close()

	// Copy the contents
	if _, err := io.Copy(dst, file); err != nil {
		return "", fmt.Errorf("failed to save file: %w", err)
	}

	// Return the relative path to store in DB
	return fmt.Sprintf("/uploads/%s/%s", dir, filename), nil
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
