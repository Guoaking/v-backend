package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"kyc-service/internal/apps/attendance/models"
	"kyc-service/pkg/logger"
)

type OrgRecordResponse struct {
	ID        string    `json:"id"`
	PunchTime time.Time `json:"punch_time"`
	PunchType string    `json:"punch_type"`
	Status    string    `json:"status"`
	Review    struct {
		Status string `json:"status"`
	} `json:"review"`
	Employee struct {
		Name       string `json:"name"`
		IDNumber   string `json:"id_number"`
		EmployeeNo string `json:"employee_no"`
	} `json:"employee"`
}

type AttendanceReviewListItem struct {
	ID                     string     `json:"id"`
	AttendancePunchEventID string     `json:"attendance_punch_event_id"`
	EmployeeID             string     `json:"employee_id"`
	ReviewStatus           string     `json:"review_status"`
	ReviewReason           string     `json:"review_reason"`
	DecisionNotes          string     `json:"decision_notes"`
	ReviewedByUserID       *string    `json:"reviewed_by_user_id,omitempty"`
	ReviewedAt             *time.Time `json:"reviewed_at,omitempty"`
	CreatedAt              time.Time  `json:"created_at"`
	Event                  struct {
		PunchTime time.Time `json:"punch_time"`
		PunchType string    `json:"punch_type"`
		Status    string    `json:"status"`
	} `json:"event"`
	Employee struct {
		Name       string `json:"name"`
		EmployeeNo string `json:"employee_no"`
	} `json:"employee"`
}

type AttendanceTimelineEvent struct {
	ID           string    `json:"id"`
	PunchTime    time.Time `json:"punch_time"`
	PunchType    string    `json:"punch_type"`
	Status       string    `json:"status"`
	ReviewStatus string    `json:"review_status,omitempty"`
}

type AttendanceEmployeeTimeline struct {
	Employee struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		EmployeeNo string `json:"employee_no"`
	} `json:"employee"`
	Events    []AttendanceTimelineEvent         `json:"events"`
	Snapshots []models.AttendanceStatusSnapshot `json:"snapshots"`
}

func (s *AttendanceService) GetOrgStats(ctx context.Context, orgID string) (map[string]interface{}, error) {
	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	endOfDay := startOfDay.Add(24 * time.Hour)
	var todaysPunches int64
	if err := s.db.Model(&models.AttendancePunchEvent{}).Where("org_id = ? AND punch_time >= ? AND punch_time < ?", orgID, startOfDay, endOfDay).Count(&todaysPunches).Error; err != nil {
		return nil, fmt.Errorf("failed to count today's punches: %w", err)
	}
	var manualReviews int64
	if err := s.db.Model(&models.AttendancePunchReview{}).Where("org_id = ? AND review_status = ?", orgID, string(models.AttendanceReviewStatusPending)).Count(&manualReviews).Error; err != nil {
		return nil, fmt.Errorf("failed to count manual reviews: %w", err)
	}
	var totalEmployees int64
	if err := s.db.Model(&models.OrganizationEmployee{}).Where("org_id = ? AND status = ?", orgID, "active").Count(&totalEmployees).Error; err != nil {
		return nil, fmt.Errorf("failed to count employees: %w", err)
	}
	return map[string]interface{}{"todays_punches": todaysPunches, "manual_reviews": manualReviews, "total_employees": totalEmployees}, nil
}

func (s *AttendanceService) GetOrgRecords(ctx context.Context, orgID string, limit int) ([]OrgRecordResponse, error) {
	type flatRecord struct {
		ID, Status, ReviewStatus, Name, IDNumber, EmployeeNo, PunchType string
		PunchTime                                                       time.Time
	}
	var flatResults []flatRecord
	if err := s.db.Table("attendance_punch_events").
		Select("attendance_punch_events.id, attendance_punch_events.punch_time, attendance_punch_events.punch_type, attendance_punch_events.status, attendance_punch_reviews.review_status, organization_employees.name, organization_employees.id_number, organization_employees.employee_no").
		Joins("LEFT JOIN organization_employees ON attendance_punch_events.employee_id = organization_employees.id").
		Joins("LEFT JOIN attendance_punch_reviews ON attendance_punch_events.id = attendance_punch_reviews.attendance_punch_event_id").
		Where("attendance_punch_events.org_id = ?", orgID).
		Order("attendance_punch_events.punch_time DESC").
		Limit(limit).
		Scan(&flatResults).Error; err != nil {
		logger.GetLogger().WithContext(ctx).Errorf("Failed to execute GetOrgRecords JOIN query: %v", err)
		return nil, fmt.Errorf("failed to fetch org records: %w", err)
	}
	results := make([]OrgRecordResponse, 0, len(flatResults))
	for _, fr := range flatResults {
		item := OrgRecordResponse{ID: fr.ID, PunchTime: fr.PunchTime, PunchType: fr.PunchType, Status: fr.Status}
		item.Review.Status = fr.ReviewStatus
		item.Employee.Name = fr.Name
		item.Employee.IDNumber = fr.IDNumber
		item.Employee.EmployeeNo = fr.EmployeeNo
		results = append(results, item)
	}
	return results, nil
}

