package api

import (
	"encoding/json"
	"math"
	"strings"
	"time"

	"kyc-service/internal/models"
	"kyc-service/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type OrgUsageHandler struct {
	service *service.KYCService
}

func NewOrgUsageHandler(svc *service.KYCService) *OrgUsageHandler {
	return &OrgUsageHandler{service: svc}
}

type ServiceQuota struct {
	ServiceType string  `json:"serviceType"`
	Used        int64   `json:"used"`
	Limit       int     `json:"limit"`
	PercentUsed float64 `json:"percentUsed"`
}

type BillingResponse struct {
	Plan struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		Price         int    `json:"price"`
		RequestsLimit int    `json:"requestsLimit"` // Legacy field
	} `json:"plan"`
	UsageSummary struct {
		TotalRequests int64          `json:"totalRequests"`
		Limit         int            `json:"limit"` // Legacy field
		PercentUsed   float64        `json:"percentUsed"`
		Period        string         `json:"period"`
		Quotas        []ServiceQuota `json:"quotas"` // New field for separated limits
	} `json:"usageSummary"`
	Invoices []struct {
		ID     string `json:"id"`
		Amount int    `json:"amount"`
		Status string `json:"status"`
		Date   string `json:"date"`
	} `json:"invoices"`
}

// GetBilling 获取账单与用量
func (h *OrgUsageHandler) GetBilling(c *gin.Context) {
	orgID := c.GetString("orgID")
	if orgID == "" {
		JSONError(c, CodeInvalidParameter, "组织信息错误")
		return
	}
	var org models.Organization
	if err := h.service.DB.First(&org, "id = ?", orgID).Error; err != nil {
		JSONError(c, CodeNotFound, "组织不存在")
		return
	}
	scope := strings.ToLower(strings.TrimSpace(c.Query("scope")))
	if scope == "" {
		scope = "org"
	}
	monthStart := time.Date(time.Now().Year(), time.Now().Month(), 1, 0, 0, 0, 0, time.Now().Location())

	// 从数据库读取 plan (包含 quota_config)
	type planRow struct {
		Name          string
		Price         int
		RequestsLimit int
		QuotaConfig   string // json string
	}
	var pr planRow
	var pm struct {
		Name        string
		Price       int
		Limit       int
		QuotaConfig map[string]struct {
			Limit  int    `json:"limit"`
			Period string `json:"period"`
		}
	}

	if err := h.service.DB.Raw("SELECT name, COALESCE(price,0) as price, COALESCE(requests_limit,0) as requests_limit, COALESCE(quota_config::text, '{}') as quota_config FROM plans WHERE id = ?", org.PlanID).Scan(&pr).Error; err == nil && pr.Name != "" {
		pm.Name = pr.Name
		pm.Price = pr.Price
		pm.Limit = pr.RequestsLimit
		if pr.QuotaConfig != "" {
			json.Unmarshal([]byte(pr.QuotaConfig), &pm.QuotaConfig)
		}
	} else {
		// Fallback for missing DB plans
		planMap := map[string]struct {
			Name  string
			Price int
			Limit int
		}{
			"starter": {Name: "Starter", Price: 0, Limit: 1000},
			"growth":  {Name: "Growth", Price: 299, Limit: 50000},
			"scale":   {Name: "Scale", Price: 999, Limit: 1000000},
		}
		p := planMap[org.PlanID]
		pm.Name = p.Name
		pm.Price = p.Price
		pm.Limit = p.Limit
	}

	// 从 organization_quotas 表读取真实配额和消耗
	var orgQuotas []models.OrganizationQuotas
	h.service.DB.Where("organization_id = ?", orgID).Find(&orgQuotas)

	quotas := []ServiceQuota{}
	var total int64 = 0

	if len(orgQuotas) > 0 {
		for _, q := range orgQuotas {
			used := int64(q.Consumed)
			total += used
			percent := float64(0)
			if q.Allocation > 0 {
				percent = float64(used) / float64(q.Allocation) * 100
			}
			quotas = append(quotas, ServiceQuota{
				ServiceType: q.ServiceType,
				Used:        used,
				Limit:       q.Allocation,
				PercentUsed: percent,
			})
		}
	} else {
		// Fallback to aggregation if no quotas found
		var usageData []struct {
			ServiceType string
			Total       int64
		}

		if scope == "personal" {
			uid := c.GetString("userID")
			_ = h.service.DB.Model(&models.UsageMetricAgg{}).
				Select("dimensions->>'service_type' as service_type, COALESCE(SUM(total_requests),0) as total").
				Where("org_id = ? AND dimensions->>'user_id' = ? AND stat_time >= ?", orgID, uid, monthStart).
				Group("dimensions->>'service_type'").
				Scan(&usageData).Error
		} else {
			_ = h.service.DB.Model(&models.UsageMetricAgg{}).
				Select("dimensions->>'service_type' as service_type, COALESCE(SUM(total_requests),0) as total").
				Where("org_id = ? AND stat_time >= ?", orgID, monthStart).
				Group("dimensions->>'service_type'").
				Scan(&usageData).Error
		}

		if len(pm.QuotaConfig) > 0 {
			usageMap := make(map[string]int64)
			for _, u := range usageData {
				usageMap[u.ServiceType] = u.Total
				total += u.Total
			}

			for svc, config := range pm.QuotaConfig {
				used := usageMap[svc]
				percent := float64(0)
				if config.Limit > 0 {
					percent = float64(used) / float64(config.Limit) * 100
				}
				quotas = append(quotas, ServiceQuota{
					ServiceType: svc,
					Used:        used,
					Limit:       config.Limit,
					PercentUsed: percent,
				})
			}
		} else {
			for _, u := range usageData {
				total += u.Total
			}
		}
	}

	percent := float64(0)
	if pm.Limit > 0 {
		percent = float64(total) / float64(pm.Limit) * 100
	}

	resp := BillingResponse{}
	resp.Plan.ID = org.PlanID
	resp.Plan.Name = pm.Name
	resp.Plan.Price = pm.Price
	resp.Plan.RequestsLimit = pm.Limit
	resp.UsageSummary.TotalRequests = total
	resp.UsageSummary.Limit = pm.Limit
	resp.UsageSummary.PercentUsed = percent
	resp.UsageSummary.Period = time.Now().Format("2006-01")
	resp.UsageSummary.Quotas = quotas

	resp.Invoices = []struct {
		ID     string `json:"id"`
		Amount int    `json:"amount"`
		Status string `json:"status"`
		Date   string `json:"date"`
	}{}
	JSONSuccess(c, resp)
}

