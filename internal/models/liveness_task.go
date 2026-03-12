package models

import (
	"time"

	"gorm.io/datatypes"
)

type LivenessTask struct {
	ID             string         `gorm:"primaryKey" json:"id"`
	OrganizationID string         `gorm:"index" json:"organization_id"`
	SessionID      string         `gorm:"uniqueIndex" json:"session_id"`
	VideoAssetID   *string        `gorm:"index" json:"video_asset_id,omitempty"`
	Actions        datatypes.JSON `gorm:"type:jsonb" json:"actions,omitempty"`
	Status         string         `gorm:"index" json:"status"`
	ExternalID     string         `gorm:"index" json:"external_id"`
	Result         datatypes.JSON `gorm:"type:jsonb" json:"result,omitempty"`
	ErrorMessage   string         `json:"error_message,omitempty"`
	RetryCount     int            `json:"retry_count"`
	TimeoutSeconds int            `json:"timeout_seconds"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}
