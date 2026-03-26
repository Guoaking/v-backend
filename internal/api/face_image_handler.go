package api

import (
	"kyc-service/internal/models"
	"kyc-service/internal/service"
	"net/http"

	"kyc-service/pkg/logger"

	"github.com/gin-gonic/gin"
)

type FaceImageHandler struct{ service *service.KYCService }

func NewFaceImageHandler(s *service.KYCService) *FaceImageHandler {
	return &FaceImageHandler{service: s}
}

func (h *FaceImageHandler) GetImage(c *gin.Context) {
	orgID := c.GetString("orgID")
	id := c.Param("id")
	var ref models.FaceImageRef
	if err := h.service.DB.First(&ref, "id = ? AND organization_id = ?", id, orgID).Error; err != nil {
		JSONError(c, CodeNotFound, "图片不存在")
		return
	}

	// 1. 调用存储服务解析访问路径和策略
	resolved, err := h.service.Storage.ResolveAccess(ref.SafeFilename)
	if err != nil {
		logger.GetLogger().WithError(err).WithField("path", ref.SafeFilename).Error("访问路径解析失败")
		JSONError(c, CodeNotFound, "图片解析错误")
		return
	}

	// 2. 环境感知降级：本地开发模式 (debug) 强制使用 local_stream 策略
	strategy := resolved.Strategy
	if h.service.Config.GinMode == "debug" {
		strategy = "local_stream"
	}

	// 3. 执行分发策略
	switch strategy {
	case "nginx_internal":
		c.Header("Content-Type", "image/jpeg")
		c.Header("X-Accel-Redirect", resolved.InternalPath)
		c.Status(200)
	case "redirect":
		c.Redirect(http.StatusFound, resolved.InternalPath)
	case "local_stream":
		// 自动判断物理路径是否存在并输出
		c.File(ref.SafeFilename)
	default:
		JSONError(c, CodeInternalError, "不支持的存储分发策略")
	}
}
