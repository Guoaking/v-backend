package tasks

import (
	"gorm.io/gorm"
	"kyc-service/pkg/logger"
	"time"
)

// StartQuotaResetter 周期重置组织配额（基于 reset_at）
func StartQuotaResetter(db *gorm.DB, interval time.Duration) {
	go func() {
		log := logger.GetLogger()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			<-ticker.C
			// 将已到期的记录重置，并设置下一期时间（仅适用于存在 reset_at 的记录）
			// 假设为月度周期：下期设为下个月1号0点
			nextMonth := time.Date(time.Now().Year(), time.Now().Month()+1, 1, 0, 0, 0, 0, time.Now().Location())
			if err := db.Exec(`UPDATE organization_quotas SET consumed = 0, updated_at = NOW(), reset_at = CASE WHEN reset_at IS NOT NULL THEN ? ELSE reset_at END WHERE reset_at IS NOT NULL AND reset_at <= NOW()`, nextMonth).Error; err != nil {
				log.WithError(err).Warn("配额重置失败")
			} else {
				log.Info("✅ 已执行周期性配额重置")
			}
		}
	}()
}