// 触发手动聚合日志到 metric_aggs 表 (测试/初始化用)
func (h *OrgUsageHandler) TriggerUsageAggregation(c *gin.Context) {
	orgID := c.Param("org_id")
	// Check basic permissions (skip deep authorization for this maintenance endpoint)
	if orgID == "" {
		JSONError(c, 400, "缺少 org_id 参数")
		return
	}

	// 1. 将明细日志按天、API Key 和 Endpoint 聚合写入 usage_metric_aggs
	aggSQL := `
		INSERT INTO usage_metric_aggs (org_id, metric_name, time_unit, stat_time, dimensions, total_requests, total_errors, usage_units)
		SELECT 
			org_id,
			'api_call' AS metric_name,
			'day' AS time_unit,
			DATE_TRUNC('day', created_at) AS stat_time,
			jsonb_build_object(
				'service_type', COALESCE(service_type, 'unknown'),
				'endpoint', endpoint,
				'user_id', COALESCE(user_id, '')
			) AS dimensions,
			COUNT(*) AS total_requests,
			COALESCE(SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END), 0) AS total_errors,
			COALESCE(SUM(usage_units), 0) AS usage_units
		FROM usage_logs
		WHERE org_id = ?
		GROUP BY org_id, DATE_TRUNC('day', created_at), service_type, endpoint, user_id
		ON CONFLICT (org_id, metric_name, time_unit, stat_time, dimensions) 
		DO UPDATE SET 
			total_requests = EXCLUDED.total_requests,
			total_errors = EXCLUDED.total_errors,
			usage_units = EXCLUDED.usage_units,
			updated_at = CURRENT_TIMESTAMP;
	`

	if err := h.service.DB.Exec(aggSQL, orgID).Error; err != nil {
		JSONError(c, CodeDatabaseError, "聚合失败")
		return
	}

	JSONSuccess(c, "聚合成功")
}

