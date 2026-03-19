#!/bin/bash

# A simple script to help create the files for refactoring
cd /Users/bytedance/Documents/project/go/v/v-backend/internal/api

touch console_user_handler.go console_apikey_handler.go console_quota_handler.go console_log_handler.go
touch admin_user_handler.go admin_org_handler.go admin_log_handler.go admin_config_handler.go admin_permission_handler.go

cd ../service
touch third_party_ocr_service.go third_party_face_service.go third_party_liveness_service.go

echo "Files created."
