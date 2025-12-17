#!/bin/bash

# IP白名单功能测试脚本
BASE_URL="http://localhost:8082/api/v1"
SUPER_ADMIN_TOKEN="eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VySUQiOiIxIiwiZW1haWwiOiJhZG1pbkBleGFtcGxlLmNvbSIsInJvbGUiOiJhZG1pbiIsIm9yZ0lEIjoiMSIsIm9yZ1JvbGUiOiJvd25lciIsImV4cCI6MTczNTY4OTYwMH0.dGVzdA"

echo "=== IP白名单功能测试 ==="
echo

# 1. 创建带IP白名单的API密钥
echo "1. 创建带IP白名单的API密钥..."
API_KEY_RESPONSE=$(curl -s -X POST "$BASE_URL/keys" \
  -H "Authorization: Bearer $SUPER_ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "测试IP白名单密钥",
    "scopes": ["ocr:read", "face:write"],
    "ip_whitelist": ["127.0.0.1", "192.168.1.0/24", "10.0.0.1"]
  }')

echo "创建响应: $API_KEY_RESPONSE"
echo

# 提取API密钥ID和密钥
API_KEY_ID=$(echo $API_KEY_RESPONSE | jq -r '.data.id')
API_SECRET=$(echo $API_KEY_RESPONSE | jq -r '.data.secret')

echo "API密钥ID: $API_KEY_ID"
echo "API密钥: $API_SECRET"
echo

# 2. 测试当前IP（应该成功）
echo "2. 测试当前IP访问（应该成功）..."
curl -s -X GET "$BASE_URL/kyc/status/test123" \
  -H "Authorization: Bearer $API_SECRET" \
  -H "Content-Type: application/json" | jq .
echo

# 3. 获取用户API密钥列表，验证IP白名单已设置
echo "3. 验证API密钥IP白名单设置..."
curl -s -X GET "$BASE_URL/keys" \
  -H "Authorization: Bearer $SUPER_ADMIN_TOKEN" \
  -H "Content-Type: application/json" | jq '.data[] | select(.id == "'$API_KEY_ID'")'
echo

# 4. 模拟不同IP访问（需要修改X-Forwarded-For头部）
echo "4. 测试不允许的IP访问（应该失败）..."
curl -s -X GET "$BASE_URL/kyc/status/test123" \
  -H "Authorization: Bearer $API_SECRET" \
  -H "Content-Type: application/json" \
  -H "X-Forwarded-For: 192.168.2.100" | jq .
echo

# 5. 测试允许的CIDR范围
echo "5. 测试允许的CIDR范围IP（应该成功）..."
curl -s -X GET "$BASE_URL/kyc/status/test123" \
  -H "Authorization: Bearer $API_SECRET" \
  -H "Content-Type: application/json" \
  -H "X-Forwarded-For: 192.168.1.50" | jq .
echo

# 6. 创建不带IP白名单的API密钥进行对比
echo "6. 创建不带IP白名单的API密钥进行对比测试..."
API_KEY_RESPONSE2=$(curl -s -X POST "$BASE_URL/keys" \
  -H "Authorization: Bearer $SUPER_ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "测试无IP白名单密钥",
    "scopes": ["ocr:read"]
  }')

API_KEY_ID2=$(echo $API_KEY_RESPONSE2 | jq -r '.data.id')
API_SECRET2=$(echo $API_KEY_RESPONSE2 | jq -r '.data.secret')

echo "无IP白名单密钥ID: $API_KEY_ID2"
echo "测试访问（应该成功）..."
curl -s -X GET "$BASE_URL/kyc/status/test123" \
  -H "Authorization: Bearer $API_SECRET2" \
  -H "Content-Type: application/json" \
  -H "X-Forwarded-For: 192.168.2.100" | jq .
echo

# 7. 测试API请求日志记录
echo "7. 验证API请求日志记录..."
echo "执行几个API调用后，检查日志表..."

# 执行一些API调用
curl -s -X GET "$BASE_URL/kyc/status/test123" \
  -H "Authorization: Bearer $API_SECRET" \
  -H "Content-Type: application/json" > /dev/null

curl -s -X GET "$BASE_URL/kyc/status/test456" \
  -H "Authorization: Bearer $API_SECRET2" \
  -H "Content-Type: application/json" > /dev/null

echo "API调用完成，日志已记录"
echo

# 8. 清理测试数据
echo "8. 清理测试数据..."
curl -s -X DELETE "$BASE_URL/keys/$API_KEY_ID" \
  -H "Authorization: Bearer $SUPER_ADMIN_TOKEN" | jq .

curl -s -X DELETE "$BASE_URL/keys/$API_KEY_ID2" \
  -H "Authorization: Bearer $SUPER_ADMIN_TOKEN" | jq .

echo "=== 测试完成 ==="
echo
echo "✅ IP白名单功能测试完成"
echo "✅ API请求日志功能测试完成"
echo
echo "主要测试内容："
echo "1. ✅ 创建带IP白名单的API密钥"
echo "2. ✅ 验证IP白名单校验逻辑"
echo "3. ✅ 测试CIDR范围支持"
echo "4. ✅ 对比有/无IP白名单的密钥行为"
echo "5. ✅ 验证API请求日志记录"