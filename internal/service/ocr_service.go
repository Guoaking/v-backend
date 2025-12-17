package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net"
	"time"

	"kyc-service/pkg/httpclient"
	"kyc-service/pkg/logger"
	"kyc-service/pkg/metrics"
	"kyc-service/pkg/tracing"
	"kyc-service/pkg/utils"

	"go.opentelemetry.io/otel/attribute"
)

type OCRRequest struct {
	Picture  *multipart.FileHeader `form:"picture" binding:"required"`
	Token    string                `form:"token"`
	Type     string                `form:"type"`
	Language string                `form:"language"`
}

type OCRItem struct {
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence"`
}

type OCRResponse struct {
	Code           int                `json:"code"`
	Msg            string             `json:"msg"`
	ParsingResults map[string]OCRItem `json:"parsing_results,omitempty"`
	FullText       string             `json:"full_text,omitempty"`
	Filename       string             `json:"filename,omitempty"`
	Error          string             `json:"error,omitempty"`
}

type StandardOCRResponse struct {
	Success     bool    `json:"success"`
	IDCard      string  `json:"id_card,omitempty"`
	Name        string  `json:"name,omitempty"`
	Nationality string  `json:"nationality,omitempty"`
	BirthDate   string  `json:"birth_date,omitempty"`
	Address     string  `json:"address,omitempty"`
	IssuedBy    string  `json:"issued_by,omitempty"`
	ValidDate   string  `json:"valid_date,omitempty"`
	Confidence  float64 `json:"confidence,omitempty"`
}

func (s *KYCService) OCR(ctx context.Context, req *OCRRequest) (*OCRResponse, error) {
	ctx, span := tracing.StartSpan(ctx, "KYCService.OCR")
	defer span.End()

	start := time.Now()
	userID := getUserID(ctx)
	//clientIP := getClientIP(ctx)
	orgID := getOrgID(ctx)
	if orgID == "" {
		return nil, fmt.Errorf("missing organization context")
	}

	defer func() {
		duration := time.Since(start)
		if s.kycProcessingTime != nil {
			s.kycProcessingTime.Record(ctx, duration.Seconds())
		}
	}()

	metrics.RecordSensitiveDataAccess(ctx, "id_card_image", userID, true, "/api/v1/kyc/ocr")

	//kycRequest := &models.KYCRequest{
	//	ID:          uuid.New().String(),
	//	UserID:      userID,
	//	RequestType: "ocr",
	//	Status:      "processing",
	//	IPAddress:   clientIP,
	//	UserAgent:   getUserAgent(ctx),
	//}
	//
	//ctx = context.WithValue(ctx, "request_id", kycRequest.ID)
	//
	//if err := s.DB.Create(kycRequest).Error; err != nil {
	//	logger.GetLogger().WithError(err).Error("create KYC request failed")
	//	metrics.RecordBusinessOperation(ctx, "ocr", false, time.Since(start), "database_error")
	//	return nil, fmt.Errorf("create KYC request failed: %w", err)
	//}

	ocrType := req.Type
	if ocrType == "" {
		ocrType = "general"
	}
	span.SetAttributes(
		attribute.String("service.name", "ocr-service"),
		attribute.String("ocr.type", ocrType),
		attribute.String("org.id", orgID),
	)

	var ocrResult *OCRResponse
	serviceType := "ocr"
	now1 := time.Now()
	if err := s.checkAndConsumeQuota(ctx, orgID, serviceType, func() error {
		proc := ocrGetProcessor(ocrType)

		if proc == nil {
			return fmt.Errorf("Unsupported OCR type")
		}
		now2 := time.Now()
		r, e := proc(ctx, s, req)
		fmt.Printf("output: %v, %v\n", "end2", time.Since(now2))

		if e != nil {
			return e
		}

		ocrResult = r
		if ocrResult.Code != 0 {
			return fmt.Errorf("OCR recognition failed: %s", ocrResult.Error)
		}
		return nil
	}); err != nil {
		//kycRequest.Status = "failed"
		//kycRequest.ErrorMessage = err.Error()
		//s.DB.Save(kycRequest)
		if s.ocrSuccessRate != nil {
			s.ocrSuccessRate.Record(ctx, 0.0)
		}
		metrics.RecordBusinessOperation(ctx, "ocr", false, time.Since(start), "ocr_service_error")
		metrics.RecordDependencyCall(ctx, "ocr_service", "recognize", false, time.Since(start))
		return nil, err
	}
	fmt.Printf("output: %v, %v\n", "end1", time.Since(now1))

	metrics.RecordDependencyCall(ctx, "ocr_service", "recognize", true, time.Since(start))

	if ocrResult.Code == 0 {
		//kycRequest.Status = "success"
		if s.ocrSuccessRate != nil {
			s.ocrSuccessRate.Record(ctx, 1.0)
		}
		metrics.RecordSensitiveDataAccess(ctx, "id_card_data", userID, true, "/api/v1/kyc/ocr")
		metrics.RecordBusinessOperation(ctx, "ocr", true, time.Since(start), "")
	} else {
		//kycRequest.Status = "failed"
		//kycRequest.ErrorMessage = ocrResult.Error
		if s.ocrSuccessRate != nil {
			s.ocrSuccessRate.Record(ctx, 0.0)
		}
		metrics.RecordBusinessOperation(ctx, "ocr", false, time.Since(start), "ocr_recognition_failed")
		return nil, fmt.Errorf("OCR recognition failed: %s", ocrResult.Error)
	}

	//s.DB.Save(kycRequest)
	s.RecordAuditLog(ctx, "ocr."+ocrType+".scan", "ocr", "", "success", "")
	return ocrResult, nil
}

