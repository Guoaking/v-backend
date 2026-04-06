package api

import (
	"time"

	"kyc-service/internal/apps/attendance/service"
	"kyc-service/pkg/response"

	"github.com/gin-gonic/gin"
)

func handleGetAttendancePolicy(svc *service.AttendanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID := getConsoleOrgID(c)
		policy, err := svc.GetAttendancePolicy(c.Request.Context(), orgID)
		if err != nil {
			response.JSONError(c, response.CodeInternalError, err.Error())
			return
		}
		response.JSONSuccess(c, policy)
	}
}

func handleUpsertAttendancePolicy(svc *service.AttendanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID := getConsoleOrgID(c)
		var req struct {
			PunchMode       string `json:"punch_mode"`
			AllowLatePunch  bool   `json:"allow_late_punch"`
			RequireLocation bool   `json:"require_location"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			response.JSONError(c, response.CodeInvalidParameter, err.Error())
			return
		}
		policy, err := svc.UpsertAttendancePolicy(c.Request.Context(), orgID, service.UpsertAttendancePolicyRequest{
			PunchMode: req.PunchMode, AllowLatePunch: req.AllowLatePunch, RequireLocation: req.RequireLocation,
		})
		if err != nil {
			response.JSONError(c, response.CodeInvalidParameter, err.Error())
			return
		}
		response.JSONSuccess(c, policy)
	}
}

func handleListAttendanceGroups(svc *service.AttendanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID := getConsoleOrgID(c)
		groups, err := svc.ListAttendanceGroups(c.Request.Context(), orgID)
		if err != nil {
			response.JSONError(c, response.CodeInternalError, err.Error())
			return
		}
		response.JSONSuccess(c, groups)
	}
}

func handleUpsertAttendanceGroup(svc *service.AttendanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID := getConsoleOrgID(c)
		var req struct {
			Code              string  `json:"code"`
			Name              string  `json:"name"`
			Description       string  `json:"description"`
			ParentGroupID     *string `json:"parent_group_id"`
			ManagerEmployeeID *string `json:"manager_employee_id"`
			Status            string  `json:"status"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			response.JSONError(c, response.CodeInvalidParameter, err.Error())
			return
		}
		group, err := svc.UpsertAttendanceGroup(c.Request.Context(), orgID, service.UpsertAttendanceGroupRequest{
			ID: c.Param("id"), Code: req.Code, Name: req.Name, Description: req.Description, ParentGroupID: req.ParentGroupID, ManagerEmployeeID: req.ManagerEmployeeID, Status: req.Status,
		})
		if err != nil {
			response.JSONError(c, response.CodeInvalidParameter, err.Error())
			return
		}
		response.JSONSuccess(c, group)
	}
}

func handleListAttendanceGroupMemberships(svc *service.AttendanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID := getConsoleOrgID(c)
		memberships, err := svc.ListAttendanceGroupMemberships(c.Request.Context(), orgID, optionalQueryString(c, "group_id"), optionalQueryString(c, "employee_id"))
		if err != nil {
			response.JSONError(c, response.CodeInternalError, err.Error())
			return
		}
		response.JSONSuccess(c, memberships)
	}
}

