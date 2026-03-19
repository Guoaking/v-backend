package api

import (
	"fmt"

	"kyc-service/internal/models"
	"kyc-service/pkg/utils"

	"github.com/gin-gonic/gin"
)

func (h *ConsoleHandler) recordAuditLog(log *models.AuditLog) error {
	h.service.LogWorker.RecordAuditLog(log)
	return nil
}

func (h *ConsoleHandler) GetLogs(c *gin.Context) {
	page := 1
	limit := 20
	if v := c.Query("page"); v != "" {
		fmt.Sscanf(v, "%d", &page)
	}
	if v := c.Query("limit"); v != "" {
		fmt.Sscanf(v, "%d", &limit)
	}
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit

	userID := c.GetString("userID")
	role := c.GetString("orgRole")
	orgID := c.GetString("orgID")
	var logs []models.UsageLog
	q := h.service.DB.Where("org_id = ?", orgID).Order("created_at DESC").Offset(offset).Limit(limit)
	// key_id 过滤与权限校验
	if kid := c.Query("key_id"); kid != "" {
		var key models.APIKey
		if err := h.service.DB.First(&key, "id = ?", kid).Error; err != nil {
			JSONError(c, CodeNotFound, "Key不存在")
			return
		}
		if role != "owner" && role != "admin" && key.CreatedByUserID != userID {
			JSONError(c, CodeForbidden, "无权查看该Key日志")
			return
		}
		q = q.Where("api_key_id = ?", kid)
	}
	// TODO: UsageLog 目前没有 api_key_owner_id，暂不限制普通用户查看自己Key的逻辑
	// else if role != "owner" && role != "admin" {
	// 	// 非管理员仅查看自己Key的日志
	// 	q = q.Where("api_key_owner_id = ?", userID)
	// }
	if status := c.Query("status"); status != "" {
		if status == "success" {
			q = q.Where("status_code < 400")
		} else if status == "failed" {
			q = q.Where("status_code >= 400")
		}
	}
	if sc := c.Query("status_code"); sc != "" {
		if sc == "2xx" {
			q = q.Where("status_code BETWEEN 200 AND 299")
		} else if sc == "4xx" {
			q = q.Where("status_code BETWEEN 400 AND 499")
		} else if sc == "5xx" {
			q = q.Where("status_code BETWEEN 500 AND 599")
		} else {
			q = q.Where("status_code = ?", sc)
		}
	}
	if p := c.Query("path"); p != "" {
		q = q.Where("endpoint LIKE ?", "%"+p+"%")
	}
	if m := c.Query("method"); m != "" {
		// q = q.Where("method = ?", strings.ToUpper(m)) // UsageLog 不支持
	}
	if sd := c.Query("start_date"); sd != "" {
		q = q.Where("created_at >= ?::date", sd)
	}
	if ed := c.Query("end_date"); ed != "" {
		q = q.Where("created_at < ?::date + interval '1 day'", ed)
	}
	if d := c.Query("date"); d != "" {
		q = q.Where("DATE(created_at) = ?", d)
	}
	if err := q.Find(&logs).Error; err != nil {
		JSONError(c, CodeDatabaseError, "查询失败")
		return
	}
	var total int64
	cq := h.service.DB.Model(&models.UsageLog{}).Where("org_id = ?", orgID)
	if kid := c.Query("key_id"); kid != "" {
		cq = cq.Where("api_key_id = ?", kid)
	}
	// TODO: UsageLog 目前没有 api_key_owner_id
	// else if role != "owner" && role != "admin" {
	// 	cq = cq.Where("api_key_owner_id = ?", userID)
	// }
	if status := c.Query("status"); status != "" {
		if status == "success" {
			cq = cq.Where("status_code < 400")
		} else if status == "failed" {
			cq = cq.Where("status_code >= 400")
		}
	}
	if sc := c.Query("status_code"); sc != "" {
		if sc == "2xx" {
			cq = cq.Where("status_code BETWEEN 200 AND 299")
		} else if sc == "4xx" {
			cq = cq.Where("status_code BETWEEN 400 AND 499")
		} else if sc == "5xx" {
			cq = cq.Where("status_code BETWEEN 500 AND 599")
		} else {
			cq = cq.Where("status_code = ?", sc)
		}
	}
	if p := c.Query("path"); p != "" {
		cq = cq.Where("endpoint LIKE ?", "%"+p+"%")
	}
	if m := c.Query("method"); m != "" {
		// cq = cq.Where("method = ?", strings.ToUpper(m)) // UsageLog 不支持
	}
	if sd := c.Query("start_date"); sd != "" {
		cq = cq.Where("created_at >= ?::date", sd)
	}
	if ed := c.Query("end_date"); ed != "" {
		cq = cq.Where("created_at < ?::date + interval '1 day'", ed)
	}
	if d := c.Query("date"); d != "" {
		cq = cq.Where("DATE(created_at) = ?", d)
	}
	_ = cq.Count(&total).Error
	items := make([]LogItem, len(logs))
	for i, lg := range logs {
		items[i] = LogItem{
			ID:           lg.ID,
			Method:       "N/A", // UsageLog 中暂未记录 Method
			Path:         lg.Endpoint,
			StatusCode:   lg.StatusCode,
			LatencyMs:    0, // UsageLog 中暂未记录 Latency
			ClientIP:     "N/A", // UsageLog 中暂未记录 ClientIP
			CreatedAt:    utils.FormatTime(lg.CreatedAt),
			TimeStamp:    utils.FormatTimeUnix(lg.CreatedAt),
			RequestBody:  "{}", // 计费日志不存 Body
			ResponseBody: "{}", // 计费日志不存 Body
			KeyID:        lg.APIKeyID,
			KeyName:      "N/A", // 需要关联查询 APIKey 表
			KeyOwnerID:   lg.UserID, // UserID 在这里相当于 OwnerID 的语义
		}
	}
	JSONSuccess(c, gin.H{"page": page, "limit": limit, "total": total, "items": items})
}
