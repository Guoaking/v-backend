package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"kyc-service/internal/models"
	"kyc-service/pkg/logger"

	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
)

const (
	BillingStream = "kyc:logs:billing:stream"
	AuditStream   = "kyc:logs:audit:stream"
	ConsumerGroup = "kyc:logs:cg"
)

type RedisLogWorker struct {
	db          *gorm.DB
	redisClient *redis.Client
	stopChan    chan struct{}
	wg          sync.WaitGroup
	consumerID  string
}

func NewRedisLogWorker(db *gorm.DB, redisClient *redis.Client) *RedisLogWorker {
	return &RedisLogWorker{
		db:          db,
		redisClient: redisClient,
		stopChan:    make(chan struct{}),
		consumerID:  fmt.Sprintf("consumer-%d", time.Now().UnixNano()),
	}
}

func (w *RedisLogWorker) Start() {
	ctx := context.Background()

	// Ensure Consumer Groups exist
	for _, stream := range []string{BillingStream, AuditStream} {
		err := w.redisClient.XGroupCreateMkStream(ctx, stream, ConsumerGroup, "0").Err()
		if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
			logger.GetLogger().WithError(err).Errorf("Failed to create consumer group for stream %s", stream)
		}
	}

	w.wg.Add(2)
	go w.processBillingLogs()
	go w.processAuditLogs()
	logger.GetLogger().Info("RedisLogWorker started")
}

func (w *RedisLogWorker) Stop() {
	close(w.stopChan)
	w.wg.Wait()
	logger.GetLogger().Info("RedisLogWorker stopped")
}

func (w *RedisLogWorker) Enqueue(log models.LogEnvelope) {
	if log.LogType != models.LogTypeBilling {
		// If other types are enqueued, ignore for now or send to audit if applicable.
		return
	}

	b, err := json.Marshal(log)
	if err != nil {
		logger.GetLogger().WithError(err).Error("Failed to marshal billing log")
		return
	}

	err = w.redisClient.XAdd(context.Background(), &redis.XAddArgs{
		Stream: BillingStream,
		Values: map[string]interface{}{"payload": string(b)},
	}).Err()

	if err != nil {
		logger.GetLogger().WithError(err).Error("Failed to enqueue billing log to redis stream")
	}
}

func (w *RedisLogWorker) RecordAuditLog(log *models.AuditLog) {
	b, err := json.Marshal(log)
	if err != nil {
		logger.GetLogger().WithError(err).Error("Failed to marshal audit log")
		return
	}

	err = w.redisClient.XAdd(context.Background(), &redis.XAddArgs{
		Stream: AuditStream,
		Values: map[string]interface{}{"payload": string(b)},
	}).Err()

	if err != nil {
		logger.GetLogger().WithError(err).Error("Failed to enqueue audit log to redis stream")
	}
}

// Internal type to handle delayed payload unmarshaling
type rawLogEnvelope struct {
	Timestamp   time.Time           `json:"timestamp"`
	LogType     models.LogType      `json:"log_type"`
	Level       string              `json:"level"`
	Trace       models.TraceInfo    `json:"trace"`
	Identity    models.IdentityInfo `json:"identity"`
	EventAction string              `json:"event_action,omitempty"`
	Message     string              `json:"message,omitempty"`
	RawPayload  json.RawMessage     `json:"payload"`
}

func (w *RedisLogWorker) processBillingLogs() {
	defer w.wg.Done()

	// 1. Recover pending messages first
	w.recoverPending(BillingStream, w.processBillingBatch)

	for {
		select {
		case <-w.stopChan:
			return
		default:
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		streams, err := w.redisClient.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    ConsumerGroup,
			Consumer: w.consumerID,
			Streams:  []string{BillingStream, ">"},
			Count:    100,
			Block:    2 * time.Second,
		}).Result()
		cancel()

		if err != nil {
			if err == redis.Nil {
				continue // block timeout
			}
			logger.GetLogger().WithError(err).Error("XReadGroup error for billing logs")
			time.Sleep(1 * time.Second)
			continue
		}

		for _, stream := range streams {
			w.processBillingBatch(stream.Messages)
		}
	}
}

