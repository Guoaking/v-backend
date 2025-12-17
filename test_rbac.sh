#!/bin/bash

# RBAC系统测试脚本
# 测试超级管理员 vs 组织管理员的权限区分

BASE_URL="http://localhost:8082/api/v1"
SUPER_ADMIN_TOKEN="eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VySUQiOiIxIiwiZW1haWwiOiJhZG1pbkBleGFtcGxlLmNvbSIsInJvbGUiOiJhZG1pbiIsIm9yZ0lEIjoiMSIsIm9yZ1JvbGUiOiJvd25lciIsImV4cCI6MTczNTY4OTYwMH0.dGVzdA"  # 模拟超级管理员token
ORG_ADMIN_TOKEN="eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VySUQiOiIyIiwiZW1haWwiOiJ1c2VyQGV4YW1wbGUuY29tIiwicm9sZSI6InVzZXIiLCJvcmdJRCI6IjEiLCJvcmdSb2xlIjoib3duZXIiLCJleHAiOjE3MzU2ODk2MDB9.dGVzdA"    # 模拟组织管理员token
REGULAR_USER_TOKEN="eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VySUQiOiIzIiwiZW1haWwiOiJ1c2VyMkBleGFtcGxlLmNvbSIsInJvbGUiOiJ1c2VyIiwib3JnSUQiOiIxIiwib3JnUm9sZSI6ImVkaXRvciIsImV4cCI6MTczNTY4OTYwMH0.dGVzdA" # 模拟普通用户token

echo "=== RBAC系统测试 ==="
echo

# 测试超级管理员访问权限
echo "1. 测试超级管理员访问 /admin/users"
curl -s -X GET "$BASE_URL/admin/users" \
  -H "Authorization: Bearer $SUPER_ADMIN_TOKEN" \
  -H "Content-Type: application/json" | jq .

echo
echo "2. 测试超级管理员访问 /admin/organizations"
curl -s -X GET "$BASE_URL/admin/organizations" \
  -H "Authorization: Bearer $SUPER_ADMIN_TOKEN" \
  -H "Content-Type: application/json" | jq .

echo
echo "3. 测试超级管理员访问 /admin/audit-logs"
curl -s -X GET "$BASE_URL/admin/audit-logs" \
  -H "Authorization: Bearer $SUPER_ADMIN_TOKEN" \
  -H "Content-Type: application/json" | jq .

echo
echo "4. 测试组织管理员访问 /orgs/current"
curl -s -X GET "$BASE_URL/orgs/current" \
  -H "Authorization: Bearer $ORG_ADMIN_TOKEN" \
  -H "Content-Type: application/json" | jq .

echo
echo "5. 测试组织管理员访问 /orgs/members"
curl -s -X GET "$BASE_URL/orgs/members" \
  -H "Authorization: Bearer $ORG_ADMIN_TOKEN" \
  -H "Content-Type: application/json" | jq .

echo
echo "6. 测试组织管理员邀请成员（需要owner权限）"
curl -s -X POST "$BASE_URL/orgs/invite" \
  -H "Authorization: Bearer $ORG_ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "email": "newuser@example.com",
    "role": "editor"
  }' | jq .

echo
echo "7. 测试普通用户访问组织信息（editor权限）"
curl -s -X GET "$BASE_URL/orgs/current" \
  -H "Authorization: Bearer $REGULAR_USER_TOKEN" \
  -H "Content-Type: application/json" | jq .

echo
echo "8. 测试普通用户尝试邀请成员（应该失败）"
curl -s -X POST "$BASE_URL/orgs/invite" \
  -H "Authorization: Bearer $REGULAR_USER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "email": "newuser2@example.com",
    "role": "editor"
  }' | jq .

echo
echo "9. 测试普通用户访问超级管理员接口（应该失败）"
curl -s -X GET "$BASE_URL/admin/users" \
  -H "Authorization: Bearer $REGULAR_USER_TOKEN" \
  -H "Content-Type: application/json" | jq .

echo
echo "=== 测试完成 ==="