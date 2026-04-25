#!/usr/bin/env bash
# ops/load/lib/ci-smoke-comment.sh
#
# Consumes: smoke summary JSON + (optional) baseline snapshot + k6 exit code.
# Produces: Markdown PR comment body on stdout.
#
# Three branches (§10.3 + A2C2.21):
#   1. k6 exit 0 + baseline exists     → delta table with color flags
#   2. k6 exit 0 + no baseline yet     → raw numbers, "NO BASELINE YET"
#   3. k6 exit != 0                    → "⚠️ DID NOT COMPLETE CLEANLY"
#
# Never fails: advisory-only contract. Always emits a usable comment.

set -euo pipefail

SUMMARY_FILE="${1:-/tmp/smoke/smoke-summary.json}"
BASELINE_FILE="${2:-ops/slo/calibration-reports/latest-baseline.json}"
K6_EXIT_CODE="${3:-0}"

emit() { printf '%s\n' "$*"; }

# ---- Branch 3: k6 failed / no summary -----------------------------------

if [[ "$K6_EXIT_CODE" != "0" ]] || [[ ! -s "$SUMMARY_FILE" ]]; then
    emit "## Load smoke (advisory) — ⚠️ DID NOT COMPLETE CLEANLY"
    emit ""
    emit "k6 smoke run failed or timed out (exit code: \`$K6_EXIT_CODE\`)."
    if [[ ! -s "$SUMMARY_FILE" ]]; then
        emit "Summary file is missing or empty."
    fi
    emit ""
    emit "This does **NOT** block the PR (advisory-only by contract, \`continue-on-error: true\`)."
    emit ""
    emit "**Possible causes:** runner resource starvation, compose startup timeout, legitimate regression."
    emit ""
    emit "Investigate via the [workflow run](${GITHUB_RUN_URL:-workflow-logs})."
    emit ""
    emit "_Phase 2C.2 advisory smoke — never gates CI._"
    exit 0
fi

# ---- Per-capability current p99 from summary ----------------------------

extract_p99() {
    local cap="$1"
    jq -r ".metrics[\"http_req_duration{phase:smoke,capability:${cap}}\"].values[\"p(99)\"] // 0 | tonumber" "$SUMMARY_FILE"
}

SHELL_P99=$(extract_p99 "shell.exec@v1")
FS_READ_P99=$(extract_p99 "filesystem.read_file@v1")
FS_WRITE_P99=$(extract_p99 "filesystem.write_file@v1")
HTTP_P99=$(extract_p99 "http.request@v1")

# ---- Branch 2: no baseline yet ------------------------------------------

if [[ ! -s "$BASELINE_FILE" ]]; then
    emit "## Load smoke (advisory) — **NO BASELINE YET**"
    emit ""
    emit "This is the first calibration PR cycle. \`latest-baseline.json\` does not exist yet — no delta comparison possible. Numbers below are informational only."
    emit ""
    emit "| capability | p99 (smoke) |"
    emit "|---|---|"
    emit "| shell.exec@v1 | ${SHELL_P99} ms |"
    emit "| filesystem.read_file@v1 | ${FS_READ_P99} ms |"
    emit "| filesystem.write_file@v1 | ${FS_WRITE_P99} ms |"
    emit "| http.request@v1 | ${HTTP_P99} ms |"
    emit ""
    emit "Once calibration is committed (Bundle 7), subsequent PRs will include delta comparison."
    emit ""
    emit "_Phase 2C.2 advisory smoke — never gates CI._"
    exit 0
fi

# ---- Branch 1: baseline exists → delta table ----------------------------

extract_baseline_p99() {
    local cap="$1"
    jq -r ".core[\"${cap}\"].p99_ms // 0 | tonumber" "$BASELINE_FILE"
}

