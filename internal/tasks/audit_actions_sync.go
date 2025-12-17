package tasks

import (
	"kyc-service/internal/service"
	"time"
)

func StartAuditActionsSync(s *service.KYCService, interval time.Duration) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for range t.C {
			_ = s.DB.Exec("INSERT INTO audit_actions(id,name,category) SELECT DISTINCT action, action, 'General' FROM audit_logs ON CONFLICT (id) DO NOTHING").Error
		}
	}()
}
