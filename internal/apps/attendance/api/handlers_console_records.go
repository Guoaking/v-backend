package api

import (
	"fmt"
	"strings"

	"kyc-service/internal/apps/attendance/service"
	"kyc-service/pkg/response"

	"github.com/gin-gonic/gin"
)

func handleConsoleGetMagicLink(svc *service.AttendanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID := getConsoleOrgID(c)
		if orgID == "" {
			response.JSONError(c, response.CodeInvalidParameter, "missing org context")
			return
		}
		forceRotate := c.Query("rotate") == "true"
		var token string
		var err error
		if forceRotate {
			token, err = svc.GenerateAppToken(orgID)
		} else {
			token, err = svc.GetActiveAppToken(orgID)
		}
		if err != nil {
			response.JSONError(c, response.CodeInternalError, "Failed to generate magic link")
			return
		}
		baseURL := strings.TrimRight(svc.GetConfig().Config.App.FrontendBaseURL, "/")
		response.JSONSuccess(c, gin.H{
			"token":      token,
			"enroll_url": fmt.Sprintf("%s/attendance/enroll?token=%s", baseURL, token),
			"punch_url":  fmt.Sprintf("%s/attendance/punch?token=%s", baseURL, token),
		})
	}
}

func handleConsoleGetRecords(svc *service.AttendanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID := getConsoleOrgID(c)
		if orgID == "" {
			response.JSONError(c, response.CodeInvalidParameter, "missing org context")
			return
		}
		records, err := svc.GetOrgRecords(c.Request.Context(), orgID, 50)
		if err != nil {
			response.JSONError(c, response.CodeInternalError, "failed to get records")
			return
		}
		response.JSONSuccess(c, records)
	}
}

func handleConsoleReviewRecord(svc *service.AttendanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID := getConsoleOrgID(c)
		if orgID == "" {
			response.JSONError(c, response.CodeInvalidParameter, "missing org context")
			return
		}
		reviewID := c.Param("id")
		if reviewID == "" {
			response.JSONError(c, response.CodeInvalidParameter, "missing review id")
			return
		}
		var req struct {
			Action        string `json:"action"`
			DecisionNotes string `json:"decision_notes"`
			ReviewReason  string `json:"review_reason"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			response.JSONError(c, response.CodeInvalidParameter, err.Error())
			return
		}
		if err := svc.ReviewPunchEvent(c.Request.Context(), orgID, reviewID, c.GetString("userID"), service.ReviewPunchRequest{
			Action:        req.Action,
			DecisionNotes: req.DecisionNotes,
			ReviewReason:  req.ReviewReason,
		}); err != nil {
			switch err {
			case service.ErrPunchReviewNotFound:
				response.JSONError(c, response.CodeNotFound, err.Error())
			case service.ErrInvalidReviewAction:
				response.JSONError(c, response.CodeInvalidParameter, err.Error())
			default:
				response.JSONError(c, response.CodeInternalError, err.Error())
			}
			return
		}
		response.JSONSuccess(c, gin.H{"status": "review updated"})
	}
}

func handleListAttendanceReviews(svc *service.AttendanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID := getConsoleOrgID(c)
		reviews, err := svc.ListAttendancePunchReviews(c.Request.Context(), orgID, c.Query("status"), 100)
		if err != nil {
			response.JSONError(c, response.CodeInternalError, err.Error())
			return
		}
		response.JSONSuccess(c, reviews)
	}
}

func handleListAttendanceSnapshots(svc *service.AttendanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID := getConsoleOrgID(c)
		snapshotDate, ok := parseDateParam(c, "date")
		if !ok {
			response.JSONError(c, response.CodeInvalidParameter, "invalid date, expected YYYY-MM-DD")
			return
		}
		snapshots, err := svc.ListAttendanceStatusSnapshots(c.Request.Context(), orgID, snapshotDate, optionalQueryString(c, "employee_id"), optionalQueryString(c, "group_id"), 100)
		if err != nil {
			response.JSONError(c, response.CodeInternalError, err.Error())
			return
		}
		response.JSONSuccess(c, snapshots)
	}
}

func handleGetAttendanceTimeline(svc *service.AttendanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID := getConsoleOrgID(c)
		employeeID := strings.TrimSpace(c.Param("employee_id"))
		if employeeID == "" {
			response.JSONError(c, response.CodeInvalidParameter, "missing employee_id")
			return
		}
		timeline, err := svc.GetAttendanceEmployeeTimeline(c.Request.Context(), orgID, employeeID, 50)
		if err != nil {
			response.JSONError(c, response.CodeInternalError, err.Error())
			return
		}
		response.JSONSuccess(c, timeline)
	}
}

func handleConsoleGetStats(svc *service.AttendanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID := getConsoleOrgID(c)
		if orgID == "" {
			response.JSONError(c, response.CodeInvalidParameter, "missing org context")
			return
		}
		stats, err := svc.GetOrgStats(c.Request.Context(), orgID)
		if err != nil {
			response.JSONError(c, response.CodeInternalError, "failed to get stats")
			return
		}
		response.JSONSuccess(c, stats)
	}
}
