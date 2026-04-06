package service

import (
	"context"
	"fmt"
	"math"
	"mime/multipart"
	"strings"
	"time"

	"kyc-service/internal/apps/attendance/models"
	"kyc-service/pkg/logger"
	"kyc-service/pkg/utils"

	"gorm.io/gorm"
)

type PunchRequest struct {
	EmployeeNo     string                `json:"employee_no"`
	IDNumber       string                `json:"id_number"`
	PunchType      string                `json:"punch_type"`
	LivenessFile   *multipart.FileHeader `json:"-"`
	LivenessTaskID string                `json:"liveness_task_id"`
	FallbackMode   bool                  `json:"fallback_mode"`
	Latitude       float64               `json:"latitude"`
	Longitude      float64               `json:"longitude"`
}

type ReviewPunchRequest struct {
	Action        string
	DecisionNotes string
	ReviewReason  string
}

var ErrPunchReviewNotFound = fmt.Errorf("attendance punch review not found")
var ErrInvalidReviewAction = fmt.Errorf("invalid review action")

func (s *AttendanceService) resolveActiveEmployee(orgID string, employeeNo string, idNumber string) (*models.OrganizationEmployee, error) {
	var emp models.OrganizationEmployee
	var err error
	switch {
	case employeeNo != "":
		err = s.db.Where("org_id = ? AND employee_no = ? AND status = ?", orgID, employeeNo, "active").First(&emp).Error
	case idNumber != "":
		err = s.db.Where("org_id = ? AND id_number = ? AND status = ?", orgID, idNumber, "active").First(&emp).Error
	default:
		return nil, ErrIdentityNotFound
	}
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrIdentityNotFound
		}
		return nil, err
	}
	return &emp, nil
}

