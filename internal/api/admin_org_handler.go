package api

import (
	"encoding/json"
	"fmt"
	"time"

	"kyc-service/internal/middleware"
	"kyc-service/internal/models"
	"kyc-service/pkg/logger"

	"github.com/gin-gonic/gin"
)

func (h *AdminHandler) GetOrganizationList(c *gin.Context) {
	start := time.Now()

	// 权限检查 - 确保是超级管理员
	userRole, exists := c.Get("userRole")
	if !exists || userRole != "admin" {
		middleware.RecordBusinessOperation("admin_org_list", false, time.Since(start), "permission_denied")
		JSONError(c, CodeForbidden, "权限不足")
		return
	}

	var req PaginationRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		middleware.RecordBusinessOperation("admin_org_list", false, time.Since(start), "invalid_request")
		JSONError(c, CodeInvalidParameter, "参数验证失败")
		return
	}

	// 设置默认值并计算分页参数
	if req.Page < 1 {
		req.Page = 1
	}
	if req.Limit < 1 || req.Limit > 100 {
		req.Limit = 10 // 默认每页10条
	}

	// 如果提供了offset，优先使用offset；否则根据page和limit计算
	var offset int
	if req.Offset > 0 {
		offset = req.Offset
	} else {
		offset = (req.Page - 1) * req.Limit
	}

	// 构建查询 - 修改字段映射以匹配结构体
	query := h.service.DB.Table("organizations o").
		Select(`o.id, o.name, o.billing_email, o.plan_id, o.status, o.created_at,
            COUNT(DISTINCT u.id) as member_count,
            COALESCE(SUM(um.request_count), 0) as total_usage,
            COALESCE((SELECT requests_limit FROM plans WHERE id = o.plan_id), 0) as requests_limit`).
		Joins("LEFT JOIN users u ON o.id::text = u.org_id::text").
		Joins("LEFT JOIN usage_metrics um ON o.id::text = um.org_id::text").
		Group("o.id, o.name, o.billing_email, o.plan_id, o.status, o.created_at")
	sortBy := c.Query("sort_by")
	order := c.Query("order")
	q := c.Query("q")

	// 应用过滤条件
	if req.Search != "" {
		searchPattern := fmt.Sprintf("%%%s%%", req.Search)
		query = query.Where("o.name LIKE ? OR o.billing_email LIKE ?", searchPattern, searchPattern)
	}
	if q != "" {
		qp := fmt.Sprintf("%%%s%%", q)
		query = query.Where("o.name LIKE ?", qp)
	}

	if req.Status != "" {
		query = query.Where("o.status = ?", req.Status)
	}

	// 计算总数
	var total int64
	countQuery := h.service.DB.Table("organizations o")
	if req.Search != "" {
		searchPattern := fmt.Sprintf("%%%s%%", req.Search)
		countQuery = countQuery.Where("o.name LIKE ? OR o.billing_email LIKE ?", searchPattern, searchPattern)
	}
	if req.Status != "" {
		countQuery = countQuery.Where("o.status = ?", req.Status)
	}

	if err := countQuery.Count(&total).Error; err != nil {
		logger.GetLogger().WithError(err).Error("查询组织总数失败")
		middleware.RecordBusinessOperation("admin_org_list", false, time.Since(start), "database_error")
		JSONError(c, CodeDatabaseError, "查询失败")
		return
	}

	// 查询组织列表 - 使用临时结构体接收查询结果
	type tempOrgResult struct {
		ID            string    `json:"id"`
		Name          string    `json:"name"`
		BillingEmail  string    `json:"billing_email"`
		PlanID        string    `json:"plan_id"`
		Status        string    `json:"status"`
		CreatedAt     time.Time `json:"created_at"`
		MemberCount   int       `json:"member_count"`
		TotalUsage    int       `json:"total_usage"`
		RequestsLimit int       `json:"requests_limit"`
	}

	orgSortCol := map[string]string{"created_at": "o.created_at", "name": "o.name", "total_usage": "total_usage"}[sortBy]
	if orgSortCol == "" {
		orgSortCol = "o.created_at"
	}
	orgSortOrder := "ASC"
	if order == "desc" {
		orgSortOrder = "DESC"
	}
	var tempResults []tempOrgResult
	if err := query.Order(orgSortCol + " " + orgSortOrder).Offset(offset).Limit(req.Limit).Find(&tempResults).Error; err != nil {
		logger.GetLogger().WithError(err).Error("查询组织列表失败")
		middleware.RecordBusinessOperation("admin_org_list", false, time.Since(start), "database_error")
		JSONError(c, CodeDatabaseError, "查询失败")
		return
	}

	// 转换为响应结构体
	organizations := make([]AdminOrganizationResponse, len(tempResults))
	planNames := map[string]string{
		"starter": "Starter Plan",
		"growth":  "Growth Plan",
		"scale":   "Scale Plan",
	}

	for i, result := range tempResults {
		organizations[i] = AdminOrganizationResponse{
			ID:           result.ID,
			Name:         result.Name,
			BillingEmail: result.BillingEmail,
			Plan: PlanInfo{
				ID:   result.PlanID,
				Name: result.PlanID,
			},
			MemberCount: result.MemberCount,
			TotalUsage:  result.TotalUsage,
			CreatedAt:   result.CreatedAt,
			Status:      result.Status,
		}
		limit := result.RequestsLimit
		percent := 0.0
		if limit > 0 {
			percent = float64(result.TotalUsage) / float64(limit) * 100
		}
		organizations[i].UsageSummary.TotalRequests = result.TotalUsage
		organizations[i].UsageSummary.Limit = limit
		organizations[i].UsageSummary.PercentUsed = percent
		organizations[i].UsageSummary.Period = time.Now().Format("2006-01")

		// 设置套餐名称
		if name, ok := planNames[result.PlanID]; ok {
			organizations[i].Plan.Name = name
		}
	}

	// 记录审计日志
	h.recordAuditLog(c, c.GetString("userID"), "admin_org_list", "success",
		fmt.Sprintf("Admin viewed organization list: page %d, limit %d", req.Page, req.Limit))

	// 记录业务操作成功
	middleware.RecordBusinessOperation("admin_org_list", true, time.Since(start), "")

	// 使用现有的分页响应函数
	JSONPaginated(c, organizations, req.Page, req.Limit, int(total))
}

