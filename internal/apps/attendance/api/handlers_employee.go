package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"kyc-service/internal/apps/attendance/middleware"
	"kyc-service/internal/apps/attendance/service"
	"kyc-service/pkg/response"

	"github.com/gin-gonic/gin"
)

type attendanceActionLivenessSessionResponse struct {
	SessionID string   `json:"session_id"`
	RequestID string   `json:"request_id,omitempty"`
	Actions   []string `json:"actions"`
}

func handleActionLivenessSession(svc *service.AttendanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID, exists := c.Get(middleware.AttendanceContextOrgID)
		if !exists {
			response.JSONError(c, response.CodeUnauthorized, "missing org_id in context")
			return
		}
		reqID := c.GetHeader("X-Request-ID")
		if reqID == "" {
			reqID = c.GetHeader("X-Trace-Id")
		}
		sid, actions, err := svc.CreateActionLivenessSession(c.Request.Context(), orgID.(string), reqID, c.ClientIP(), c.Request.UserAgent())
		if err != nil {
			response.JSONError(c, response.CodeBusinessError, err.Error())
			return
		}
		response.JSONSuccess(c, attendanceActionLivenessSessionResponse{SessionID: sid, RequestID: reqID, Actions: actions})
	}
}

func handleActionLivenessUpload(svc *service.AttendanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID, exists := c.Get(middleware.AttendanceContextOrgID)
		if !exists {
			response.JSONError(c, response.CodeUnauthorized, "missing org_id in context")
			return
		}
		sessionID := c.PostForm("session_id")
		if sessionID == "" {
			response.JSONError(c, response.CodeMissingParameter, "Missing session_id")
			return
		}
		file, err := c.FormFile("video")
		if err != nil {
			response.JSONError(c, response.CodeInvalidParameter, "Missing video")
			return
		}
		reqID := c.GetHeader("X-Request-ID")
		if reqID == "" {
			reqID = c.GetHeader("X-Trace-Id")
		}
		if err := svc.UploadActionLivenessVideo(c.Request.Context(), orgID.(string), sessionID, reqID, c.ClientIP(), c.Request.UserAgent(), file); err != nil {
			if strings.Contains(err.Error(), "Quota exceeded") {
				c.Header("X-RateLimit-Remaining", "0")
				response.JSONErrorWithStatus(c, response.CodeTooManyRequests, "Quota Limit Reached", http.StatusTooManyRequests)
				return
			}
			response.JSONError(c, response.CodeBusinessError, err.Error())
			return
		}
		response.JSONSuccess(c, gin.H{"session_id": sessionID, "uploaded": true, "submitted": true})
	}
}

func handleActionLivenessVerify(svc *service.AttendanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID, exists := c.Get(middleware.AttendanceContextOrgID)
		if !exists {
			response.JSONError(c, response.CodeUnauthorized, "missing org_id in context")
			return
		}
		var body struct {
			SessionID string `json:"session_id"`
		}
		_ = c.ShouldBindJSON(&body)
		if body.SessionID == "" {
			response.JSONError(c, response.CodeMissingParameter, "Missing session_id")
			return
		}
		reqID := c.GetHeader("X-Request-ID")
		if reqID == "" {
			reqID = c.GetHeader("X-Trace-Id")
		}
		res, err := svc.VerifyActionLiveness(c.Request.Context(), orgID.(string), body.SessionID, reqID, c.ClientIP(), c.Request.UserAgent())
		if err != nil {
			response.JSONError(c, response.CodeBusinessError, err.Error())
			return
		}
		response.JSONSuccess(c, res)
	}
}

func handleOCR(svc *service.AttendanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID, exists := c.Get(middleware.AttendanceContextOrgID)
		if !exists {
			response.JSONError(c, response.CodeInvalidParameter, "Missing org_id in context")
			return
		}
		file, err := c.FormFile("image")
		if err != nil {
			response.JSONError(c, response.CodeInvalidParameter, "Missing image file")
			return
		}
		idType := c.DefaultPostForm("id_type", "thai_id")
		res, err := svc.EnrollOCR(c.Request.Context(), orgID.(string), file, idType)
		if err != nil {
			response.JSONError(c, response.CodeInternalError, err.Error())
			return
		}
		response.JSONSuccess(c, res)
	}
}

func handleCheckEnrollment(svc *service.AttendanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID, exists := c.Get(middleware.AttendanceContextOrgID)
		if !exists {
			response.JSONError(c, response.CodeUnauthorized, "missing org context")
			return
		}
		idNumber := c.Query("id_number")
		if idNumber == "" {
			response.JSONError(c, response.CodeInvalidParameter, "id_number is required")
			return
		}
		emp, err := svc.GetEmployeeByIDNumber(c.Request.Context(), orgID.(string), idNumber)
		if err != nil {
			response.JSONSuccess(c, gin.H{"enrolled": false})
			return
		}
		response.JSONSuccess(c, gin.H{"enrolled": true, "employee_no": emp.EmployeeNo})
	}
}

func handleFaceDetect(svc *service.AttendanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID, exists := c.Get(middleware.AttendanceContextOrgID)
		if !exists {
			response.JSONError(c, response.CodeUnauthorized, "missing org context")
			return
		}
		file, err := c.FormFile("picture")
		if err != nil {
			response.JSONError(c, response.CodeInvalidParameter, "missing picture file")
			return
		}
		ctx := context.WithValue(c.Request.Context(), "org_id", orgID.(string))
		res, err := svc.GetKYCService().FaceDetect(ctx, file)
		if err != nil {
			response.JSONError(c, response.CodeThirdPartyError, err.Error())
			return
		}
		response.JSONSuccess(c, res)
	}
}

