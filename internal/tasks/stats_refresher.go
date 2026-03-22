package tasks

import (
	"fmt"
	"kyc-service/internal/models"
	"kyc-service/internal/service"
	"time"
)

func StartStatsRefresher(svc *service.KYCService, interval time.Duration) {
	go func() {
		for {
			now := time.Now()
			since := now.Add(-24 * time.Hour)
			var orgRows []struct {
				OrgID string
				Total int64
			}
			svc.DB.Raw(`SELECT org_id AS org_id, COUNT(*) AS total FROM usage_logs WHERE created_at >= ? GROUP BY org_id`, since).Scan(&orgRows)
			for _, o := range orgRows {
				var org models.Organization
				if err := svc.DB.First(&org, "id = ?", o.OrgID).Error; err == nil {
					limit := 0
					switch org.PlanID {
					case "starter":
						limit = 1000
					case "growth":
						limit = 50000
					case "scale":
						limit = 1000000
					}
					percent := float64(0)
					if limit > 0 {
						percent = float64(o.Total) / float64(limit) * 100
					}
					org.UsageSummary = []byte(fmt.Sprintf(`{"total_requests": %d, "limit": %d, "percent_used": %.2f}`, o.Total, limit, percent))
					_ = svc.DB.Save(&org).Error
				}
				// 更新 usage_metrics 聚合表
				_ = svc.DB.Exec("INSERT INTO usage_metrics (org_id, request_count, updated_at) VALUES (?, ?, NOW()) ON CONFLICT (org_id) DO UPDATE SET request_count = EXCLUDED.request_count, updated_at = NOW()", o.OrgID, o.Total).Error
			}
			time.Sleep(interval)
		}
	}()
}
