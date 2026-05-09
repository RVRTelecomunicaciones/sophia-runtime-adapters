#!/usr/bin/env bash
#
# ops/smoke/logs-smoke.sh — Phase 2C.4 / D B3 logs smoke target.
#
# End-to-end: bring up base + logs overlay, emit a marker record,
# poll Loki HTTP API until the record appears, assert label set
# matches D2C4D.8.
#
# Operator-driven (NOT a CI gate). Pre-merge confirmation that the
# Loki ingestion path works end-to-end.
#
# Exit codes:
#   0 — record reached Loki within timeout, label set ok
#   1 — record did not arrive, or label set violated allowlist
#   2 — preflight failure (compose up failed, etc.)

set -euo pipefail

LOKI_URL="${LOKI_URL:-http://localhost:3100}"
RUNTIME_URL="${RUNTIME_URL:-http://localhost:8080}"
TIMEOUT_SECONDS="${TIMEOUT_SECONDS:-60}"

log_info() { printf '[%s] [INFO]  %s\n'  "$(date -u +%FT%TZ)" "$*" >&2; }
log_err()  { printf '[%s] [ERROR] %s\n'  "$(date -u +%FT%TZ)" "$*" >&2; }

# 1. Bring stack up.
log_info "bringing up base + logs overlay"
docker compose -f ops/local/compose.yaml -f ops/local/compose.logs.yaml up -d --wait \
    || { log_err "compose up failed"; exit 2; }

cleanup() {
    log_info "tearing down stack"
    docker compose -f ops/local/compose.yaml -f ops/local/compose.logs.yaml down >/dev/null 2>&1 || true
}
trap cleanup EXIT

# 2. Generate a unique marker per run.
MARKER="logs-smoke-$(date +%s)-$RANDOM"
log_info "marker=$MARKER"

# 3. Trigger a runtime log emission. POST a structurally-failing
# request; the runtime emits an INFO log carrying our correlation_id
# (which contains the marker — but the marker is also part of the
# free-form payload so | json filter finds it).
log_info "POST /api/v1/execute to runtime"
curl -sSf -X POST "$RUNTIME_URL/api/v1/execute" \
    -H 'Content-Type: application/json' \
    --data "{
        \"correlation_id\": \"$(printf '01%024s' "$RANDOM" | tr ' ' 'K' | head -c 26)\",
        \"adapter_id\": \"shell\",
        \"capability_name\": \"$MARKER\",
        \"capability_version\": \"v1\",
        \"payload\": {},
        \"timeout_budget_ms\": 1000
    }" >/dev/null 2>&1 || true

# 4. Poll Loki for the marker.
log_info "polling Loki for marker (timeout ${TIMEOUT_SECONDS}s)"
deadline=$(( $(date +%s) + TIMEOUT_SECONDS ))
while [ "$(date +%s)" -lt "$deadline" ]; do
    end_ns=$(date +%s%N)
    start_ns=$(( end_ns - 60000000000 ))   # last 60s
    resp=$(curl -sSf -G "$LOKI_URL/loki/api/v1/query_range" \
        --data-urlencode "query={service_name=\"runtime-adapters\"} |= \"$MARKER\"" \
        --data-urlencode "start=$start_ns" \
        --data-urlencode "end=$end_ns" || echo "")
    count=$(printf '%s' "$resp" | jq '[.data.result[].values[]?] | length' 2>/dev/null || echo 0)
    if [ "${count:-0}" -gt 0 ]; then
        log_info "Loki has $count record(s) matching the marker"

        # 5. Assert label set: only allowlist keys in stream labels.
        keys=$(printf '%s' "$resp" | jq -r '[.data.result[].stream | keys[]] | unique | .[]')
        bad=""
        for k in $keys; do
            case "$k" in
                service|service_name|service_namespace|level|environment|tenant_type|detected_level) ;;
                *) bad="$bad $k" ;;
            esac
        done
        if [ -n "$bad" ]; then
            log_err "forbidden Loki label(s) found:$bad (D2C4D.8)"
            exit 1
        fi
        log_info "PASS — labels=$keys"
        exit 0
    fi
    sleep 2
done

log_err "FAIL — marker not in Loki within ${TIMEOUT_SECONDS}s"
exit 1
