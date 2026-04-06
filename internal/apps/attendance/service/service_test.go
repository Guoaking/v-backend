package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	attendanceModels "kyc-service/internal/apps/attendance/models"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newAttendanceServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&attendanceModels.OrganizationEmployee{},
		&attendanceModels.AttendancePunchEvent{},
		&attendanceModels.AttendancePolicy{},
		&attendanceModels.AttendancePunchReview{},
		&attendanceModels.AttendanceStatusSnapshot{},
		&attendanceModels.AttendanceGroup{},
		&attendanceModels.AttendanceGroupMembership{},
		&attendanceModels.AttendanceSite{},
		&attendanceModels.AttendanceShiftTemplate{},
		&attendanceModels.AttendanceShiftAssignment{},
		&attendanceModels.AttendanceDailyReport{},
		&attendanceModels.AttendanceMonthlyReport{},
	))

	return db
}

func TestResolveActiveEmployeePrefersEmployeeNo(t *testing.T) {
	db := newAttendanceServiceTestDB(t)
	svc := &AttendanceService{db: db}

	require.NoError(t, db.Create(&attendanceModels.OrganizationEmployee{
		ID:         "emp-1",
		OrgID:      "org-1",
		EmployeeNo: "00001234",
		IDNumber:   "ID-1234567890",
		Name:       "Alice",
		Status:     "active",
	}).Error)

	emp, err := svc.resolveActiveEmployee("org-1", "00001234", "WRONG-ID")
	require.NoError(t, err)
	require.Equal(t, "emp-1", emp.ID)
}

func TestResolveActiveEmployeeFallsBackToIDNumber(t *testing.T) {
	db := newAttendanceServiceTestDB(t)
	svc := &AttendanceService{db: db}

	require.NoError(t, db.Create(&attendanceModels.OrganizationEmployee{
		ID:         "emp-2",
		OrgID:      "org-1",
		EmployeeNo: "00005678",
		IDNumber:   "ID-2222",
		Name:       "Bob",
		Status:     "active",
	}).Error)

	emp, err := svc.resolveActiveEmployee("org-1", "", "ID-2222")
	require.NoError(t, err)
	require.Equal(t, "emp-2", emp.ID)
}

func TestResolveActiveEmployeeRequiresIdentity(t *testing.T) {
	db := newAttendanceServiceTestDB(t)
	svc := &AttendanceService{db: db}

	_, err := svc.resolveActiveEmployee("org-1", "", "")
	require.ErrorIs(t, err, ErrIdentityNotFound)
}

func TestReviewPunchEventApproveUpdatesEventAndSnapshot(t *testing.T) {
	db := newAttendanceServiceTestDB(t)
	svc := &AttendanceService{db: db}

	eventTime := time.Date(2026, 4, 4, 9, 0, 0, 0, time.UTC)
	require.NoError(t, db.Create(&attendanceModels.AttendancePunchEvent{
		ID:         "event-1",
		OrgID:      "org-1",
		EmployeeID: "emp-1",
		PunchTime:  eventTime,
		PunchType:  "in",
		Status:     string(attendanceModels.PunchStatusManualReview),
	}).Error)
	require.NoError(t, db.Create(&attendanceModels.AttendancePunchReview{
		ID:                     "review-1",
		OrgID:                  "org-1",
		AttendancePunchEventID: "event-1",
		EmployeeID:             "emp-1",
		ReviewStatus:           string(attendanceModels.AttendanceReviewStatusPending),
	}).Error)
	require.NoError(t, db.Create(&attendanceModels.AttendanceStatusSnapshot{
		ID:            "snapshot-1",
		OrgID:         "org-1",
		SnapshotDate:  time.Date(2026, 4, 4, 0, 0, 0, 0, time.UTC),
		EmployeeID:    "emp-1",
		Status:        string(attendanceModels.AttendanceSnapshotStatusReviewPending),
		ReviewPending: true,
	}).Error)

	err := svc.ReviewPunchEvent(context.Background(), "org-1", "review-1", "user-1", ReviewPunchRequest{
		Action:        "approve",
		DecisionNotes: "looks valid",
	})
	require.NoError(t, err)

	var event attendanceModels.AttendancePunchEvent
	require.NoError(t, db.First(&event, "id = ?", "event-1").Error)
	require.Equal(t, string(attendanceModels.PunchStatusSuccess), event.Status)

	var review attendanceModels.AttendancePunchReview
	require.NoError(t, db.First(&review, "id = ?", "review-1").Error)
	require.Equal(t, string(attendanceModels.AttendanceReviewStatusApproved), review.ReviewStatus)
	require.NotNil(t, review.ReviewedByUserID)
	require.Equal(t, "user-1", *review.ReviewedByUserID)

	var snapshot attendanceModels.AttendanceStatusSnapshot
	require.NoError(t, db.First(&snapshot, "employee_id = ?", "emp-1").Error)
	require.Equal(t, string(attendanceModels.AttendanceSnapshotStatusMissingPunch), snapshot.Status)
	require.False(t, snapshot.ReviewPending)
}