func handleUpsertAttendanceGroupMembership(svc *service.AttendanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID := getConsoleOrgID(c)
		var req struct {
			GroupID, EmployeeID, MembershipRole, EffectiveFrom, Status string
			IsPrimary                                                  bool
			EffectiveTo                                                *string `json:"effective_to"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			response.JSONError(c, response.CodeInvalidParameter, err.Error())
			return
		}
		effectiveFrom, err := time.Parse("2006-01-02", req.EffectiveFrom)
		if err != nil {
			response.JSONError(c, response.CodeInvalidParameter, "invalid effective_from, expected YYYY-MM-DD")
			return
		}
		var effectiveTo *time.Time
		if req.EffectiveTo != nil && *req.EffectiveTo != "" {
			parsed, err := time.Parse("2006-01-02", *req.EffectiveTo)
			if err != nil {
				response.JSONError(c, response.CodeInvalidParameter, "invalid effective_to, expected YYYY-MM-DD")
				return
			}
			effectiveTo = &parsed
		}
		membership, err := svc.UpsertAttendanceGroupMembership(c.Request.Context(), orgID, service.UpsertAttendanceGroupMembershipRequest{
			ID: c.Param("id"), GroupID: req.GroupID, EmployeeID: req.EmployeeID, MembershipRole: req.MembershipRole, IsPrimary: req.IsPrimary, EffectiveFrom: effectiveFrom, EffectiveTo: effectiveTo, Status: req.Status,
		})
		if err != nil {
			response.JSONError(c, response.CodeInvalidParameter, err.Error())
			return
		}
		response.JSONSuccess(c, membership)
	}
}

func handleListAttendanceSites(svc *service.AttendanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID := getConsoleOrgID(c)
		sites, err := svc.ListAttendanceSites(c.Request.Context(), orgID)
		if err != nil {
			response.JSONError(c, response.CodeInternalError, err.Error())
			return
		}
		response.JSONSuccess(c, sites)
	}
}

func handleUpsertAttendanceSite(svc *service.AttendanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID := getConsoleOrgID(c)
		var req struct {
			SiteCode, Name, Description, AddressLine1, AddressLine2, City, State, CountryCode, PostalCode, Timezone, Status string
			Latitude, Longitude                                                                                             float64
			RadiusMeters                                                                                                    int
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			response.JSONError(c, response.CodeInvalidParameter, err.Error())
			return
		}
		site, err := svc.UpsertAttendanceSite(c.Request.Context(), orgID, service.UpsertAttendanceSiteRequest{
			ID: c.Param("id"), SiteCode: req.SiteCode, Name: req.Name, Description: req.Description, AddressLine1: req.AddressLine1, AddressLine2: req.AddressLine2, City: req.City, State: req.State, CountryCode: req.CountryCode, PostalCode: req.PostalCode, Latitude: req.Latitude, Longitude: req.Longitude, RadiusMeters: req.RadiusMeters, Timezone: req.Timezone, Status: req.Status,
		})
		if err != nil {
			response.JSONError(c, response.CodeInvalidParameter, err.Error())
			return
		}
		response.JSONSuccess(c, site)
	}
}

func handleListAttendanceShiftTemplates(svc *service.AttendanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID := getConsoleOrgID(c)
		shifts, err := svc.ListAttendanceShiftTemplates(c.Request.Context(), orgID)
		if err != nil {
			response.JSONError(c, response.CodeInternalError, err.Error())
			return
		}
		response.JSONSuccess(c, shifts)
	}
}

func handleUpsertAttendanceShiftTemplate(svc *service.AttendanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID := getConsoleOrgID(c)
		var req struct {
			ShiftCode, Name, Description, StartTime, EndTime, Status string
			CrossesDayBoundary, RequireLocation                      bool
			CheckInWindowBeforeMinutes, CheckInWindowAfterMinutes    int
			CheckOutWindowBeforeMinutes, CheckOutWindowAfterMinutes  int
			LateGraceMinutes, EarlyLeaveGraceMinutes, WorkMinutes    int
			DefaultSiteID                                            *string `json:"default_site_id"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			response.JSONError(c, response.CodeInvalidParameter, err.Error())
			return
		}
		shift, err := svc.UpsertAttendanceShiftTemplate(c.Request.Context(), orgID, service.UpsertAttendanceShiftTemplateRequest{
			ID: c.Param("id"), ShiftCode: req.ShiftCode, Name: req.Name, Description: req.Description, StartTime: req.StartTime, EndTime: req.EndTime, CrossesDayBoundary: req.CrossesDayBoundary, CheckInWindowBeforeMinutes: req.CheckInWindowBeforeMinutes, CheckInWindowAfterMinutes: req.CheckInWindowAfterMinutes, CheckOutWindowBeforeMinutes: req.CheckOutWindowBeforeMinutes, CheckOutWindowAfterMinutes: req.CheckOutWindowAfterMinutes, LateGraceMinutes: req.LateGraceMinutes, EarlyLeaveGraceMinutes: req.EarlyLeaveGraceMinutes, WorkMinutes: req.WorkMinutes, RequireLocation: req.RequireLocation, DefaultSiteID: req.DefaultSiteID, Status: req.Status,
		})
		if err != nil {
			response.JSONError(c, response.CodeInvalidParameter, err.Error())
			return
		}
		response.JSONSuccess(c, shift)
	}
}

func handleListAttendanceShiftAssignments(svc *service.AttendanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID := getConsoleOrgID(c)
		assignmentDate, ok := parseDateParam(c, "date")
		if !ok {
			response.JSONError(c, response.CodeInvalidParameter, "invalid date, expected YYYY-MM-DD")
			return
		}
		assignments, err := svc.ListAttendanceShiftAssignments(c.Request.Context(), orgID, assignmentDate)
		if err != nil {
			response.JSONError(c, response.CodeInternalError, err.Error())
			return
		}
		response.JSONSuccess(c, assignments)
	}
}

func handleUpsertAttendanceShiftAssignment(svc *service.AttendanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID := getConsoleOrgID(c)
		var req struct {
			AssignmentDate, ShiftTemplateID, AssignmentSource, Status, Notes string
			EmployeeID, GroupID, SiteID                                      *string
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			response.JSONError(c, response.CodeInvalidParameter, err.Error())
			return
		}
		assignmentDate, err := time.Parse("2006-01-02", req.AssignmentDate)
		if err != nil {
			response.JSONError(c, response.CodeInvalidParameter, "invalid assignment_date, expected YYYY-MM-DD")
			return
		}
		assignment, err := svc.UpsertAttendanceShiftAssignment(c.Request.Context(), orgID, service.UpsertAttendanceShiftAssignmentRequest{
			ID: c.Param("id"), AssignmentDate: assignmentDate, EmployeeID: req.EmployeeID, GroupID: req.GroupID, SiteID: req.SiteID, ShiftTemplateID: req.ShiftTemplateID, AssignmentSource: req.AssignmentSource, Status: req.Status, Notes: req.Notes,
		})
		if err != nil {
			response.JSONError(c, response.CodeInvalidParameter, err.Error())
			return
		}
		response.JSONSuccess(c, assignment)
	}
}
