package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"kyc-service/pkg/logger"
	"kyc-service/pkg/metrics"
	"kyc-service/pkg/utils"

	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
)

func (s *KYCService) checkAndConsumeQuota(ctx context.Context, orgID, serviceType string, run func() error) error {
	if s.Redis != nil {
		ok, rerr := s.consumeQuotaRedis(ctx, orgID, serviceType)
		if rerr == nil && ok {
			metrics.IncOrgQuotaUsed(ctx, orgID, serviceType, 1)
			if err := run(); err != nil {
				_ = s.Redis.Decr(ctx, quotaConsumedKey(orgID, serviceType)).Err()
				metrics.IncOrgQuotaUsed(ctx, orgID, serviceType, -1)
				return err
			}
			go func() {
				type rid struct{ ID string }
				var r rid
				var perr error
				for i := 0; i < 3; i++ {
					perr = s.DB.Raw("UPDATE organization_quotas SET consumed = consumed + 1 WHERE organization_id = ? AND service_type = ? AND consumed < allocation RETURNING id", orgID, serviceType).Scan(&r).Error
					if perr == nil {
						break
					}
					time.Sleep(time.Duration(i+1) * 200 * time.Millisecond)
				}
				if perr != nil || r.ID == "" {
					logger.GetLogger().WithError(perr).Error("quota db persist failed, reverting redis")
					_ = s.Redis.Decr(context.Background(), quotaConsumedKey(orgID, serviceType)).Err()
					metrics.IncOrgQuotaUsed(context.Background(), orgID, serviceType, -1)
					metrics.RecordQuotaPersistFailure(context.Background(), orgID, serviceType, "db_update_failed")
				}
			}()
			return nil
		}
		if rerr != nil && rerr != redis.Nil {
			logger.GetLogger().WithError(rerr).Warn("quota redis path error, fallback to db")
		}
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		type rid struct{ ID string }
		var r rid
		if err := tx.Raw("UPDATE organization_quotas SET consumed = consumed + 1 WHERE organization_id = ? AND service_type = ? AND consumed < allocation RETURNING id", orgID, serviceType).Scan(&r).Error; err != nil {
			return err
		}
		if r.ID == "" {
			return fmt.Errorf("QUOTA_EXCEEDED")
		}
		metrics.IncOrgQuotaUsed(ctx, orgID, serviceType, 1)
		if err := run(); err != nil {
			_ = tx.Exec("UPDATE organization_quotas SET consumed = consumed - 1 WHERE id = ?", r.ID).Error
			metrics.IncOrgQuotaUsed(ctx, orgID, serviceType, -1)
			return err
		}
		return nil
	})
}

func quotaLimitKey(orgID, serviceType string) string {
	return "quota:limit:" + orgID + ":" + serviceType
}
func quotaConsumedKey(orgID, serviceType string) string {
	return "quota:consumed:" + orgID + ":" + serviceType
}

func (s *KYCService) consumeQuotaRedis(ctx context.Context, orgID, serviceType string) (bool, error) {
	// initialize cache if missing
	limitKey := quotaLimitKey(orgID, serviceType)
	consumedKey := quotaConsumedKey(orgID, serviceType)
	limitStr, err := s.Redis.Get(ctx, limitKey).Result()
	if err == redis.Nil || limitStr == "" {
		type row struct {
			Allocation int
			Consumed   int
		}
		var q row
		if err := s.DB.Raw("SELECT allocation, consumed FROM organization_quotas WHERE organization_id = ? AND service_type = ?", orgID, serviceType).Scan(&q).Error; err != nil {
			return false, err
		}
		_ = s.Redis.Set(ctx, limitKey, fmt.Sprintf("%d", q.Allocation), 30*time.Second).Err()
		_ = s.Redis.Set(ctx, consumedKey, fmt.Sprintf("%d", q.Consumed), 30*time.Second).Err()
	}
	// atomic check and increment
	lua := redis.NewScript(`
    local limit = tonumber(redis.call('GET', KEYS[1]))
    if not limit then return 0 end
    local consumed = tonumber(redis.call('GET', KEYS[2])) or 0
    if consumed + 1 > limit then return -1 end
    redis.call('INCR', KEYS[2])
    return 1
    `)
	r, err := lua.Run(ctx, s.Redis, []string{limitKey, consumedKey}).Int()
	if err != nil {
		return false, err
	}
	if r == 1 {
		return true, nil
	}
	if r == -1 {
		return false, fmt.Errorf("QUOTA_EXCEEDED")
	}
	return false, fmt.Errorf("QUOTA_UNAVAILABLE")
}

func (s *KYCService) SyncOrganizationQuotas(orgID string, planID string) error {
	return s.SyncOrganizationQuotasWithPolicy(orgID, planID, false)
}

func (s *KYCService) SyncOrganizationQuotasWithPolicy(orgID string, planID string, resetUsage bool) error {
	var raw string
	if err := s.DB.Raw("SELECT quota_config::text FROM plans WHERE id = ?", planID).Scan(&raw).Error; err != nil {
		return err
	}
	if raw == "" {
		return nil
	}
	var m map[string]map[string]interface{}
	_ = json.Unmarshal([]byte(raw), &m)
	for svc, v := range m {
		alloc := 0
		if l, ok := v["limit"].(float64); ok {
			alloc = int(l)
		}
		var reset interface{}
		if p, ok := v["period"].(string); ok && p == "monthly" {
			nm := time.Date(time.Now().Year(), time.Now().Month()+1, 1, 0, 0, 0, 0, time.Now().Location())
			reset = nm
		} else {
			reset = nil
		}
		if resetUsage {
			_ = s.DB.Exec("INSERT INTO organization_quotas(id, organization_id, service_type, allocation, consumed, reset_at, updated_at) VALUES(?, ?, ?, ?, 0, ?, NOW()) ON CONFLICT (organization_id, service_type) DO UPDATE SET allocation = EXCLUDED.allocation, consumed = 0, reset_at = EXCLUDED.reset_at, updated_at = NOW()", utils.GenerateID(), orgID, svc, alloc, reset).Error
		} else {
			_ = s.DB.Exec("INSERT INTO organization_quotas(id, organization_id, service_type, allocation, consumed, reset_at, updated_at) VALUES(?, ?, ?, ?, 0, ?, NOW()) ON CONFLICT (organization_id, service_type) DO UPDATE SET allocation = EXCLUDED.allocation, consumed = LEAST(organization_quotas.consumed, EXCLUDED.allocation), reset_at = EXCLUDED.reset_at, updated_at = NOW()", utils.GenerateID(), orgID, svc, alloc, reset).Error
		}
		metrics.SetOrgQuotaLimit(context.Background(), orgID, svc, alloc)
	}
	return nil
}
