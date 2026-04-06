package api

import (
	"strings"
	"time"

	"kyc-service/internal/apps/attendance/middleware"
	"kyc-service/internal/apps/attendance/service"
	coreMiddleware "kyc-service/internal/middleware"
	"kyc-service/internal/models"
	coreService "kyc-service/internal/service"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(r *gin.RouterGroup, jwtSecret string, svc *service.AttendanceService) {
	attendanceGroup := r.Group("/attendance")
	attendanceGroup.Use(middleware.RateLimitMiddleware(10))
	attendanceGroup.Use(middleware.MagicLinkAuth(jwtSecret))
	attendanceGroup.Use(coreMiddleware.AsyncMediaIngest(svc.GetKYCService()))
	{
		enrollGroup := attendanceGroup.Group("/enroll")
		{
			enrollGroup.POST("/ocr", handleOCR(svc))
			enrollGroup.POST("/detect", handleFaceDetect(svc))
			enrollGroup.GET("/check", handleCheckEnrollment(svc))
			enrollGroup.POST("/submit", handleSubmit(svc))
		}

		punchGroup := attendanceGroup.Group("/punch")
		{
			punchGroup.GET("/config", handleGetConfig(svc))
			punchGroup.POST("/identity", handleIdentityMatch(svc))
			punchGroup.POST("/liveness/session", handleActionLivenessSession(svc))
			punchGroup.POST("/liveness/upload", handleActionLivenessUpload(svc))
			punchGroup.POST("/liveness/verify", handleActionLivenessVerify(svc))
			punchGroup.POST("", handlePunch(svc))
		}

		selfGroup := attendanceGroup.Group("/self")
		{
			selfGroup.POST("/otp", handleRequestOTP(svc))
			selfGroup.GET("/records", handleGetSelfRecords(svc))
		}
	}
}

func RegisterConsoleRoutes(r *gin.RouterGroup, svc *service.AttendanceService, kycService *coreService.KYCService) {
	consoleAttendanceGroup := r.Group("/attendance")
	consoleAttendanceGroup.Use(coreMiddleware.RequireOrganizationHeader(kycService))
	{
		consoleAttendanceGroup.GET("/magic-link", coreMiddleware.RequirePermission(models.PermAttendanceWrite), handleConsoleGetMagicLink(svc))
		consoleAttendanceGroup.GET("/records", coreMiddleware.RequirePermission(models.PermAttendanceRead), handleConsoleGetRecords(svc))
		consoleAttendanceGroup.PUT("/records/:id/review", coreMiddleware.RequirePermission(models.PermAttendanceReview), handleConsoleReviewRecord(svc))
		consoleAttendanceGroup.GET("/reviews", coreMiddleware.RequirePermission(models.PermAttendanceReview), handleListAttendanceReviews(svc))
		consoleAttendanceGroup.GET("/snapshots", coreMiddleware.RequirePermission(models.PermAttendanceRead), handleListAttendanceSnapshots(svc))
		consoleAttendanceGroup.GET("/timeline/:employee_id", coreMiddleware.RequirePermission(models.PermAttendanceRead), handleGetAttendanceTimeline(svc))
		consoleAttendanceGroup.GET("/stats", coreMiddleware.RequirePermission(models.PermAttendanceRead), handleConsoleGetStats(svc))
		consoleAttendanceGroup.GET("/policy", coreMiddleware.RequirePermission(models.PermAttendanceRead), handleGetAttendancePolicy(svc))
		consoleAttendanceGroup.PUT("/policy", coreMiddleware.RequirePermission(models.PermAttendanceWrite), handleUpsertAttendancePolicy(svc))
		consoleAttendanceGroup.GET("/groups", coreMiddleware.RequirePermission(models.PermAttendanceRead), handleListAttendanceGroups(svc))
		consoleAttendanceGroup.POST("/groups", coreMiddleware.RequirePermission(models.PermAttendanceWrite), handleUpsertAttendanceGroup(svc))
		consoleAttendanceGroup.PUT("/groups/:id", coreMiddleware.RequirePermission(models.PermAttendanceWrite), handleUpsertAttendanceGroup(svc))
		consoleAttendanceGroup.GET("/group-memberships", coreMiddleware.RequirePermission(models.PermAttendanceRead), handleListAttendanceGroupMemberships(svc))
		consoleAttendanceGroup.POST("/group-memberships", coreMiddleware.RequirePermission(models.PermAttendanceWrite), handleUpsertAttendanceGroupMembership(svc))
		consoleAttendanceGroup.PUT("/group-memberships/:id", coreMiddleware.RequirePermission(models.PermAttendanceWrite), handleUpsertAttendanceGroupMembership(svc))
		consoleAttendanceGroup.GET("/sites", coreMiddleware.RequirePermission(models.PermAttendanceRead), handleListAttendanceSites(svc))
		consoleAttendanceGroup.POST("/sites", coreMiddleware.RequirePermission(models.PermAttendanceWrite), handleUpsertAttendanceSite(svc))
		consoleAttendanceGroup.PUT("/sites/:id", coreMiddleware.RequirePermission(models.PermAttendanceWrite), handleUpsertAttendanceSite(svc))
		consoleAttendanceGroup.GET("/shift-templates", coreMiddleware.RequirePermission(models.PermAttendanceRead), handleListAttendanceShiftTemplates(svc))
		consoleAttendanceGroup.POST("/shift-templates", coreMiddleware.RequirePermission(models.PermAttendanceWrite), handleUpsertAttendanceShiftTemplate(svc))
		consoleAttendanceGroup.PUT("/shift-templates/:id", coreMiddleware.RequirePermission(models.PermAttendanceWrite), handleUpsertAttendanceShiftTemplate(svc))
		consoleAttendanceGroup.GET("/shift-assignments", coreMiddleware.RequirePermission(models.PermAttendanceRead), handleListAttendanceShiftAssignments(svc))
		consoleAttendanceGroup.POST("/shift-assignments", coreMiddleware.RequirePermission(models.PermAttendanceWrite), handleUpsertAttendanceShiftAssignment(svc))
		consoleAttendanceGroup.PUT("/shift-assignments/:id", coreMiddleware.RequirePermission(models.PermAttendanceWrite), handleUpsertAttendanceShiftAssignment(svc))
		consoleAttendanceGroup.GET("/reports/daily", coreMiddleware.RequirePermission(models.PermAttendanceReport), handleGetAttendanceDailyReport(svc))
		consoleAttendanceGroup.GET("/reports/monthly", coreMiddleware.RequirePermission(models.PermAttendanceReport), handleGetAttendanceMonthlyReport(svc))
		consoleAttendanceGroup.GET("/reports/export/daily", coreMiddleware.RequirePermission(models.PermAttendanceReport), handleExportAttendanceDailyReport(svc))
	}
}

func getConsoleOrgID(c *gin.Context) string {
	orgID := c.GetString("orgID")
	if orgID != "" {
		return orgID
	}
	return c.Query("org_id")
}

func optionalQueryString(c *gin.Context, key string) *string {
	value := strings.TrimSpace(c.Query(key))
	if value == "" {
		return nil
	}
	return &value
}

func parseDateParam(c *gin.Context, key string) (*time.Time, bool) {
	rawDate := strings.TrimSpace(c.Query(key))
	if rawDate == "" {
		return nil, true
	}
	parsed, err := time.Parse("2006-01-02", rawDate)
	if err != nil {
		return nil, false
	}
	return &parsed, true
}