func TestReviewPunchEventRejectMarksSnapshotException(t *testing.T) {
	db := newAttendanceServiceTestDB(t)
	svc := &AttendanceService{db: db}

	eventTime := time.Date(2026, 4, 4, 18, 0, 0, 0, time.UTC)
	require.NoError(t, db.Create(&attendanceModels.AttendancePunchEvent{
		ID:         "event-2",
		OrgID:      "org-1",
		EmployeeID: "emp-2",
		PunchTime:  eventTime,
		PunchType:  "out",
		Status:     string(attendanceModels.PunchStatusManualReview),
	}).Error)
	require.NoError(t, db.Create(&attendanceModels.AttendancePunchReview{
		ID:                     "review-2",
		OrgID:                  "org-1",
		AttendancePunchEventID: "event-2",
		EmployeeID:             "emp-2",
		ReviewStatus:           string(attendanceModels.AttendanceReviewStatusPending),
	}).Error)

	err := svc.ReviewPunchEvent(context.Background(), "org-1", "review-2", "user-2", ReviewPunchRequest{
		Action:       "reject",
		ReviewReason: "spoof suspected",
	})
	require.NoError(t, err)

	var snapshot attendanceModels.AttendanceStatusSnapshot
	require.NoError(t, db.First(&snapshot, "employee_id = ?", "emp-2").Error)
	require.Equal(t, string(attendanceModels.AttendanceSnapshotStatusException), snapshot.Status)
	require.False(t, snapshot.ReviewPending)
}

func TestUpsertStatusSnapshotUsesEmployeeAssignmentAndComputesLateMinutes(t *testing.T) {
	db := newAttendanceServiceTestDB(t)
	svc := &AttendanceService{db: db}

	require.NoError(t, db.Create(&attendanceModels.AttendanceShiftTemplate{
		ID:               "shift-1",
		OrgID:            "org-1",
		ShiftCode:        "morning",
		Name:             "Morning Shift",
		StartTime:        "09:00",
		EndTime:          "18:00",
		LateGraceMinutes: 10,
		Status:           "active",
	}).Error)
	require.NoError(t, db.Create(&attendanceModels.AttendanceShiftAssignment{
		ID:              "assign-1",
		OrgID:           "org-1",
		AssignmentDate:  time.Date(2026, 4, 5, 0, 0, 0, 0, time.UTC),
		EmployeeID:      ptrString("emp-3"),
		ShiftTemplateID: "shift-1",
		Status:          "active",
	}).Error)

	event := attendanceModels.AttendancePunchEvent{
		ID:         "event-3",
		OrgID:      "org-1",
		EmployeeID: "emp-3",
		PunchTime:  time.Date(2026, 4, 5, 9, 25, 0, 0, time.UTC),
		PunchType:  "in",
		Status:     string(attendanceModels.PunchStatusSuccess),
	}

	require.NoError(t, svc.upsertStatusSnapshot(db, "org-1", "emp-3", &event))

	var snapshot attendanceModels.AttendanceStatusSnapshot
	require.NoError(t, db.First(&snapshot, "employee_id = ?", "emp-3").Error)
	require.NotNil(t, snapshot.ShiftAssignmentID)
	require.Equal(t, "assign-1", *snapshot.ShiftAssignmentID)
	require.Equal(t, 15, snapshot.LateMinutes)
	require.Equal(t, string(attendanceModels.AttendanceSnapshotStatusMissingPunch), snapshot.Status)
}