func (s *KYCService) callOCRService(ctx context.Context, request *OCRRequest) (*OCRResponse, error) {
	ctx, span := tracing.StartSpan(ctx, "KYCService.callOCRService")
	defer span.End()

	startTime := time.Now()
	result := metrics.ResultSuccess
	httpStatusCode := ""

	defer func() {
		duration := time.Since(startTime)
		metrics.RecordThirdPartyRequest(ctx, "ocr_service", result, httpStatusCode, duration)
	}()

	file, err := request.Picture.Open()
	if err != nil {
		result = metrics.ResultRequestPrepareFailed
		return nil, fmt.Errorf("open image file failed: %w", err)
	}
	defer file.Close()

	data := map[string]string{
		//"token":      "testuser",
		"type":       request.Type,
		"request_id": ctx.Value("request_id").(string),
		//"language":   request.Language,
		"country": request.Language,
	}

	var respBody []byte
	if s.Config.UseMock {
		time.Sleep(time.Duration(utils.GenerateRandomNumbers(5)) * time.Second)
		errmsg := "{\n  \"code\": 500,\n  \"error\": \"mock error\",\n  \"msg\": \"recognition error\"\n}"
		succmsg := `{"code":0,"msg":"recognition success","parsing_results":{},"full_text":"","filename":"t1.jpg"}`
		if utils.GenerateRandomBool() {
			respBody = []byte(succmsg)
		} else {
			respBody = []byte(errmsg)
		}
	} else {
		ocrConfig := s.Config.ThirdParty.OCRService
		url := ocrConfig.URL
		s.HTTPClient.SetConfig(ocrConfig.RetryCount, ocrConfig.Timeout)
		now3 := time.Now()
		respBody, err = s.HTTPClient.PostMultipart(ctx, url, data, file, request.Picture.Filename)
		fmt.Printf("output: %v, %v\n", "end3", time.Since(now3))
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				result = metrics.ResultRequestTimeout
				return nil, fmt.Errorf("%w: %v", ErrUpstreamTimeout, err)
			}
			if errors.Is(err, context.Canceled) {
				result = metrics.ResultContextCanceled
				return nil, fmt.Errorf("%w: %v", ErrUpstreamUnavailable, err)
			}
			result = metrics.ResultDialError
			return nil, fmt.Errorf("%w: %v", ErrUpstreamUnavailable, err)
		}
		httpStatusCode = "200"
	}

	logger.GetLogger().Infof("callOCRService resp: %v", string(respBody))

	codeStr, ok := httpclient.ExtractTopLevelCode(respBody)
	metrics.RecordDependencyCallCode(ctx, "ocr_service", "recognize", ok, time.Since(startTime), codeStr)

	var ocrResp OCRResponse
	if err := json.Unmarshal(respBody, &ocrResp); err != nil {
		result = metrics.ResultResponseUnmarshalFailed
		return nil, fmt.Errorf("failed to parse OCR response: %w, %s", err, respBody)
	}
	if ocrResp.Code != 0 {
		result = metrics.ResultBusinessFailed
	}
	return &ocrResp, nil
}
