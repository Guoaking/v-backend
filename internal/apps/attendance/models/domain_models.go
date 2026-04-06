package models

import (
	"time"

	"gorm.io/datatypes"
)

type AttendanceGroup struct {
	ID                string         `gorm:"primaryKey;type:varchar(64)" json:"id"`
	OrgID             string         `gorm:"index:idx_attendance_group_code,unique;not null;type:varchar(64)" json:"org_id"`
	Code              string         `gorm:"index:idx_attendance_group_code,unique;not null;type:varchar(64)" json:"code"`
	Name              string         `gorm:"not null;type:varchar(128)" json:"name"`
	Description       string         `gorm:"type:text" json:"description"`
	ParentGroupID     *string        `gorm:"index;type:varchar(64)" json:"parent_group_id,omitempty"`
	ManagerEmployeeID *string        `gorm:"index;type:varchar(64)" json:"manager_employee_id,omitempty"`
	Status            string         `gorm:"index;type:varchar(20);default:'active'" json:"status"`
	Metadata          datatypes.JSON `gorm:"type:jsonb" json:"metadata"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
}

func (AttendanceGroup) TableName() string {
	return "attendance_groups"
}

type AttendanceGroupMembership struct {
	ID             string     `gorm:"primaryKey;type:varchar(64)" json:"id"`
	OrgID          string     `gorm:"index;not null;type:varchar(64)" json:"org_id"`
	GroupID        string     `gorm:"index:idx_attendance_group_member,unique;not null;type:varchar(64)" json:"group_id"`
	EmployeeID     string     `gorm:"index:idx_attendance_group_member,unique;not null;type:varchar(64)" json:"employee_id"`
	MembershipRole string     `gorm:"type:varchar(32);default:'member'" json:"membership_role"`
	IsPrimary      bool       `gorm:"default:false" json:"is_primary"`
	EffectiveFrom  time.Time  `gorm:"index;not null" json:"effective_from"`
	EffectiveTo    *time.Time `gorm:"index" json:"effective_to,omitempty"`
	Status         string     `gorm:"index;type:varchar(20);default:'active'" json:"status"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (AttendanceGroupMembership) TableName() string {
	return "attendance_group_memberships"
}

type AttendanceSite struct {
	ID           string         `gorm:"primaryKey;type:varchar(64)" json:"id"`
	OrgID        string         `gorm:"index:idx_attendance_site_code,unique;not null;type:varchar(64)" json:"org_id"`
	SiteCode     string         `gorm:"index:idx_attendance_site_code,unique;not null;type:varchar(64)" json:"site_code"`
	Name         string         `gorm:"not null;type:varchar(128)" json:"name"`
	Description  string         `gorm:"type:text" json:"description"`
	AddressLine1 string         `gorm:"type:varchar(255)" json:"address_line_1"`
	AddressLine2 string         `gorm:"type:varchar(255)" json:"address_line_2"`
	City         string         `gorm:"type:varchar(128)" json:"city"`
	State        string         `gorm:"type:varchar(128)" json:"state"`
	CountryCode  string         `gorm:"type:varchar(16)" json:"country_code"`
	PostalCode   string         `gorm:"type:varchar(32)" json:"postal_code"`
	Latitude     float64        `json:"latitude"`
	Longitude    float64        `json:"longitude"`
	RadiusMeters int            `gorm:"default:100" json:"radius_meters"`
	Timezone     string         `gorm:"type:varchar(64);default:'UTC'" json:"timezone"`
	Status       string         `gorm:"index;type:varchar(20);default:'active'" json:"status"`
	Metadata     datatypes.JSON `gorm:"type:jsonb" json:"metadata"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

func (AttendanceSite) TableName() string {
	return "attendance_sites"
}

type AttendanceShiftTemplate struct {
	ID                          string         `gorm:"primaryKey;type:varchar(64)" json:"id"`
	OrgID                       string         `gorm:"index:idx_attendance_shift_code,unique;not null;type:varchar(64)" json:"org_id"`
	ShiftCode                   string         `gorm:"index:idx_attendance_shift_code,unique;not null;type:varchar(64)" json:"shift_code"`
	Name                        string         `gorm:"not null;type:varchar(128)" json:"name"`
	Description                 string         `gorm:"type:text" json:"description"`
	StartTime                   string         `gorm:"type:varchar(8);not null" json:"start_time"`
	EndTime                     string         `gorm:"type:varchar(8);not null" json:"end_time"`
	CrossesDayBoundary          bool           `gorm:"default:false" json:"crosses_day_boundary"`
	CheckInWindowBeforeMinutes  int            `gorm:"default:60" json:"check_in_window_before_minutes"`
	CheckInWindowAfterMinutes   int            `gorm:"default:120" json:"check_in_window_after_minutes"`
	CheckOutWindowBeforeMinutes int            `gorm:"default:120" json:"check_out_window_before_minutes"`
	CheckOutWindowAfterMinutes  int            `gorm:"default:240" json:"check_out_window_after_minutes"`
	LateGraceMinutes            int            `gorm:"default:0" json:"late_grace_minutes"`
	EarlyLeaveGraceMinutes      int            `gorm:"default:0" json:"early_leave_grace_minutes"`
	WorkMinutes                 int            `gorm:"default:0" json:"work_minutes"`
	RequireLocation             bool           `gorm:"default:false" json:"require_location"`
	DefaultSiteID               *string        `gorm:"index;type:varchar(64)" json:"default_site_id,omitempty"`
	Status                      string         `gorm:"index;type:varchar(20);default:'active'" json:"status"`
	Metadata                    datatypes.JSON `gorm:"type:jsonb" json:"metadata"`
	CreatedAt                   time.Time      `json:"created_at"`
	UpdatedAt                   time.Time      `json:"updated_at"`
}

func (AttendanceShiftTemplate) TableName() string {
	return "attendance_shift_templates"
}

type AttendanceShiftAssignment struct {
	ID               string    `gorm:"primaryKey;type:varchar(64)" json:"id"`
	OrgID            string    `gorm:"index;not null;type:varchar(64)" json:"org_id"`
	AssignmentDate   time.Time `gorm:"index:idx_attendance_assignment_scope;not null" json:"assignment_date"`
	EmployeeID       *string   `gorm:"index:idx_attendance_assignment_scope;type:varchar(64)" json:"employee_id,omitempty"`
	GroupID          *string   `gorm:"index:idx_attendance_assignment_scope;type:varchar(64)" json:"group_id,omitempty"`
	SiteID           *string   `gorm:"index;type:varchar(64)" json:"site_id,omitempty"`
	ShiftTemplateID  string    `gorm:"index;not null;type:varchar(64)" json:"shift_template_id"`
	AssignmentSource string    `gorm:"type:varchar(32);default:'manual'" json:"assignment_source"`
	Status           string    `gorm:"index;type:varchar(20);default:'active'" json:"status"`
	Notes            string    `gorm:"type:text" json:"notes"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func (AttendanceShiftAssignment) TableName() string {
	return "attendance_shift_assignments"
}

type AttendancePunchReview struct {
	ID                     string     `gorm:"primaryKey;type:varchar(64)" json:"id"`
	OrgID                  string     `gorm:"index;not null;type:varchar(64)" json:"org_id"`
	AttendancePunchEventID string     `gorm:"index;not null;type:varchar(64)" json:"attendance_punch_event_id"`
	EmployeeID             string     `gorm:"index;not null;type:varchar(64)" json:"employee_id"`
	ReviewStatus           string     `gorm:"index;type:varchar(20);default:'pending'" json:"review_status"`
	ReviewReason           string     `gorm:"type:text" json:"review_reason"`
	ReviewedByUserID       *string    `gorm:"index;type:varchar(64)" json:"reviewed_by_user_id,omitempty"`
	ReviewedAt             *time.Time `json:"reviewed_at,omitempty"`
	DecisionNotes          string     `gorm:"type:text" json:"decision_notes"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

func (AttendancePunchReview) TableName() string {
	return "attendance_punch_reviews"
}

type AttendanceStatusSnapshot struct {
	ID                string     `gorm:"primaryKey;type:varchar(64)" json:"id"`
	OrgID             string     `gorm:"index;not null;type:varchar(64)" json:"org_id"`
	SnapshotDate      time.Time  `gorm:"index:idx_attendance_status_unique,unique;not null" json:"snapshot_date"`
	EmployeeID        string     `gorm:"index:idx_attendance_status_unique,unique;not null;type:varchar(64)" json:"employee_id"`
	GroupID           *string    `gorm:"index;type:varchar(64)" json:"group_id,omitempty"`
	SiteID            *string    `gorm:"index;type:varchar(64)" json:"site_id,omitempty"`
	ShiftAssignmentID *string    `gorm:"index;type:varchar(64)" json:"shift_assignment_id,omitempty"`
	Status            string     `gorm:"index;type:varchar(32);not null" json:"status"`
	FirstPunchAt      *time.Time `json:"first_punch_at,omitempty"`
	LastPunchAt       *time.Time `json:"last_punch_at,omitempty"`
	LateMinutes       int        `gorm:"default:0" json:"late_minutes"`
	EarlyLeaveMinutes int        `gorm:"default:0" json:"early_leave_minutes"`
	MissingPunch      bool       `gorm:"default:false" json:"missing_punch"`
	ReviewPending     bool       `gorm:"default:false" json:"review_pending"`
	ExceptionCode     string     `gorm:"type:varchar(64)" json:"exception_code"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

func (AttendanceStatusSnapshot) TableName() string {
	return "attendance_status_snapshots"
}

type AttendanceReviewStatus string

const (
	AttendanceReviewStatusPending  AttendanceReviewStatus = "pending"
	AttendanceReviewStatusApproved AttendanceReviewStatus = "approved"
	AttendanceReviewStatusRejected AttendanceReviewStatus = "rejected"
)

type AttendanceSnapshotStatus string

const (
	AttendanceSnapshotStatusScheduled     AttendanceSnapshotStatus = "scheduled"
	AttendanceSnapshotStatusCheckedIn     AttendanceSnapshotStatus = "checked_in"
	AttendanceSnapshotStatusCheckedOut    AttendanceSnapshotStatus = "checked_out"
	AttendanceSnapshotStatusLate          AttendanceSnapshotStatus = "late"
	AttendanceSnapshotStatusMissingPunch  AttendanceSnapshotStatus = "missing_punch"
	AttendanceSnapshotStatusOffsite       AttendanceSnapshotStatus = "offsite"
	AttendanceSnapshotStatusReviewPending AttendanceSnapshotStatus = "review_pending"
	AttendanceSnapshotStatusException     AttendanceSnapshotStatus = "exception"
)

type AttendanceDailyReport struct {
	ID                 string    `gorm:"primaryKey;type:varchar(64)" json:"id"`
	OrgID              string    `gorm:"index:idx_attendance_daily_report,unique;not null;type:varchar(64)" json:"org_id"`
	ReportDate         time.Time `gorm:"index:idx_attendance_daily_report,unique;not null" json:"report_date"`
	GroupID            string    `gorm:"index:idx_attendance_daily_report,unique;type:varchar(64);default:''" json:"group_id"`
	SiteID             string    `gorm:"index:idx_attendance_daily_report,unique;type:varchar(64);default:''" json:"site_id"`
	TotalEmployees     int       `gorm:"default:0" json:"total_employees"`
	CheckedInCount     int       `gorm:"default:0" json:"checked_in_count"`
	CheckedOutCount    int       `gorm:"default:0" json:"checked_out_count"`
	LateCount          int       `gorm:"default:0" json:"late_count"`
	MissingPunchCount  int       `gorm:"default:0" json:"missing_punch_count"`
	ReviewPendingCount int       `gorm:"default:0" json:"review_pending_count"`
	ExceptionCount     int       `gorm:"default:0" json:"exception_count"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

func (AttendanceDailyReport) TableName() string {
	return "attendance_daily_reports"
}

type AttendanceMonthlyReport struct {
	ID                 string    `gorm:"primaryKey;type:varchar(64)" json:"id"`
	OrgID              string    `gorm:"index:idx_attendance_monthly_report,unique;not null;type:varchar(64)" json:"org_id"`
	ReportMonth        string    `gorm:"index:idx_attendance_monthly_report,unique;not null;type:varchar(7)" json:"report_month"`
	GroupID            string    `gorm:"index:idx_attendance_monthly_report,unique;type:varchar(64);default:''" json:"group_id"`
	SiteID             string    `gorm:"index:idx_attendance_monthly_report,unique;type:varchar(64);default:''" json:"site_id"`
	TotalEmployees     int       `gorm:"default:0" json:"total_employees"`
	AttendanceDays     int       `gorm:"default:0" json:"attendance_days"`
	CheckedInDays      int       `gorm:"default:0" json:"checked_in_days"`
	CheckedOutDays     int       `gorm:"default:0" json:"checked_out_days"`
	LateCount          int       `gorm:"default:0" json:"late_count"`
	MissingPunchCount  int       `gorm:"default:0" json:"missing_punch_count"`
	ReviewPendingCount int       `gorm:"default:0" json:"review_pending_count"`
	ExceptionCount     int       `gorm:"default:0" json:"exception_count"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

func (AttendanceMonthlyReport) TableName() string {
	return "attendance_monthly_reports"
}