// GetUsageDetailedV2 使用预聚合表并严格错误处理
func (h *OrgUsageHandler) GetUsageDetailedV2(c *gin.Context) {
	orgID := c.GetString("orgID")
	if orgID == "" {
		JSONError(c, CodeInvalidParameter, "组织信息错误")
		return
	}
	scope := strings.ToLower(strings.TrimSpace(c.Query("scope")))
	if scope == "" {
		scope = "org"
	}
	period := strings.ToLower(strings.TrimSpace(c.Query("period")))
	days := 30
	if period == "today" || period == "24h" {
		days = 1
	} else if period == "7d" {
		days = 7
	} else {
		period = "30d"
	}

	// Ensure we capture today by resetting time to start of day if days=1, or relative days otherwise
	var sinceDate time.Time
	if days == 1 {
		now := time.Now()
		sinceDate = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	} else {
		sinceDate = time.Now().AddDate(0, 0, -days)
	}

	// 1. Get overall metrics directly from usage_metric_aggs
	var totalRequests, totalErrors int64

	queryBase := h.service.DB.Model(&models.UsageMetricAgg{}).
		Where("org_id = ? AND stat_time >= ? AND time_unit = 'day'", orgID, sinceDate)

	if scope == "personal" {
		uid := c.GetString("userID")
		queryBase = queryBase.Where("dimensions->>'user_id' = ?", uid)
	}

	// Calculate totals
	if err := queryBase.Select("COALESCE(SUM(total_requests), 0) as total, COALESCE(SUM(total_errors), 0) as failed").Row().Scan(&totalRequests, &totalErrors); err != nil {
		JSONError(c, CodeDatabaseError, "查询用量失败")
		return
	}

	var org models.Organization
	if err := h.service.DB.First(&org, "id = ?", orgID).Error; err != nil {
		JSONError(c, CodeDatabaseError, "查询组织失败")
		return
	}
	// 2. Quota Check (using ServiceQuota array)
	var orgQuotas []models.OrganizationQuotas
	h.service.DB.Where("organization_id = ?", orgID).Find(&orgQuotas)

	quotas := []ServiceQuota{}
	var totalLimit int = 0
	var totalUsed int = 0
	var resetAt *time.Time

	if len(orgQuotas) > 0 {
		resetAt = orgQuotas[0].ResetAt
		for _, q := range orgQuotas {
			used := int(q.Consumed)
			totalUsed += used
			totalLimit += q.Allocation
			percent := float64(0)
			if q.Allocation > 0 {
				percent = float64(used) / float64(q.Allocation) * 100
			}
			quotas = append(quotas, ServiceQuota{
				ServiceType: q.ServiceType,
				Used:        int64(used),
				Limit:       q.Allocation,
				PercentUsed: percent,
			})
		}
	} else {
		// Fallback if no specific quotas
		totalLimit = 100000
		var fallbackUsed int64
		h.service.DB.Model(&models.UsageMetricAgg{}).
			Where("org_id = ? AND time_unit = 'day'", orgID).
			Select("COALESCE(SUM(usage_units), 0)").Row().Scan(&fallbackUsed)
		totalUsed = int(fallbackUsed)
		now := time.Now()
		nextMonth := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, now.Location())
		resetAt = &nextMonth

		// If we still want to show something in fallback
		quotas = append(quotas, ServiceQuota{
			ServiceType: "global",
			Used:        fallbackUsed,
			Limit:       totalLimit,
			PercentUsed: float64(fallbackUsed) / float64(totalLimit) * 100,
		})
	}

	remaining := 0
	percentUsed := 0.0
	if totalLimit > 0 {
		remaining = int(math.Max(0, float64(totalLimit-totalUsed)))
		percentUsed = math.Round((float64(totalUsed)/float64(totalLimit))*10000) / 100
	}

	var forecast *string
	avgPerDay := float64(totalUsed) / float64(maxInt(days, 1))
	if avgPerDay > 0 && totalLimit > 0 {
		supportDays := float64(remaining) / avgPerDay
		if supportDays > 0 && supportDays <= float64(days) {
			d := time.Now().Add(time.Duration(int(math.Ceil(supportDays))) * 24 * time.Hour).Format("2006-01-02")
			forecast = &d
		}
	}

	type timelineItem struct {
		Date     string           `json:"date"`
		Requests int64            `json:"requests"`
		Errors   int64            `json:"errors"`
		Services map[string]int64 `json:"services,omitempty"`
	}
	timeline := make([]timelineItem, 0, days)

	// 3. Timeline data from usage_metric_aggs
	var ts []struct {
		D time.Time
		T int64
		F int64
	}

	timelineQuery := h.service.DB.Model(&models.UsageMetricAgg{}).
		Select("stat_time as d, SUM(total_requests) as t, SUM(total_errors) as f").
		Where("org_id = ? AND stat_time >= ? AND time_unit = 'day'", orgID, sinceDate).
		Group("stat_time").
		Order("d")

	if scope == "personal" {
		timelineQuery = timelineQuery.Where("dimensions->>'user_id' = ?", c.GetString("userID"))
	}

	if err := timelineQuery.Scan(&ts).Error; err != nil {
		JSONError(c, CodeDatabaseError, "查询时间序列失败")
		return
	}
	for _, r := range ts {
		timeline = append(timeline, timelineItem{Date: r.D.Format("2006-01-02"), Requests: r.T, Errors: r.F, Services: map[string]int64{}})
	}

	// 4. Service breakdown per day from usage_metric_aggs
	var svcDaily []struct {
		D         time.Time
		ServiceID string
		T         int64
	}

	svcDailyQuery := h.service.DB.Model(&models.UsageMetricAgg{}).
		Select("stat_time as d, dimensions->>'service_type' as service_id, SUM(usage_units) as t").
		Where("org_id = ? AND stat_time >= ? AND time_unit = 'day' AND dimensions->>'service_type' IS NOT NULL", orgID, sinceDate).
		Group("stat_time, dimensions->>'service_type'")

	if scope == "personal" {
		svcDailyQuery = svcDailyQuery.Where("dimensions->>'user_id' = ?", c.GetString("userID"))
	}

	if err := svcDailyQuery.Scan(&svcDaily).Error; err != nil {
		JSONError(c, CodeDatabaseError, "查询服务细分失败")
		return
	}

	tidx := map[string]int{}
	for i, it := range timeline {
		tidx[it.Date] = i
	}
	for _, r := range svcDaily {
		d := r.D.Format("2006-01-02")
		if i, ok := tidx[d]; ok {
			if timeline[i].Services == nil {
				timeline[i].Services = map[string]int64{}
			}
			timeline[i].Services[r.ServiceID] += r.T
		}
	}

	// 5. Aggregated services from usage_metric_aggs
	var svcAgg []struct {
		ServiceID string
		Cnt       int64
	}

	svcAggQuery := h.service.DB.Model(&models.UsageMetricAgg{}).
		Select("dimensions->>'service_type' as service_id, SUM(usage_units) as cnt").
		Where("org_id = ? AND stat_time >= ? AND time_unit = 'day' AND dimensions->>'service_type' IS NOT NULL AND dimensions->>'service_type' != '' AND dimensions->>'service_type' != 'unknown'", orgID, sinceDate).
		Group("dimensions->>'service_type'")

	if scope == "personal" {
		svcAggQuery = svcAggQuery.Where("dimensions->>'user_id' = ?", c.GetString("userID"))
	}

	if err := svcAggQuery.Scan(&svcAgg).Error; err != nil {
		JSONError(c, CodeDatabaseError, "查询服务聚合失败")
		return
	}
	type byItem struct {
		ID         string  `json:"id"`
		Label      string  `json:"label"`
		Count      int64   `json:"count"`
		Percentage float64 `json:"percentage"`
	}
	byService := make([]byItem, 0, len(svcAgg))
	var totalForPct int64
	for _, r := range svcAgg {
		totalForPct += r.Cnt
	}
	for _, r := range svcAgg {
		// Only include items with count > 0 to prevent ghost data
		if r.Cnt <= 0 {
			continue
		}

		label := map[string]string{"ocr": "OCR", "face_verify": "Face Verification", "face": "Face Recognition", "liveness": "Liveness", "other": "Other"}[r.ServiceID]
		if label == "" {
			// fallback to capitalize the ID if it's not in the map
			if len(r.ServiceID) > 0 {
				label = strings.ToUpper(r.ServiceID[:1]) + r.ServiceID[1:]
			} else {
				label = "Unknown"
			}
		}
		pct := 0.0
		if totalForPct > 0 {
			pct = math.Round((float64(r.Cnt)/float64(totalForPct))*1000) / 10
		}
		byService = append(byService, byItem{ID: r.ServiceID, Label: label, Count: r.Cnt, Percentage: pct})
	}

	// 6. Endpoint breakdown from usage_metric_aggs
	var epRows []struct {
		EP  string
		Cnt int64
		Err int64 // Added error count for endpoint
	}

	epQuery := h.service.DB.Model(&models.UsageMetricAgg{}).
		Select("dimensions->>'endpoint' as ep, SUM(total_requests) as cnt, SUM(total_errors) as err").
		Where("org_id = ? AND stat_time >= ? AND time_unit = 'day' AND dimensions->>'endpoint' IS NOT NULL", orgID, sinceDate).
		Group("dimensions->>'endpoint'").
		Order("cnt DESC").
		Limit(10)

	if scope == "personal" {
		epQuery = epQuery.Where("dimensions->>'user_id' = ?", c.GetString("userID"))
	}

	if err := epQuery.Scan(&epRows).Error; err != nil {
		JSONError(c, CodeDatabaseError, "查询接口聚合失败")
		return
	}

	type endpointItem struct {
		ID         string  `json:"id"`
		Endpoint   string  `json:"endpoint"`
		Count      int64   `json:"count"`
		Errors     int64   `json:"errors"`
		Percentage float64 `json:"percentage"`
	}

	byEndpoint := make([]endpointItem, 0, len(epRows))
	for _, r := range epRows {
		pct := 0.0
		if totalForPct > 0 {
			pct = math.Round((float64(r.Cnt)/float64(totalForPct))*1000) / 10
		}
		byEndpoint = append(byEndpoint, endpointItem{
			ID:         r.EP,
			Endpoint:   r.EP,
			Count:      r.Cnt,
			Errors:     r.Err,
			Percentage: pct,
		})
	}

	data := gin.H{
		"totalRequests": totalRequests,
		"totalErrors":   totalErrors,
		"period":        period,
		"quotaStatus": gin.H{"limit": totalLimit, "used": totalUsed, "remaining": remaining, "percentUsed": percentUsed, "quotas": quotas, "resetDate": func() *string {
			if resetAt != nil {
				s := resetAt.UTC().Format("2006-01-02T15:04:05Z")
				return &s
			}
			return nil
		}(), "forecastDepletionDate": forecast},
		"timeline":   timeline,
		"byService":  byService,
		"byEndpoint": byEndpoint,
	}
	respBytes, _ := json.Marshal(gin.H{"success": true, "data": data})
	c.Data(200, "application/json", respBytes)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

