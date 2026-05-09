#!/usr/bin/env bash
#
# ops/smoke/annotations-smoke.sh — Phase 2C.4 / D B3 annotations smoke.
#
# Bypasses alertmanager (the alertmanager → adapter path is covered by
# the extended smoke-receivers in B3.9). This script POSTs directly to
# the adapter's /webhook, then queries Grafana's annotations API.
#
# Required env (from .env):
#   GRAFANA_TEST_URL                           e.g. http://localhost:3000
#   GRAFANA_TEST_SERVICE_ACCOUNT_TOKEN
#
# Exit codes:
#   0 — annotation visible in Grafana with expected tags
#   1 — verification failure
#   2 — preflight failure

set -euo pipefail

ADAPTER_URL="${ADAPTER_URL:-http://localhost:9096}"
GRAFANA_URL="${GRAFANA_TEST_URL:-http://localhost:3000}"
TOKEN="${GRAFANA_TEST_SERVICE_ACCOUNT_TOKEN:-}"
TIMEOUT_SECONDS="${TIMEOUT_SECONDS:-30}"

log_info() { printf '[%s] [INFO]  %s\n'  "$(date -u +%FT%TZ)" "$*" >&2; }
log_err()  { printf '[%s] [ERROR] %s\n'  "$(date -u +%FT%TZ)" "$*" >&2; }

[ -z "$TOKEN" ] && { log_err "GRAFANA_TEST_SERVICE_ACCOUNT_TOKEN is required"; exit 2; }

log_info "bringing up base + receivers + logs"
docker compose \
    -f ops/local/compose.yaml \
    -f ops/local/compose.receivers.yaml \
    -f ops/local/compose.logs.yaml \
    up -d --wait grafana-annotations-webhook grafana \
    || { log_err "compose up failed"; exit 2; }

cleanup() {
    log_info "tearing down stack"
    docker compose -f ops/local/compose.yaml -f ops/local/compose.receivers.yaml -f ops/local/compose.logs.yaml down >/dev/null 2>&1 || true
}
trap cleanup EXIT

# Marker derived from timestamp so we can filter Grafana later by tag.
MARKER_NAME="AnnotationsSmoke$(date +%s)"
NOW_MS=$(($(date -u +%s) * 1000))
log_info "marker=$MARKER_NAME"

# 1. POST synthetic alertmanager v4 payload to adapter.
log_info "POST $ADAPTER_URL/webhook"
curl -sSf -X POST "$ADAPTER_URL/webhook" \
    -H 'Content-Type: application/json' \
    --data "{
        \"receiver\": \"ops-critical\",
        \"status\": \"firing\",
        \"alerts\": [{
            \"status\": \"firing\",
            \"labels\": {\"alertname\":\"$MARKER_NAME\",\"severity\":\"critical\"},
            \"annotations\": {\"summary\":\"end-to-end annotations smoke\"},
            \"startsAt\": \"$(date -u +%FT%TZ)\"
        }]
    }" >/dev/null || { log_err "adapter POST failed"; exit 1; }

# 2. Query Grafana annotations API by tag.
log_info "querying Grafana annotations API"
deadline=$(( $(date +%s) + TIMEOUT_SECONDS ))
while [ "$(date +%s)" -lt "$deadline" ]; do
    from_ms=$(( NOW_MS - 60000 ))
    to_ms=$(( NOW_MS + 60000 ))
    annotations=$(curl -sSf -H "Authorization: Bearer $TOKEN" \
        "$GRAFANA_URL/api/annotations?from=$from_ms&to=$to_ms&tags=$MARKER_NAME" || echo "[]")
    count=$(printf '%s' "$annotations" | jq 'length' 2>/dev/null || echo 0)
    if [ "${count:-0}" -gt 0 ]; then
        log_info "Grafana has $count annotation(s) matching tag=$MARKER_NAME"
        log_info "PASS"
        exit 0
    fi
    sleep 2
done

log_err "FAIL — annotation not visible in Grafana within ${TIMEOUT_SECONDS}s"
exit 1
