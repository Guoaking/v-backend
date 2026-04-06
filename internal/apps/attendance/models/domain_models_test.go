package models

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newAttendanceModelsTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	require.NoError(t, err)

	require.NoError(t, db.AutoMigrate(
		&OrganizationEmployee{},
		&AttendanceGroup{},
		&AttendanceGroupMembership{},
		&AttendanceSite{},
		&AttendanceShiftTemplate{},
		&AttendanceShiftAssignment{},
		&AttendancePunchReview{},
		&AttendanceStatusSnapshot{},
	))

	return db
}

func TestAttendanceDomainModelsMigrateAndRelate(t *testing.T) {
	db := newAttendanceModelsTestDB(t)

	employee := OrganizationEmployee{
		ID:         "emp-1",
		OrgID:      "org-1",
		EmployeeNo: "EMP-001",
		IDNumber:   "ID-001",
		Name:       "Alice",
		Status:     "active",
	}
	require.NoError(t, db.Create(&employee).Error)

	group := AttendanceGroup{
		ID:     "grp-1",
		OrgID:  "org-1",
		Code:   "store-shanghai",
		Name:   "Shanghai Store",
		Status: "active",
	}
	require.NoError(t, db.Create(&group).Error)

	site := AttendanceSite{
		ID:           "site-1",
		OrgID:        "org-1",
		SiteCode:     "shanghai-main",
		Name:         "Shanghai Main Site",
		RadiusMeters: 150,
		Status:       "active",
	}
	require.NoError(t, db.Create(&site).Error)

	shift := AttendanceShiftTemplate{
		ID:            "shift-1",
		OrgID:         "org-1",
		ShiftCode:     "morning",
		Name:          "Morning Shift",
		StartTime:     "09:00",
		EndTime:       "18:00",
		DefaultSiteID: &site.ID,
		Status:        "active",
	}
	require.NoError(t, db.Create(&shift).Error)

	effectiveFrom := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	membership := AttendanceGroupMembership{
		ID:             "membership-1",
		OrgID:          "org-1",
		GroupID:        group.ID,
		EmployeeID:     employee.ID,
		MembershipRole: "member",
		IsPrimary:      true,
		EffectiveFrom:  effectiveFrom,
		Status:         "active",
	}
	require.NoError(t, db.Create(&membership).Error)

	assignmentDate := time.Date(2026, 4, 3, 0, 0, 0, 0, time.UTC)
	assignment := AttendanceShiftAssignment{
		ID:               "assign-1",
		OrgID:            "org-1",
		AssignmentDate:   assignmentDate,
		EmployeeID:       &employee.ID,
		GroupID:          &group.ID,
		SiteID:           &site.ID,
		ShiftTemplateID:  shift.ID,
		AssignmentSource: "manual",
		Status:           "active",
	}
	require.NoError(t, db.Create(&assignment).Error)

	snapshot := AttendanceStatusSnapshot{
		ID:                "snapshot-1",
		OrgID:             "org-1",
		SnapshotDate:      assignmentDate,
		EmployeeID:        employee.ID,
		GroupID:           &group.ID,
		SiteID:            &site.ID,
		ShiftAssignmentID: &assignment.ID,
		Status:            "scheduled",
	}
	require.NoError(t, db.Create(&snapshot).Error)

	review := AttendancePunchReview{
		ID:                     "review-1",
		OrgID:                  "org-1",
		AttendancePunchEventID: "event-1",
		EmployeeID:             employee.ID,
		ReviewStatus:           "pending",
	}
	require.NoError(t, db.Create(&review).Error)

	var membershipCount int64
	require.NoError(t, db.Model(&AttendanceGroupMembership{}).Where("group_id = ? AND employee_id = ?", group.ID, employee.ID).Count(&membershipCount).Error)
	require.EqualValues(t, 1, membershipCount)

	var snapshotCount int64
	require.NoError(t, db.Model(&AttendanceStatusSnapshot{}).Where("employee_id = ? AND shift_assignment_id = ?", employee.ID, assignment.ID).Count(&snapshotCount).Error)
	require.EqualValues(t, 1, snapshotCount)
}
