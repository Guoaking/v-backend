package service

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
)

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
