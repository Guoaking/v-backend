package api

import (
	"fmt"

	"kyc-service/internal/models"

	"github.com/gin-gonic/gin"
)

func (h *ConsoleHandler) GetNotifications(c *gin.Context) {
	userID := c.GetString("userID")
	unreadOnly := c.Query("unread_only") == "true"
	// 分页参数
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

	var notifs []models.Notification
	qb := h.service.DB.Where("user_id = ?", userID)
	if unreadOnly {
		qb = qb.Where("is_read = ?", false)
	}
	if err := qb.Order("created_at DESC").Offset(offset).Limit(limit).Find(&notifs).Error; err != nil {
		JSONError(c, CodeDatabaseError, "查询失败")
		return
	}
	JSONSuccess(c, gin.H{"items": notifs, "page": page, "limit": limit})
}

func (h *ConsoleHandler) MarkNotificationRead(c *gin.Context) {
	userID := c.GetString("userID")
	id := c.Param("id")
	if id == "" {
		JSONError(c, CodeInvalidParameter, "ID不能为空")
		return
	}
	if err := h.service.DB.Model(&models.Notification{}).Where("id = ? AND user_id = ?", id, userID).Update("is_read", true).Error; err != nil {
		JSONError(c, CodeDatabaseError, "更新失败")
		return
	}
	JSONSuccess(c, gin.H{"read": id})
}
