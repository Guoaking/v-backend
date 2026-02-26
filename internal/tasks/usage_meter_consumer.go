package tasks

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"kyc-service/internal/models"
	"kyc-service/internal/service"

	"github.com/google/uuid"
)

func StartUsageMeterConsumer(svc *service.KYCService, batchSize int, flushInterval time.Duration) {
	go func() {
		ctx := context.Background()
		buf := make([]models.UsageLog, 0, batchSize)
		type ev struct {
			OrgID         string    `json:"org_id"`
			APIKeyID      string    `json:"api_key_id"`
			UserID        string    `json:"user_id"`
			APIKeyOwnerID string    `json:"api_key_owner_id"`
			ActorUserID   string    `json:"actor_user_id"`
			OAuthClientID string    `json:"oauth_client_id"`
			Endpoint      string    `json:"endpoint"`
			StatusCode    int       `json:"status_code"`
			RequestID     string    `json:"request_id"`
			CreatedAt     time.Time `json:"created_at"`
		}
		evbuf := make([]ev, 0, batchSize)
		lastFlush := time.Now()
		for {
			res, err := svc.Redis.BLPop(ctx, 5*time.Second, "usage:events").Result()
			if err == nil && len(res) == 2 {
				payload := res[1]
				var e ev
				_ = json.Unmarshal([]byte(payload), &e)
				var ocp *string
				if e.OAuthClientID != "" {
					ocp = &e.OAuthClientID
				}
				buf = append(buf, models.UsageLog{ID: uuid.NewString(), OrgID: e.OrgID, APIKeyID: e.APIKeyID, UserID: e.UserID, OAuthClientID: ocp, Endpoint: e.Endpoint, StatusCode: e.StatusCode, RequestID: e.RequestID, CreatedAt: e.CreatedAt})
				evbuf = append(evbuf, e)
			}
			needFlush := len(buf) >= batchSize || time.Since(lastFlush) >= flushInterval
			if needFlush && len(buf) > 0 {
				_ = svc.DB.Create(&buf).Error
				type agg struct {
					OrgID string
					Cnt   int
				}
				m := map[string]int{}
				for _, r := range buf {
					m[r.OrgID]++
				}
				for org, cnt := range m {
					_ = svc.DB.Exec("INSERT INTO usage_metrics(org_id, request_count, updated_at) VALUES(?, ?, NOW()) ON CONFLICT (org_id) DO UPDATE SET request_count = usage_metrics.request_count + EXCLUDED.request_count, updated_at = NOW()", org, cnt).Error
				}
				orgDay := map[string]struct {
					S int
					F int
				}{}
				userDay := map[string]struct {
					S int
					F int
				}{}
				svcDay := map[string]struct {
					S int
					F int
				}{}
				epDay := map[string]struct {
					S int
					F int
				}{}
				keyDay := map[string]struct {
					S int
					F int
				}{}
				keyUserDay := map[string]struct {
					S int
					F int
				}{}
				for i, r := range buf {
					e := evbuf[i]
					d := r.CreatedAt.Format("2006-01-02")
					k := r.OrgID + "|" + d
					actor := e.ActorUserID
					if actor != "" {
						ku := r.OrgID + "|" + actor + "|" + d
						if r.StatusCode >= 400 {
							vu := userDay[ku]
							vu.F++
							userDay[ku] = vu
						} else {
							vu := userDay[ku]
							vu.S++
							userDay[ku] = vu
						}
					}
					if r.StatusCode >= 400 {
						v := orgDay[k]
						v.F++
						orgDay[k] = v
					} else {
						v := orgDay[k]
						v.S++
						orgDay[k] = v
					}
					p := strings.ToLower(r.Endpoint)
					s := "other"
					if strings.Contains(p, "/kyc/ocr") {
						s = "ocr"
					} else if strings.Contains(p, "/kyc/face") {
						s = "face_verify"
					} else if strings.Contains(p, "/kyc/liveness") {
						s = "liveness"
					}
					ks := r.OrgID + "|" + s + "|" + d
					if r.StatusCode >= 400 {
						vs := svcDay[ks]
						vs.F++
						svcDay[ks] = vs
					} else {
						vs := svcDay[ks]
						vs.S++
						svcDay[ks] = vs
					}
					ke := r.OrgID + "|" + r.Endpoint + "|" + d
					if r.StatusCode >= 400 {
						ve := epDay[ke]
						ve.F++
						epDay[ke] = ve
					} else {
						ve := epDay[ke]
						ve.S++
						epDay[ke] = ve
					}
					if e.APIKeyID != "" {
						kk := r.OrgID + "|" + e.APIKeyID + "|" + d
						if r.StatusCode >= 400 {
							vk := keyDay[kk]
							vk.F++
							keyDay[kk] = vk
						} else {
							vk := keyDay[kk]
							vk.S++
							keyDay[kk] = vk
						}
						if e.APIKeyOwnerID != "" {
							kku := r.OrgID + "|" + e.APIKeyOwnerID + "|" + e.APIKeyID + "|" + d
							if r.StatusCode >= 400 {
								vku := keyUserDay[kku]
								vku.F++
								keyUserDay[kku] = vku
							} else {
								vku := keyUserDay[kku]
								vku.S++
								keyUserDay[kku] = vku
							}
						}
					}
				}
				for k, v := range orgDay {
					parts := strings.Split(k, "|")
					org := parts[0]
					day := parts[1]
					total := v.S + v.F
					_ = svc.DB.Exec("INSERT INTO usage_daily(org_id, date, success, failed, total, updated_at) VALUES(?, ?, ?, ?, ?, NOW()) ON CONFLICT (org_id, date) DO UPDATE SET success = usage_daily.success + EXCLUDED.success, failed = usage_daily.failed + EXCLUDED.failed, total = usage_daily.total + EXCLUDED.total, updated_at = NOW()", org, day, v.S, v.F, total).Error
				}
				for k, v := range userDay {
					parts := strings.Split(k, "|")
					org := parts[0]
					uid := parts[1]
					day := parts[2]
					total := v.S + v.F
					_ = svc.DB.Exec("INSERT INTO usage_daily_user(org_id, user_id, date, success, failed, total, updated_at) VALUES(?, ?, ?, ?, ?, ?, NOW()) ON CONFLICT (org_id, user_id, date) DO UPDATE SET success = usage_daily_user.success + EXCLUDED.success, failed = usage_daily_user.failed + EXCLUDED.failed, total = usage_daily_user.total + EXCLUDED.total, updated_at = NOW()", org, uid, day, v.S, v.F, total).Error
				}
				clientDay := map[string]struct {
					S int
					F int
				}{}
				for i, r := range buf {
					e := evbuf[i]
					if e.OAuthClientID == "" {
						continue
					}
					d := r.CreatedAt.Format("2006-01-02")
					kc := r.OrgID + "|" + e.OAuthClientID + "|" + d
					if r.StatusCode >= 400 {
						vc := clientDay[kc]
						vc.F++
						clientDay[kc] = vc
					} else {
						vc := clientDay[kc]
						vc.S++
						clientDay[kc] = vc
					}
				}
				for k, v := range clientDay {
					parts := strings.Split(k, "|")
					org := parts[0]
					cid := parts[1]
					day := parts[2]
					total := v.S + v.F
					_ = svc.DB.Exec("INSERT INTO usage_daily_client(org_id, oauth_client_id, date, success, failed, total, updated_at) VALUES(?, ?, ?, ?, ?, ?, NOW()) ON CONFLICT (org_id, oauth_client_id, date) DO UPDATE SET success = usage_daily_client.success + EXCLUDED.success, failed = usage_daily_client.failed + EXCLUDED.failed, total = usage_daily_client.total + EXCLUDED.total, updated_at = NOW()", org, cid, day, v.S, v.F, total).Error
				}
				for k, v := range svcDay {
					parts := strings.Split(k, "|")
					org := parts[0]
					sid := parts[1]
					day := parts[2]
					total := v.S + v.F
					_ = svc.DB.Exec("INSERT INTO usage_daily_service(org_id, service_id, date, success, failed, total, updated_at) VALUES(?, ?, ?, ?, ?, ?, NOW()) ON CONFLICT (org_id, service_id, date) DO UPDATE SET success = usage_daily_service.success + EXCLUDED.success, failed = usage_daily_service.failed + EXCLUDED.failed, total = usage_daily_service.total + EXCLUDED.total, updated_at = NOW()", org, sid, day, v.S, v.F, total).Error
				}
				for k, v := range epDay {
					parts := strings.Split(k, "|")
					org := parts[0]
					ep := parts[1]
					day := parts[2]
					total := v.S + v.F
					_ = svc.DB.Exec("INSERT INTO usage_daily_endpoint(org_id, endpoint, date, success, failed, total, updated_at) VALUES(?, ?, ?, ?, ?, ?, NOW()) ON CONFLICT (org_id, endpoint, date) DO UPDATE SET success = usage_daily_endpoint.success + EXCLUDED.success, failed = usage_daily_endpoint.failed + EXCLUDED.failed, total = usage_daily_endpoint.total + EXCLUDED.total, updated_at = NOW()", org, ep, day, v.S, v.F, total).Error
				}
				for k, v := range keyDay {
					parts := strings.Split(k, "|")
					org := parts[0]
					key := parts[1]
					day := parts[2]
					total := v.S + v.F
					_ = svc.DB.Exec("INSERT INTO usage_daily_key(org_id, api_key_id, date, success, failed, total, updated_at) VALUES(?, ?, ?, ?, ?, ?, NOW()) ON CONFLICT (org_id, api_key_id, date) DO UPDATE SET success = usage_daily_key.success + EXCLUDED.success, failed = usage_daily_key.failed + EXCLUDED.failed, total = usage_daily_key.total + EXCLUDED.total, updated_at = NOW()", org, key, day, v.S, v.F, total).Error
				}
				for k, v := range keyUserDay {
					parts := strings.Split(k, "|")
					org := parts[0]
					uid := parts[1]
					key := parts[2]
					day := parts[3]
					total := v.S + v.F
					_ = svc.DB.Exec("INSERT INTO usage_daily_key_user(org_id, user_id, api_key_id, date, success, failed, total, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?, NOW()) ON CONFLICT (org_id, user_id, api_key_id, date) DO UPDATE SET success = usage_daily_key_user.success + EXCLUDED.success, failed = usage_daily_key_user.failed + EXCLUDED.failed, total = usage_daily_key_user.total + EXCLUDED.total, updated_at = NOW()", org, uid, key, day, v.S, v.F, total).Error
				}
				buf = buf[:0]
				evbuf = evbuf[:0]
				lastFlush = time.Now()
			}
		}
	}()
}
