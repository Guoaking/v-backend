package worker

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"kyc-service/internal/models"
	"kyc-service/internal/storage"
	"kyc-service/pkg/logger"

	"gorm.io/datatypes"
)

// AsyncLogWorker 异步日志写入器
type AsyncLogWorker struct {
	billingStore  storage.BillingLogStorage
	auditStore    storage.AuditLogStorage
	logChan       chan models.LogEnvelope
	auditLogChan  chan models.AuditLog
	stopChan      chan struct{}
	wg            sync.WaitGroup
	batchSize     int
	flushInterval time.Duration
}

// mapToJSONB 将 map 转换为 datatypes.JSON
func mapToJSONB(m map[string]interface{}) datatypes.JSON {
	if m == nil {
		return nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	return datatypes.JSON(b)
}

// NewAsyncLogWorker 创建新的日志 Worker
func NewAsyncLogWorker(billingStore storage.BillingLogStorage, auditStore storage.AuditLogStorage) *AsyncLogWorker {
	return &AsyncLogWorker{
		billingStore:  billingStore,
		auditStore:    auditStore,
		logChan:       make(chan models.LogEnvelope, 10000), // 缓冲区大小 10000
		auditLogChan:  make(chan models.AuditLog, 10000),    // 审计日志缓冲区
		stopChan:      make(chan struct{}),
		batchSize:     100,
		flushInterval: 5 * time.Second,
	}
}

// Start 启动 Worker
func (w *AsyncLogWorker) Start() {
	w.wg.Add(1)
	go w.processLogs()
	logger.GetLogger().Info("AsyncLogWorker started")
}

// Stop 优雅停止
func (w *AsyncLogWorker) Stop() {
	close(w.stopChan)
	w.wg.Wait()
	logger.GetLogger().Info("AsyncLogWorker stopped")
}

// Enqueue 将日志入队
func (w *AsyncLogWorker) Enqueue(log models.LogEnvelope) {
	select {
	case w.logChan <- log:
	default:
		// 队列满时丢弃或降级，防止阻塞业务
		logger.GetLogger().Error("Log worker queue full, dropping log")
	}
}

// RecordAuditLog 记录审计日志
func (w *AsyncLogWorker) RecordAuditLog(log *models.AuditLog) {
	select {
	case w.auditLogChan <- *log:
	default:
		logger.GetLogger().Error("Audit log worker queue full, dropping log")
	}
}

// processLogs 消费循环
func (w *AsyncLogWorker) processLogs() {
	defer w.wg.Done()

	var billingLogs []models.UsageLog
	var auditLogs []models.AuditLog

	ticker := time.NewTicker(w.flushInterval)
	defer ticker.Stop()

	flush := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if len(billingLogs) > 0 {
			if err := w.billingStore.Save(ctx, billingLogs); err != nil {
				logger.GetLogger().WithError(err).Error("Failed to flush billing logs")
			}
			billingLogs = nil
		}
		if len(auditLogs) > 0 {
			if err := w.auditStore.Save(ctx, auditLogs); err != nil {
				logger.GetLogger().WithError(err).Error("Failed to flush audit logs")
			}
			auditLogs = nil
		}
	}

	for {
		select {
		case log := <-w.logChan:
			switch log.LogType {
			case models.LogTypeBilling:
				// 强类型转换 LogEnvelope.Payload -> models.BillingPayload
				if payload, ok := log.Payload.(models.BillingPayload); ok {
					billingLogs = append(billingLogs, models.UsageLog{
						ID:            payload.ID,
						OrgID:         log.Identity.TenantID,
						UserID:        log.Identity.UserID,
						OAuthClientID: payload.OAuthClientID,
						Endpoint:      payload.Endpoint,
						StatusCode:    payload.StatusCode,
						RequestID:     log.Trace.RequestID,
						ServiceType:   payload.ServiceType,
						UsageUnits:    payload.UsageUnits,
						Billable:      payload.Billable,
						SessionID:     payload.SessionID,
						Metadata:      mapToJSONB(payload.Metadata),
						CreatedAt:     log.Timestamp,
					})
				} else {
					logger.GetLogger().Warn("Failed to cast BillingPayload in log_worker")
				}
			case models.LogTypeAudit:
				// 转换 LogEnvelope -> models.AuditLog
				payloadBytes, err := json.Marshal(log.Payload)
				if err != nil {
					logger.GetLogger().WithError(err).Error("Failed to marshal audit log payload")
					continue
				}
				auditLogs = append(auditLogs, models.AuditLog{
					OrgID:     log.Identity.TenantID,
					UserID:    log.Identity.UserID,
					RequestID: log.Trace.RequestID,
					Action:    log.EventAction,
					IP:        log.Identity.ClientIP,
					Details:   datatypes.JSON(payloadBytes),
					CreatedAt: log.Timestamp,
				})
			}

			// 达到批次大小触发刷新
			if len(billingLogs) >= w.batchSize || len(auditLogs) >= w.batchSize {
				flush()
			}

		case log := <-w.auditLogChan:
			auditLogs = append(auditLogs, log)
			if len(auditLogs) >= w.batchSize {
				flush()
			}

		case <-ticker.C:
			flush()

		case <-w.stopChan:
			flush() // 退出前刷新剩余日志
			return
		}
	}
}

// 辅助函数
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getInt(m map[string]interface{}, key string) int {
	if v, ok := m[key]; ok {
		if i, ok := v.(int); ok {
			return i
		}
		if f, ok := v.(float64); ok {
			return int(f)
		}
	}
	return 0
}
