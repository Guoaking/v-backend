package api

import (
	"fmt"
	"path/filepath"
	"strings"

	"kyc-service/internal/models"
	"kyc-service/internal/service"

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

	//name := path.Base(ref.SafeFilename)
	name, err := subpathAfterData(ref.SafeFilename)
	if err != nil {
		JSONError(c, CodeNotFound, "图片获取错误")
		return
	}
	c.Header("Content-Type", "image/jpeg")
	c.Header("X-Accel-Redirect", fmt.Sprintf("/_protected/faces/%s", name))
	c.Header("Cache-Control", "private, max-age=60")
	c.Status(200)
}

func subpathAfterData(fullPath string) (string, error) {
	base := "/root/test/vrlFaceServer/vrlFace/data"
	full := filepath.Clean(fullPath)
	baseClean := filepath.Clean(base)
	rel, err := filepath.Rel(baseClean, full)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path outside base")
	}
	return filepath.ToSlash(rel), nil
}
