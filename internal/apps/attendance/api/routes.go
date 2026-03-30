package api

import (
	"fmt"
	"strings"

	"kyc-service/internal/apps/attendance/middleware"
	"kyc-service/internal/apps/attendance/service"
	"kyc-service/pkg/response"

	"github.com/gin-gonic/gin"
)

// RegisterRoutes 注册考勤微应用的所有路由
// 这里展示了微应用的路由是如何从核心代码中物理隔离的。
// 我们将 jwtSecret 作为参数传入，保持了底层对配置的解耦。
func RegisterRoutes(r *gin.RouterGroup, jwtSecret string, svc *service.AttendanceService) {
	// 所有考勤 C端 (H5) 相关的路由，挂载在 /api/v1/attendance 下
	// 这些路由不需要登录，但需要一个特殊的 Magic Link Token 中间件来提取 OrgID
	attendanceGroup := r.Group("/attendance")

	// 这里挂载打卡的限流中间件 (Rate Limiter)，防止高并发或恶意请求打挂底层 GPU
	// 根据架构设计，限制为 10 QPS，触发限流时直接返回 429 降级
	attendanceGroup.Use(middleware.RateLimitMiddleware(10))
	attendanceGroup.Use(middleware.MagicLinkAuth(jwtSecret))
	{
		// 1. 注册相关
		enrollGroup := attendanceGroup.Group("/enroll")
		{
			enrollGroup.POST("/ocr", handleOCR(svc))
			enrollGroup.POST("/submit", handleSubmit(svc))
		}

		// 2. 打卡相关
		punchGroup := attendanceGroup.Group("/punch")
		{
			punchGroup.GET("/config", handleGetConfig(svc))
			punchGroup.GET("/identity", handleIdentityMatch(svc))
			punchGroup.POST("", handlePunch(svc))
		}

		// 3. 员工自助查询
		selfGroup := attendanceGroup.Group("/self")
		{
			selfGroup.POST("/otp", handleRequestOTP(svc))
			selfGroup.GET("/records", handleGetSelfRecords(svc))
		}
	}

	// 所有考勤 B端 (老板/HR Console) 相关的路由，挂载在 /api/v1/console/attendance 下
	// 注意：这些路由应该在外部被现有的 Console JWT Auth 中间件保护
	consoleAttendanceGroup := r.Group("/console/attendance")
	{
		// 生成/获取 Magic Link 接口
		consoleAttendanceGroup.GET("/magic-link", handleConsoleGetMagicLink(svc, jwtSecret))

		consoleAttendanceGroup.GET("/records", handleConsoleGetRecords(svc))
		consoleAttendanceGroup.PUT("/records/:id/review", handleConsoleReviewRecord(svc))
		consoleAttendanceGroup.GET("/stats", handleConsoleGetStats(svc))
	}
}

// ==============================================================================
// 以下为 Handler 骨架 (待填充具体业务逻辑)
// ==============================================================================

func handleConsoleGetMagicLink(svc *service.AttendanceService, jwtSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 在真实的 Console 路由中，orgID 应该是从登录老板的 Context/Token 里拿到的
		orgID := c.Query("org_id")
		if orgID == "" {
			response.JSONError(c, response.CodeInvalidParameter, "Missing org_id parameter")
			return
		}

		// 1. Get or Generate Active Token
		token, err := svc.GetActiveAppToken(orgID)
		if err != nil {
			response.JSONError(c, response.CodeInternalError, "Failed to generate magic link")
			return
		}

		// 2. Fetch the frontend return URL from the application config
		// Use the dedicated App.FrontendBaseURL configuration
		baseURL := svc.GetConfig().Config.App.FrontendBaseURL
		baseURL = strings.TrimRight(baseURL, "/")

		// 拼接成完整的 H5 URL
		// 注意：实际部署时，域名应该从配置中读取
		enrollURL := fmt.Sprintf("%s/attendance/enroll?token=%s", baseURL, token)
		punchURL := fmt.Sprintf("%s/attendance/punch?token=%s", baseURL, token)

		response.JSONSuccess(c, gin.H{
			"token":      token,
			"enroll_url": enrollURL,
			"punch_url":  punchURL,
		})
	}
}

func handleOCR(svc *service.AttendanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID, exists := c.Get(middleware.AttendanceContextOrgID)
		if !exists {
			response.JSONError(c, response.CodeInvalidParameter, "Missing org_id in context")
			return
		}

		// 这里假设前端使用 multipart/form-data 上传
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

		// 提取图片文件
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

		if err := svc.EnrollEmployee(c.Request.Context(), orgID.(string), &req); err != nil {
			if err == service.ErrAlreadyEnrolled {
				response.JSONError(c, response.CodeAlreadyEnrolled, err.Error())
				return
			}
			response.JSONError(c, response.CodeInternalError, err.Error())
			return
		}

		response.JSONSuccess(c, map[string]string{
			"status": "enrolled",
		})
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
			response.JSONError(c, response.CodeInternalError, "failed to get organization settings")
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

		// 解析 multipart form
		if err := c.Request.ParseMultipartForm(10 << 20); err != nil {
			response.JSONError(c, response.CodeBadRequest, "failed to parse multipart form")
			return
		}

		req := service.PunchRequest{
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

		if req.IDNumber == "" || req.PunchType == "" {
			response.JSONError(c, response.CodeInvalidParameter, "id_number and punch_type are required")
			return
		}

		// 提取图片文件 (如果不是动作活体模式)
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

		response.JSONSuccess(c, map[string]string{
			"status": "punch successful",
		})
	}
}

func handleRequestOTP(svc *service.AttendanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		response.JSONSuccess(c, gin.H{"status": "ok"})
	}
}

func handleGetSelfRecords(svc *service.AttendanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		response.JSONSuccess(c, gin.H{"status": "ok"})
	}
}

func handleConsoleGetRecords(svc *service.AttendanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		response.JSONSuccess(c, gin.H{"status": "ok"})
	}
}

func handleConsoleReviewRecord(svc *service.AttendanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		response.JSONSuccess(c, gin.H{"status": "ok"})
	}
}

func handleConsoleGetStats(svc *service.AttendanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		response.JSONSuccess(c, gin.H{"status": "ok"})
	}
}
