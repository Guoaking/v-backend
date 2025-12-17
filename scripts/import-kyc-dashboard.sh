#!/usr/bin/env bash
set -euo pipefail

GRAFANA_URL=${GRAFANA_URL:-http://localhost:3000}
GRAFANA_USER=${GRAFANA_USER:-admin}
GRAFANA_PASS=${GRAFANA_PASS:-admin123}

DASH_JSON_PATH=${DASH_JSON_PATH:-grafana/dashboards/kyc_overview.json}

PAYLOAD=$(jq -n --argjson dash "$(cat "$DASH_JSON_PATH")" '{dashboard: $dash, overwrite: true, folderId: 0}')

curl -s -u "$GRAFANA_USER:$GRAFANA_PASS" -H "Content-Type: application/json" \
  -X POST "$GRAFANA_URL/api/dashboards/db" -d "$PAYLOAD" | jq .

echo "Imported dashboard to $GRAFANA_URL"