func (s *AttendanceService) PunchIn(ctx context.Context, orgID string, req *PunchRequest) error {
	log := logger.GetLogger().WithContext(ctx)
	identityKey := req.EmployeeNo
	if identityKey == "" {
		identityKey = req.IDNumber
	}
	debounceKey := fmt.Sprintf("attendance:punch:debounce:%s:%s:%s", orgID, identityKey, req.PunchType)
	exists, err := s.redis.Exists(ctx, debounceKey).Result()
	if err == nil && exists > 0 {
		log.Infof("Punch-in debounced for %s via Redis", identityKey)
		return nil
	}

	emp, err := s.resolveActiveEmployee(orgID, req.EmployeeNo, req.IDNumber)
	if err != nil {
		return ErrIdentityNotFound
	}

	event := models.AttendancePunchEvent{
		ID:         utils.GenerateID(),
		OrgID:      orgID,
		EmployeeID: emp.ID,
		PunchTime:  time.Now(),
		PunchType:  req.PunchType,
		Latitude:   req.Latitude,
		Longitude:  req.Longitude,
	}

	if req.FallbackMode {
		event.Status = string(models.PunchStatusManualReview)
		go func(oID, baseImg string, fileHeader *multipart.FileHeader) {
			bgCtx := context.Background()
			asset, err := s.kycService.IngestImage(bgCtx, oID, fileHeader)
			if err != nil {
				log.Errorf("failed to save fallback image via IngestImage: %v", err)
				return
			}
			dbCopy := s.db.Session(&gorm.Session{})
			dbCopy.Create(&models.DataCollectionFace{
				ID:             utils.GenerateID(),
				OrgID:          oID,
				BaseImageURL:   baseImg,
				PunchImageURL:  asset.FilePath,
				Confidence:     0,
				IsSameFace:     0,
				IsFallback:     true,
				EnvironmentEnv: string(models.ScenarioFallbackPunch),
			})
		}(orgID, emp.FaceImageURL, req.LivenessFile)
	} else {
		config, err := s.GetPunchConfig(ctx, orgID)
		if err != nil {
			log.Warnf("Failed to get punch config, defaulting to active: %v", err)
		}

		if config != nil && config.PunchMode == string(models.PunchModeLivenessActive) {
			if req.LivenessTaskID == "" {
				return fmt.Errorf("liveness_task_id is required for active liveness mode")
			}
			verifyRes, err := s.VerifyActionLiveness(ctx, orgID, req.LivenessTaskID, "", "", "")
			if err != nil {
				return fmt.Errorf("active liveness verification failed: %w", err)
			}
			if verifyRes == nil || verifyRes.Status != "succeeded" || verifyRes.Details == nil || verifyRes.Details.ActionVerify == nil || !verifyRes.Details.ActionVerify.Passed {
				return fmt.Errorf("active liveness verification failed")
			}
			event.Status = string(models.PunchStatusSuccess)
			event.LivenessScore = verifyRes.Details.LivenessConfidence
			if verifyRes.Details.FaceInfo != nil {
				event.FaceScore = verifyRes.Details.FaceInfo.Confidence
			}
		} else {
			if req.LivenessFile == nil {
				return fmt.Errorf("liveness_image is required for silent liveness mode")
			}
			punchFaceHeader := req.LivenessFile
			if config == nil || config.PunchMode != string(models.PunchModePhotoOnly) {
				ctxWithOrg := context.WithValue(ctx, "org_id", orgID)
				livenessRes, err := s.kycService.LivenessSilent(ctxWithOrg, punchFaceHeader, "zh-CN")
				if err != nil {
					return fmt.Errorf("liveness check failed: %w", err)
				}
				if livenessRes == nil || livenessRes.Code != 0 || livenessRes.LivenessResults.IsLiveness == 0 || livenessRes.LivenessResults.Confidence < 0.85 {
					return fmt.Errorf("liveness detection failed: face not genuine")
				}
				event.LivenessScore = livenessRes.LivenessResults.Confidence
			}

			baseFaceHeader, err := ConvertLocalFileToMultipartHeader(emp.FaceImageURL)
			if err != nil {
				return fmt.Errorf("system error: unable to load employee base image")
			}
			ctx = context.WithValue(ctx, "org_id", orgID)
			compareRes, err := s.kycService.FaceCompare(ctx, baseFaceHeader, punchFaceHeader)
			if err != nil {
				return fmt.Errorf("face verification failed: %w", err)
			}
			if compareRes.Code != 0 || compareRes.ComparisonResults.IsSameFace == 0 {
				return ErrFaceVerificationFailed
			}
			event.Status = string(models.PunchStatusSuccess)
			event.FaceScore = compareRes.ComparisonResults.Confidence
		}
	}

	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&event).Error; err != nil {
			return fmt.Errorf("failed to save attendance punch event: %w", err)
		}
		if event.Status == string(models.PunchStatusManualReview) {
			review := models.AttendancePunchReview{
				ID:                     utils.GenerateID(),
				OrgID:                  orgID,
				AttendancePunchEventID: event.ID,
				EmployeeID:             emp.ID,
				ReviewStatus:           string(models.AttendanceReviewStatusPending),
				ReviewReason:           "fallback punch requires manual review",
			}
			if err := tx.Create(&review).Error; err != nil {
				return fmt.Errorf("failed to create attendance punch review: %w", err)
			}
		}
		return s.upsertStatusSnapshot(tx, orgID, emp.ID, &event)
	}); err != nil {
		return err
	}

	if event.Status == string(models.PunchStatusSuccess) || event.Status == string(models.PunchStatusManualReview) {
		_ = s.redis.Set(ctx, debounceKey, "1", 5*time.Minute).Err()
	}
	return nil
}