func handleSubmit(svc *service.AttendanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID, exists := c.Get(middleware.AttendanceContextOrgID)
		if !exists {
			response.JSONError(c, response.CodeUnauthorized, "missing org_id in context")
			return
		}
		if err := c.Request.ParseMultipartForm(10 << 20); err != nil {
			response.JSONError(c, response.CodeBadRequest, "failed to parse multipart form")
			return
		}
		req := service.EnrollRequest{
			SessionID:   c.PostForm("session_id"),
			IDNumber:    c.PostForm("id_number"),
			Name:        c.PostForm("name"),
			Phone:       c.PostForm("phone"),
			IDType:      c.PostForm("id_type"),
			RawImageURL: c.PostForm("raw_image_url"),
			RawOCRJSON:  c.PostForm("raw_ocr_json"),
		}
		file, err := c.FormFile("face_image")
		if err != nil {
			response.JSONError(c, response.CodeInvalidParameter, "face_image is required")
			return
		}
		req.FaceFile = file
		if req.IDNumber == "" || req.Name == "" || req.Phone == "" {
			response.JSONError(c, response.CodeInvalidParameter, "missing required fields (id_number, name, phone)")
			return
		}
		emp, err := svc.EnrollEmployee(c.Request.Context(), orgID.(string), &req)
		if err != nil {
			if err == service.ErrAlreadyEnrolled {
				c.JSON(http.StatusConflict, gin.H{"code": 2009, "message": "already enrolled", "data": gin.H{"employee_no": emp.EmployeeNo}})
				return
			}
			response.JSONError(c, response.CodeInternalError, err.Error())
			return
		}
		response.JSONSuccess(c, gin.H{"message": "enrollment success", "employee_no": emp.EmployeeNo})
	}
}

func handleGetConfig(svc *service.AttendanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID, exists := c.Get(middleware.AttendanceContextOrgID)
		if !exists {
			response.JSONError(c, response.CodeUnauthorized, "missing org_id in context")
			return
		}
		config, err := svc.GetPunchConfig(c.Request.Context(), orgID.(string))
		if err != nil {
			response.JSONError(c, response.CodeInternalError, "failed to get attendance policy")
			return
		}
		response.JSONSuccess(c, config)
	}
}

func handleIdentityMatch(svc *service.AttendanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID, exists := c.Get(middleware.AttendanceContextOrgID)
		if !exists {
			response.JSONError(c, response.CodeInvalidParameter, "Missing org_id in context")
			return
		}
		var req service.IdentityMatchRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			response.JSONError(c, response.CodeInvalidParameter, err.Error())
			return
		}
		res, err := svc.MatchIdentity(c.Request.Context(), orgID.(string), req.Query)
		if err != nil {
			if err == service.ErrIdentityNotFound {
				response.JSONError(c, response.CodeIdentityNotFound, err.Error())
				return
			}
			response.JSONError(c, response.CodeInternalError, err.Error())
			return
		}
		response.JSONSuccess(c, res)
	}
}

func handlePunch(svc *service.AttendanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID, exists := c.Get(middleware.AttendanceContextOrgID)
		if !exists {
			response.JSONError(c, response.CodeUnauthorized, "missing org_id in context")
			return
		}
		if err := c.Request.ParseMultipartForm(10 << 20); err != nil {
			response.JSONError(c, response.CodeBadRequest, "failed to parse multipart form")
			return
		}
		req := service.PunchRequest{
			EmployeeNo:   c.PostForm("employee_no"),
			IDNumber:     c.PostForm("id_number"),
			PunchType:    c.PostForm("punch_type"),
			FallbackMode: c.PostForm("fallback_mode") == "true",
		}
		if lat := c.PostForm("latitude"); lat != "" {
			fmt.Sscanf(lat, "%f", &req.Latitude)
		}
		if lng := c.PostForm("longitude"); lng != "" {
			fmt.Sscanf(lng, "%f", &req.Longitude)
		}
		if (req.EmployeeNo == "" && req.IDNumber == "") || req.PunchType == "" {
			response.JSONError(c, response.CodeInvalidParameter, "employee_no or id_number and punch_type are required")
			return
		}
		file, _ := c.FormFile("liveness_image")
		req.LivenessFile = file
		req.LivenessTaskID = c.PostForm("liveness_task_id")
		if err := svc.PunchIn(c.Request.Context(), orgID.(string), &req); err != nil {
			if err == service.ErrIdentityNotFound {
				response.JSONError(c, response.CodeIdentityNotFound, err.Error())
				return
			}
			if err == service.ErrFaceVerificationFailed {
				response.JSONError(c, response.CodeFaceVerifyFailed, err.Error())
				return
			}
			response.JSONError(c, response.CodeInternalError, err.Error())
			return
		}
		response.JSONSuccess(c, map[string]string{"status": "punch successful"})
	}
}

func handleRequestOTP(svc *service.AttendanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		response.JSONErrorWithStatus(c, response.CodeForbidden, "employee self-service is disabled until employee session auth is implemented", http.StatusForbidden)
	}
}

func handleGetSelfRecords(svc *service.AttendanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		response.JSONErrorWithStatus(c, response.CodeForbidden, "employee self-service records are disabled until employee session auth is implemented", http.StatusForbidden)
	}
}