func (h *AdminHandler) GetPlans(c *gin.Context) {
	var rows []AdminPlan
	err := h.service.DB.Raw(`SELECT id, name, COALESCE(price,0) AS price, COALESCE(currency,'USD') AS currency, COALESCE(requests_limit,0) AS requests_limit, COALESCE(features,'[]') AS features, COALESCE(quota_config,'{}') AS quota_config, COALESCE(is_active, true) AS is_active, updated_at FROM plans`).Scan(&rows).Error
	if err != nil {
		JSONError(c, CodeDatabaseError, "查询失败")
		return
	}
	JSONSuccess(c, rows)
}

func (h *AdminHandler) UpdatePlan(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		JSONError(c, CodeInvalidParameter, "缺少计划ID")
		return
	}
	var req PlanUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		JSONError(c, CodeInvalidParameter, "参数错误")
		return
	}
	updates := map[string]interface{}{}
	if req.Price != nil {
		updates["price"] = *req.Price
	}
	if req.Currency != nil {
		updates["currency"] = *req.Currency
	}
	if req.RequestsLimit != nil {
		updates["requests_limit"] = *req.RequestsLimit
	}
	if req.Features != nil {
		updates["features"] = *req.Features
	}
	if req.QuotaConfig != nil {
		updates["quota_config"] = *req.QuotaConfig
	}
	if req.IsActive != nil {
		updates["is_active"] = *req.IsActive
	}
	if len(updates) == 0 {
		JSONError(c, CodeInvalidParameter, "无更新内容")
		return
	}
	if err := h.service.DB.Table("plans").Where("id = ?", id).Updates(updates).Error; err != nil {
		JSONError(c, CodeDatabaseError, "更新失败")
		return
	}
	// 审计日志
	h.recordAuditLog(c, c.GetString("userID"), "admin_update_plan", "success", fmt.Sprintf("Update plan %s: %+v", id, updates))
	JSONSuccess(c, gin.H{"updated": id})
}

func (h *AdminHandler) GetOrganizationQuotas(c *gin.Context) {
	orgID := c.Param("id")
	if orgID == "" {
		JSONError(c, CodeInvalidParameter, "缺少组织ID")
		return
	}
	type item struct {
		ServiceType string
		Allocation  int
		Consumed    int
		ResetAt     *time.Time
	}
	var qs []item
	if err := h.service.DB.Raw("SELECT service_type, allocation, consumed, reset_at FROM organization_quotas WHERE organization_id = ?", orgID).Scan(&qs).Error; err != nil {
		JSONError(c, CodeDatabaseError, "查询失败")
		return
	}
	type view struct {
		Limit     int     `json:"limit"`
		Used      int     `json:"used"`
		Remaining int     `json:"remaining"`
		ResetAt   *string `json:"resetAt"`
	}
	resp := map[string]view{}
	for _, q := range qs {
		var ra *string
		if q.ResetAt != nil {
			s := q.ResetAt.UTC().Format("2006-01-02T15:04:00Z")
			ra = &s
		}
		resp[q.ServiceType] = view{Limit: q.Allocation, Used: q.Consumed, Remaining: q.Allocation - q.Consumed, ResetAt: ra}
	}
	JSONSuccess(c, resp)
}