func TestUpsertStatusSnapshotFallsBackToGroupAssignment(t *testing.T) {
	db := newAttendanceServiceTestDB(t)
	svc := &AttendanceService{db: db}

	require.NoError(t, db.Create(&attendanceModels.AttendanceShiftTemplate{
		ID:                     "shift-2",
		OrgID:                  "org-1",
		ShiftCode:              "evening",
		Name:                   "Evening Shift",
		StartTime:              "12:00",
		EndTime:                "20:00",
		EarlyLeaveGraceMinutes: 15,
		Status:                 "active",
	}).Error)
	require.NoError(t, db.Create(&attendanceModels.AttendanceGroupMembership{
		ID:            "membership-1",
		OrgID:         "org-1",
		GroupID:       "group-1",
		EmployeeID:    "emp-4",
		IsPrimary:     true,
		EffectiveFrom: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		Status:        "active",
	}).Error)
	require.NoError(t, db.Create(&attendanceModels.AttendanceShiftAssignment{
		ID:              "assign-2",
		OrgID:           "org-1",
		AssignmentDate:  time.Date(2026, 4, 5, 0, 0, 0, 0, time.UTC),
		GroupID:         ptrString("group-1"),
		ShiftTemplateID: "shift-2",
		Status:          "active",
	}).Error)

	event := attendanceModels.AttendancePunchEvent{
		ID:         "event-4",
		OrgID:      "org-1",
		EmployeeID: "emp-4",
		PunchTime:  time.Date(2026, 4, 5, 19, 20, 0, 0, time.UTC),
		PunchType:  "out",
		Status:     string(attendanceModels.PunchStatusSuccess),
	}

	require.NoError(t, svc.upsertStatusSnapshot(db, "org-1", "emp-4", &event))

	var snapshot attendanceModels.AttendanceStatusSnapshot
	require.NoError(t, db.First(&snapshot, "employee_id = ?", "emp-4").Error)
	require.NotNil(t, snapshot.ShiftAssignmentID)
	require.Equal(t, "assign-2", *snapshot.ShiftAssignmentID)
	require.NotNil(t, snapshot.GroupID)
	require.Equal(t, "group-1", *snapshot.GroupID)
	require.Equal(t, 25, snapshot.EarlyLeaveMinutes)
	require.Equal(t, string(attendanceModels.AttendanceSnapshotStatusCheckedOut), snapshot.Status)
}

func TestUpsertStatusSnapshotMarksOffsite(t *testing.T) {
	db := newAttendanceServiceTestDB(t)
	svc := &AttendanceService{db: db}

	require.NoError(t, db.Create(&attendanceModels.AttendanceSite{
		ID:           "site-off",
		OrgID:        "org-1",
		SiteCode:     "hq",
		Name:         "HQ",
		Latitude:     13.7563,
		Longitude:    100.5018,
		RadiusMeters: 50,
		Status:       "active",
	}).Error)
	require.NoError(t, db.Create(&attendanceModels.AttendanceShiftTemplate{
		ID:            "shift-off",
		OrgID:         "org-1",
		ShiftCode:     "onsite",
		Name:          "Onsite Shift",
		StartTime:     "09:00",
		EndTime:       "18:00",
		DefaultSiteID: ptrString("site-off"),
		Status:        "active",
	}).Error)
	require.NoError(t, db.Create(&attendanceModels.AttendanceShiftAssignment{
		ID:              "assign-off",
		OrgID:           "org-1",
		AssignmentDate:  time.Date(2026, 4, 8, 0, 0, 0, 0, time.UTC),
		EmployeeID:      ptrString("emp-off"),
		ShiftTemplateID: "shift-off",
		Status:          "active",
	}).Error)

	event := attendanceModels.AttendancePunchEvent{
		ID:         "event-off",
		OrgID:      "org-1",
		EmployeeID: "emp-off",
		PunchTime:  time.Date(2026, 4, 8, 9, 0, 0, 0, time.UTC),
		PunchType:  "in",
		Status:     string(attendanceModels.PunchStatusSuccess),
		Latitude:   13.7600,
		Longitude:  100.5100,
	}

	require.NoError(t, svc.upsertStatusSnapshot(db, "org-1", "emp-off", &event))

	var snapshot attendanceModels.AttendanceStatusSnapshot
	require.NoError(t, db.First(&snapshot, "employee_id = ?", "emp-off").Error)
	require.Equal(t, string(attendanceModels.AttendanceSnapshotStatusOffsite), snapshot.Status)
	require.Equal(t, "offsite", snapshot.ExceptionCode)
}

func ptrString(v string) *string {
	return &v
}

