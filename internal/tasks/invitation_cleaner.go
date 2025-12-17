package tasks

import (
	"kyc-service/internal/service"
	"time"
)

func StartInvitationCleaner(s *service.KYCService, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			_ = s.DB.Exec("UPDATE invitations SET status = 'expired' WHERE status = 'pending' AND expires_at < now()").Error
		}
	}()
}
