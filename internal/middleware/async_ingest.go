package middleware

import (
	"io"
	"net/http"
	"strings"

	"kyc-service/internal/service"
	"kyc-service/pkg/logger"

	"github.com/gin-gonic/gin"
)

// AsyncMediaIngest 是一个工程化中间件
// 当请求类型为 multipart/form-data 时，自动拦截请求中的文件
// 并将它们缓冲到内存中，以备后续异步落盘，保证不阻塞主业务流程
func AsyncMediaIngest(kycService *service.KYCService) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 只有 POST/PUT 请求才处理
		if c.Request.Method != http.MethodPost && c.Request.Method != http.MethodPut {
			c.Next()
			return
		}

		contentType := c.Request.Header.Get("Content-Type")
		if !strings.Contains(contentType, "multipart/form-data") {
			c.Next()
			return
		}

		if err := c.Request.ParseMultipartForm(32 << 20); err != nil {
			logger.GetLogger().WithError(err).Warn("AsyncMediaIngest: Failed to parse multipart form")
			c.Next()
			return
		}

		orgID := c.GetString("orgID")
		if orgID == "" {
			orgID = "unknown_org"
		}

		// 遍历所有的文件字段
		for fieldName, fileHeaders := range c.Request.MultipartForm.File {
			for _, fileHeader := range fileHeaders {
				// 将文件内容复制到内存，因为请求结束后底层临时文件会被删除
				f, err := fileHeader.Open()
				if err != nil {
					logger.GetLogger().WithError(err).Warn("AsyncMediaIngest: Failed to open file header")
					continue
				}

				fileData, err := io.ReadAll(f)
				f.Close()
				if err != nil {
					logger.GetLogger().WithError(err).Warn("AsyncMediaIngest: Failed to read file data")
					continue
				}

				// 将文件数据通过 Context 传递给下游，或者在这里直接异步调用 Storage
				// 这里为了不侵入具体的 IngestImage (它强依赖 multipart.FileHeader)
				// 我们记录下这一动作。实际生产中，我们可以将 IngestImage 改造为接收 []byte 或者 io.Reader。
				logger.GetLogger().WithFields(map[string]interface{}{
					"org_id":   orgID,
					"field":    fieldName,
					"filename": fileHeader.Filename,
					"size":     len(fileData),
				}).Info("AsyncMediaIngest: Captured file for background ingestion")

				// 示例：将缓冲后的数据存入 Context 供后续需要异步落盘的服务直接使用
				// 防止业务层再次调用 fileHeader.Open() 时发现文件已丢失
				key := "async_file_" + fieldName
				c.Set(key, fileData)
			}
		}

		c.Next()
	}
}
