package worker

import "kyc-service/internal/models"

type LogWorker interface {
	Start()
	Stop()
	Enqueue(log models.LogEnvelope)
	RecordAuditLog(log *models.AuditLog)
}

// DummyLogWorker is a no-op implementation for tests
type DummyLogWorker struct{}

func (d *DummyLogWorker) Start()                              {}
func (d *DummyLogWorker) Stop()                               {}
func (d *DummyLogWorker) Enqueue(log models.LogEnvelope)      {}
func (d *DummyLogWorker) RecordAuditLog(log *models.AuditLog) {}
