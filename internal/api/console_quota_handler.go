package api

import (
	"kyc-service/internal/models"
	"kyc-service/pkg/logger"
	"time"

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
	period := c.Query("period") // '30d' or '7d'
	if period == "" {
		period = "30d"
	}

	scope := c.Query("scope") // 'org' or 'personal'

	days := 30
	if period == "7d" {
		days = 7
	}

	endDate := time.Now()
	startDate := endDate.AddDate(0, 0, -days)

	type Daily struct {
		Date     string `json:"date"`
		Requests int64  `json:"requests"`
		Errors   int64  `json:"errors"`
		OCR      int64  `json:"ocr"`
		Face     int64  `json:"face"`
		Liveness int64  `json:"liveness"`
	}

	var results []Daily
	q := h.service.DB.Model(&models.UsageLog{}).
		Select(`
			to_char(created_at, 'YYYY-MM-DD') as date, 
			count(*) as requests, 
			sum(case when status_code >= 400 then 1 else 0 end) as errors,
			sum(case when service_name = 'ocr' then 1 else 0 end) as ocr,
			sum(case when service_name = 'face' then 1 else 0 end) as face,
			sum(case when service_name = 'liveness' then 1 else 0 end) as liveness
		`).
		Where("created_at >= ? AND created_at <= ?", startDate, endDate).
		Group("to_char(created_at, 'YYYY-MM-DD')").
		Order("date ASC")

	if scope == "personal" {
		// Try to match via token's owner or fallback if schema doesn't support personal usage yet
		// For now, we will return org usage if personal isn't strictly tracked per request
		orgID := c.GetString("orgID")
		q = q.Where("org_id = ?", orgID)
	} else {
		orgID := c.GetString("orgID")
		q = q.Where("org_id = ?", orgID)
	}

	if err := q.Scan(&results).Error; err != nil {
		logger.GetLogger().WithError(err).Error("获取使用统计失败")
		JSONError(c, CodeDatabaseError, "获取使用统计失败")
		return
	}

	// Calculate totals and build timeline
	var totalReq, totalErr int64
	timelineMap := make(map[string]Daily)
	for _, r := range results {
		timelineMap[r.Date] = r
		totalReq += r.Requests
		totalErr += r.Errors
	}

	// Fill missing days with 0
	var timeline []map[string]interface{}
	for i := days - 1; i >= 0; i-- {
		d := endDate.AddDate(0, 0, -i).Format("2006-01-02")
		if val, ok := timelineMap[d]; ok {
			timeline = append(timeline, map[string]interface{}{
				"date":     val.Date,
				"requests": val.Requests,
				"errors":   val.Errors,
				"services": map[string]int64{
					"ocr":      val.OCR,
					"face":     val.Face,
					"liveness": val.Liveness,
				},
			})
		} else {
			timeline = append(timeline, map[string]interface{}{
				"date":     d,
				"requests": 0,
				"errors":   0,
				"services": map[string]int64{"ocr": 0, "face": 0, "liveness": 0},
			})
		}
	}

	JSONSuccess(c, gin.H{
		"totalRequests": totalReq,
		"totalErrors":   totalErr,
		"timeline":      timeline,
	})
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
