#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")"/.. && pwd)"
cd "$ROOT_DIR"

if ! command -v swag >/dev/null 2>&1; then
  echo "swag not found, installing..."
  go install github.com/swaggo/swag/cmd/swag@latest
fi

swag init -g cmd/server/main.go -o docs
echo "swagger.json generated at docs/swagger.json"
rm -f docs/docs.go