func snapshotDateFor(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func (s *AttendanceService) resolveShiftContext(tx *gorm.DB, orgID, employeeID string, punchTime time.Time) (*models.AttendanceShiftAssignment, *models.AttendanceShiftTemplate, *models.AttendanceGroupMembership, error) {
	snapshotDate := snapshotDateFor(punchTime)
	var assignment models.AttendanceShiftAssignment
	if err := tx.Where("org_id = ? AND assignment_date = ? AND employee_id = ? AND status = ?", orgID, snapshotDate, employeeID, "active").Order("created_at DESC").First(&assignment).Error; err == nil {
		var shiftTemplate models.AttendanceShiftTemplate
		if err := tx.Where("org_id = ? AND id = ?", orgID, assignment.ShiftTemplateID).First(&shiftTemplate).Error; err != nil {
			return nil, nil, nil, fmt.Errorf("failed to load attendance shift template: %w", err)
		}
		return &assignment, &shiftTemplate, nil, nil
	} else if err != gorm.ErrRecordNotFound {
		return nil, nil, nil, fmt.Errorf("failed to load employee shift assignment: %w", err)
	}

	var memberships []models.AttendanceGroupMembership
	if err := tx.Where("org_id = ? AND employee_id = ? AND status = ? AND effective_from <= ? AND (effective_to IS NULL OR effective_to >= ?)", orgID, employeeID, "active", snapshotDate, snapshotDate).Order("is_primary DESC, created_at ASC").Find(&memberships).Error; err != nil {
		return nil, nil, nil, fmt.Errorf("failed to load attendance group membership: %w", err)
	}
	for i := range memberships {
		membership := memberships[i]
		if err := tx.Where("org_id = ? AND assignment_date = ? AND group_id = ? AND status = ?", orgID, snapshotDate, membership.GroupID, "active").Order("created_at DESC").First(&assignment).Error; err == nil {
			var shiftTemplate models.AttendanceShiftTemplate
			if err := tx.Where("org_id = ? AND id = ?", orgID, assignment.ShiftTemplateID).First(&shiftTemplate).Error; err != nil {
				return nil, nil, nil, fmt.Errorf("failed to load attendance shift template: %w", err)
			}
			return &assignment, &shiftTemplate, &membership, nil
		} else if err != gorm.ErrRecordNotFound {
			return nil, nil, nil, fmt.Errorf("failed to load group shift assignment: %w", err)
		}
	}
	if len(memberships) > 0 {
		return nil, nil, &memberships[0], nil
	}
	return nil, nil, nil, nil
}

func parseShiftDateTime(snapshotDate time.Time, hhmm string, crossesDay bool) (*time.Time, error) {
	if hhmm == "" {
		return nil, nil
	}
	tm, err := time.Parse("15:04", hhmm)
	if err != nil {
		return nil, err
	}
	result := time.Date(snapshotDate.Year(), snapshotDate.Month(), snapshotDate.Day(), tm.Hour(), tm.Minute(), 0, 0, snapshotDate.Location())
	if crossesDay {
		result = result.Add(24 * time.Hour)
	}
	return &result, nil
}

func computeLateMinutes(event *models.AttendancePunchEvent, shiftTemplate *models.AttendanceShiftTemplate) (int, error) {
	if event.PunchType != "in" || shiftTemplate == nil {
		return 0, nil
	}
	startAt, err := parseShiftDateTime(snapshotDateFor(event.PunchTime), shiftTemplate.StartTime, false)
	if err != nil || startAt == nil {
		return 0, err
	}
	dueAt := startAt.Add(time.Duration(shiftTemplate.LateGraceMinutes) * time.Minute)
	if event.PunchTime.After(dueAt) {
		return int(event.PunchTime.Sub(dueAt).Minutes()), nil
	}
	return 0, nil
}

func computeEarlyLeaveMinutes(event *models.AttendancePunchEvent, shiftTemplate *models.AttendanceShiftTemplate) (int, error) {
	if event.PunchType != "out" || shiftTemplate == nil {
		return 0, nil
	}
	endAt, err := parseShiftDateTime(snapshotDateFor(event.PunchTime), shiftTemplate.EndTime, shiftTemplate.CrossesDayBoundary)
	if err != nil || endAt == nil {
		return 0, err
	}
	allowedLeaveAt := endAt.Add(-time.Duration(shiftTemplate.EarlyLeaveGraceMinutes) * time.Minute)
	if event.PunchTime.Before(allowedLeaveAt) {
		return int(allowedLeaveAt.Sub(event.PunchTime).Minutes()), nil
	}
	return 0, nil
}

func distanceMeters(lat1, lng1, lat2, lng2 float64) float64 {
	const earthRadius = 6371000.0
	toRad := func(v float64) float64 { return v * math.Pi / 180 }
	dLat := toRad(lat2 - lat1)
	dLng := toRad(lng2 - lng1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(toRad(lat1))*math.Cos(toRad(lat2))*math.Sin(dLng/2)*math.Sin(dLng/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadius * c
}

func (s *AttendanceService) computeOffsiteException(tx *gorm.DB, snapshot *models.AttendanceStatusSnapshot, event *models.AttendancePunchEvent, shiftTemplate *models.AttendanceShiftTemplate) (bool, error) {
	if event.Latitude == 0 && event.Longitude == 0 {
		return false, nil
	}
	var siteID string
	switch {
	case snapshot.SiteID != nil && *snapshot.SiteID != "":
		siteID = *snapshot.SiteID
	case shiftTemplate != nil && shiftTemplate.DefaultSiteID != nil && *shiftTemplate.DefaultSiteID != "":
		siteID = *shiftTemplate.DefaultSiteID
	default:
		return false, nil
	}
	var site models.AttendanceSite
	if err := tx.Where("org_id = ? AND id = ?", snapshot.OrgID, siteID).First(&site).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return false, nil
		}
		return false, fmt.Errorf("failed to load attendance site for offsite detection: %w", err)
	}
	if site.Latitude == 0 && site.Longitude == 0 {
		return false, nil
	}
	return distanceMeters(event.Latitude, event.Longitude, site.Latitude, site.Longitude) > float64(site.RadiusMeters), nil
}

func (s *AttendanceService) upsertStatusSnapshot(tx *gorm.DB, orgID, employeeID string, event *models.AttendancePunchEvent) error {
	snapshotDate := snapshotDateFor(event.PunchTime)
	assignment, shiftTemplate, membership, err := s.resolveShiftContext(tx, orgID, employeeID, event.PunchTime)
	if err != nil {
		return err
	}
	var snapshot models.AttendanceStatusSnapshot
	err = tx.Where("org_id = ? AND employee_id = ? AND snapshot_date = ?", orgID, employeeID, snapshotDate).First(&snapshot).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return fmt.Errorf("failed to load attendance status snapshot: %w", err)
	}
	if err == gorm.ErrRecordNotFound {
		snapshot = models.AttendanceStatusSnapshot{
			ID:           utils.GenerateID(),
			OrgID:        orgID,
			SnapshotDate: snapshotDate,
			EmployeeID:   employeeID,
			Status:       string(models.AttendanceSnapshotStatusScheduled),
		}
	}
	if assignment != nil {
		snapshot.ShiftAssignmentID = &assignment.ID
		snapshot.GroupID = assignment.GroupID
		if assignment.SiteID != nil {
			snapshot.SiteID = assignment.SiteID
		} else if shiftTemplate != nil && shiftTemplate.DefaultSiteID != nil {
			snapshot.SiteID = shiftTemplate.DefaultSiteID
		}
	} else if membership != nil {
		snapshot.GroupID = &membership.GroupID
	}
	lateMinutes, err := computeLateMinutes(event, shiftTemplate)
	if err != nil {
		return fmt.Errorf("failed to compute late minutes: %w", err)
	}
	earlyLeaveMinutes, err := computeEarlyLeaveMinutes(event, shiftTemplate)
	if err != nil {
		return fmt.Errorf("failed to compute early leave minutes: %w", err)
	}
	offsite, err := s.computeOffsiteException(tx, &snapshot, event, shiftTemplate)
	if err != nil {
		return err
	}
	switch event.Status {
	case string(models.PunchStatusManualReview):
		snapshot.Status = string(models.AttendanceSnapshotStatusReviewPending)
		snapshot.ReviewPending = true
		snapshot.ExceptionCode = ""
	case string(models.PunchStatusSuccess):
		snapshot.ReviewPending = false
		if event.PunchType == "out" {
			snapshot.Status = string(models.AttendanceSnapshotStatusCheckedOut)
			lastPunchAt := event.PunchTime
			snapshot.LastPunchAt = &lastPunchAt
			snapshot.EarlyLeaveMinutes = earlyLeaveMinutes
			snapshot.MissingPunch = false
			if snapshot.FirstPunchAt == nil {
				firstPunchAt := event.PunchTime
				snapshot.FirstPunchAt = &firstPunchAt
			}
		} else {
			snapshot.Status = string(models.AttendanceSnapshotStatusCheckedIn)
			firstPunchAt := event.PunchTime
			snapshot.FirstPunchAt = &firstPunchAt
			snapshot.LateMinutes = lateMinutes
			snapshot.MissingPunch = true
		}
		if offsite {
			snapshot.Status = string(models.AttendanceSnapshotStatusOffsite)
			snapshot.ExceptionCode = "offsite"
		} else if snapshot.MissingPunch {
			snapshot.Status = string(models.AttendanceSnapshotStatusMissingPunch)
			snapshot.ExceptionCode = "missing_punch"
		} else if lateMinutes > 0 {
			snapshot.Status = string(models.AttendanceSnapshotStatusLate)
			snapshot.ExceptionCode = "late"
		} else {
			snapshot.ExceptionCode = ""
		}
	case string(models.PunchStatusFailed):
		snapshot.Status = string(models.AttendanceSnapshotStatusException)
		snapshot.ReviewPending = false
		snapshot.ExceptionCode = "verification_failed"
	}
	return tx.Save(&snapshot).Error
}