func (w *RedisLogWorker) processAuditLogs() {
	defer w.wg.Done()

	// 1. Recover pending messages
	w.recoverPending(AuditStream, w.processAuditBatch)

	for {
		select {
		case <-w.stopChan:
			return
		default:
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		streams, err := w.redisClient.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    ConsumerGroup,
			Consumer: w.consumerID,
			Streams:  []string{AuditStream, ">"},
			Count:    100,
			Block:    2 * time.Second,
		}).Result()
		cancel()

		if err != nil {
			if err == redis.Nil {
				continue
			}
			logger.GetLogger().WithError(err).Error("XReadGroup error for audit logs")
			time.Sleep(1 * time.Second)
			continue
		}

		for _, stream := range streams {
			w.processAuditBatch(stream.Messages)
		}
	}
}

func (w *RedisLogWorker) recoverPending(streamName string, processor func([]redis.XMessage)) {
	ctx := context.Background()
	for {
		pending, err := w.redisClient.XPendingExt(ctx, &redis.XPendingExtArgs{
			Stream:   streamName,
			Group:    ConsumerGroup,
			Idle:     10 * time.Minute, // recover messages idle for 10+ mins
			Start:    "-",
			End:      "+",
			Count:    100,
			Consumer: w.consumerID, // recover for this consumer first, could be extended
		}).Result()

		if err != nil {
			logger.GetLogger().WithError(err).Error("Failed to check pending messages")
			break
		}

		if len(pending) == 0 {
			break
		}

		var messageIDs []string
		for _, p := range pending {
			messageIDs = append(messageIDs, p.ID)
		}

		// Claim the messages
		msgs, err := w.redisClient.XClaim(ctx, &redis.XClaimArgs{
			Stream:   streamName,
			Group:    ConsumerGroup,
			Consumer: w.consumerID,
			MinIdle:  10 * time.Minute,
			Messages: messageIDs,
		}).Result()

		if err != nil {
			logger.GetLogger().WithError(err).Error("Failed to claim messages")
			break
		}

		if len(msgs) > 0 {
			processor(msgs)
		}
	}
}

func (w *RedisLogWorker) processBillingBatch(messages []redis.XMessage) {
	if len(messages) == 0 {
		return
	}

	var billingLogs []models.UsageLog
	var messageIDs []string

	for _, msg := range messages {
		payloadStr, ok := msg.Values["payload"].(string)
		if !ok {
			messageIDs = append(messageIDs, msg.ID)
			continue
		}

		var env rawLogEnvelope
		if err := json.Unmarshal([]byte(payloadStr), &env); err != nil {
			messageIDs = append(messageIDs, msg.ID)
			continue
		}

		var payload models.BillingPayload
		if err := json.Unmarshal(env.RawPayload, &payload); err != nil {
			messageIDs = append(messageIDs, msg.ID)
			continue
		}

		billingLogs = append(billingLogs, models.UsageLog{
			ID:            payload.ID,
			OrgID:         env.Identity.TenantID,
			UserID:        env.Identity.UserID,
			OAuthClientID: payload.OAuthClientID,
			Endpoint:      payload.Endpoint,
			StatusCode:    payload.StatusCode,
			RequestID:     env.Trace.RequestID,
			ServiceType:   payload.ServiceType,
			UsageUnits:    payload.UsageUnits,
			Billable:      payload.Billable,
			SessionID:     payload.SessionID,
			Metadata:      mapToJSONB(payload.Metadata),
			CreatedAt:     env.Timestamp,
		})
		messageIDs = append(messageIDs, msg.ID)
	}

	if len(billingLogs) > 0 {
		err := w.db.Transaction(func(tx *gorm.DB) error {
			// 1. Batch Insert Usage Logs
			if err := tx.Create(&billingLogs).Error; err != nil {
				return err
			}

			// 2. Upsert Aggregations
			return w.aggregateAndUpsert(tx, billingLogs)
		})

		if err != nil {
			logger.GetLogger().WithError(err).Error("DB Transaction failed for billing logs batch, will retry")
			// We do NOT ack the messages, so they stay in PEL and will be retried
			return
		}
	}

	// Ack all messages in batch
	if len(messageIDs) > 0 {
		w.redisClient.XAck(context.Background(), BillingStream, ConsumerGroup, messageIDs...)
	}
}

