package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"kyc-service/internal/apps/attendance/models"
	"kyc-service/pkg/utils"

	"gorm.io/gorm"
)

type UpsertAttendanceGroupRequest struct {
	ID                string
	Code              string
	Name              string
	Description       string
	ParentGroupID     *string
	ManagerEmployeeID *string
	Status            string
}

type UpsertAttendanceSiteRequest struct {
	ID           string
	SiteCode     string
	Name         string
	Description  string
	AddressLine1 string
	AddressLine2 string
	City         string
	State        string
	CountryCode  string
	PostalCode   string
	Latitude     float64
	Longitude    float64
	RadiusMeters int
	Timezone     string
	Status       string
}

type UpsertAttendanceShiftTemplateRequest struct {
	ID                          string
	ShiftCode                   string
	Name                        string
	Description                 string
	StartTime                   string
	EndTime                     string
	CrossesDayBoundary          bool
	CheckInWindowBeforeMinutes  int
	CheckInWindowAfterMinutes   int
	CheckOutWindowBeforeMinutes int
	CheckOutWindowAfterMinutes  int
	LateGraceMinutes            int
	EarlyLeaveGraceMinutes      int
	WorkMinutes                 int
	RequireLocation             bool
	DefaultSiteID               *string
	Status                      string
}

type UpsertAttendanceShiftAssignmentRequest struct {
	ID               string
	AssignmentDate   time.Time
	EmployeeID       *string
	GroupID          *string
	SiteID           *string
	ShiftTemplateID  string
	AssignmentSource string
	Status           string
	Notes            string
}

type UpsertAttendancePolicyRequest struct {
	PunchMode       string
	AllowLatePunch  bool
	RequireLocation bool
}

type UpsertAttendanceGroupMembershipRequest struct {
	ID             string
	GroupID        string
	EmployeeID     string
	MembershipRole string
	IsPrimary      bool
	EffectiveFrom  time.Time
	EffectiveTo    *time.Time
	Status         string
}

var ErrInvalidAssignmentScope = fmt.Errorf("exactly one of employee_id or group_id is required")

func normalizeOptionalString(v *string) *string {
	if v == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*v)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func ptrOrNil(v string) *string {
	trimmed := strings.TrimSpace(v)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func (s *AttendanceService) ensureEmployeeExists(tx *gorm.DB, orgID string, employeeID *string) error {
	if employeeID == nil || *employeeID == "" {
		return nil
	}
	var count int64
	if err := tx.Model(&models.OrganizationEmployee{}).Where("org_id = ? AND id = ?", orgID, *employeeID).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return ErrIdentityNotFound
	}
	return nil
}

