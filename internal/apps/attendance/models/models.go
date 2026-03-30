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
	FaceImageURL string    `gorm:"type:varchar(255)" json:"face_image_url"`         // 原始人脸图的文件路径
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
	Status           string    `gorm:"type:varchar(20)" json:"status"`                        // "success", "manual_review" (需人工复核)
	FallbackImageURL string    `gorm:"type:varchar(255)" json:"fallback_image_url,omitempty"` // 失败降级时的现场照片的文件路径
	Latitude         float64   `json:"latitude"`                                              // 打卡时的纬度
	Longitude        float64   `json:"longitude"`                                             // 打卡时的经度
	CreatedAt        time.Time `json:"created_at"`
}

// DataCollectionDocument 算法数据反哺表 (OCR 注册域)
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

// OrganizationSettings 租户级别的考勤配置
type OrganizationSettings struct {
	OrgID           string    `gorm:"primaryKey;type:varchar(64)" json:"org_id"`
	PunchMode       string    `gorm:"type:varchar(50);default:'liveness_active'" json:"punch_mode"` // photo_only, liveness_silent, liveness_active
	AllowLatePunch  bool      `gorm:"default:true" json:"allow_late_punch"`
	RequireLocation bool      `gorm:"default:false" json:"require_location"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// DataCollectionFace 算法数据反哺表 (人脸打卡域)
// 收集极端光照、佩戴口罩等边缘场景下的人脸比对数据，供算法团队微调 FaceCompare 和 Liveness 模型。
type DataCollectionFace struct {
	ID             string    `gorm:"primaryKey;type:varchar(64)" json:"id"`
	OrgID          string    `gorm:"index;not null;type:varchar(64)" json:"org_id"`
	BaseImageURL   string    `gorm:"type:varchar(255)" json:"base_image_url"`  // 注册时的底库图
	PunchImageURL  string    `gorm:"type:varchar(255)" json:"punch_image_url"` // 打卡时的现场图
	Confidence     float64   `json:"confidence"`                               // 模型返回的相似度得分
	IsSameFace     int       `json:"is_same_face"`                             // 1=是同一人, 0=不是同一人
	IsFallback     bool      `json:"is_fallback"`                              // 是否因为多次失败触发了降级
	EnvironmentEnv string    `gorm:"type:varchar(50)" json:"environment_env"`  // 可选: "dark", "backlight" (由人工审核后标注)
	CreatedAt      time.Time `json:"created_at"`
}