type UsageDailyResponse struct {
	Date     string  `json:"date"`
	Requests int64   `json:"requests"`
	Errors   int64   `json:"errors"`
	Cost     float64 `json:"cost,omitempty"`
}

// GetUsageDaily 获取日统计
func (h *OrgUsageHandler) GetUsageDaily(c *gin.Context) {
	orgID := c.GetString("orgID")
	if orgID == "" {
		JSONError(c, CodeInvalidParameter, "组织信息错误")
		return
	}
	scope := strings.ToLower(strings.TrimSpace(c.Query("scope")))
	if scope == "" {
		scope = "org"
	}
	period := c.Query("period")
	days := 30
	if period == "7d" {
		days = 7
	} else if period == "90d" {
		days = 90
	}
	since := time.Now().AddDate(0, 0, -days)
	var rows []struct {
		D time.Time
		T int64
		F int64
	}
	if scope == "personal" {
		uid := c.GetString("userID")
		if err := h.service.DB.Model(&models.UsageMetricAgg{}).
			Select("stat_time AS d, SUM(total_requests) AS t, SUM(total_errors) AS f").
			Where("org_id = ? AND dimensions->>'user_id' = ? AND stat_time >= ? AND time_unit = 'day'", orgID, uid, since).
			Group("stat_time").
			Order("stat_time").
			Scan(&rows).Error; err != nil {
			JSONError(c, CodeDatabaseError, "查询失败")
			return
		}
	} else {
		if err := h.service.DB.Model(&models.UsageMetricAgg{}).
			Select("stat_time AS d, SUM(total_requests) AS t, SUM(total_errors) AS f").
			Where("org_id = ? AND stat_time >= ? AND time_unit = 'day'", orgID, since).
			Group("stat_time").
			Order("stat_time").
			Scan(&rows).Error; err != nil {
			JSONError(c, CodeDatabaseError, "查询失败")
			return
		}
	}
	resp := make([]UsageDailyResponse, len(rows))
	// 计算单位成本：按计划 price / requests_limit（缺失时成本为0）
	var org models.Organization
	_ = h.service.DB.First(&org, "id = ?", orgID).Error
	var unitCost float64
	type planRow struct {
		Price         int
		RequestsLimit int
	}
	var pr planRow
	_ = h.service.DB.Raw("SELECT COALESCE(price,0) AS price, COALESCE(requests_limit,0) AS requests_limit FROM plans WHERE id = ?", org.PlanID).Scan(&pr).Error
	if pr.Price > 0 && pr.RequestsLimit > 0 {
		unitCost = float64(pr.Price) / float64(pr.RequestsLimit)
	}
	for i, r := range rows {
		resp[i] = UsageDailyResponse{Date: r.D.Format("2006-01-02"), Requests: r.T, Errors: r.F, Cost: unitCost * float64(r.T)}
	}
	JSONSuccess(c, resp)
}

