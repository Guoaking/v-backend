package api

import (
	"github.com/gin-gonic/gin"
	"kyc-service/internal/models"
	"kyc-service/internal/service"
	"net/http"
)

type ImageHandler struct{ service *service.KYCService }

func NewImageHandler(s *service.KYCService) *ImageHandler { return &ImageHandler{service: s} }

func (h *ImageHandler) Upload(c *gin.Context) {
	file, err := c.FormFile("picture")
	if err != nil {
		JSONError(c, CodeInvalidParameter, "缺少图片")
		return
	}
	orgID := c.GetString("orgID")
	ctx := c.Request.Context()
	ctx = c.Request.Context()
	asset, e := h.service.IngestImage(ctx, orgID, file)
	if e != nil {
		JSONError(c, CodeBusinessError, e.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"id":            asset.ID,
		"hash":          asset.Hash,
		"path":          asset.FilePath,
		"safe_filename": asset.SafeFilename,
		"thumb_url":     "",
		"image_url":     "/api/v1/images/" + asset.ID + "/image",
	})
}

func (h *ImageHandler) GetImage(c *gin.Context) {
	orgID := c.GetString("orgID")
	id := c.Param("id")
	var asset models.ImageAsset
	if err := h.service.DB.First(&asset, "id = ? AND organization_id = ?", id, orgID).Error; err != nil {
		JSONError(c, CodeNotFound, "图片不存在")
		return
	}
	c.Header("Content-Type", "image/jpeg")
	c.Header("X-Accel-Redirect", "/_protected/images/"+asset.SafeFilename)
	c.Status(http.StatusOK)
}
