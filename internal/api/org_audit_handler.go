package api

import (
	"fmt"
	"strings"
	"time"

	"kyc-service/internal/service"

	"github.com/gin-gonic/gin"
)

type OrgAuditHandler struct {
	service *service.KYCService
}

func NewOrgAuditHandler(svc *service.KYCService) *OrgAuditHandler {
	return &OrgAuditHandler{service: svc}
}

// GetOrgAuditLogs 获取组织审计日志
func (h *OrgAuditHandler) GetOrgAuditLogs(c *gin.Context) {
	orgID := c.GetString("orgID")
	if orgID == "" {
		JSONError(c, CodeInvalidParameter, "组织信息错误")
		return
	}
	role := c.GetString("orgRole")
	if role != "owner" && role != "admin" {
		JSONError(c, CodeForbidden, "权限不足")
		return
	}
	page := 1
	pageSize := 50
	if v := c.Query("page"); v != "" {
		fmt.Sscanf(v, "%d", &page)
	}
	if v := c.Query("page_size"); v != "" {
		fmt.Sscanf(v, "%d", &pageSize)
	} else if v := c.Query("limit"); v != "" {
		fmt.Sscanf(v, "%d", &pageSize)
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}
	offset := (page - 1) * pageSize
	action := c.Query("action")
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")
	userID := c.Query("user_id")
	ip := c.Query("ip")
	logID := c.Query("log_id")

	type Row struct {
		ID        uint      `json:"id"`
		OrgID     string    `json:"org_id"`
		UserID    string    `json:"user_id"`
		UserName  string    `json:"user_name"`
		Action    string    `json:"action"`
		Message   string    `json:"message"`
		Resource  string    `json:"resource"`
		IP        string    `json:"ip"`
		Status    string    `json:"status"`
		CreatedAt time.Time `json:"created_at"`
	}
	qb := h.service.DB.Table("audit_logs al").Select("al.id, al.org_id, al.user_id, COALESCE(u.full_name,u.name) as user_name, al.action, al.message, al.resource, al.ip, al.status, al.created_at").Joins("LEFT JOIN users u ON u.id = al.user_id").Where("al.org_id = ?", orgID)
	if action != "" && action != "all" {
		qb = qb.Where("al.action = ?", action)
	}
	if startDate != "" {
		qb = qb.Where("al.created_at >= ?::date", startDate)
	}
	if endDate != "" {
		qb = qb.Where("al.created_at < ?::date + interval '1 day'", endDate)
	}
	if userID != "" {
		qb = qb.Where("al.user_id = ?", userID)
	}
	if ip != "" {
		qb = qb.Where("al.ip LIKE ?", "%"+ip+"%")
	}
	if logID != "" {
		qb = qb.Where("al.id = ?", logID)
	}
	var total int64
	if err := qb.Count(&total).Error; err != nil {
		JSONError(c, CodeDatabaseError, "统计失败")
		return
	}
	var rows []Row
	if err := qb.Order("al.created_at DESC").Offset(offset).Limit(pageSize).Scan(&rows).Error; err != nil {
		JSONError(c, CodeDatabaseError, "查询失败")
		return
	}

	totalPage := (int(total) + pageSize - 1) / pageSize
	JSONSuccess(c, gin.H{"items": rows, "pagination": Pagination{
		Page:      page,
		PageSize:  pageSize,
		Total:     int(total),
		TotalPage: totalPage,
	}})

}

func (h *OrgAuditHandler) ExportOrgAuditLogs(c *gin.Context) {
	orgID := c.GetString("orgID")
	if orgID == "" {
		JSONError(c, CodeInvalidParameter, "组织信息错误")
		return
	}
	_ = c.DefaultQuery("format", "csv")
	action := c.Query("action")
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")
	userID := c.Query("user_id")
	qb := h.service.DB.Table("audit_logs al").Select("al.id, al.org_id, al.user_id, COALESCE(u.full_name,u.name) as user_name, al.action, al.message, al.resource, al.ip, al.status, al.created_at").Joins("LEFT JOIN users u ON u.id = al.user_id").Where("al.org_id = ?", orgID)
	if action != "" {
		qb = qb.Where("al.action = ?", action)
	}
	if startDate != "" {
		qb = qb.Where("al.created_at >= ?::date", startDate)
	}
	if endDate != "" {
		qb = qb.Where("al.created_at < ?::date + interval '1 day'", endDate)
	}
	if userID != "" {
		qb = qb.Where("al.user_id = ?", userID)
	}
	type Row struct {
		ID                                                             uint
		OrgID, UserID, UserName, Action, Message, Resource, IP, Status string
		CreatedAt                                                      time.Time
	}
	var rows []Row
	if err := qb.Order("al.created_at DESC").Scan(&rows).Error; err != nil {
		JSONError(c, CodeDatabaseError, "查询失败")
		return
	}
	filename := "audit_logs.csv"
	c.Header("Content-Disposition", "attachment; filename="+filename)
	c.Header("Content-Type", "text/csv; charset=utf-8")
	w := c.Writer
	_, _ = w.Write([]byte("id,org_id,user_id,user_name,action,message,resource,ip,status,created_at\n"))
	for _, r := range rows {
		ts := r.CreatedAt.Format("2006-01-02T15:04:05Z07:00")
		esc := func(s string) string {
			if strings.ContainsAny(s, ",\n\r\"") {
				return "\"" + strings.ReplaceAll(s, "\"", "\"\"") + "\""
			}
			return s
		}
		line := fmt.Sprintf("%d,%s,%s,%s,%s,%s,%s,%s,%s,%s\n", r.ID, esc(r.OrgID), esc(r.UserID), esc(r.UserName), esc(r.Action), esc(r.Message), esc(r.Resource), esc(r.IP), esc(r.Status), ts)
		_, _ = w.Write([]byte(line))
	}
}

func (h *OrgAuditHandler) GetAuditActions(c *gin.Context) {
	orgID := c.GetString("orgID")
	if orgID == "" {
		JSONError(c, CodeInvalidParameter, "组织信息错误")
		return
	}
	var actions []string
	var rows []struct{ ID string }
	if err := h.service.DB.Table("audit_actions").Select("id").Order("id ASC").Scan(&rows).Error; err == nil {
		for _, r := range rows {
			actions = append(actions, r.ID)
		}
	}
	if len(actions) == 0 {
		var al []struct{ Action string }
		_ = h.service.DB.Table("audit_logs").Select("DISTINCT action").Where("org_id = ?", orgID).Order("action").Scan(&al).Error
		for _, a := range al {
			actions = append(actions, a.Action)
		}
	}
	JSONSuccess(c, actions)
}