pct_delta() {
    local now="$1" base="$2"
    if [[ "$(echo "$base == 0" | bc -l 2>/dev/null || echo 1)" == "1" ]]; then
        echo "—"
        return
    fi
    awk -v n="$now" -v b="$base" 'BEGIN { printf "%+.1f%%", (n-b)/b*100 }'
}

flag() {
    local pct_raw="$1"
    # No-baseline sentinel — guard BEFORE sed/awk so the em-dash doesn't
    # get stripped and silently converted to 0.0 (which would emit 🟢 and
    # mislead reviewers into thinking "no baseline" == "no delta").
    if [[ "$pct_raw" == "—" ]]; then echo "⚪"; return; fi
    # Strip leading + and % (keep unary - so awk reads the signed number).
    local abs
    abs="$(echo "$pct_raw" | sed 's/[+%]//g' | tr -d '-' | awk '{printf "%.1f", $1+0}')"
    local f
    f="$(awk -v a="$abs" 'BEGIN {
        if (a <= 20)      print "🟢"
        else if (a <= 50) print "🟡"
        else              print "🔴"
    }')"
    echo "$f"
}

SHELL_BASE=$(extract_baseline_p99 "shell.exec@v1")
FS_READ_BASE=$(extract_baseline_p99 "filesystem.read_file@v1")
FS_WRITE_BASE=$(extract_baseline_p99 "filesystem.write_file@v1")
HTTP_BASE=$(extract_baseline_p99 "http.request@v1")

SHELL_DELTA=$(pct_delta "$SHELL_P99" "$SHELL_BASE")
FS_READ_DELTA=$(pct_delta "$FS_READ_P99" "$FS_READ_BASE")
FS_WRITE_DELTA=$(pct_delta "$FS_WRITE_P99" "$FS_WRITE_BASE")
HTTP_DELTA=$(pct_delta "$HTTP_P99" "$HTTP_BASE")

BASELINE_REPORT="$(jq -r '.from_report // "unknown"' "$BASELINE_FILE")"
BASELINE_AT="$(jq -r '.generated_at // "unknown"' "$BASELINE_FILE")"

emit "## Load smoke (advisory)"
emit ""
emit "Ran against the PR head on \`ubuntu-latest\` (noisy runner — advisory only)."
emit ""
emit "| capability | p99 (smoke) | baseline p99 | delta | flag |"
emit "|---|---|---|---|---|"
emit "| shell.exec@v1 | ${SHELL_P99} ms | ${SHELL_BASE} ms | ${SHELL_DELTA} | $(flag "$SHELL_DELTA") |"
emit "| filesystem.read_file@v1 | ${FS_READ_P99} ms | ${FS_READ_BASE} ms | ${FS_READ_DELTA} | $(flag "$FS_READ_DELTA") |"
emit "| filesystem.write_file@v1 | ${FS_WRITE_P99} ms | ${FS_WRITE_BASE} ms | ${FS_WRITE_DELTA} | $(flag "$FS_WRITE_DELTA") |"
emit "| http.request@v1 | ${HTTP_P99} ms | ${HTTP_BASE} ms | ${HTTP_DELTA} | $(flag "$HTTP_DELTA") |"
emit ""
emit "**Flag legend:** 🟢 ≤ 20% | 🟡 20–50% | 🔴 > 50% (likely real regression — investigate)"
emit ""
emit "Baseline: \`${BASELINE_REPORT}\` generated ${BASELINE_AT}."
emit ""
emit "**Envelope mismatch reminder:** smoke runs on GHA \`ubuntu-latest\` (public repo: 4 CPU / 16 GiB actual; \`compose.ci-smoke.yaml\` pins the runtime to 0.75 CPU / 768 MiB anyway, no collector), while the baseline was captured under the local pinned compose (2 CPU / 2 GiB full stack). Absolute comparison is NOT apples-to-apples; the flag thresholds above are calibrated to surface gross regressions only. For real calibration, re-run \`make load-baseline\` locally."
emit ""
emit "_Phase 2C.2 advisory smoke — never gates CI._"
