#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")"/.. && pwd)"
cd "$ROOT_DIR"

if ! command -v swag >/dev/null 2>&1; then
  echo "swag not found, installing..."
  go install github.com/swaggo/swag/cmd/swag@latest
fi

# 排除内部/管理端/控制台相关 Handler，仅生成对外业务 API（如 KYC）
EXCLUDES=(
  internal/api/admin_handler.go
  internal/api/console_handler.go
  internal/api/console_auth_handler.go
  internal/api/user_auth_handler.go
  internal/api/api_key_handler.go
  internal/api/organization_handler.go
  internal/api/password_reset_handler.go
  internal/api/webhook_handler.go
  internal/api/meta_handler.go
  internal/api/jwt_generator.go
  internal/api/google_oauth_handler.go
)

EXCLUDE_ARGS=()
for f in "${EXCLUDES[@]}"; do
  EXCLUDE_ARGS+=("--exclude" "$f")
done

swag init -g cmd/server/main.go -o docs/public "${EXCLUDE_ARGS[@]}"
if command -v jq >/dev/null 2>&1; then
  tmp="docs/public/swagger.tmp.json"
  cp docs/public/swagger.json "$tmp"
  jq '.paths |= with_entries(
        .value |= with_entries(
          select((.value|type=="object") and (((.value.tags // []) | index("Public"))))
        )
      )
      | .paths |= with_entries(select(.value | length > 0))' "$tmp" > docs/public/swagger.json
  rm -f "$tmp"
fi
echo "swagger-public.json generated at docs/public/swagger.json (filtered by @Tags Public if jq present)"
rm -f docs/public/docs.go