func (s *AttendanceService) ensureGroupExists(tx *gorm.DB, orgID string, groupID *string) error {
	if groupID == nil || *groupID == "" {
		return nil
	}
	var count int64
	if err := tx.Model(&models.AttendanceGroup{}).Where("org_id = ? AND id = ?", orgID, *groupID).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (s *AttendanceService) ensureSiteExists(tx *gorm.DB, orgID string, siteID *string) error {
	if siteID == nil || *siteID == "" {
		return nil
	}
	var count int64
	if err := tx.Model(&models.AttendanceSite{}).Where("org_id = ? AND id = ?", orgID, *siteID).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (s *AttendanceService) ensureShiftTemplateExists(tx *gorm.DB, orgID, shiftTemplateID string) error {
	var count int64
	if err := tx.Model(&models.AttendanceShiftTemplate{}).Where("org_id = ? AND id = ?", orgID, shiftTemplateID).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (s *AttendanceService) ListAttendanceGroups(ctx context.Context, orgID string) ([]models.AttendanceGroup, error) {
	var groups []models.AttendanceGroup
	if err := s.db.Where("org_id = ?", orgID).Order("created_at DESC").Find(&groups).Error; err != nil {
		return nil, fmt.Errorf("failed to list attendance groups: %w", err)
	}
	return groups, nil
}

func (s *AttendanceService) UpsertAttendanceGroup(ctx context.Context, orgID string, req UpsertAttendanceGroupRequest) (*models.AttendanceGroup, error) {
	groupID := strings.TrimSpace(req.ID)
	group := models.AttendanceGroup{}
	if groupID != "" {
		if err := s.db.Where("org_id = ? AND id = ?", orgID, groupID).First(&group).Error; err != nil {
			return nil, fmt.Errorf("failed to load attendance group: %w", err)
		}
	} else {
		group.ID = utils.GenerateID()
		group.OrgID = orgID
	}
	group.Code = strings.TrimSpace(req.Code)
	group.Name = strings.TrimSpace(req.Name)
	group.Description = strings.TrimSpace(req.Description)
	group.ParentGroupID = normalizeOptionalString(req.ParentGroupID)
	group.ManagerEmployeeID = normalizeOptionalString(req.ManagerEmployeeID)
	group.Status = defaultString(req.Status, "active")
	if group.Code == "" || group.Name == "" {
		return nil, fmt.Errorf("group code and name are required")
	}
	if err := s.ensureEmployeeExists(s.db, orgID, group.ManagerEmployeeID); err != nil {
		return nil, err
	}
	if err := s.ensureGroupExists(s.db, orgID, group.ParentGroupID); err != nil && err != gorm.ErrRecordNotFound {
		return nil, fmt.Errorf("failed to validate parent attendance group: %w", err)
	}
	if err := s.db.Save(&group).Error; err != nil {
		return nil, fmt.Errorf("failed to save attendance group: %w", err)
	}
	return &group, nil
}

func (s *AttendanceService) ListAttendanceSites(ctx context.Context, orgID string) ([]models.AttendanceSite, error) {
	var sites []models.AttendanceSite
	if err := s.db.Where("org_id = ?", orgID).Order("created_at DESC").Find(&sites).Error; err != nil {
		return nil, fmt.Errorf("failed to list attendance sites: %w", err)
	}
	return sites, nil
}

func (s *AttendanceService) UpsertAttendanceSite(ctx context.Context, orgID string, req UpsertAttendanceSiteRequest) (*models.AttendanceSite, error) {
	siteID := strings.TrimSpace(req.ID)
	site := models.AttendanceSite{}
	if siteID != "" {
		if err := s.db.Where("org_id = ? AND id = ?", orgID, siteID).First(&site).Error; err != nil {
			return nil, fmt.Errorf("failed to load attendance site: %w", err)
		}
	} else {
		site.ID = utils.GenerateID()
		site.OrgID = orgID
	}
	site.SiteCode = strings.TrimSpace(req.SiteCode)
	site.Name = strings.TrimSpace(req.Name)
	site.Description = strings.TrimSpace(req.Description)
	site.AddressLine1 = strings.TrimSpace(req.AddressLine1)
	site.AddressLine2 = strings.TrimSpace(req.AddressLine2)
	site.City = strings.TrimSpace(req.City)
	site.State = strings.TrimSpace(req.State)
	site.CountryCode = strings.TrimSpace(req.CountryCode)
	site.PostalCode = strings.TrimSpace(req.PostalCode)
	site.Latitude = req.Latitude
	site.Longitude = req.Longitude
	if req.RadiusMeters > 0 {
		site.RadiusMeters = req.RadiusMeters
	} else if site.RadiusMeters == 0 {
		site.RadiusMeters = 100
	}
	site.Timezone = defaultString(req.Timezone, "UTC")
	site.Status = defaultString(req.Status, "active")
	if site.SiteCode == "" || site.Name == "" {
		return nil, fmt.Errorf("site_code and name are required")
	}
	if err := s.db.Save(&site).Error; err != nil {
		return nil, fmt.Errorf("failed to save attendance site: %w", err)
	}
	return &site, nil
}

func (s *AttendanceService) ListAttendanceShiftTemplates(ctx context.Context, orgID string) ([]models.AttendanceShiftTemplate, error) {
	var shifts []models.AttendanceShiftTemplate
	if err := s.db.Where("org_id = ?", orgID).Order("created_at DESC").Find(&shifts).Error; err != nil {
		return nil, fmt.Errorf("failed to list attendance shift templates: %w", err)
	}
	return shifts, nil
}

func (s *AttendanceService) UpsertAttendanceShiftTemplate(ctx context.Context, orgID string, req UpsertAttendanceShiftTemplateRequest) (*models.AttendanceShiftTemplate, error) {
	shiftID := strings.TrimSpace(req.ID)
	shift := models.AttendanceShiftTemplate{}
	if shiftID != "" {
		if err := s.db.Where("org_id = ? AND id = ?", orgID, shiftID).First(&shift).Error; err != nil {
			return nil, fmt.Errorf("failed to load attendance shift template: %w", err)
		}
	} else {
		shift.ID = utils.GenerateID()
		shift.OrgID = orgID
	}
	shift.ShiftCode = strings.TrimSpace(req.ShiftCode)
	shift.Name = strings.TrimSpace(req.Name)
	shift.Description = strings.TrimSpace(req.Description)
	shift.StartTime = strings.TrimSpace(req.StartTime)
	shift.EndTime = strings.TrimSpace(req.EndTime)
	shift.CrossesDayBoundary = req.CrossesDayBoundary
	shift.CheckInWindowBeforeMinutes = req.CheckInWindowBeforeMinutes
	shift.CheckInWindowAfterMinutes = req.CheckInWindowAfterMinutes
	shift.CheckOutWindowBeforeMinutes = req.CheckOutWindowBeforeMinutes
	shift.CheckOutWindowAfterMinutes = req.CheckOutWindowAfterMinutes
	shift.LateGraceMinutes = req.LateGraceMinutes
	shift.EarlyLeaveGraceMinutes = req.EarlyLeaveGraceMinutes
	shift.WorkMinutes = req.WorkMinutes
	shift.RequireLocation = req.RequireLocation
	shift.DefaultSiteID = normalizeOptionalString(req.DefaultSiteID)
	shift.Status = defaultString(req.Status, "active")
	if shift.ShiftCode == "" || shift.Name == "" || shift.StartTime == "" || shift.EndTime == "" {
		return nil, fmt.Errorf("shift_code, name, start_time and end_time are required")
	}
	if err := s.ensureSiteExists(s.db, orgID, shift.DefaultSiteID); err != nil && err != gorm.ErrRecordNotFound {
		return nil, fmt.Errorf("failed to validate default attendance site: %w", err)
	}
	if err := s.db.Save(&shift).Error; err != nil {
		return nil, fmt.Errorf("failed to save attendance shift template: %w", err)
	}
	return &shift, nil
}

func (s *AttendanceService) ListAttendanceShiftAssignments(ctx context.Context, orgID string, date *time.Time) ([]models.AttendanceShiftAssignment, error) {
	var assignments []models.AttendanceShiftAssignment
	query := s.db.Where("org_id = ?", orgID)
	if date != nil {
		query = query.Where("assignment_date = ?", snapshotDateFor(*date))
	}
	if err := query.Order("assignment_date DESC, created_at DESC").Find(&assignments).Error; err != nil {
		return nil, fmt.Errorf("failed to list attendance shift assignments: %w", err)
	}
	return assignments, nil
}

func (s *AttendanceService) GetAttendancePolicy(ctx context.Context, orgID string) (*models.AttendancePolicy, error) {
	return s.GetPunchConfig(ctx, orgID)
}

func (s *AttendanceService) UpsertAttendancePolicy(ctx context.Context, orgID string, req UpsertAttendancePolicyRequest) (*models.AttendancePolicy, error) {
	policy, err := s.GetPunchConfig(ctx, orgID)
	if err != nil {
		return nil, err
	}
	policy.PunchMode = defaultString(req.PunchMode, policy.PunchMode)
	policy.AllowLatePunch = req.AllowLatePunch
	policy.RequireLocation = req.RequireLocation
	if err := s.db.Save(policy).Error; err != nil {
		return nil, fmt.Errorf("failed to save attendance policy: %w", err)
	}
	return policy, nil
}

func (s *AttendanceService) ListAttendanceGroupMemberships(ctx context.Context, orgID string, groupID, employeeID *string) ([]models.AttendanceGroupMembership, error) {
	query := s.db.Where("org_id = ?", orgID)
	if normalized := normalizeOptionalString(groupID); normalized != nil {
		query = query.Where("group_id = ?", *normalized)
	}
	if normalized := normalizeOptionalString(employeeID); normalized != nil {
		query = query.Where("employee_id = ?", *normalized)
	}
	var memberships []models.AttendanceGroupMembership
	if err := query.Order("effective_from DESC, created_at DESC").Find(&memberships).Error; err != nil {
		return nil, fmt.Errorf("failed to list attendance group memberships: %w", err)
	}
	return memberships, nil
}

func (s *AttendanceService) UpsertAttendanceGroupMembership(ctx context.Context, orgID string, req UpsertAttendanceGroupMembershipRequest) (*models.AttendanceGroupMembership, error) {
	if strings.TrimSpace(req.GroupID) == "" || strings.TrimSpace(req.EmployeeID) == "" || req.EffectiveFrom.IsZero() {
		return nil, fmt.Errorf("group_id, employee_id and effective_from are required")
	}
	if err := s.ensureGroupExists(s.db, orgID, ptrOrNil(strings.TrimSpace(req.GroupID))); err != nil {
		return nil, err
	}
	if err := s.ensureEmployeeExists(s.db, orgID, ptrOrNil(strings.TrimSpace(req.EmployeeID))); err != nil {
		return nil, err
	}
	membershipID := strings.TrimSpace(req.ID)
	membership := models.AttendanceGroupMembership{}
	if membershipID != "" {
		if err := s.db.Where("org_id = ? AND id = ?", orgID, membershipID).First(&membership).Error; err != nil {
			return nil, fmt.Errorf("failed to load attendance group membership: %w", err)
		}
	} else {
		membership.ID = utils.GenerateID()
		membership.OrgID = orgID
	}
	membership.GroupID = strings.TrimSpace(req.GroupID)
	membership.EmployeeID = strings.TrimSpace(req.EmployeeID)
	membership.MembershipRole = defaultString(req.MembershipRole, "member")
	membership.IsPrimary = req.IsPrimary
	membership.EffectiveFrom = snapshotDateFor(req.EffectiveFrom)
	if req.EffectiveTo != nil {
		effectiveTo := snapshotDateFor(*req.EffectiveTo)
		membership.EffectiveTo = &effectiveTo
	} else {
		membership.EffectiveTo = nil
	}
	membership.Status = defaultString(req.Status, "active")
	if err := s.db.Save(&membership).Error; err != nil {
		return nil, fmt.Errorf("failed to save attendance group membership: %w", err)
	}
	return &membership, nil
}

func (s *AttendanceService) UpsertAttendanceShiftAssignment(ctx context.Context, orgID string, req UpsertAttendanceShiftAssignmentRequest) (*models.AttendanceShiftAssignment, error) {
	employeeID := normalizeOptionalString(req.EmployeeID)
	groupID := normalizeOptionalString(req.GroupID)
	if (employeeID == nil && groupID == nil) || (employeeID != nil && groupID != nil) {
		return nil, ErrInvalidAssignmentScope
	}
	assignmentID := strings.TrimSpace(req.ID)
	assignment := models.AttendanceShiftAssignment{}
	if assignmentID != "" {
		if err := s.db.Where("org_id = ? AND id = ?", orgID, assignmentID).First(&assignment).Error; err != nil {
			return nil, fmt.Errorf("failed to load attendance shift assignment: %w", err)
		}
	} else {
		assignment.ID = utils.GenerateID()
		assignment.OrgID = orgID
	}
	assignment.AssignmentDate = snapshotDateFor(req.AssignmentDate)
	assignment.EmployeeID = employeeID
	assignment.GroupID = groupID
	assignment.SiteID = normalizeOptionalString(req.SiteID)
	assignment.ShiftTemplateID = strings.TrimSpace(req.ShiftTemplateID)
	assignment.AssignmentSource = defaultString(req.AssignmentSource, "manual")
	assignment.Status = defaultString(req.Status, "active")
	assignment.Notes = strings.TrimSpace(req.Notes)
	if assignment.AssignmentDate.IsZero() || assignment.ShiftTemplateID == "" {
		return nil, fmt.Errorf("assignment_date and shift_template_id are required")
	}
	if err := s.ensureEmployeeExists(s.db, orgID, assignment.EmployeeID); err != nil {
		return nil, err
	}
	if err := s.ensureGroupExists(s.db, orgID, assignment.GroupID); err != nil && err != gorm.ErrRecordNotFound {
		return nil, fmt.Errorf("failed to validate attendance group: %w", err)
	}
	if err := s.ensureSiteExists(s.db, orgID, assignment.SiteID); err != nil && err != gorm.ErrRecordNotFound {
		return nil, fmt.Errorf("failed to validate attendance site: %w", err)
	}
	if err := s.ensureShiftTemplateExists(s.db, orgID, assignment.ShiftTemplateID); err != nil {
		return nil, err
	}
	if err := s.db.Save(&assignment).Error; err != nil {
		return nil, fmt.Errorf("failed to save attendance shift assignment: %w", err)
	}
	return &assignment, nil
}