func (s *AttendanceService) ReviewPunchEvent(ctx context.Context, orgID, reviewID, reviewerUserID string, req ReviewPunchRequest) error {
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if action != "approve" && action != "reject" {
		return ErrInvalidReviewAction
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		var review models.AttendancePunchReview
		if err := tx.Where("org_id = ? AND id = ?", orgID, reviewID).First(&review).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return ErrPunchReviewNotFound
			}
			return fmt.Errorf("failed to load attendance punch review: %w", err)
		}
		var event models.AttendancePunchEvent
		if err := tx.Where("org_id = ? AND id = ?", orgID, review.AttendancePunchEventID).First(&event).Error; err != nil {
			return fmt.Errorf("failed to load attendance punch event: %w", err)
		}
		now := time.Now()
		review.DecisionNotes = req.DecisionNotes
		if req.ReviewReason != "" {
			review.ReviewReason = req.ReviewReason
		}
		review.ReviewedAt = &now
		if reviewerUserID != "" {
			review.ReviewedByUserID = &reviewerUserID
		}
		if action == "approve" {
			review.ReviewStatus = string(models.AttendanceReviewStatusApproved)
			event.Status = string(models.PunchStatusSuccess)
		} else {
			review.ReviewStatus = string(models.AttendanceReviewStatusRejected)
			event.Status = string(models.PunchStatusFailed)
		}
		if err := tx.Save(&review).Error; err != nil {
			return fmt.Errorf("failed to save attendance punch review: %w", err)
		}
		if err := tx.Save(&event).Error; err != nil {
			return fmt.Errorf("failed to save attendance punch event: %w", err)
		}
		return s.upsertStatusSnapshot(tx, orgID, review.EmployeeID, &event)
	})
}