func (s *AttendanceService) GetEmployeeRecords(ctx context.Context, orgID string, idNumber string, limit int, yearMonth string) ([]models.AttendancePunchEvent, error) {
	var emp models.OrganizationEmployee
	if err := s.db.Where("org_id = ? AND id_number = ?", orgID, idNumber).First(&emp).Error; err != nil {
		return nil, ErrIdentityNotFound
	}
	query := s.db.Where("org_id = ? AND employee_id = ?", orgID, emp.ID)
	if yearMonth != "" {
		query = query.Where("to_char(punch_time, 'YYYY-MM') = ?", yearMonth)
	}
	var records []models.AttendancePunchEvent
	if err := query.Order("punch_time DESC").Limit(limit).Find(&records).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch records: %w", err)
	}
	return records, nil
}

func (s *AttendanceService) GetEmployeeRecordsByNo(ctx context.Context, orgID string, employeeNo string, limit int, yearMonth string) ([]models.AttendancePunchEvent, error) {
	var emp models.OrganizationEmployee
	if err := s.db.Where("org_id = ? AND employee_no = ?", orgID, employeeNo).First(&emp).Error; err != nil {
		return nil, fmt.Errorf("employee not found: %w", err)
	}
	query := s.db.Where("org_id = ? AND employee_id = ?", orgID, emp.ID)
	if yearMonth != "" {
		query = query.Where("to_char(punch_time, 'YYYY-MM') = ?", yearMonth)
	}
	var records []models.AttendancePunchEvent
	if err := query.Order("punch_time DESC").Limit(limit).Find(&records).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch records: %w", err)
	}
	return records, nil
}

func (s *AttendanceService) ListAttendancePunchReviews(ctx context.Context, orgID string, status string, limit int) ([]AttendanceReviewListItem, error) {
	type flatReview struct {
		ID, AttendancePunchEventID, EmployeeID, ReviewStatus, ReviewReason, DecisionNotes, PunchType, EventStatus, Name, EmployeeNo string
		ReviewedByUserID                                                                                                            *string
		ReviewedAt, CreatedAt                                                                                                       *time.Time
		PunchTime                                                                                                                   time.Time
	}
	query := s.db.Table("attendance_punch_reviews").
		Select("attendance_punch_reviews.id, attendance_punch_reviews.attendance_punch_event_id, attendance_punch_reviews.employee_id, attendance_punch_reviews.review_status, attendance_punch_reviews.review_reason, attendance_punch_reviews.decision_notes, attendance_punch_reviews.reviewed_by_user_id, attendance_punch_reviews.reviewed_at, attendance_punch_reviews.created_at, attendance_punch_events.punch_time, attendance_punch_events.punch_type, attendance_punch_events.status as event_status, organization_employees.name, organization_employees.employee_no").
		Joins("LEFT JOIN attendance_punch_events ON attendance_punch_reviews.attendance_punch_event_id = attendance_punch_events.id").
		Joins("LEFT JOIN organization_employees ON attendance_punch_reviews.employee_id = organization_employees.id").
		Where("attendance_punch_reviews.org_id = ?", orgID)
	if strings.TrimSpace(status) != "" {
		query = query.Where("attendance_punch_reviews.review_status = ?", strings.TrimSpace(status))
	}
	if limit <= 0 {
		limit = 100
	}
	var flatReviews []flatReview
	if err := query.Order("attendance_punch_reviews.created_at DESC").Limit(limit).Scan(&flatReviews).Error; err != nil {
		return nil, fmt.Errorf("failed to list attendance punch reviews: %w", err)
	}
	results := make([]AttendanceReviewListItem, 0, len(flatReviews))
	for _, fr := range flatReviews {
		item := AttendanceReviewListItem{
			ID:                     fr.ID,
			AttendancePunchEventID: fr.AttendancePunchEventID,
			EmployeeID:             fr.EmployeeID,
			ReviewStatus:           fr.ReviewStatus,
			ReviewReason:           fr.ReviewReason,
			DecisionNotes:          fr.DecisionNotes,
			ReviewedByUserID:       fr.ReviewedByUserID,
		}
		if fr.ReviewedAt != nil {
			item.ReviewedAt = fr.ReviewedAt
		}
		if fr.CreatedAt != nil {
			item.CreatedAt = *fr.CreatedAt
		}
		item.Event.PunchTime = fr.PunchTime
		item.Event.PunchType = fr.PunchType
		item.Event.Status = fr.EventStatus
		item.Employee.Name = fr.Name
		item.Employee.EmployeeNo = fr.EmployeeNo
		results = append(results, item)
	}
	return results, nil
}

