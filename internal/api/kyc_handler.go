package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"kyc-service/internal/service"
	"kyc-service/pkg/logger"

	"github.com/gin-gonic/gin"
)

type KYCHandler struct {
	service *service.KYCService
}

func NewKYCHandler(svc *service.KYCService) *KYCHandler {
	return &KYCHandler{service: svc}
}

// OCR
// @Summary OCR recognition
// @Description Upload ID card image for OCR recognition
// @Tags Public
// @Tags OCR
// @Accept multipart/form-data
// @Produce json
// @Param picture formData file true "ID card image"
// @Param language formData string false "Language default:thai" Enums(thai,vietnamese,indonesian,chinese,english,tagalog,malay) example(thai)
// @Success 200 {object} OCRSuccessResponse
// @Router /kyc/ocr [post]
// @Security ApiKeyAuth
func (h *KYCHandler) OCR(c *gin.Context) {
	//for key, v := range c.Request.Header {
	//	fmt.Printf("output: %v, %v\n", key, v)
	//}

	fmt.Printf("output: %v\n", "start")
	var req service.OCRRequest
	if err := c.ShouldBind(&req); err != nil {
		logger.GetLogger().Errorf("get params err: %v", err)
		JSONError(c, CodeInvalidParameter, fmt.Sprintf("Invalid request body: %v", err))
		return
	}

	if req.Type == "" {
		c.Header("Warning", "299 - Deprecation: default type id_card; pass 'type' param")
		req.Type = "general"
	}

	req.Language = GetSupportLang(req.Language)

	//if req.Language
	ctx := c.Request.Context()
	ctx = context.WithValue(ctx, "org_id", c.GetString("orgID"))
	now := time.Now()
	result, err := h.service.OCR(ctx, &req)
	fmt.Printf("output: %v, %v\n", "end", time.Since(now))
	if err != nil {
		if strings.Contains(err.Error(), "Quota exceeded") {
			JSONError(c, CodePaymentRequired, "Quota exceeded. Please upgrade your plan.")
			return
		}
		if errors.Is(err, service.ErrUpstreamTimeout) {
			JSONErrorWithStatus(c, CodeThirdPartyError, err.Error(), http.StatusGatewayTimeout)
			return
		}
		if errors.Is(err, service.ErrUpstreamUnavailable) {
			JSONErrorWithStatus(c, CodeThirdPartyError, err.Error(), http.StatusBadGateway)
			return
		}
		JSONError(c, CodeOCRFailed, err.Error())
		return
	}

	JSONSuccess(c, result)
}

//	func GetSupportLang(lang string) string {
//		switch lang {
//		case "zh", "en", "th", "id", "tl", "ms", "vi":
//			return lang
//		default:
//			return "th"
//		}
//	}
func GetSupportLang2(lang string) string {
	switch lang {
	case "chinese", "english", "tagalog", "malay", "indonesian", "vietnamese", "thai":
		return lang
	default:
		return "thai"
	}
}

// { code: 'th', label: 'Thai', flag: '🇹🇭' },
// { code: 'vi', label: 'Vietnamese', flag: '🇻🇳' },
// { code: 'id', label: 'Indonesian', flag: '🇮🇩' },
// { code: 'ms', label: 'Malay', flag: '🇲🇾' },
// { code: 'tl', label: 'Tagalog', flag: '🇵🇭' },
// { code: 'en', label: 'English', flag: '🇬🇧' },
// { code: 'zh', label: 'Chinese', flag: '🇨🇳' },
func GetSupportLang(lang string) string {
	switch strings.ToLower(lang) {
	case "zh", "chinese":
		return "chinese"
	case "en", "english":
		return "english"
	case "th", "thai":
		return "thai"
	case "id", "indonesian":
		return "indonesian"
	case "tl", "tagalog":
		return "tagalog"
	case "ms", "malay":
		return "malay"
	case "vi", "vietnamese":
		return "vietnamese"
	default:
		return "thai"
	}
}

