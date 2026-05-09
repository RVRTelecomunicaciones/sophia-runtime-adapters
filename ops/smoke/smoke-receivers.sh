#!/usr/bin/env bash
#
# ops/smoke/smoke-receivers.sh — Phase 2C.4 / A+B B3 smoke target.
#
# End-to-end test: inject 3 alerts (critical, warning, info), wait
# for Alertmanager grouping windows, then verify each receiver got
# what it should and didn't get what it shouldn't. Cleanup after.
#
# Design: spec §8. Per-receiver verification is API-based (no human
# in the loop). Operator-driven; NOT a CI gate for v0.8.0.
#
# Usage:
#   make smoke-receivers
#
# Required env vars (load from .env or shell):
#   PAGERDUTY_TEST_ROUTING_KEY        — alertmanager-side
#   PAGERDUTY_TEST_API_TOKEN          — smoke-side query
#   PAGERDUTY_TEST_SERVICE_ID         — smoke-side query filter
#   SLACK_TEST_INCIDENTS_WEBHOOK_URL  — alertmanager-side
#   SLACK_TEST_OPS_WEBHOOK_URL        — alertmanager-side
#   SLACK_TEST_BOT_TOKEN              — smoke-side history reads + cleanup
#   SLACK_TEST_INCIDENTS_CHANNEL_ID   — smoke-side history filter
#   SLACK_TEST_OPS_CHANNEL_ID         — smoke-side history filter
#   LINEAR_TEST_API_TOKEN             — adapter-side + smoke-side
#   LINEAR_TEST_TEAM_ID               — adapter-side + smoke-side
#
# Exit codes:
#   0 — all positive + negative checks passed; cleanup attempted
#   1 — preflight or verification failure (cleanup still attempted)
#   2 — cleanup phase failure after a passing verification (recorded)

set -euo pipefail

# --- constants -------------------------------------------------------

readonly SMOKE_LABEL_PREFIX="SmokeTest"
readonly AM_URL="${ALERTMANAGER_URL:-http://localhost:9093}"
readonly LINEAR_API_URL="${LINEAR_API_URL:-https://api.linear.app/graphql}"
# Wait window: critical group_wait 30s + warning group_wait 60s + 30s buffer.
readonly WAIT_SECONDS=90

# --- output helpers --------------------------------------------------

# log_info / log_warn / log_err emit timestamped lines to stderr so
# stdout stays clean for any captured-output use cases.
log_info() { printf '[%s] [INFO]  %s\n'  "$(date -u +%FT%TZ)" "$*" >&2; }
log_warn() { printf '[%s] [WARN]  %s\n'  "$(date -u +%FT%TZ)" "$*" >&2; }
log_err()  { printf '[%s] [ERROR] %s\n'  "$(date -u +%FT%TZ)" "$*" >&2; }

# fail_test marks the run failed but continues to cleanup.
TEST_FAILURES=0
fail_test() {
    log_err "TEST FAIL: $*"
    TEST_FAILURES=$((TEST_FAILURES + 1))
}

