package service

import "context"

type ocrProc func(ctx context.Context, s *KYCService, req *OCRRequest) (*OCRResponse, error)

func ocrGetProcessor(name string) ocrProc {
	switch name {
	case "id_card", "driver_license", "vehicle_license", "bank_card", "business_license", "general":
		fallthrough
	case "vat_certificate":
		fallthrough
	case "NPWP":
		return func(ctx context.Context, s *KYCService, req *OCRRequest) (*OCRResponse, error) {
			return s.callOCRService(ctx, req)
		}
	default:
		return nil
	}
}