func (h *AdminHandler) AdjustOrganizationQuota(c *gin.Context) {
	orgID := c.Param("id")
	if orgID == "" {
		JSONError(c, CodeInvalidParameter, "缺少组织ID")
		return
	}
	var req QuotaAdjustRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		JSONError(c, CodeInvalidParameter, "参数错误")
		return
	}
	// 原子调整，避免负数
	type rid struct{ ID string }
	var r rid
	err := h.service.DB.Raw(`UPDATE organization_quotas SET allocation = CASE WHEN allocation + ? < 0 THEN 0 ELSE allocation + ? END, updated_at = NOW() WHERE organization_id = ? AND service_type = ? RETURNING id`, req.Adjustment, req.Adjustment, orgID, req.ServiceType).Scan(&r).Error
	if err != nil {
		JSONError(c, CodeDatabaseError, "更新失败")
		return
	}
	if r.ID == "" {
		JSONError(c, CodeNotFound, "记录不存在")
		return
	}
	// 审计日志
	details := map[string]interface{}{"org_id": orgID, "service_type": req.ServiceType, "adjustment": req.Adjustment, "reason": req.Reason}
	b, _ := json.Marshal(details)
	al := &models.AuditLog{UserID: c.GetString("userID"), OrgID: orgID, Action: "quota.adjust", Resource: "organization_quotas", Details: string(b), IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), Status: "success", Message: "Admin adjusted quota"}
	_ = h.service.DB.Create(al).Error
	JSONSuccess(c, gin.H{"adjusted": r.ID})
}

func (h *AdminHandler) UpdateOrganizationPlan(c *gin.Context) {
	orgID := c.Param("id")
	if orgID == "" {
		JSONError(c, CodeInvalidParameter, "缺少组织ID")
		return
	}
	var req UpdateOrgPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		JSONError(c, CodeInvalidParameter, "参数验证失败")
		return
	}
	imm := true
	if req.Immediate != nil {
		imm = *req.Immediate
	}
	reset := false
	if req.ResetUsage != nil {
		reset = *req.ResetUsage
	}
	if err := h.service.DB.Model(&models.Organization{}).Where("id = ?", orgID).Update("plan_id", req.PlanID).Error; err != nil {
		JSONError(c, CodeDatabaseError, "更新失败")
		return
	}
	if imm {
		_ = h.service.SyncOrganizationQuotasWithPolicy(orgID, req.PlanID, reset)
	}
	type item struct {
		ServiceType string
		Allocation  int
		Consumed    int
	}
	var qs []item
	_ = h.service.DB.Raw("SELECT service_type, allocation, consumed FROM organization_quotas WHERE organization_id = ?", orgID).Scan(&qs).Error
	resp := map[string]map[string]int{}
	for _, q := range qs {
		resp[q.ServiceType] = map[string]int{"limit": q.Allocation, "used": q.Consumed, "remaining": q.Allocation - q.Consumed}
	}
	details := map[string]interface{}{"org_id": orgID, "plan_id": req.PlanID, "immediate": imm, "reset_usage": reset}
	b, _ := json.Marshal(details)
	al := &models.AuditLog{UserID: c.GetString("userID"), OrgID: orgID, Action: "admin.update_org_plan", Resource: "organizations", Details: string(b), IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), Status: "success", Message: "Update organization plan"}
	_ = h.service.DB.Create(al).Error
	JSONSuccess(c, gin.H{"org_id": orgID, "new_plan": req.PlanID, "quotas": resp})
}

func (h *AdminHandler) UpdatePlanQuota(c *gin.Context) { // ignore_security_alert
	planID := c.Param("plan_id")
	var req UpdatePlanQuotaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		JSONError(c, CodeInvalidParameter, "参数验证失败")
		return
	}
	cfg := map[string]map[string]interface{}{
		"ocr":  {"limit": req.OCRLimit, "period": req.OCRPeriod},
		"face": {"limit": req.FaceLimit, "period": req.FacePeriod},
	}
	b, _ := json.Marshal(cfg)
	if err := h.service.DB.Exec("UPDATE plans SET quota_config = ?, updated_at = NOW() WHERE id = ?", string(b), planID).Error; err != nil {
		JSONError(c, CodeDatabaseError, "更新失败")
		return
	}
	JSONSuccess(c, gin.H{"updated": planID})
}