// FaceSearch 人脸搜索
// @Summary Face search
// @Description Upload a face image to search similar faces
// @Tags Public
// @Accept multipart/form-data
// @Produce json
// @Param picture formData file true "Face image"
// @Success 200 {object} FaceSearchSuccessResponse
// @Router /kyc/face/search [post]
// @Security ApiKeyAuth
func (h *KYCHandler) FaceSearch(c *gin.Context) {
	file, err := c.FormFile("picture")
	if err != nil {
		JSONError(c, CodeInvalidParameter, "Missing image")
		return
	}
	ctx := c.Request.Context()
	ctx = context.WithValue(ctx, "org_id", c.GetString("orgID"))
	res, e := h.service.FaceSearch(ctx, file)
	if e != nil {
		if errors.Is(e, service.ErrUpstreamTimeout) {
			JSONErrorWithStatus(c, CodeThirdPartyError, e.Error(), http.StatusGatewayTimeout)
			return
		}
		if errors.Is(e, service.ErrUpstreamUnavailable) {
			JSONErrorWithStatus(c, CodeThirdPartyError, e.Error(), http.StatusBadGateway)
			return
		}
		JSONError(c, CodeBusinessError, e.Error())
		return
	}
	JSONSuccess(c, res)
}

// FaceCompare 人脸比对
// @Summary Face comparison
// @Description Upload two images for face comparison
// @Tags Public
// @Accept multipart/form-data
// @Produce json
// @Param source_image formData file true "Source image"
// @Param target_image formData file true "Target image"
// @Success 200 {object} FaceCompareSuccessResponse
// @Router /kyc/face/compare [post]
// @Security ApiKeyAuth
func (h *KYCHandler) FaceCompare(c *gin.Context) {
	src, err1 := c.FormFile("source_image")
	dst, err2 := c.FormFile("target_image")
	if err1 != nil || err2 != nil {
		JSONError(c, CodeInvalidParameter, "Missing image")
		return
	}
	ctx := c.Request.Context()
	ctx = context.WithValue(ctx, "org_id", c.GetString("orgID"))
	res, e := h.service.FaceCompare(ctx, src, dst)
	if e != nil {
		if errors.Is(e, service.ErrUpstreamTimeout) {
			JSONErrorWithStatus(c, CodeThirdPartyError, e.Error(), http.StatusGatewayTimeout)
			return
		}
		if errors.Is(e, service.ErrUpstreamUnavailable) {
			JSONErrorWithStatus(c, CodeThirdPartyError, e.Error(), http.StatusBadGateway)
			return
		}
		JSONError(c, CodeBusinessError, e.Error())
		return
	}
	JSONSuccess(c, res)
}

// FaceDetect 人脸检测
// @Summary Face detection
// @Description Upload an image for face detection
// @Tags Public
// @Accept multipart/form-data
// @Produce json
// @Param picture formData file true "Face image"
// @Success 200 {object} FaceDetectSuccessResponse
// @Router /kyc/face/detect [post]
// @Security ApiKeyAuth
func (h *KYCHandler) FaceDetect(c *gin.Context) {
	file, err := c.FormFile("picture")
	if err != nil {
		JSONError(c, CodeInvalidParameter, "Missing image")
		return
	}
	ctx := c.Request.Context()
	ctx = context.WithValue(ctx, "org_id", c.GetString("orgID"))
	res, e := h.service.FaceDetect(ctx, file)
	if e != nil {
		if errors.Is(e, service.ErrUpstreamTimeout) {
			JSONErrorWithStatus(c, CodeThirdPartyError, e.Error(), http.StatusGatewayTimeout)
			return
		}
		if errors.Is(e, service.ErrUpstreamUnavailable) {
			JSONErrorWithStatus(c, CodeThirdPartyError, e.Error(), http.StatusBadGateway)
			return
		}
		JSONError(c, CodeBusinessError, e.Error())
		return
	}
	JSONSuccess(c, res)
}

// LivenessSilent 静态活体
// @Summary Silent liveness
// @Description Upload an image for silent liveness detection
// @Tags Public
// @Accept multipart/form-data
// @Produce json
// @Param picture formData file true "Image"
// @Param language formData string false "Language" Enums(zh,zh-CN,en,en-US) example(zh-CN)
// @Success 200 {object} LivenessSilentSuccessResponse
// @Router /kyc/liveness/silent [post]
// @Security ApiKeyAuth
func (h *KYCHandler) LivenessSilent(c *gin.Context) {
	file, err := c.FormFile("picture")
	if err != nil {
		JSONError(c, CodeInvalidParameter, "缺少图片")
		return
	}
	language := c.PostForm("language")
	ctx := c.Request.Context()
	ctx = context.WithValue(ctx, "org_id", c.GetString("orgID"))
	res, e := h.service.LivenessSilent(ctx, file, language)
	if e != nil {
		if errors.Is(e, service.ErrUpstreamTimeout) {
			JSONErrorWithStatus(c, CodeThirdPartyError, e.Error(), http.StatusGatewayTimeout)
			return
		}
		if errors.Is(e, service.ErrUpstreamUnavailable) {
			JSONErrorWithStatus(c, CodeThirdPartyError, e.Error(), http.StatusBadGateway)
			return
		}
		JSONError(c, CodeBusinessError, e.Error())
		return
	}
	JSONSuccess(c, res)
}

