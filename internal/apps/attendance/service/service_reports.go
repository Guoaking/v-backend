package service

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"time"

	"kyc-service/internal/apps/attendance/models"
	"kyc-service/pkg/utils"
)

func reportDimensionValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func (s *AttendanceService) BuildDailyReport(ctx context.Context, orgID string, reportDate time.Time, groupID, siteID *string) (*models.AttendanceDailyReport, error) {
	reportDate = snapshotDateFor(reportDate)
	query := s.db.Model(&models.AttendanceStatusSnapshot{}).Where("org_id = ? AND snapshot_date = ?", orgID, reportDate)
	if normalized := normalizeOptionalString(groupID); normalized != nil {
		query = query.Where("group_id = ?", *normalized)
		groupID = normalized
	} else {
		groupID = nil
	}
	if normalized := normalizeOptionalString(siteID); normalized != nil {
		query = query.Where("site_id = ?", *normalized)
		siteID = normalized
	} else {
		siteID = nil
	}

	var snapshots []models.AttendanceStatusSnapshot
	if err := query.Find(&snapshots).Error; err != nil {
		return nil, fmt.Errorf("failed to load attendance snapshots for daily report: %w", err)
	}

	report := &models.AttendanceDailyReport{
		OrgID:      orgID,
		ReportDate: reportDate,
		GroupID:    reportDimensionValue(groupID),
		SiteID:     reportDimensionValue(siteID),
	}
	for _, snapshot := range snapshots {
		report.TotalEmployees++
		switch snapshot.Status {
		case string(models.AttendanceSnapshotStatusCheckedIn):
			report.CheckedInCount++
		case string(models.AttendanceSnapshotStatusCheckedOut):
			report.CheckedOutCount++
		case string(models.AttendanceSnapshotStatusLate):
			report.CheckedInCount++
		case string(models.AttendanceSnapshotStatusOffsite):
			report.CheckedInCount++
			report.ExceptionCount++
		case string(models.AttendanceSnapshotStatusMissingPunch):
			report.CheckedInCount++
			report.MissingPunchCount++
		case string(models.AttendanceSnapshotStatusReviewPending):
			report.ReviewPendingCount++
		case string(models.AttendanceSnapshotStatusException):
			report.ExceptionCount++
		}
		if snapshot.ReviewPending && snapshot.Status != string(models.AttendanceSnapshotStatusReviewPending) {
			report.ReviewPendingCount++
		}
		if snapshot.MissingPunch && snapshot.Status != string(models.AttendanceSnapshotStatusMissingPunch) {
			report.MissingPunchCount++
		}
		if snapshot.LateMinutes > 0 || snapshot.Status == string(models.AttendanceSnapshotStatusLate) {
			report.LateCount++
		}
	}

	var existing models.AttendanceDailyReport
	err := s.db.Where("org_id = ? AND report_date = ? AND group_id = ? AND site_id = ?", orgID, reportDate, report.GroupID, report.SiteID).First(&existing).Error
	if err == nil {
		report.ID = existing.ID
	} else {
		report.ID = utils.GenerateID()
	}
	if err := s.db.Save(report).Error; err != nil {
		return nil, fmt.Errorf("failed to save attendance daily report: %w", err)
	}
	return report, nil
}

func (s *AttendanceService) BuildMonthlyReport(ctx context.Context, orgID string, reportMonth string, groupID, siteID *string) (*models.AttendanceMonthlyReport, error) {
	monthStart, err := time.Parse("2006-01", reportMonth)
	if err != nil {
		return nil, fmt.Errorf("invalid report month: %w", err)
	}
	monthEnd := monthStart.AddDate(0, 1, 0)
	query := s.db.Model(&models.AttendanceDailyReport{}).Where("org_id = ? AND report_date >= ? AND report_date < ?", orgID, monthStart, monthEnd)
	if normalized := normalizeOptionalString(groupID); normalized != nil {
		query = query.Where("group_id = ?", *normalized)
		groupID = normalized
	} else {
		groupID = nil
	}
	if normalized := normalizeOptionalString(siteID); normalized != nil {
		query = query.Where("site_id = ?", *normalized)
		siteID = normalized
	} else {
		siteID = nil
	}

	var dailyReports []models.AttendanceDailyReport
	if err := query.Find(&dailyReports).Error; err != nil {
		return nil, fmt.Errorf("failed to load daily reports for monthly report: %w", err)
	}

	report := &models.AttendanceMonthlyReport{
		OrgID:       orgID,
		ReportMonth: reportMonth,
		GroupID:     reportDimensionValue(groupID),
		SiteID:      reportDimensionValue(siteID),
	}
	for _, daily := range dailyReports {
		if daily.TotalEmployees > report.TotalEmployees {
			report.TotalEmployees = daily.TotalEmployees
		}
		report.AttendanceDays++
		if daily.CheckedInCount > 0 {
			report.CheckedInDays++
		}
		if daily.CheckedOutCount > 0 {
			report.CheckedOutDays++
		}
		report.LateCount += daily.LateCount
		report.MissingPunchCount += daily.MissingPunchCount
		report.ReviewPendingCount += daily.ReviewPendingCount
		report.ExceptionCount += daily.ExceptionCount
	}

	var existing models.AttendanceMonthlyReport
	err = s.db.Where("org_id = ? AND report_month = ? AND group_id = ? AND site_id = ?", orgID, reportMonth, report.GroupID, report.SiteID).First(&existing).Error
	if err == nil {
		report.ID = existing.ID
	} else {
		report.ID = utils.GenerateID()
	}
	if err := s.db.Save(report).Error; err != nil {
		return nil, fmt.Errorf("failed to save attendance monthly report: %w", err)
	}
	return report, nil
}

func (s *AttendanceService) ExportDailyReportCSV(ctx context.Context, orgID string, reportDate time.Time, groupID, siteID *string) (string, error) {
	report, err := s.BuildDailyReport(ctx, orgID, reportDate, groupID, siteID)
	if err != nil {
		return "", err
	}
	buf := &bytes.Buffer{}
	writer := csv.NewWriter(buf)
	_ = writer.Write([]string{"report_date", "group_id", "site_id", "total_employees", "checked_in_count", "checked_out_count", "late_count", "missing_punch_count", "review_pending_count", "exception_count"})
	_ = writer.Write([]string{
		report.ReportDate.Format("2006-01-02"),
		report.GroupID,
		report.SiteID,
		fmt.Sprintf("%d", report.TotalEmployees),
		fmt.Sprintf("%d", report.CheckedInCount),
		fmt.Sprintf("%d", report.CheckedOutCount),
		fmt.Sprintf("%d", report.LateCount),
		fmt.Sprintf("%d", report.MissingPunchCount),
		fmt.Sprintf("%d", report.ReviewPendingCount),
		fmt.Sprintf("%d", report.ExceptionCount),
	})
	writer.Flush()
	return buf.String(), writer.Error()
}