func TestAttendanceMasterDataCRUDFlow(t *testing.T) {
	db := newAttendanceServiceTestDB(t)
	svc := &AttendanceService{db: db}

	group, err := svc.UpsertAttendanceGroup(context.Background(), "org-1", UpsertAttendanceGroupRequest{
		Code:   "ops-team",
		Name:   "Ops Team",
		Status: "active",
	})
	require.NoError(t, err)
	require.Equal(t, "ops-team", group.Code)

	site, err := svc.UpsertAttendanceSite(context.Background(), "org-1", UpsertAttendanceSiteRequest{
		SiteCode:     "bangkok-hq",
		Name:         "Bangkok HQ",
		RadiusMeters: 200,
		Status:       "active",
	})
	require.NoError(t, err)
	require.Equal(t, "bangkok-hq", site.SiteCode)

	shift, err := svc.UpsertAttendanceShiftTemplate(context.Background(), "org-1", UpsertAttendanceShiftTemplateRequest{
		ShiftCode:                   "general",
		Name:                        "General Shift",
		StartTime:                   "09:00",
		EndTime:                     "18:00",
		CheckInWindowBeforeMinutes:  60,
		CheckInWindowAfterMinutes:   120,
		CheckOutWindowBeforeMinutes: 120,
		CheckOutWindowAfterMinutes:  240,
		DefaultSiteID:               &site.ID,
		Status:                      "active",
	})
	require.NoError(t, err)
	require.Equal(t, "general", shift.ShiftCode)

	require.NoError(t, db.Create(&attendanceModels.OrganizationEmployee{
		ID:         "emp-10",
		OrgID:      "org-1",
		EmployeeNo: "EMP-10",
		IDNumber:   "ID-10",
		Name:       "Shift Owner",
		Status:     "active",
	}).Error)

	assignment, err := svc.UpsertAttendanceShiftAssignment(context.Background(), "org-1", UpsertAttendanceShiftAssignmentRequest{
		AssignmentDate:  time.Date(2026, 4, 6, 10, 0, 0, 0, time.UTC),
		EmployeeID:      ptrString("emp-10"),
		SiteID:          &site.ID,
		ShiftTemplateID: shift.ID,
		Status:          "active",
	})
	require.NoError(t, err)
	require.Equal(t, snapshotDateFor(time.Date(2026, 4, 6, 10, 0, 0, 0, time.UTC)), assignment.AssignmentDate)

	groups, err := svc.ListAttendanceGroups(context.Background(), "org-1")
	require.NoError(t, err)
	require.Len(t, groups, 1)

	sites, err := svc.ListAttendanceSites(context.Background(), "org-1")
	require.NoError(t, err)
	require.Len(t, sites, 1)

	shifts, err := svc.ListAttendanceShiftTemplates(context.Background(), "org-1")
	require.NoError(t, err)
	require.Len(t, shifts, 1)

	assignments, err := svc.ListAttendanceShiftAssignments(context.Background(), "org-1", ptrTime(time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC)))
	require.NoError(t, err)
	require.Len(t, assignments, 1)
}

