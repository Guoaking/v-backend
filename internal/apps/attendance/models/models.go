package models

import (
	"time"

	"gorm.io/datatypes"
)

// OrganizationEmployee 组织员工表
// 用于记录打卡员工的核心身份锚点。
// 独立于 SaaS 核心的 User 表，防止业务耦合。
type OrganizationEmployee struct {
	ID           string    `gorm:"primaryKey;type:varchar(64)" json:"id"`
	OrgID        string    `gorm:"index;not null;type:varchar(64)" json:"org_id"`
	EmployeeSN   string    `gorm:"index;type:varchar(64)" json:"employee_sn"`        // 客户内部工号(可选)
	IDNumber     string    `gorm:"index;not null;type:varchar(64)" json:"id_number"` // 核心锚点：证件号
	Name         string    `gorm:"not null;type:varchar(64)" json:"name"`
	Phone        string    `gorm:"type:varchar(20)" json:"phone"`
	FaceFeature  []byte    `gorm:"type:bytea" json:"-"`                             // 提取出的人脸特征向量，不对外暴露
	FaceImageURL string    `gorm:"type:text" json:"face_image_url"`                 // 原始人脸图(Base64或URL)，放开长度限制
	Status       string    `gorm:"type:varchar(20);default:'active'" json:"status"` // active, inactive, deleted
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// AttendanceRecord 考勤记录表
// 记录员工打卡流水，Append-Only 设计。
type AttendanceRecord struct {
	ID               string    `gorm:"primaryKey;type:varchar(64)" json:"id"`
	OrgID            string    `gorm:"index:idx_org_time;not null;type:varchar(64)" json:"org_id"`
	EmployeeID       string    `gorm:"index;not null;type:varchar(64)" json:"employee_id"`
	PunchTime        time.Time `gorm:"index:idx_org_time;not null" json:"punch_time"`
	PunchType        string    `gorm:"type:varchar(10)" json:"punch_type"` // "in", "out"
	LivenessScore    float64   `json:"liveness_score"`
	FaceScore        float64   `json:"face_score"`
	Status           string    `gorm:"type:varchar(20)" json:"status"`                // "success", "manual_review" (需人工复核)
	FallbackImageURL string    `gorm:"type:text" json:"fallback_image_url,omitempty"` // 失败降级时的现场照片(Base64或URL)
	Latitude         float64   `json:"latitude"`                                      // 打卡时的纬度
	Longitude        float64   `json:"longitude"`                                     // 打卡时的经度
	CreatedAt        time.Time `json:"created_at"`
}

// DataCollectionDocument 算法数据反哺表
// 核心资产表，专供算法团队优化模型设计的“Ground Truth”数据池。
// 故意不关联 EmployeeID，以满足数据隐私脱敏和生命周期解耦。
type DataCollectionDocument struct {
	ID             string         `gorm:"primaryKey;type:varchar(64)" json:"id"`
	OrgID          string         `gorm:"index;not null;type:varchar(64)" json:"org_id"`
	IDType         string         `gorm:"type:varchar(20)" json:"id_type"` // passport, thai_id 等
	RawImageURL    string         `gorm:"type:varchar(255)" json:"raw_image_url"`
	RawOCRResult   datatypes.JSON `gorm:"type:jsonb" json:"raw_ocr_result"`   // 模型原始输出
	FinalUserInput datatypes.JSON `gorm:"type:jsonb" json:"final_user_input"` // 员工手动修正后的最终结果
	IsCorrected    bool           `gorm:"index" json:"is_corrected"`          // 如果 Final != Raw，则为 true
	CreatedAt      time.Time      `json:"created_at"`
}