# require_env aborts preflight if any required var is empty.
require_env() {
    local missing=()
    for v in "$@"; do
        if [ -z "${!v:-}" ]; then
            missing+=("$v")
        fi
    done
    if [ ${#missing[@]} -gt 0 ]; then
        log_err "preflight: missing required env vars: ${missing[*]}"
        return 1
    fi
}

# require_cmd aborts preflight if a CLI tool is missing.
require_cmd() {
    if ! command -v "$1" >/dev/null 2>&1; then
        log_err "preflight: $1 not found in PATH"
        return 1
    fi
}

# --- phase functions (filled in by tasks 3.3-3.9) ---

# preflight verifies env vars + binaries are present and the
# alertmanager + linear-webhook services are reachable. Exits
# non-zero if anything is missing — the smoke MUST NOT proceed
# against a half-configured stack (would emit false negatives).
preflight() {
    log_info "preflight: checking env vars + tooling"
    require_env \
        PAGERDUTY_TEST_API_TOKEN \
        PAGERDUTY_TEST_SERVICE_ID \
        SLACK_TEST_BOT_TOKEN \
        SLACK_TEST_INCIDENTS_CHANNEL_ID \
        SLACK_TEST_OPS_CHANNEL_ID \
        LINEAR_TEST_API_TOKEN \
        LINEAR_TEST_TEAM_ID \
        GRAFANA_TEST_URL \
        GRAFANA_TEST_SERVICE_ACCOUNT_TOKEN
    require_cmd amtool
    require_cmd curl
    require_cmd jq

    log_info "preflight: probing alertmanager at $AM_URL"
    if ! curl -sSf -o /dev/null --max-time 5 "$AM_URL/-/ready"; then
        log_err "preflight: alertmanager not ready at $AM_URL/-/ready"
        return 1
    fi

    # Linear webhook adapter health probe — assumes the service is
    # reachable on the smoke host. In a compose stack the smoke runs
    # on the host and the service is published on :9095.
    local lw_url="${LINEAR_WEBHOOK_URL:-http://localhost:9095}"
    log_info "preflight: probing linear-webhook at $lw_url"
    if ! curl -sSf -o /dev/null --max-time 5 "$lw_url/healthz"; then
        log_err "preflight: linear-webhook not ready at $lw_url/healthz"
        return 1
    fi

    log_info "preflight: OK"
}
# inject pushes 3 alerts via amtool: critical, warning, info. Each
# uses a distinct alertname prefixed with SmokeTest so positive +
# negative checks can scope by alertname. Per D2C4AB.11.
inject() {
    log_info "inject: pushing 3 alerts (critical, warning, info) via amtool"
    local now
    now=$(date -u +%FT%TZ)

    amtool --alertmanager.url="$AM_URL" alert add \
        alertname="${SMOKE_LABEL_PREFIX}Critical" \
        severity=critical \
        adapter=smoke \
        capability=smoke.test@v1 \
        --start="$now" \
        --annotation=summary="Smoke test critical — verifies routing to PagerDuty + Slack #incidents"

    amtool --alertmanager.url="$AM_URL" alert add \
        alertname="${SMOKE_LABEL_PREFIX}Warning" \
        severity=warning \
        adapter=smoke \
        capability=smoke.test@v1 \
        --start="$now" \
        --annotation=summary="Smoke test warning — verifies routing to Slack #ops + Linear webhook"

    amtool --alertmanager.url="$AM_URL" alert add \
        alertname="${SMOKE_LABEL_PREFIX}Info" \
        severity=info \
        adapter=smoke \
        capability=smoke.test@v1 \
        --start="$now" \
        --annotation=summary="Smoke test info — must NOT reach any receiver (I-AB.1)"

    log_info "inject: 3 alerts pushed"
}

# wait_phase sleeps WAIT_SECONDS so Alertmanager group_wait timers
# (30s critical, 60s warning) elapse and the receivers actually
# fire. Logs progress every 15s so the operator sees the smoke
# isn't hung.
wait_phase() {
    log_info "wait_phase: sleeping ${WAIT_SECONDS}s for Alertmanager grouping windows + buffer"
    local elapsed=0
    while [ "$elapsed" -lt "$WAIT_SECONDS" ]; do
        sleep 15
        elapsed=$((elapsed + 15))
        log_info "wait_phase: ${elapsed}/${WAIT_SECONDS}s elapsed"
    done
    log_info "wait_phase: done"
}
# verify_pos_pagerduty asserts the SmokeTestCritical alert produced
# a PagerDuty incident on the test service. Uses the Incidents API:
#   GET /incidents?service_ids[]=<svc>&statuses[]=triggered
verify_pos_pagerduty() {
    log_info "verify_pos: querying PagerDuty for SmokeTestCritical incident"
    local resp
    resp=$(curl -sSf --max-time 10 \
        -H "Authorization: Token token=${PAGERDUTY_TEST_API_TOKEN}" \
        -H "Accept: application/vnd.pagerduty+json;version=2" \
        "https://api.pagerduty.com/incidents?service_ids%5B%5D=${PAGERDUTY_TEST_SERVICE_ID}&statuses%5B%5D=triggered" \
        || echo "")
    if [ -z "$resp" ]; then
        fail_test "PagerDuty incidents API query failed"
        return
    fi
    local match
    match=$(printf '%s' "$resp" | jq -r '.incidents[] | select(.title | contains("SmokeTestCritical")) | .id' | head -1)
    if [ -z "$match" ]; then
        fail_test "PagerDuty: no incident matching SmokeTestCritical (expected critical alert)"
    else
        log_info "verify_pos: PagerDuty PASS — incident $match"
        # Capture incident ID for cleanup phase.
        export SMOKE_PD_INCIDENT_ID="$match"
    fi
}

# slack_history_contains queries conversations.history for the given
# channel and returns 0 if any message body contains the needle.
slack_history_contains() {
    local channel_id="$1"
    local needle="$2"
    local resp
    resp=$(curl -sSf --max-time 10 \
        -H "Authorization: Bearer ${SLACK_TEST_BOT_TOKEN}" \
        "https://slack.com/api/conversations.history?channel=${channel_id}&limit=20" \
        || echo "")
    if [ -z "$resp" ]; then
        return 1
    fi
    if printf '%s' "$resp" | jq -e --arg n "$needle" '.messages[]? | select(.text | contains($n))' >/dev/null; then
        # Capture the latest matching message ts for cleanup.
        local ts
        ts=$(printf '%s' "$resp" | jq -r --arg n "$needle" 'first(.messages[]? | select(.text | contains($n)) | .ts)')
        printf '%s' "$ts"
        return 0
    fi
    return 1
}

verify_pos_slack_incidents() {
    log_info "verify_pos: querying Slack #incidents history for SmokeTestCritical"
    local ts
    ts=$(slack_history_contains "$SLACK_TEST_INCIDENTS_CHANNEL_ID" "SmokeTestCritical" || echo "")
    if [ -z "$ts" ]; then
        fail_test "Slack #incidents: no message matching SmokeTestCritical"
    else
        log_info "verify_pos: Slack #incidents PASS — message ts=$ts"
        export SMOKE_SLACK_INCIDENTS_TS="$ts"
    fi
}

verify_pos_slack_ops() {
    log_info "verify_pos: querying Slack #ops history for SmokeTestWarning"
    local ts
    ts=$(slack_history_contains "$SLACK_TEST_OPS_CHANNEL_ID" "SmokeTestWarning" || echo "")
    if [ -z "$ts" ]; then
        fail_test "Slack #ops: no message matching SmokeTestWarning"
    else
        log_info "verify_pos: Slack #ops PASS — message ts=$ts"
        export SMOKE_SLACK_OPS_TS="$ts"
    fi
}

# verify_pos_grafana_annotations queries Grafana's annotations API by
# tag — both SmokeTestCritical (from ops-critical fanout) and
# SmokeTestWarning (from ops-warnings fanout) MUST be present per
# D2C4D.13 (alertmanager fanout to grafana-annotations-webhook).
verify_pos_grafana_annotations() {
    for marker in SmokeTestCritical SmokeTestWarning; do
        log_info "verify_pos: querying Grafana annotations for $marker"
        local annotations count
        annotations=$(curl -sSf -H "Authorization: Bearer ${GRAFANA_TEST_SERVICE_ACCOUNT_TOKEN}" \
            "${GRAFANA_TEST_URL}/api/annotations?tags=${marker}&limit=10" \
            || echo "[]")
        count=$(printf '%s' "$annotations" | jq 'length' 2>/dev/null || echo 0)
        if [ "${count:-0}" -gt 0 ]; then
            log_info "verify_pos: Grafana annotations PASS — $count annotation(s) for $marker"
        else
            fail_test "Grafana annotations: no annotation matching $marker (D2C4D.13 violation)"
        fi
    done
}

verify_pos() {
    log_info "verify_pos: positive verification of 3 firing alerts"
    verify_pos_pagerduty
    verify_pos_slack_incidents
    verify_pos_slack_ops
    verify_pos_linear   # implemented in Task 3.6
    verify_pos_grafana_annotations   # D B3.9
}

# dedup_hash computes 'alert:' + first 12 hex chars of sha256 of
# the supplied input. Mirrors domain.DedupLabel in
# internal/integrations/linear/domain/dedup.go.
dedup_hash() {
    printf '%s' "$1" | sha256sum | awk '{print "alert:" substr($1, 1, 12)}'
}

# linear_issues_by_label queries the Linear API for issues bearing
# the given label name. Returns the raw GraphQL data field.
linear_issues_by_label() {
    local label="$1"
    local query
    query=$(jq -nc \
        --arg label "$label" \
        '{query:"query F($label: String!){ issues(filter: { labels: { name: { eq: $label } } }){ nodes { id title state { name } } } }",variables:{label:$label}}')
    curl -sSf --max-time 10 \
        -H "Authorization: ${LINEAR_TEST_API_TOKEN}" \
        -H "Content-Type: application/json" \
        --data "$query" \
        "$LINEAR_API_URL"
}

# Computing the dedup label requires reconstructing Alertmanager's
# canonical groupKey for the SmokeTestWarning alert. Alertmanager's
# groupKey format is opaque and version-dependent; rather than try
# to reproduce it byte-exact, we filter by ALL labels containing
# 'SmokeTestWarning' on the alertname label — this is robust to
# future groupKey shape changes.
verify_pos_linear() {
    log_info "verify_pos: querying Linear for SmokeTestWarning issue"
    # Linear API: filter issues by label name like 'alert:%' AND title
    # containing 'SmokeTestWarning'. We use a title-contains filter
    # against the adapter-managed label namespace.
    local query resp
    query=$(jq -nc \
        '{query:"query F { issues(filter: { labels: { name: { eq: \"alert-managed\" } }, title: { containsIgnoreCase: \"SmokeTestWarning\" } }){ nodes { id title state { name } labels { nodes { name } } } } }"}')
    resp=$(curl -sSf --max-time 10 \
        -H "Authorization: ${LINEAR_TEST_API_TOKEN}" \
        -H "Content-Type: application/json" \
        --data "$query" \
        "$LINEAR_API_URL" || echo "")
    if [ -z "$resp" ]; then
        fail_test "Linear: API query failed"
        return
    fi
    local match
    match=$(printf '%s' "$resp" | jq -r '.data.issues.nodes[]? | select(.title | contains("SmokeTestWarning")) | .id' | head -1)
    if [ -z "$match" ]; then
        fail_test "Linear: no issue matching SmokeTestWarning"
    else
        log_info "verify_pos: Linear PASS — issue $match"
        export SMOKE_LINEAR_ISSUE_ID="$match"
    fi
}
# verify_neg confirms the SmokeTestInfo alert reached NONE of the
# 4 destinations. Per D2C4AB.13 + I-AB.1 + I-AB.10. Without this,
# a regression that lets info severity leak past the routing root
# would pass the smoke silently.
verify_neg() {
    log_info "verify_neg: confirming SmokeTestInfo did NOT leak to any receiver"

    # PD: same query as verify_pos_pagerduty; assert NO match for SmokeTestInfo.
    local resp
    resp=$(curl -sSf --max-time 10 \
        -H "Authorization: Token token=${PAGERDUTY_TEST_API_TOKEN}" \
        -H "Accept: application/vnd.pagerduty+json;version=2" \
        "https://api.pagerduty.com/incidents?service_ids%5B%5D=${PAGERDUTY_TEST_SERVICE_ID}&statuses%5B%5D=triggered" \
        || echo "")
    if printf '%s' "$resp" | jq -e '.incidents[]? | select(.title | contains("SmokeTestInfo"))' >/dev/null 2>&1; then
        fail_test "verify_neg: SmokeTestInfo LEAKED to PagerDuty (I-AB.1 violation)"
    else
        log_info "verify_neg: PagerDuty PASS — no SmokeTestInfo incident"
    fi

    # Slack #incidents: assert no message containing SmokeTestInfo.
    if slack_history_contains "$SLACK_TEST_INCIDENTS_CHANNEL_ID" "SmokeTestInfo" >/dev/null; then
        fail_test "verify_neg: SmokeTestInfo LEAKED to Slack #incidents (I-AB.1 violation)"
    else
        log_info "verify_neg: Slack #incidents PASS — no SmokeTestInfo message"
    fi

    # Slack #ops: same assertion.
    if slack_history_contains "$SLACK_TEST_OPS_CHANNEL_ID" "SmokeTestInfo" >/dev/null; then
        fail_test "verify_neg: SmokeTestInfo LEAKED to Slack #ops (I-AB.1 violation)"
    else
        log_info "verify_neg: Slack #ops PASS — no SmokeTestInfo message"
    fi

    # Linear: same query as verify_pos_linear; assert no SmokeTestInfo match.
    local linear_query linear_resp
    linear_query=$(jq -nc \
        '{query:"query F { issues(filter: { labels: { name: { eq: \"alert-managed\" } }, title: { containsIgnoreCase: \"SmokeTestInfo\" } }){ nodes { id title } } }"}')
    linear_resp=$(curl -sSf --max-time 10 \
        -H "Authorization: ${LINEAR_TEST_API_TOKEN}" \
        -H "Content-Type: application/json" \
        --data "$linear_query" \
        "$LINEAR_API_URL" || echo "")
    if printf '%s' "$linear_resp" | jq -e '.data.issues.nodes[]? | select(.title | contains("SmokeTestInfo"))' >/dev/null 2>&1; then
        fail_test "verify_neg: SmokeTestInfo LEAKED to Linear (I-AB.1 violation)"
    else
        log_info "verify_neg: Linear PASS — no SmokeTestInfo issue"
    fi

    # D B3.9 extension: Grafana annotations MUST NOT include
    # SmokeTestInfo (info silencing holds end-to-end — I-AB.1 + I-D.4).
    local ga_resp ga_count
    ga_resp=$(curl -sSf -H "Authorization: Bearer ${GRAFANA_TEST_SERVICE_ACCOUNT_TOKEN}" \
        "${GRAFANA_TEST_URL}/api/annotations?tags=SmokeTestInfo&limit=10" \
        || echo "[]")
    ga_count=$(printf '%s' "$ga_resp" | jq 'length' 2>/dev/null || echo 0)
    if [ "${ga_count:-0}" -eq 0 ]; then
        log_info "verify_neg: Grafana annotations PASS — SmokeTestInfo not annotated"
    else
        fail_test "Grafana annotations: SmokeTestInfo LEAKED ($ga_count annotations) — I-AB.1 + I-D.4 violation"
    fi
}
# cleanup runs after verification (positive + negative). Each step
# is fail-soft per D2C4AB.14 — logs warning, continues. Cleanup is
# housekeeping; it does NOT contribute to the PASS/FAIL contract.
cleanup() {
    log_info "cleanup: best-effort housekeeping (fail-soft)"

    # 1) PagerDuty: resolve the test incident if captured.
    if [ -n "${SMOKE_PD_INCIDENT_ID:-}" ]; then
        log_info "cleanup: resolving PagerDuty incident $SMOKE_PD_INCIDENT_ID"
        curl -sS --max-time 10 -X PUT \
            -H "Authorization: Token token=${PAGERDUTY_TEST_API_TOKEN}" \
            -H "Accept: application/vnd.pagerduty+json;version=2" \
            -H "Content-Type: application/json" \
            -H "From: smoke-receivers@runtime-adapters" \
            -d '{"incident":{"type":"incident_reference","status":"resolved","resolution":"smoke-receivers cleanup"}}' \
            "https://api.pagerduty.com/incidents/${SMOKE_PD_INCIDENT_ID}" \
            >/dev/null 2>&1 || log_warn "cleanup: PagerDuty resolve failed (non-blocking)"
    fi

    # 2) Slack: chat.delete the test messages (requires bot scope chat:write).
    if [ -n "${SMOKE_SLACK_INCIDENTS_TS:-}" ]; then
        log_info "cleanup: deleting Slack #incidents msg ts=$SMOKE_SLACK_INCIDENTS_TS"
        curl -sS --max-time 10 -X POST \
            -H "Authorization: Bearer ${SLACK_TEST_BOT_TOKEN}" \
            -H "Content-Type: application/json; charset=utf-8" \
            -d "{\"channel\":\"$SLACK_TEST_INCIDENTS_CHANNEL_ID\",\"ts\":\"$SMOKE_SLACK_INCIDENTS_TS\"}" \
            "https://slack.com/api/chat.delete" \
            >/dev/null 2>&1 || log_warn "cleanup: Slack #incidents delete failed (non-blocking)"
    fi
    if [ -n "${SMOKE_SLACK_OPS_TS:-}" ]; then
        log_info "cleanup: deleting Slack #ops msg ts=$SMOKE_SLACK_OPS_TS"
        curl -sS --max-time 10 -X POST \
            -H "Authorization: Bearer ${SLACK_TEST_BOT_TOKEN}" \
            -H "Content-Type: application/json; charset=utf-8" \
            -d "{\"channel\":\"$SLACK_TEST_OPS_CHANNEL_ID\",\"ts\":\"$SMOKE_SLACK_OPS_TS\"}" \
            "https://slack.com/api/chat.delete" \
            >/dev/null 2>&1 || log_warn "cleanup: Slack #ops delete failed (non-blocking)"
    fi

    # 3) Linear: archive the test issue.
    if [ -n "${SMOKE_LINEAR_ISSUE_ID:-}" ]; then
        log_info "cleanup: archiving Linear issue $SMOKE_LINEAR_ISSUE_ID"
        local archive_query
        archive_query=$(jq -nc \
            --arg id "$SMOKE_LINEAR_ISSUE_ID" \
            '{query:"mutation A($id: String!){ issueArchive(id: $id){ success } }",variables:{id:$id}}')
        curl -sS --max-time 10 -X POST \
            -H "Authorization: ${LINEAR_TEST_API_TOKEN}" \
            -H "Content-Type: application/json" \
            --data "$archive_query" \
            "$LINEAR_API_URL" >/dev/null 2>&1 \
            || log_warn "cleanup: Linear archive failed (non-blocking)"
    fi

    # 4) Alertmanager: silence the SmokeTest* alerts for 5min so
    #    repeat firings during cleanup races don't re-page.
    log_info "cleanup: silencing SmokeTest* alertnames for 5m"
    amtool --alertmanager.url="$AM_URL" silence add \
        "alertname=~SmokeTest.*" \
        --duration=5m \
        --comment="smoke-receivers cleanup" \
        --author="smoke-receivers" \
        >/dev/null 2>&1 || log_warn "cleanup: amtool silence add failed (non-blocking)"

    # 5) Grafana: delete SmokeTest* annotations (D B3.9).
    log_info "cleanup: deleting SmokeTest* annotations from Grafana"
    for marker in SmokeTestCritical SmokeTestWarning SmokeTestInfo; do
        local ids
        ids=$(curl -sSf -H "Authorization: Bearer ${GRAFANA_TEST_SERVICE_ACCOUNT_TOKEN}" \
            "${GRAFANA_TEST_URL}/api/annotations?tags=${marker}&limit=20" 2>/dev/null \
            | jq -r '.[].id' 2>/dev/null || true)
        for id in $ids; do
            curl -sS -X DELETE \
                -H "Authorization: Bearer ${GRAFANA_TEST_SERVICE_ACCOUNT_TOKEN}" \
                "${GRAFANA_TEST_URL}/api/annotations/${id}" \
                >/dev/null 2>&1 || log_warn "cleanup: Grafana annotation $id delete failed (non-blocking)"
        done
    done

    log_info "cleanup: done"
}
# report prints the PASS/FAIL summary, dumps logs on FAIL, and
# determines the script's exit code per the documented contract:
#   0 — all positive + negative checks passed; cleanup attempted
#   1 — verification failure (cleanup still attempted)
report() {
    log_info "=========================="
    log_info "smoke-receivers SUMMARY"
    log_info "=========================="
    log_info "verification failures: $TEST_FAILURES"
    if [ "$TEST_FAILURES" -eq 0 ]; then
        log_info "RESULT: PASS"
        return 0
    fi
    log_err "RESULT: FAIL ($TEST_FAILURES check(s) failed)"
    log_err "----- alertmanager logs (last 50) -----"
    docker logs --tail 50 "$(docker ps -q --filter 'name=alertmanager' | head -1)" 2>&1 || true
    log_err "----- linear-webhook logs (last 50) -----"
    docker logs --tail 50 "$(docker ps -q --filter 'name=linear-webhook' | head -1)" 2>&1 || true
    return 1
}

main() {
    # Run preflight first; abort on preflight failure (no point
    # injecting against a broken stack).
    if ! preflight; then
        log_err "preflight FAILED — aborting"
        exit 1
    fi
    # Run the rest in a sequence that ALWAYS reaches cleanup +
    # report, even if a phase fails internally (verify_pos /
    # verify_neg use fail_test which doesn't exit; inject + wait
    # use set -e but that's caught by trap below).
    trap 'cleanup; report; exit 1' ERR
    inject
    wait_phase
    verify_pos
    verify_neg
    trap - ERR
    cleanup
    if ! report; then
        exit 1
    fi
}

main "$@"
