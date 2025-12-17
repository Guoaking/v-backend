#!/bin/bash

# 测试分页接口功能
BASE_URL="http://localhost:8082/api/v1"
SUPER_ADMIN_TOKEN="eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VySUQiOiIxIiwiZW1haWwiOiJhZG1pbkBleGFtcGxlLmNvbSIsInJvbGUiOiJhZG1pbiIsIm9yZ0lEIjoiMSIsIm9yZ1JvbGUiOiJvd25lciIsImV4cCI6MTczNTY4OTYwMH0.dGVzdA"

echo "=== 测试分页接口功能 ==="
echo

echo "1. 测试用户列表分页 - 默认参数"
curl -s -X GET "$BASE_URL/admin/users" \
  -H "Authorization: Bearer $SUPER_ADMIN_TOKEN" \
  -H "Content-Type: application/json" | jq .

echo
echo "2. 测试用户列表分页 - 指定page=2&limit=5"
curl -s -X GET "$BASE_URL/admin/users?page=2&limit=5" \
  -H "Authorization: Bearer $SUPER_ADMIN_TOKEN" \
  -H "Content-Type: application/json" | jq .

echo
echo "3. 测试用户列表分页 - 指定offset&limit"
curl -s -X GET "$BASE_URL/admin/users?offset=10&limit=3" \
  -H "Authorization: Bearer $SUPER_ADMIN_TOKEN" \
  -H "Content-Type: application/json" | jq .

echo
echo "4. 测试组织列表分页 - 默认参数"
curl -s -X GET "$BASE_URL/admin/organizations" \
  -H "Authorization: Bearer $SUPER_ADMIN_TOKEN" \
  -H "Content-Type: application/json" | jq .

echo
echo "5. 测试组织列表分页 - 指定page=1&limit=2"
curl -s -X GET "$BASE_URL/admin/organizations?page=1&limit=2" \
  -H "Authorization: Bearer $SUPER_ADMIN_TOKEN" \
  -H "Content-Type: application/json" | jq .

echo
echo "6. 测试审计日志分页 - 默认参数"
curl -s -X GET "$BASE_URL/admin/audit-logs" \
  -H "Authorization: Bearer $SUPER_ADMIN_TOKEN" \
  -H "Content-Type: application/json" | jq .

echo
echo "7. 测试审计日志分页 - 指定page=1&limit=10"
curl -s -X GET "$BASE_URL/admin/audit-logs?page=1&limit=10" \
  -H "Authorization: Bearer $SUPER_ADMIN_TOKEN" \
  -H "Content-Type: application/json" | jq .

echo
echo "=== 测试完成 ==="