package api

import (
	"time"

	"kyc-service/internal/models"

	"github.com/gin-gonic/gin"
)

func (h *ConsoleHandler) GetUsage(c *gin.Context) {
	orgID := c.GetString("orgID")
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")
	period := c.Query("period")
	if period != "" {
		now := time.Now()
		var dur time.Duration
		if period == "7d" {
			dur = 7 * 24 * time.Hour
		} else if period == "30d" {
			dur = 30 * 24 * time.Hour
		} else {
			dur = 7 * 24 * time.Hour
		}
		startDate = now.Add(-dur).Format("2006-01-02")
		endDate = now.Format("2006-01-02")
	}
	if startDate == "" || endDate == "" {
		JSONError(c, CodeInvalidParameter, "缺少日期范围")
		return
	}
	var rows []UsageItem
	// 按天聚合成功/失败
	if err := h.service.DB.Raw(`
        SELECT to_char(DATE(created_at), 'YYYY-MM-DD') AS date,
               SUM(CASE WHEN status_code < 400 THEN 1 ELSE 0 END) AS success,
               SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END) AS failed
        FROM api_request_logs
        WHERE org_id = ? AND created_at >= ?::date AND created_at <= ?::date + interval '1 day' - interval '1 second'
        GROUP BY DATE(created_at)
        ORDER BY DATE(created_at)
    `, orgID, startDate, endDate).Scan(&rows).Error; err != nil {
		JSONError(c, CodeDatabaseError, "查询失败")
		return
	}
	JSONSuccess(c, rows)
}

func (h *ConsoleHandler) GetUsageStats(c *gin.Context) {
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")
	if startDate == "" || endDate == "" {
		JSONError(c, CodeInvalidParameter, "缺少日期范围")
		return
	}
	type Daily struct {
		Date     string `json:"date"`
		Requests int64  `json:"requests"`
		Errors   int64  `json:"errors"`
	}
	var daily []Daily
	if err := h.service.DB.Raw(`
        SELECT to_char(DATE(created_at), 'YYYY-MM-DD') AS date,
               COUNT(*) AS requests,
               SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END) AS errors
        FROM api_request_logs
        WHERE org_id = ? AND created_at >= ?::date AND created_at <= ?::date + interval '1 day' - interval '1 second'
        GROUP BY DATE(created_at)
        ORDER BY DATE(created_at)
    `, c.GetString("orgID"), startDate, endDate).Scan(&daily).Error; err != nil {
		JSONError(c, CodeDatabaseError, "查询失败")
		return
	}
	type ErrItem struct {
		Code  int   `json:"code"`
		Count int64 `json:"count"`
	}
	var errorsBreakdown []ErrItem
	if err := h.service.DB.Raw(`
        SELECT status_code AS code, COUNT(*) AS count
        FROM api_request_logs
        WHERE org_id = ? AND created_at >= ?::date AND created_at <= ?::date + interval '1 day' - interval '1 second'
          AND status_code >= 400
        GROUP BY status_code
        ORDER BY status_code
    `, c.GetString("orgID"), startDate, endDate).Scan(&errorsBreakdown).Error; err != nil {
		JSONError(c, CodeDatabaseError, "查询失败")
		return
	}
	JSONSuccess(c, gin.H{"daily": daily, "errors_breakdown": errorsBreakdown})
}

func (h *ConsoleHandler) GetQuotaStatus(c *gin.Context) {
	orgID := c.GetString("orgID")
	var quotas []models.OrganizationQuotas
	_ = h.service.DB.Where("organization_id = ?", orgID).Find(&quotas).Error
	m := map[string]QuotaStatusItem{}
	for _, q := range quotas {
		var resetStr *string
		if q.ResetAt != nil {
			s := q.ResetAt.UTC().Format("2006-01-02T15:04:05Z")
			resetStr = &s
		}
		m[q.ServiceType] = QuotaStatusItem{Limit: q.Allocation, Used: q.Consumed, Remaining: q.Allocation - q.Consumed, ResetAt: resetStr}
	}
	JSONSuccess(c, m)
}
