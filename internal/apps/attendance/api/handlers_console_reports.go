package api

import (
	"net/http"
	"strings"

	"kyc-service/internal/apps/attendance/service"
	"kyc-service/pkg/response"

	"github.com/gin-gonic/gin"
)

func handleGetAttendanceDailyReport(svc *service.AttendanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID := getConsoleOrgID(c)
		reportDate, ok := parseDateParam(c, "date")
		if !ok || reportDate == nil {
			response.JSONError(c, response.CodeInvalidParameter, "invalid date, expected YYYY-MM-DD")
			return
		}
		report, err := svc.BuildDailyReport(c.Request.Context(), orgID, *reportDate, optionalQueryString(c, "group_id"), optionalQueryString(c, "site_id"))
		if err != nil {
			response.JSONError(c, response.CodeInternalError, err.Error())
			return
		}
		response.JSONSuccess(c, report)
	}
}

func handleGetAttendanceMonthlyReport(svc *service.AttendanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID := getConsoleOrgID(c)
		reportMonth := strings.TrimSpace(c.Query("month"))
		if len(reportMonth) != 7 {
			response.JSONError(c, response.CodeInvalidParameter, "invalid month, expected YYYY-MM")
			return
		}
		report, err := svc.BuildMonthlyReport(c.Request.Context(), orgID, reportMonth, optionalQueryString(c, "group_id"), optionalQueryString(c, "site_id"))
		if err != nil {
			response.JSONError(c, response.CodeInternalError, err.Error())
			return
		}
		response.JSONSuccess(c, report)
	}
}

func handleExportAttendanceDailyReport(svc *service.AttendanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID := getConsoleOrgID(c)
		reportDate, ok := parseDateParam(c, "date")
		if !ok || reportDate == nil {
			response.JSONError(c, response.CodeInvalidParameter, "invalid date, expected YYYY-MM-DD")
			return
		}
		csvContent, err := svc.ExportDailyReportCSV(c.Request.Context(), orgID, *reportDate, optionalQueryString(c, "group_id"), optionalQueryString(c, "site_id"))
		if err != nil {
			response.JSONError(c, response.CodeInternalError, err.Error())
			return
		}
		c.Header("Content-Type", "text/csv; charset=utf-8")
		c.Header("Content-Disposition", "attachment; filename=attendance_daily_report.csv")
		c.String(http.StatusOK, csvContent)
	}
}
