#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
INTERVAL="${INTERVAL:-1}"

echo "Sending traffic to $BASE_URL every ${INTERVAL}s — press Ctrl+C to stop"

send() {
  local scenario="$1"
  local amount="$2"
  local result
  result=$(curl -sf -X POST "$BASE_URL/api/payments" \
    -H "Content-Type: application/json" \
    -d "{\"merchant_id\":\"merchant_demo\",\"amount\":$amount,\"currency\":\"INR\",\"scenario\":\"$scenario\"}" \
    2>/dev/null | jq -c '{status: .status, payment_id: .payment_id, duration_ms: .duration_ms}' 2>/dev/null || echo '{"status":"error"}')
  echo "$(date +%H:%M:%S) [$scenario] $result"
}

while true; do
  send "success"  1000
  send "fraud"    2500
  send "failure"  3000
  send "slow"     5000
  sleep "$INTERVAL"
done
