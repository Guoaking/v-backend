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

type BillingResponse struct {
	Plan struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		Price         int    `json:"price"`
		RequestsLimit int    `json:"requestsLimit"`
	} `json:"plan"`
	UsageSummary struct {
		TotalRequests int64   `json:"totalRequests"`
		Limit         int     `json:"limit"`
		PercentUsed   float64 `json:"percentUsed"`
		Period        string  `json:"period"`
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
	planMap := map[string]struct {
		Name  string
		Price int
		Limit int
	}{
		"starter": {Name: "Starter", Price: 0, Limit: 1000},
		"growth":  {Name: "Growth", Price: 299, Limit: 50000},
		"scale":   {Name: "Scale", Price: 999, Limit: 1000000},
	}
	pm := planMap[org.PlanID]
	var total int64
	monthStart := time.Date(time.Now().Year(), time.Now().Month(), 1, 0, 0, 0, 0, time.Now().Location())
	if scope == "personal" {
		uid := c.GetString("userID")
		_ = h.service.DB.Raw(`SELECT COALESCE(SUM(total),0) FROM usage_daily_user WHERE org_id = ? AND user_id = ? AND date >= ?`, orgID, uid, monthStart).Scan(&total).Error
	} else {
		_ = h.service.DB.Raw(`SELECT COALESCE(SUM(total),0) FROM usage_daily WHERE org_id = ? AND date >= ?`, orgID, monthStart).Scan(&total).Error
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
	resp.Invoices = []struct {
		ID     string `json:"id"`
		Amount int    `json:"amount"`
		Status string `json:"status"`
		Date   string `json:"date"`
	}{}
	JSONSuccess(c, resp)
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
	if period == "7d" {
		days = 7
	} else {
		period = "30d"
	}
	sinceDate := time.Now().AddDate(0, 0, -days)

	var totalRequests, totalErrors int64
	if scope == "personal" {
		uid := c.GetString("userID")
		if err := h.service.DB.Raw(`SELECT COALESCE(SUM(total),0), COALESCE(SUM(failed),0) FROM usage_daily_user WHERE org_id = ? AND user_id = ? AND date >= ?`, orgID, uid, sinceDate).Row().Scan(&totalRequests, &totalErrors); err != nil {
			JSONError(c, CodeDatabaseError, "查询用量失败")
			return
		}
	} else {
		if err := h.service.DB.Raw(`SELECT COALESCE(SUM(total),0), COALESCE(SUM(failed),0) FROM usage_daily WHERE org_id = ? AND date >= ?`, orgID, sinceDate).Row().Scan(&totalRequests, &totalErrors); err != nil {
			JSONError(c, CodeDatabaseError, "查询用量失败")
			return
		}
	}

	var org models.Organization
	if err := h.service.DB.First(&org, "id = ?", orgID).Error; err != nil {
		JSONError(c, CodeDatabaseError, "查询组织失败")
		return
	}
	var q struct {
		Limit   int64
		Used    int64
		ResetAt *time.Time
	}
	if err := h.service.DB.Raw(`SELECT COALESCE(SUM(allocation),0) AS quota_limit, COALESCE(SUM(consumed),0) AS quota_used, MIN(reset_at) AS reset_at FROM organization_quotas WHERE organization_id = ?`, orgID).Row().Scan(&q.Limit, &q.Used, &q.ResetAt); err != nil {
		JSONError(c, CodeDatabaseError, "查询配额失败")
		return
	}
	limit := int(q.Limit)
	used := int(q.Used)
	remaining := 0
	percentUsed := 0.0
	if limit > 0 {
		remaining = int(math.Max(0, float64(limit-used)))
		percentUsed = math.Round((float64(used)/float64(limit))*10000) / 100
	}
	var resetAt *time.Time
	resetAt = q.ResetAt
	var forecast *string
	avgPerDay := float64(used) / float64(maxInt(days, 1))
	if avgPerDay > 0 && limit > 0 {
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
	if scope == "personal" {
		uid := c.GetString("userID")
		var ts []struct {
			D time.Time
			T int64
			F int64
		}
		if err := h.service.DB.Raw(`SELECT date AS d, total AS t, failed AS f FROM usage_daily_user WHERE org_id = ? AND user_id = ? AND date >= ? ORDER BY date`, orgID, uid, sinceDate).Scan(&ts).Error; err != nil {
			JSONError(c, CodeDatabaseError, "查询时间序列失败")
			return
		}
		for _, r := range ts {
			timeline = append(timeline, timelineItem{Date: r.D.Format("2006-01-02"), Requests: r.T, Errors: r.F})
		}
	} else {
		var ts []struct {
			D time.Time
			T int64
			F int64
		}
		if err := h.service.DB.Raw(`SELECT date AS d, total AS t, failed AS f FROM usage_daily WHERE org_id = ? AND date >= ? ORDER BY date`, orgID, sinceDate).Scan(&ts).Error; err != nil {
			JSONError(c, CodeDatabaseError, "查询时间序列失败")
			return
		}
		for _, r := range ts {
			timeline = append(timeline, timelineItem{Date: r.D.Format("2006-01-02"), Requests: r.T, Errors: r.F})
		}
	}
	var svcDaily []struct {
		D         time.Time
		ServiceID string
		T         int64
	}
	if scope == "personal" {
		uid := c.GetString("userID")
		if err := h.service.DB.Raw(`SELECT s.date AS d, s.service_id AS service_id, s.total AS t FROM usage_daily_service s JOIN usage_daily_user u ON s.org_id = u.org_id AND s.date = u.date WHERE s.org_id = ? AND u.user_id = ? AND s.date >= ?`, orgID, uid, sinceDate).Scan(&svcDaily).Error; err != nil {
			JSONError(c, CodeDatabaseError, "查询服务细分失败")
			return
		}
	} else {
		if err := h.service.DB.Raw(`SELECT date AS d, service_id AS service_id, total AS t FROM usage_daily_service WHERE org_id = ? AND date >= ?`, orgID, sinceDate).Scan(&svcDaily).Error; err != nil {
			JSONError(c, CodeDatabaseError, "查询服务细分失败")
			return
		}
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

	var svcAgg []struct {
		ServiceID string
		Cnt       int64
	}
	if scope == "personal" {
		uid := c.GetString("userID")
		if err := h.service.DB.Raw(`SELECT s.service_id AS service_id, SUM(s.total) AS cnt FROM usage_daily_service s JOIN usage_daily_user u ON s.org_id = u.org_id AND s.date = u.date WHERE s.org_id = ? AND u.user_id = ? AND s.date >= ? GROUP BY s.service_id`, orgID, uid, sinceDate).Scan(&svcAgg).Error; err != nil {
			JSONError(c, CodeDatabaseError, "查询服务聚合失败")
			return
		}
	} else {
		if err := h.service.DB.Raw(`SELECT service_id AS service_id, SUM(total) AS cnt FROM usage_daily_service WHERE org_id = ? AND date >= ? GROUP BY service_id`, orgID, sinceDate).Scan(&svcAgg).Error; err != nil {
			JSONError(c, CodeDatabaseError, "查询服务聚合失败")
			return
		}
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
		label := map[string]string{"ocr": "OCR", "face_verify": "Face Verification", "liveness": "Liveness", "other": "Other"}[r.ServiceID]
		pct := 0.0
		if totalForPct > 0 {
			pct = math.Round((float64(r.Cnt)/float64(totalForPct))*1000) / 10
		}
		byService = append(byService, byItem{ID: r.ServiceID, Label: label, Count: r.Cnt, Percentage: pct})
	}

	var epRows []struct {
		EP  string
		Cnt int64
	}
	if scope == "personal" {
		uid := c.GetString("userID")
		if err := h.service.DB.Raw(`SELECT e.endpoint AS ep, SUM(e.total) AS cnt FROM usage_daily_endpoint e JOIN usage_daily_user u ON e.org_id = u.org_id AND e.date = u.date WHERE e.org_id = ? AND u.user_id = ? AND e.date >= ? GROUP BY e.endpoint ORDER BY cnt DESC LIMIT 10`, orgID, uid, sinceDate).Scan(&epRows).Error; err != nil {
			JSONError(c, CodeDatabaseError, "查询接口聚合失败")
			return
		}
	} else {
		if err := h.service.DB.Raw(`SELECT endpoint AS ep, SUM(total) AS cnt FROM usage_daily_endpoint WHERE org_id = ? AND date >= ? GROUP BY endpoint ORDER BY cnt DESC LIMIT 10`, orgID, sinceDate).Scan(&epRows).Error; err != nil {
			JSONError(c, CodeDatabaseError, "查询接口聚合失败")
			return
		}
	}
	byEndpoint := make([]byItem, 0, len(epRows))
	for _, r := range epRows {
		pct := 0.0
		if totalForPct > 0 {
			pct = math.Round((float64(r.Cnt)/float64(totalForPct))*1000) / 10
		}
		byEndpoint = append(byEndpoint, byItem{ID: r.EP, Label: r.EP, Count: r.Cnt, Percentage: pct})
	}

	var keyRows []struct {
		ID  string
		Cnt int64
	}
	if scope == "personal" {
		uid := c.GetString("userID")
		if err := h.service.DB.Raw(`SELECT api_key_id AS id, SUM(total) AS cnt FROM usage_daily_key_user WHERE org_id = ? AND user_id = ? AND date >= ? GROUP BY api_key_id ORDER BY cnt DESC LIMIT 10`, orgID, uid, sinceDate).Scan(&keyRows).Error; err != nil {
			JSONError(c, CodeDatabaseError, "查询密钥聚合失败")
			return
		}
	} else {
		if err := h.service.DB.Raw(`SELECT api_key_id AS id, SUM(total) AS cnt FROM usage_daily_key WHERE org_id = ? AND date >= ? GROUP BY api_key_id ORDER BY cnt DESC LIMIT 10`, orgID, sinceDate).Scan(&keyRows).Error; err != nil {
			JSONError(c, CodeDatabaseError, "查询密钥聚合失败")
			return
		}
	}
	byKey := make([]byItem, 0, len(keyRows))
	for _, r := range keyRows {
		pct := 0.0
		if totalForPct > 0 {
			pct = math.Round((float64(r.Cnt)/float64(totalForPct))*1000) / 10
		}
		byKey = append(byKey, byItem{ID: r.ID, Label: r.ID, Count: r.Cnt, Percentage: pct})
	}

	data := gin.H{
		"totalRequests": totalRequests,
		"totalErrors":   totalErrors,
		"period":        period,
		"quotaStatus": gin.H{"limit": limit, "used": used, "remaining": remaining, "percentUsed": percentUsed, "resetDate": func() *string {
			if resetAt != nil {
				s := resetAt.UTC().Format("2006-01-02T15:04:05Z")
				return &s
			}
			return nil
		}(), "forecastDepletionDate": forecast},
		"timeline":   timeline,
		"byService":  byService,
		"byEndpoint": byEndpoint,
		"byKey":      byKey,
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
		if err := h.service.DB.Raw(`SELECT date AS d, total AS t, failed AS f FROM usage_daily_user WHERE org_id = ? AND user_id = ? AND date >= ? ORDER BY date`, orgID, uid, since).Scan(&rows).Error; err != nil {
			JSONError(c, CodeDatabaseError, "查询失败")
			return
		}
	} else {
		if err := h.service.DB.Raw(`SELECT date AS d, total AS t, failed AS f FROM usage_daily WHERE org_id = ? AND date >= ? ORDER BY date`, orgID, since).Scan(&rows).Error; err != nil {
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
	if err := h.service.DB.Raw(`SELECT COUNT(*) FROM api_request_logs WHERE organization_id = ? AND created_at >= date_trunc('month', now()) AND created_at < date_trunc('month', now()) + interval '1 month'`, orgID).Scan(&total).Error; err != nil {
		JSONError(c, CodeDatabaseError, "查询失败")
		return
	}
	planLimits := map[string]int{"starter": 50000, "growth": 200000, "scale": 1000000}
	limit := planLimits[org.PlanID]
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