// GetUsageSummary 组织用量汇总
func (h *OrgUsageHandler) GetUsageSummary(c *gin.Context) {
	orgID := c.Param("org_id")
	if orgID == "" {
		JSONError(c, CodeInvalidParameter, "组织ID不能为空")
		return
	}
	var org models.Organization
	if err := h.service.DB.Where("id = ?", orgID).First(&org).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			JSONError(c, CodeNotFound, "组织不存在")
			return
		}
		JSONError(c, CodeDatabaseError, "系统错误")
		return
	}
	now := time.Now()
	period := now.Format("2006-01")
	var total int64

	// Get month start and end dates in Go
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	nextMonthStart := monthStart.AddDate(0, 1, 0)

	if err := h.service.DB.Model(&models.UsageMetricAgg{}).
		Select("COALESCE(SUM(total_requests),0)").
		Where("org_id = ? AND stat_time >= ? AND stat_time < ?", orgID, monthStart, nextMonthStart).
		Scan(&total).Error; err != nil {
		JSONError(c, CodeDatabaseError, "查询失败")
		return
	}

	// Default to reading from db instead of hardcoding
	type planRow struct {
		RequestsLimit int
	}
	var pr planRow
	var limit int

	if err := h.service.DB.Raw("SELECT COALESCE(requests_limit, 0) as requests_limit FROM plans WHERE id = ?", org.PlanID).Scan(&pr).Error; err == nil && pr.RequestsLimit > 0 {
		limit = pr.RequestsLimit
	} else {
		// Fallback if plan doesn't exist in DB yet
		planLimits := map[string]int{"starter": 50000, "growth": 200000, "scale": 1000000}
		limit = planLimits[org.PlanID]
	}

	percent := float64(0)
	status := "healthy"
	if limit > 0 {
		percent = float64(total) / float64(limit) * 100
		if percent >= 100 {
			status = "exceeded"
		} else if percent >= 80 {
			status = "warning"
		}
	}
	JSONSuccess(c, gin.H{"period": period, "total_requests": total, "plan_limit": limit, "usage_percent": percent, "status": status})
}
