package middleware

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"kyc-service/internal/service"
	"kyc-service/pkg/logger"
	"kyc-service/pkg/utils"

	"github.com/gin-gonic/gin"
)

// AsyncMediaIngest is a middleware to intercept and log multipart/form-data files.
// It safely copies the uploaded temp file to a persistent local temp file,
// avoiding loading the entire file into memory (preventing memory spikes).
func AsyncMediaIngest(kycService *service.KYCService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost && c.Request.Method != http.MethodPut {
			c.Next()
			return
		}
		if !strings.Contains(c.Request.Header.Get("Content-Type"), "multipart/form-data") {
			c.Next()
			return
		}
		if err := c.Request.ParseMultipartForm(32 << 20); err != nil {
			c.Next()
			return
		}

		orgID := c.GetString("orgID")
		if orgID == "" {
			orgID = "unknown_org"
		}

		// The UnifiedContextMiddleware uses "X-Request-ID" context key
		// internally it sets models.ContextKeyRequestID, which is typically "request_id"
		reqID := c.GetString("request_id")
		if reqID == "" {
			reqID = c.GetHeader("X-Request-ID")
		}

		// Store a map of filename to tempFilePath in the context
		// This makes it easy for the service layer to find the right temp file
		// regardless of what the form field name was.
		tempFileMap := make(map[string]string)

		for fieldName, fileHeaders := range c.Request.MultipartForm.File {
			for _, fileHeader := range fileHeaders {
				// We create a permanent local temp file to hold the data,
				// avoiding OOM (Out Of Memory) issues that `io.ReadAll` would cause on large videos.
				srcFile, err := fileHeader.Open()
				if err != nil {
					logger.GetLogger().WithError(err).Warn("AsyncMediaIngest: Failed to open file header")
					continue
				}

				// Create a temporary directory if it doesn't exist
				tempDir := filepath.Join(os.TempDir(), "kyc_async_uploads")
				os.MkdirAll(tempDir, os.ModePerm)

				// Generate a unique filename embedding the RequestID for traceability
				tempFilePath := filepath.Join(tempDir, fmt.Sprintf("%s_%s_%s", reqID, utils.GenerateID(), fileHeader.Filename))

				dstFile, err := os.Create(tempFilePath)
				if err != nil {
					srcFile.Close()
					logger.GetLogger().WithError(err).Warn("AsyncMediaIngest: Failed to create temp file")
					continue
				}

				// Stream copy, memory efficient
				size, err := io.Copy(dstFile, srcFile)
				dstFile.Close()
				srcFile.Close()

				if err != nil {
					logger.GetLogger().WithError(err).Warn("AsyncMediaIngest: Failed to copy file data")
					os.Remove(tempFilePath)
					continue
				}

				logger.GetLogger().WithFields(map[string]interface{}{
					"field":      fieldName,
					"filename":   fileHeader.Filename,
					"size":       size,
					"request_id": reqID,
					"temp_path":  tempFilePath,
				}).Info("AsyncMediaIngest: Safely copied file to disk for background ingestion")

				// Map the original filename (and fieldName) to the temp path
				tempFileMap[fileHeader.Filename] = tempFilePath
			}
		}

		if len(tempFileMap) > 0 {
			c.Set("async_temp_files", tempFileMap)
		}

		c.Next()
	}
}
