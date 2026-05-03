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
        LINEAR_TEST_TEAM_ID
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
inject()     { :; }
wait_phase() { :; }
verify_pos() { :; }
verify_neg() { :; }
cleanup()    { :; }
report()     { :; }

main() {
    preflight
    inject
    wait_phase
    verify_pos
    verify_neg
    cleanup
    report
}

main "$@"
