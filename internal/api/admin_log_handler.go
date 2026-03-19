package api

import (
	"fmt"
	"time"

	"kyc-service/internal/middleware"
	"kyc-service/internal/models"
	"kyc-service/pkg/logger"

	"github.com/gin-gonic/gin"
)

type AdminAuditLogResponse struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	UserName  string    `json:"user_name"`
	Action    string    `json:"action"`
	Target    string    `json:"target"`
	IP        string    `json:"ip"`
	Timestamp time.Time `json:"timestamp"`
	Status    string    `json:"status"`
}

func (h *AdminHandler) GetAuditLogs(c *gin.Context) {
	start := time.Now()

	// 权限检查 - 确保是超级管理员
	userRole, exists := c.Get("userRole")
	if !exists || userRole != "admin" {
		middleware.RecordBusinessOperation("admin_audit_logs", false, time.Since(start), "permission_denied")
		JSONError(c, CodeForbidden, "权限不足")
		return
	}

	var req AdminAuditLogRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		middleware.RecordBusinessOperation("admin_audit_logs", false, time.Since(start), "invalid_request")
		JSONError(c, CodeInvalidParameter, "参数验证失败")
		return
	}

	// 设置默认值并计算分页参数
	if req.Page < 1 {
		req.Page = 1
	}
	if req.Limit <= 0 || req.Limit > 1000 {
		req.Limit = 50 // 默认每页50条
	}

	// 如果提供了offset，优先使用offset；否则根据page和limit计算
	var offset int
	if req.Offset > 0 {
		offset = req.Offset
	} else {
		offset = (req.Page - 1) * req.Limit
	}

	// 构建查询
	query := h.service.DB.Table("audit_logs al").
		Select(`al.id, al.user_id, al.action, al.resource as target, al.ip, al.created_at as timestamp, al.status,
			COALESCE(u.full_name, u.name, 'System') as user_name`).
		Joins("LEFT JOIN users u ON u.id = al.user_id").
		Order("al.created_at DESC")

	// 应用筛选条件
	if req.UserID != "" {
		query = query.Where("al.user_id = ?", req.UserID)
	}

	if req.Action != "" {
		query = query.Where("al.action = ?", req.Action)
	}

	if req.Status != "" {
		query = query.Where("al.status = ?", req.Status)
	}

	// 日期范围筛选
	if req.DateFrom != "" || req.FromDate != "" {
		if fromDate, err := time.Parse("2006-01-02", req.DateFrom); err == nil {
			query = query.Where("al.created_at >= ?", fromDate)
		}
		if req.FromDate != "" {
			if fd, err := time.Parse("2006-01-02", req.FromDate); err == nil {
				query = query.Where("al.created_at >= ?", fd)
			}
		}
	}

	if req.DateTo != "" || req.ToDate != "" {
		if toDate, err := time.Parse("2006-01-02", req.DateTo); err == nil {
			// 包含指定日期的整天
			toDate = toDate.Add(24 * time.Hour)
			query = query.Where("al.created_at < ?", toDate)
		}
		if req.ToDate != "" {
			if td, err := time.Parse("2006-01-02", req.ToDate); err == nil {
				td = td.Add(24 * time.Hour)
				query = query.Where("al.created_at < ?", td)
			}
		}
	}

	// 计算总数 - 重新构建计数查询，避免ORDER BY导致的GROUP BY错误
	var total int64
	countQuery := h.service.DB.Table("audit_logs al").
		Joins("LEFT JOIN users u ON u.id = al.user_id")

	// 应用相同的筛选条件
	if req.UserID != "" {
		countQuery = countQuery.Where("al.user_id = ?", req.UserID)
	}
	if req.Action != "" {
		countQuery = countQuery.Where("al.action = ?", req.Action)
	}
	if req.Status != "" {
		countQuery = countQuery.Where("al.status = ?", req.Status)
	}
	if req.DateFrom != "" {
		if fromDate, err := time.Parse("2006-01-02", req.DateFrom); err == nil {
			countQuery = countQuery.Where("al.created_at >= ?", fromDate)
		}
	}
	if req.DateTo != "" {
		if toDate, err := time.Parse("2006-01-02", req.DateTo); err == nil {
			toDate = toDate.Add(24 * time.Hour)
			countQuery = countQuery.Where("al.created_at < ?", toDate)
		}
	}

	if err := countQuery.Count(&total).Error; err != nil {
		logger.GetLogger().WithError(err).Error("查询审计日志总数失败")
		middleware.RecordBusinessOperation("admin_audit_logs", false, time.Since(start), "database_error")
		JSONError(c, CodeDatabaseError, "查询失败")
		return
	}

	// 查询审计日志列表
	var logs []AdminAuditLogResponse
	if err := query.Offset(offset).Limit(req.Limit).Find(&logs).Error; err != nil {
		logger.GetLogger().WithError(err).Error("查询审计日志列表失败")
		middleware.RecordBusinessOperation("admin_audit_logs", false, time.Since(start), "database_error")
		JSONError(c, CodeDatabaseError, "查询失败")
		return
	}

	// 记录审计日志
	h.recordAuditLog(c, c.GetString("userID"), "admin_audit_logs", "success",
		fmt.Sprintf("Admin viewed audit logs: page %d, limit %d", req.Page, req.Limit))

	// 记录业务操作成功
	middleware.RecordBusinessOperation("admin_audit_logs", true, time.Since(start), "")

	// 审计日志保持原有的数据结构，但使用分页响应格式
	JSONSuccess(c, gin.H{
		"logs":   logs,
		"total":  total,
		"limit":  req.Limit,
		"offset": offset,
		"page":   req.Page,
	})
}

func (h *AdminHandler) recordAuditLog(c *gin.Context, userID, action, status, message string) {
	auditLog := &models.AuditLog{
		UserID:    userID,
		Action:    action,
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
		Status:    status,
		Message:   message,
	}
	h.service.LogWorker.RecordAuditLog(auditLog)
}