// LivenessVideo
// @Summary Video liveness
// @Description Upload a video for movement liveness detection
// @Accept multipart/form-data
// @Produce json
// @Param video formData file true "Video"
// @Param language formData string false "Language" Enums(zh,zh-CN,en,en-US) example(en-US)
// @Success 200 {object} LivenessVideoSuccessResponse
// @Router /kyc/liveness/video [post]
// @Security ApiKeyAuth
func (h *KYCHandler) LivenessVideo(c *gin.Context) {
	file, err := c.FormFile("video")
	if err != nil {
		JSONError(c, CodeInvalidParameter, "Missing video")
		return
	}
	language := c.PostForm("language")
	ctx := c.Request.Context()
	ctx = context.WithValue(ctx, "org_id", c.GetString("orgID"))
	res, e := h.service.LivenessVideo(ctx, file, language)
	if e != nil {
		if errors.Is(e, service.ErrUpstreamTimeout) {
			JSONErrorWithStatus(c, CodeThirdPartyError, e.Error(), http.StatusGatewayTimeout)
			return
		}
		if errors.Is(e, service.ErrUpstreamUnavailable) {
			JSONErrorWithStatus(c, CodeThirdPartyError, e.Error(), http.StatusBadGateway)
			return
		}
		JSONError(c, CodeBusinessError, e.Error())
		return
	}
	JSONSuccess(c, res)
}

// LivenessWebSocket
// @Summary Liveness WebSocket
// @Description Real-time liveness detection via WebSocket
// @Tags KYC
// @Success 101 {string} string "Switching Protocols"
// @Router /kyc/liveness/ws [get]
// @Security ApiKeyAuth
func (h *KYCHandler) LivenessWebSocket(c *gin.Context) {
	// 升级WebSocket连接
	conn, err := h.service.Upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.GetLogger().WithError(err).Error("WebSocket升级失败")
		return
	}

	if err := h.service.LivenessWebSocket(c.Request.Context(), conn); err != nil {
		logger.GetLogger().WithError(err).Error("活体检测处理失败")
	}
}

// CompleteKYC
// @Summary Complete KYC verification
// @Description Upload ID card image, face image and basic info for full KYC verification
// @Tags KYC
// @Accept multipart/form-data
// @Produce json
// @Param idcard_image formData file true "ID card image"
// @Param face_image formData file true "Face image"
// @Param name formData string true "Name"
// @Param idcard formData string true "ID card number"
// @Param phone formData string false "Phone number"
// @Success 200 {object} SuccessResponse
// @Router /kyc/verify [post]
// @Security ApiKeyAuth
func (h *KYCHandler) CompleteKYC(c *gin.Context) {
	var req service.CompleteKYCRequest
	if err := c.ShouldBind(&req); err != nil {
		JSONError(c, CodeInvalidParameter, "Invalid request body")
		return
	}

	result, err := h.service.CompleteKYC(c.Request.Context(), &req)
	if err != nil {
		JSONError(c, CodeKYCFailed, err.Error())
		return
	}

	JSONSuccess(c, result)
}

// GetKYCStatus
// @Summary 查询KYC状态
// @Description 根据请求ID查询KYC认证状态
// @Tags KYC
// @Produce json
// @Param request_id path string true "请求ID"
// @Success 200 {object} SuccessResponse
// @Router /kyc/status/{request_id} [get]
// @Security ApiKeyAuth
func (h *KYCHandler) GetKYCStatus(c *gin.Context) { //ignore_security_alert IDOR
	requestID := c.Param("request_id")
	if requestID == "" {
		JSONError(c, CodeMissingParameter, "请求ID不能为空")
		return
	}

	result, err := h.service.GetKYCStatus(c.Request.Context(), requestID)
	if err != nil {
		JSONError(c, CodeNotFound, err.Error())
		return
	}

	JSONSuccess(c, result)
}