func (s *AttendanceService) ListAttendanceStatusSnapshots(ctx context.Context, orgID string, snapshotDate *time.Time, employeeID, groupID *string, limit int) ([]models.AttendanceStatusSnapshot, error) {
	query := s.db.Where("org_id = ?", orgID)
	if snapshotDate != nil {
		query = query.Where("snapshot_date = ?", snapshotDateFor(*snapshotDate))
	}
	if normalized := normalizeOptionalString(employeeID); normalized != nil {
		query = query.Where("employee_id = ?", *normalized)
	}
	if normalized := normalizeOptionalString(groupID); normalized != nil {
		query = query.Where("group_id = ?", *normalized)
	}
	if limit <= 0 {
		limit = 100
	}
	var snapshots []models.AttendanceStatusSnapshot
	if err := query.Order("snapshot_date DESC, created_at DESC").Limit(limit).Find(&snapshots).Error; err != nil {
		return nil, fmt.Errorf("failed to list attendance status snapshots: %w", err)
	}
	return snapshots, nil
}

func (s *AttendanceService) GetAttendanceEmployeeTimeline(ctx context.Context, orgID, employeeID string, limit int) (*AttendanceEmployeeTimeline, error) {
	var employee models.OrganizationEmployee
	if err := s.db.Where("org_id = ? AND id = ?", orgID, employeeID).First(&employee).Error; err != nil {
		return nil, fmt.Errorf("failed to load attendance employee: %w", err)
	}
	if limit <= 0 {
		limit = 50
	}
	var events []models.AttendancePunchEvent
	if err := s.db.Where("org_id = ? AND employee_id = ?", orgID, employeeID).Order("punch_time DESC").Limit(limit).Find(&events).Error; err != nil {
		return nil, fmt.Errorf("failed to load attendance punch events: %w", err)
	}
	eventIDs := make([]string, 0, len(events))
	for _, event := range events {
		eventIDs = append(eventIDs, event.ID)
	}
	reviewMap := map[string]string{}
	if len(eventIDs) > 0 {
		var reviews []models.AttendancePunchReview
		if err := s.db.Where("org_id = ? AND attendance_punch_event_id IN ?", orgID, eventIDs).Find(&reviews).Error; err != nil {
			return nil, fmt.Errorf("failed to load attendance punch reviews: %w", err)
		}
		for _, review := range reviews {
			reviewMap[review.AttendancePunchEventID] = review.ReviewStatus
		}
	}
	var snapshots []models.AttendanceStatusSnapshot
	if err := s.db.Where("org_id = ? AND employee_id = ?", orgID, employeeID).Order("snapshot_date DESC").Limit(31).Find(&snapshots).Error; err != nil {
		return nil, fmt.Errorf("failed to load attendance status snapshots: %w", err)
	}
	timeline := &AttendanceEmployeeTimeline{Snapshots: snapshots}
	timeline.Employee.ID = employee.ID
	timeline.Employee.Name = employee.Name
	timeline.Employee.EmployeeNo = employee.EmployeeNo
	for _, event := range events {
		timeline.Events = append(timeline.Events, AttendanceTimelineEvent{
			ID:           event.ID,
			PunchTime:    event.PunchTime,
			PunchType:    event.PunchType,
			Status:       event.Status,
			ReviewStatus: reviewMap[event.ID],
		})
	}
	return timeline, nil
}