func TestAttendanceOpsQueriesAndPolicyFlow(t *testing.T) {
	db := newAttendanceServiceTestDB(t)
	svc := &AttendanceService{db: db}

	policy, err := svc.UpsertAttendancePolicy(context.Background(), "org-1", UpsertAttendancePolicyRequest{
		PunchMode:       "photo_only",
		AllowLatePunch:  false,
		RequireLocation: true,
	})
	require.NoError(t, err)
	require.Equal(t, "photo_only", policy.PunchMode)
	require.True(t, policy.RequireLocation)

	require.NoError(t, db.Create(&attendanceModels.AttendanceGroup{
		ID:     "group-ops",
		OrgID:  "org-1",
		Code:   "ops",
		Name:   "Ops",
		Status: "active",
	}).Error)
	require.NoError(t, db.Create(&attendanceModels.OrganizationEmployee{
		ID:         "emp-20",
		OrgID:      "org-1",
		EmployeeNo: "EMP-20",
		IDNumber:   "ID-20",
		Name:       "Timeline User",
		Status:     "active",
	}).Error)

	membership, err := svc.UpsertAttendanceGroupMembership(context.Background(), "org-1", UpsertAttendanceGroupMembershipRequest{
		GroupID:       "group-ops",
		EmployeeID:    "emp-20",
		IsPrimary:     true,
		EffectiveFrom: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		Status:        "active",
	})
	require.NoError(t, err)
	require.Equal(t, "group-ops", membership.GroupID)

	memberships, err := svc.ListAttendanceGroupMemberships(context.Background(), "org-1", ptrString("group-ops"), nil)
	require.NoError(t, err)
	require.Len(t, memberships, 1)

	require.NoError(t, db.Create(&attendanceModels.AttendancePunchEvent{
		ID:         "event-20",
		OrgID:      "org-1",
		EmployeeID: "emp-20",
		PunchTime:  time.Date(2026, 4, 7, 9, 0, 0, 0, time.UTC),
		PunchType:  "in",
		Status:     string(attendanceModels.PunchStatusManualReview),
	}).Error)
	require.NoError(t, db.Create(&attendanceModels.AttendancePunchReview{
		ID:                     "review-20",
		OrgID:                  "org-1",
		AttendancePunchEventID: "event-20",
		EmployeeID:             "emp-20",
		ReviewStatus:           string(attendanceModels.AttendanceReviewStatusPending),
		ReviewReason:           "manual check",
	}).Error)
	require.NoError(t, db.Create(&attendanceModels.AttendanceStatusSnapshot{
		ID:            "snapshot-20",
		OrgID:         "org-1",
		SnapshotDate:  time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC),
		EmployeeID:    "emp-20",
		GroupID:       ptrString("group-ops"),
		Status:        string(attendanceModels.AttendanceSnapshotStatusReviewPending),
		ReviewPending: true,
	}).Error)

	reviews, err := svc.ListAttendancePunchReviews(context.Background(), "org-1", string(attendanceModels.AttendanceReviewStatusPending), 20)
	require.NoError(t, err)
	require.Len(t, reviews, 1)
	require.Equal(t, "event-20", reviews[0].AttendancePunchEventID)

	snapshots, err := svc.ListAttendanceStatusSnapshots(context.Background(), "org-1", ptrTime(time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC)), ptrString("emp-20"), nil, 20)
	require.NoError(t, err)
	require.Len(t, snapshots, 1)

	timeline, err := svc.GetAttendanceEmployeeTimeline(context.Background(), "org-1", "emp-20", 20)
	require.NoError(t, err)
	require.Equal(t, "emp-20", timeline.Employee.ID)
	require.Len(t, timeline.Events, 1)
	require.Len(t, timeline.Snapshots, 1)
}

func TestAttendanceReportsAndExportFlow(t *testing.T) {
	db := newAttendanceServiceTestDB(t)
	svc := &AttendanceService{db: db}

	require.NoError(t, db.Create(&attendanceModels.AttendanceStatusSnapshot{
		ID:            "snapshot-r1",
		OrgID:         "org-1",
		SnapshotDate:  time.Date(2026, 4, 9, 0, 0, 0, 0, time.UTC),
		EmployeeID:    "emp-r1",
		Status:        string(attendanceModels.AttendanceSnapshotStatusLate),
		LateMinutes:   8,
		MissingPunch:  true,
		ReviewPending: false,
		ExceptionCode: "late",
	}).Error)
	require.NoError(t, db.Create(&attendanceModels.AttendanceStatusSnapshot{
		ID:           "snapshot-r2",
		OrgID:        "org-1",
		SnapshotDate: time.Date(2026, 4, 9, 0, 0, 0, 0, time.UTC),
		EmployeeID:   "emp-r2",
		Status:       string(attendanceModels.AttendanceSnapshotStatusCheckedOut),
	}).Error)

	daily, err := svc.BuildDailyReport(context.Background(), "org-1", time.Date(2026, 4, 9, 0, 0, 0, 0, time.UTC), nil, nil)
	require.NoError(t, err)
	require.Equal(t, 2, daily.TotalEmployees)
	require.Equal(t, 1, daily.LateCount)
	require.Equal(t, 1, daily.MissingPunchCount)

	monthly, err := svc.BuildMonthlyReport(context.Background(), "org-1", "2026-04", nil, nil)
	require.NoError(t, err)
	require.Equal(t, 1, monthly.AttendanceDays)
	require.Equal(t, 1, monthly.LateCount)

	csvContent, err := svc.ExportDailyReportCSV(context.Background(), "org-1", time.Date(2026, 4, 9, 0, 0, 0, 0, time.UTC), nil, nil)
	require.NoError(t, err)
	require.Contains(t, csvContent, "report_date")
	require.Contains(t, csvContent, "2026-04-09")
}

func ptrTime(v time.Time) *time.Time {
	return &v
}
