package service

import (
	"context"
	"encoding/json"
	"fmt"
	mrand "math/rand"
	"mime/multipart"
	"strings"

	"kyc-service/internal/apps/attendance/models"
	coreService "kyc-service/internal/service"
	"kyc-service/pkg/logger"
	"kyc-service/pkg/utils"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type OCRResult struct {
	SessionID  string                 `json:"session_id"`
	Fields     map[string]interface{} `json:"fields"`
	Confidence float64                `json:"confidence"`
	RawJSON    string                 `json:"raw_json"`
}

type IdentityMatchRequest struct {
	Query string `json:"query" binding:"required"`
}

type IdentityMatchResult struct {
	IDNumber   string `json:"id_number"`
	EmployeeNo string `json:"employee_no"`
	Name       string `json:"name"`
	Phone      string `json:"phone"`
}

type EnrollRequest struct {
	SessionID   string                 `json:"session_id"`
	IDType      string                 `json:"id_type"`
	IDNumber    string                 `json:"id_number"`
	Name        string                 `json:"name"`
	Phone       string                 `json:"phone"`
	FaceFile    *multipart.FileHeader  `json:"-"`
	RawImageURL string                 `json:"raw_image_url"`
	RawOCRJSON  string                 `json:"raw_ocr_json"`
	FinalFields map[string]interface{} `json:"final_fields"`
}

var ErrAlreadyEnrolled = fmt.Errorf("employee already enrolled")
var ErrIdentityNotFound = fmt.Errorf("employee not found or inactive")
var ErrFaceVerificationFailed = fmt.Errorf("face verification failed: not the same person")

func (s *AttendanceService) ProcessOCR(ctx context.Context, orgID string, file *multipart.FileHeader, idType string) (*OCRResult, error) {
	backendOcrType := idType
	switch idType {
	case "thai_id", "id_card":
		backendOcrType = "id_card"
	case "driving_license":
		backendOcrType = "driver_license"
	case "voter_id":
		backendOcrType = "general"
	}

	req := &coreService.OCRRequest{Picture: file, Type: backendOcrType}
	ctx = context.WithValue(ctx, "org_id", orgID)
	resp, err := s.kycService.OCR(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("underlying OCR failed: %w", err)
	}
	if resp.Code != 200 && resp.Code != 0 {
		return nil, fmt.Errorf("ocr service error: %s", resp.Msg)
	}

	mappedFields := map[string]interface{}{}
	normalizedResults := make(map[string]string)
	for key, val := range resp.ParsingResults {
		if val.Text != "" {
			normKey := strings.ToLower(key)
			normKey = strings.ReplaceAll(normKey, " ", "")
			normKey = strings.ReplaceAll(normKey, "_", "")
			normKey = strings.ReplaceAll(normKey, "-", "")
			normalizedResults[normKey] = val.Text
			mappedFields[key] = val.Text
		}
	}

	for _, alias := range []string{"idnumber", "id", "nik", "documentnumber", "no", "personalid", "identitynumber"} {
		if val, ok := normalizedResults[alias]; ok {
			mappedFields["id_number"] = val
			break
		}
	}
	for _, alias := range []string{"name", "nama", "fullname", "givenname", "surname", "firstname"} {
		if val, ok := normalizedResults[alias]; ok {
			mappedFields["name"] = val
			break
		}
	}
	if _, ok := mappedFields["id_number"]; !ok {
		mappedFields["id_number"] = ""
	}
	if _, ok := mappedFields["name"]; !ok {
		mappedFields["name"] = ""
	}

	result := &OCRResult{
		SessionID:  utils.GenerateID(),
		Fields:     mappedFields,
		Confidence: 0.95,
	}
	rawBytes, _ := json.Marshal(resp.ParsingResults)
	result.RawJSON = string(rawBytes)
	return result, nil
}

func (s *AttendanceService) EnrollOCR(ctx context.Context, orgID string, file *multipart.FileHeader, idType string) (*OCRResult, error) {
	return s.ProcessOCR(ctx, orgID, file, idType)
}

func (s *AttendanceService) MatchIdentity(ctx context.Context, orgID string, query string) ([]IdentityMatchResult, error) {
	var emps []models.OrganizationEmployee
	if err := s.db.Where("org_id = ? AND status = ? AND employee_no LIKE ?", orgID, "active", "%"+query).Find(&emps).Error; err != nil {
		return nil, fmt.Errorf("database error: %w", err)
	}
	if len(emps) == 0 {
		return nil, ErrIdentityNotFound
	}
	results := make([]IdentityMatchResult, 0, len(emps))
	for _, emp := range emps {
		results = append(results, IdentityMatchResult{
			IDNumber:   emp.IDNumber,
			EmployeeNo: emp.EmployeeNo,
			Name:       emp.Name,
			Phone:      emp.Phone,
		})
	}
	return results, nil
}

func (s *AttendanceService) GetEmployeeByIDNumber(ctx context.Context, orgID string, idNumber string) (*models.OrganizationEmployee, error) {
	var emp models.OrganizationEmployee
	if err := s.db.Where("org_id = ? AND id_number = ? AND status = ?", orgID, idNumber, "active").First(&emp).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrIdentityNotFound
		}
		return nil, fmt.Errorf("database error: %w", err)
	}
	return &emp, nil
}