func (w *RedisLogWorker) processAuditBatch(messages []redis.XMessage) {
	if len(messages) == 0 {
		return
	}

	var auditLogs []models.AuditLog
	var messageIDs []string

	for _, msg := range messages {
		payloadStr, ok := msg.Values["payload"].(string)
		if !ok {
			messageIDs = append(messageIDs, msg.ID)
			continue
		}

		var auditLog models.AuditLog
		if err := json.Unmarshal([]byte(payloadStr), &auditLog); err != nil {
			messageIDs = append(messageIDs, msg.ID)
			continue
		}

		auditLogs = append(auditLogs, auditLog)
		messageIDs = append(messageIDs, msg.ID)
	}

	if len(auditLogs) > 0 {
		if err := w.db.Create(&auditLogs).Error; err != nil {
			logger.GetLogger().WithError(err).Error("DB insert failed for audit logs batch, will retry")
			return
		}
	}

	if len(messageIDs) > 0 {
		w.redisClient.XAck(context.Background(), AuditStream, ConsumerGroup, messageIDs...)
	}
}

func (w *RedisLogWorker) aggregateAndUpsert(tx *gorm.DB, logs []models.UsageLog) error {
	type AggKey struct {
		OrgID       string
		StatTime    time.Time
		ServiceType string
		Endpoint    string
		UserID      string
	}

	type AggData struct {
		TotalRequests int64
		TotalErrors   int64
		UsageUnits    int64
	}

	aggMap := make(map[AggKey]*AggData)

	for _, logItem := range logs {
		statTime := time.Date(logItem.CreatedAt.Year(), logItem.CreatedAt.Month(), logItem.CreatedAt.Day(), 0, 0, 0, 0, logItem.CreatedAt.Location())

		key := AggKey{
			OrgID:       logItem.OrgID,
			StatTime:    statTime,
			ServiceType: logItem.ServiceType,
			Endpoint:    logItem.Endpoint,
			UserID:      logItem.UserID,
		}

		if key.ServiceType == "" {
			key.ServiceType = "unknown"
		}

		data, exists := aggMap[key]
		if !exists {
			data = &AggData{}
			aggMap[key] = data
		}

		data.TotalRequests++
		if logItem.StatusCode >= 400 {
			data.TotalErrors++
		}
		data.UsageUnits += int64(logItem.UsageUnits)
	}

	for key, data := range aggMap {
		dimensions := map[string]string{
			"service_type": key.ServiceType,
			"endpoint":     key.Endpoint,
			"user_id":      key.UserID,
		}

		dimBytes, _ := json.Marshal(dimensions)
		dimJSONB := string(dimBytes)

		sql := `
			INSERT INTO usage_metric_aggs (
				org_id, metric_name, time_unit, stat_time, dimensions, total_requests, total_errors, usage_units
			) VALUES (
				?, 'api_call', 'day', ?, ?::jsonb, ?, ?, ?
			) ON CONFLICT (org_id, metric_name, time_unit, stat_time, dimensions) 
			DO UPDATE SET 
				total_requests = usage_metric_aggs.total_requests + EXCLUDED.total_requests,
				total_errors = usage_metric_aggs.total_errors + EXCLUDED.total_errors,
				usage_units = usage_metric_aggs.usage_units + EXCLUDED.usage_units,
				updated_at = CURRENT_TIMESTAMP;
		`

		if err := tx.Exec(sql, key.OrgID, key.StatTime, dimJSONB, data.TotalRequests, data.TotalErrors, data.UsageUnits).Error; err != nil {
			return err
		}
	}
	return nil
}
