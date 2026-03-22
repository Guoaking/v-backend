package api

import (
	"fmt"

	"kyc-service/internal/models"

	"github.com/gin-gonic/gin"
)

func (h *AdminHandler) GetOverviewStats(c *gin.Context) {
	var totalUsers int64
	_ = h.service.DB.Model(&models.User{}).Count(&totalUsers).Error
	var activeClients int64
	_ = h.service.DB.Model(&models.OAuthClient{}).Where("status = ?", "active").Count(&activeClients).Error
	var todayRequests int64
	_ = h.service.DB.Model(&models.UsageLog{}).Where("DATE(created_at) = CURRENT_DATE").Count(&todayRequests).Error
	JSONSuccess(c, AdminOverviewStats{TotalUsers: totalUsers, ActiveClients: activeClients, TodayRequests: todayRequests})
}

func (h *AdminHandler) UpdateGlobalConfig(c *gin.Context) {
	var req UpdateGlobalConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		JSONError(c, CodeInvalidParameter, "参数验证失败")
		return
	}
	if err := h.service.DB.Exec("INSERT INTO global_configs(key,value,updated_at) VALUES('daily_registration_cap', ?, NOW()) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at", fmt.Sprintf("%d", req.DailyRegistrationCap)).Error; err != nil {
		JSONError(c, CodeDatabaseError, "更新失败")
		return
	}
	JSONSuccess(c, gin.H{"daily_registration_cap": req.DailyRegistrationCap})
}