func (s *AttendanceService) EnrollEmployee(ctx context.Context, orgID string, req *EnrollRequest) (*models.OrganizationEmployee, error) {
	log := logger.GetLogger().WithContext(ctx)
	var newEmp models.OrganizationEmployee

	err := s.db.Transaction(func(tx *gorm.DB) error {
		var existing models.OrganizationEmployee
		err := tx.Where("org_id = ? AND id_number = ?", orgID, req.IDNumber).First(&existing).Error
		if err == nil && existing.ID != "" {
			newEmp = existing
			return ErrAlreadyEnrolled
		}

		faceHeader := req.FaceFile
		if faceHeader == nil {
			return fmt.Errorf("face image is required")
		}

		syncCtx := context.WithValue(ctx, "sync_upload", true)
		asset, err := s.kycService.IngestImage(syncCtx, orgID, faceHeader)
		if err != nil {
			return fmt.Errorf("failed to save face image: %w", err)
		}
		faceImagePath := asset.FilePath

		ctxWithOrg := context.WithValue(ctx, "org_id", orgID)
		detectRes, detectErr := s.kycService.FaceDetect(ctxWithOrg, faceHeader)
		if detectErr != nil || detectRes == nil || detectRes.Code != 0 {
			log.Warnf("Face detect failed during enrollment: %v", detectErr)
			go func(oID, badImg string) {
				dbCopy := s.db.Session(&gorm.Session{})
				dbCopy.Create(&models.DataCollectionFace{
					ID:             utils.GenerateID(),
					OrgID:          oID,
					BaseImageURL:   badImg,
					Confidence:     0,
					IsSameFace:     0,
					IsFallback:     false,
					EnvironmentEnv: string(models.ScenarioEnrollDetectFailed),
				})
			}(orgID, faceImagePath)
			return fmt.Errorf("face detection failed: please upload a clear face image")
		}
		if detectRes.DetectionResults.IsFaceExist == 0 || detectRes.DetectionResults.FaceNum == 0 {
			go func(oID, badImg string) {
				dbCopy := s.db.Session(&gorm.Session{})
				dbCopy.Create(&models.DataCollectionFace{
					ID:             utils.GenerateID(),
					OrgID:          oID,
					BaseImageURL:   badImg,
					Confidence:     0,
					IsSameFace:     0,
					IsFallback:     false,
					EnvironmentEnv: string(models.ScenarioEnrollNoFace),
				})
			}(orgID, faceImagePath)
			return fmt.Errorf("no face detected in the image")
		}

		var employeeNo string
		for i := 0; i < 10; i++ {
			employeeNo = fmt.Sprintf("%08d", mrand.Intn(100000000))
			var count int64
			tx.Model(&models.OrganizationEmployee{}).Where("org_id = ? AND employee_no = ?", orgID, employeeNo).Count(&count)
			if count == 0 {
				break
			}
			if i == 9 {
				return fmt.Errorf("failed to generate unique employee number")
			}
		}

		emp := models.OrganizationEmployee{
			ID:           utils.GenerateID(),
			OrgID:        orgID,
			EmployeeNo:   employeeNo,
			IDNumber:     req.IDNumber,
			Name:         req.Name,
			Phone:        req.Phone,
			FaceImageURL: faceImagePath,
			Status:       "active",
		}
		if existing.ID != "" {
			emp.ID = existing.ID
			if err := tx.Save(&emp).Error; err != nil {
				return err
			}
		} else if err := tx.Create(&emp).Error; err != nil {
			return err
		}
		newEmp = emp

		finalBytes, _ := json.Marshal(req.FinalFields)
		doc := models.DataCollectionDocument{
			ID:             utils.GenerateID(),
			OrgID:          orgID,
			IDType:         req.IDType,
			RawImageURL:    req.RawImageURL,
			RawOCRResult:   datatypes.JSON(req.RawOCRJSON),
			FinalUserInput: datatypes.JSON(string(finalBytes)),
			IsCorrected:    string(finalBytes) != req.RawOCRJSON,
		}
		if err := tx.Create(&doc).Error; err != nil {
			log.Warnf("Failed to collect data document, but proceeding with enrollment: %v", err)
		}
		return nil
	})
	if err != nil {
		if err == ErrAlreadyEnrolled {
			return &newEmp, err
		}
		return nil, err
	}
	return &newEmp, nil
}
