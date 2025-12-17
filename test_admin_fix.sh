#!/bin/bash

# 测试修复后的管理员接口
BASE_URL="http://localhost:8082/api/v1"
SUPER_ADMIN_TOKEN="eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VySUQiOiIxIiwiZW1haWwiOiJhZG1pbkBleGFtcGxlLmNvbSIsInJvbGUiOiJhZG1pbiIsIm9yZ0lEIjoiMSIsIm9yZ1JvbGUiOiJvd25lciIsImV4cCI6MTczNTY4OTYwMH0.dGVzdA"

echo "=== 测试修复后的管理员接口 ==="
echo

echo "1. 测试管理员组织列表接口"
curl -s -X GET "$BASE_URL/admin/organizations" \
  -H "Authorization: Bearer $SUPER_ADMIN_TOKEN" \
  -H "Content-Type: application/json" | jq .

echo
echo "2. 测试管理员审计日志接口"
curl -s -X GET "$BASE_URL/admin/audit-logs" \
  -H "Authorization: Bearer $SUPER_ADMIN_TOKEN" \
  -H "Content-Type: application/json" | jq .

echo
echo "=== 测试完成 ==="