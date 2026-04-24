#!/usr/bin/env bash
# ops/load/lib/generate-report.sh
#
# Consumes:
#   - /summaries/summary.json      (from k6 handleSummary, mounted)
#   - docker inspect output        (envelope)
#   - PromQL instant + query_range (via curl to prometheus:9090)
# Produces:
#   - ops/slo/calibration-reports/YYYY-MM-DD-baseline-v<N>.md
#   - ops/slo/calibration-reports/evidence/<dir>/manifest.json
#   - ops/slo/calibration-reports/evidence/<dir>/summary.json
#   - ops/slo/calibration-reports/evidence/<dir>/<cap>-baseline-prom-*.json
#   - ops/slo/calibration-reports/evidence/<dir>/git-status-smoke-split.json
#   - ops/slo/calibration-reports/evidence/<dir>/git-rough-observations.json
#   - ops/slo/calibration-reports/latest-baseline.json
#
# This script does NOT edit ops/slo/*.yaml. Target changes are
# review-driven manual edits by the operator (D2C2.5).

set -euo pipefail

# ---- paths --------------------------------------------------------------

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LOAD_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_ROOT="$(cd "$LOAD_DIR/../.." && pwd)"

REPORTS_DIR="$REPO_ROOT/ops/slo/calibration-reports"
EVIDENCE_ROOT="$REPORTS_DIR/evidence"
TEMPLATE="$SCRIPT_DIR/report-template.md.tmpl"

# k6 writes to a compose volume; copy out via docker.
# shellcheck disable=SC2034  # documents the volume path; used as reference only
SUMMARY_IN_VOLUME="/summaries/summary.json"

# Prometheus exposed at :9090 on the host (compose maps it).
PROM_URL="${PROM_URL:-http://localhost:9090}"

# Compose project name (matches `name:` in compose.yaml).
COMPOSE_PROJECT="${COMPOSE_PROJECT:-runtime-load}"

# ---- setup --------------------------------------------------------------

mkdir -p "$REPORTS_DIR" "$EVIDENCE_ROOT"

DATE_UTC="$(date -u +%Y-%m-%d)"

# Auto-increment -v<N> suffix if today's baseline already exists.
VERSION=1
while [[ -f "$REPORTS_DIR/$DATE_UTC-baseline-v$VERSION.md" ]]; do
    VERSION=$((VERSION + 1))
done
REPORT_FILE="$REPORTS_DIR/$DATE_UTC-baseline-v$VERSION.md"
EVIDENCE_DIR="$DATE_UTC-baseline-v$VERSION"
mkdir -p "$EVIDENCE_ROOT/$EVIDENCE_DIR"

echo "== generate-report.sh =="
echo "   report:   $REPORT_FILE"
echo "   evidence: $EVIDENCE_ROOT/$EVIDENCE_DIR/"

# ---- collect summary.json from k6 volume --------------------------------

echo "-- copying k6 summary.json --"
# Copy from the k6 named volume to local evidence dir.
# Approach: run a temp alpine container mounting the volume.
docker run --rm \
    -v "${COMPOSE_PROJECT}_summaries:/in:ro" \
    -v "$EVIDENCE_ROOT/$EVIDENCE_DIR:/out" \
    alpine:3.19 \
    cp /in/summary.json /out/summary.json

# ---- envelope: host + docker + cgroup verification ----------------------

echo "-- collecting envelope metadata --"

HOST_MACHINE=""
HOST_OS="$(uname -s) $(uname -r)"
if [[ "$(uname)" == "Darwin" ]]; then
    HOST_MACHINE="$(sysctl -n machdep.cpu.brand_string)"
    HOST_CPU_COUNT="$(sysctl -n hw.ncpu)"
    HOST_RAM_BYTES="$(sysctl -n hw.memsize)"
    HOST_RAM="$(awk "BEGIN{printf \"%.1f GiB\", $HOST_RAM_BYTES/1024/1024/1024}")"
elif [[ "$(uname)" == "Linux" ]]; then
    HOST_MACHINE="$(awk -F: '/model name/{print $2; exit}' /proc/cpuinfo | sed 's/^ //')"
    HOST_CPU_COUNT="$(nproc)"
    HOST_RAM="$(awk '/MemTotal/{printf "%.1f GiB", $2/1024/1024}' /proc/meminfo)"
else
    HOST_MACHINE="unknown"
    HOST_CPU_COUNT="?"
    HOST_RAM="?"
fi

DOCKER_VERSION="$(docker version --format '{{.Server.Version}}')"
COMPOSE_VERSION="$(docker compose version --short)"
GIT_SHA="$(git -C "$REPO_ROOT" rev-parse HEAD)"
GIT_AUTHOR="$(git -C "$REPO_ROOT" config user.name || echo 'unknown')"

K6_VERSION="$(cat "$REPO_ROOT/ops/load/.k6-version")"
COLLECTOR_VERSION="$(cat "$REPO_ROOT/ops/otel-collector/.collector-version")"
SLOTH_VERSION="$(cat "$REPO_ROOT/ops/slo/.sloth-version")"
PROMTOOL_VERSION="$(cat "$REPO_ROOT/ops/prometheus/.prometheus-version")"

# Runtime image digest (built locally via docker build).
RUNTIME_IMAGE="$(docker inspect --format '{{.Id}}' runtime-adapters:ci-test 2>/dev/null | head -c 19 || echo 'runtime-adapters:local')"

# cgroup verification — capture the raw output.
CGROUP_JSON="$("$LOAD_DIR/lib/verify-limits.sh" "${COMPOSE_PROJECT}-runtime-adapters-1" 2>&1 | grep -v '^OK:' | grep -v '^===' || echo '{}')"
# Strip to just the JSON body.
CGROUP_JSON="$(echo "$CGROUP_JSON" | awk '/^{/,/^}/')"

# Run duration: Manual for now — ideally derived from summary.json's
# state.testRunDurationMs field.
RUN_DURATION="$(jq -r '.state.testRunDurationMs / 1000 / 60 | tostring + " min"' "$EVIDENCE_ROOT/$EVIDENCE_DIR/summary.json" 2>/dev/null || echo 'unknown')"

# ---- PromQL evidence (instant + range) per core capability --------------

echo "-- collecting PromQL evidence --"

CAPABILITIES=( "shell.exec@v1" "filesystem.read_file@v1" "filesystem.write_file@v1" "http.request@v1" )

# Instant: p99 at test end for each capability.
for cap in "${CAPABILITIES[@]}"; do
    cap_safe="${cap//@/-}"; cap_safe="${cap_safe//./-}"
    EXPR="histogram_quantile(0.99, sum by (le) (rate(runtime_adapters_execution_duration_seconds_bucket{capability=\"${cap}\"}[5m])))"
    curl -sG "$PROM_URL/api/v1/query" \
        --data-urlencode "query=$EXPR" \
        > "$EVIDENCE_ROOT/$EVIDENCE_DIR/${cap_safe}-baseline-prom-instant.json" \
        || echo "WARN: instant query failed for $cap" >&2
done

# Range: full run window; start + end timestamps derived from k6 state.
TEST_START="$(jq -r '.state.testRunDurationMs as $d | (now - $d/1000)' "$EVIDENCE_ROOT/$EVIDENCE_DIR/summary.json")"
TEST_END="$(date -u +%s)"
for cap in "${CAPABILITIES[@]}"; do
    cap_safe="${cap//@/-}"; cap_safe="${cap_safe//./-}"
    EXPR="histogram_quantile(0.99, sum by (le) (rate(runtime_adapters_execution_duration_seconds_bucket{capability=\"${cap}\"}[5m])))"
    curl -sG "$PROM_URL/api/v1/query_range" \
        --data-urlencode "query=$EXPR" \
        --data-urlencode "start=$TEST_START" \
        --data-urlencode "end=$TEST_END" \
        --data-urlencode "step=30" \
        > "$EVIDENCE_ROOT/$EVIDENCE_DIR/${cap_safe}-baseline-prom-range.json" \
        || echo "WARN: range query failed for $cap" >&2
done

# ---- git.status clean/dirty split evidence ------------------------------

echo "-- extracting git-status clean vs dirty split --"
jq '{
    aggregate:  .metrics.http_req_duration,
    by_tree: {
        small_repo: (.metrics["http_req_duration{tree:small-repo}"] // null),
        dirty_tree: (.metrics["http_req_duration{tree:dirty-tree}"] // null)
    }
}' "$EVIDENCE_ROOT/$EVIDENCE_DIR/summary.json" \
    > "$EVIDENCE_ROOT/$EVIDENCE_DIR/git-status-smoke-split.json"

# ---- git rough observations ---------------------------------------------

echo "-- extracting git-rough observations --"
jq '{
    git_clone:  (.metrics["http_req_duration{capability:git.clone@v1,tier:rough}"]  // null),
    git_diff:   (.metrics["http_req_duration{capability:git.diff@v1,tier:rough}"]   // null),
    git_commit: (.metrics["http_req_duration{capability:git.commit@v1,tier:rough}"] // null)
}' "$EVIDENCE_ROOT/$EVIDENCE_DIR/summary.json" \
    > "$EVIDENCE_ROOT/$EVIDENCE_DIR/git-rough-observations.json"

# ---- manifest.json ------------------------------------------------------

echo "-- writing manifest.json --"
cat > "$EVIDENCE_ROOT/$EVIDENCE_DIR/manifest.json" <<EOF
{
  "generated_at":      "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "report":            "ops/slo/calibration-reports/$DATE_UTC-baseline-v$VERSION.md",
  "git_sha":           "$GIT_SHA",
  "git_author":        "$GIT_AUTHOR",
  "run_duration":      "$RUN_DURATION",
  "host": {
    "machine":         "$HOST_MACHINE",
    "os":              "$HOST_OS",
    "cpu_count":       "$HOST_CPU_COUNT",
    "ram":             "$HOST_RAM"
  },
  "docker": {
    "server_version":  "$DOCKER_VERSION",
    "compose_version": "$COMPOSE_VERSION"
  },
  "tool_versions": {
    "k6":              "$K6_VERSION",
    "sloth":           "$SLOTH_VERSION",
    "promtool":        "$PROMTOOL_VERSION",
    "collector":       "$COLLECTOR_VERSION",
    "go":              "1.26.2"
  },
  "envelope": {
    "runtime_adapters_cpus":  "2.0",
    "runtime_adapters_mem":   "2g",
    "mem_reservation":        "1g",
    "cgroup_verification_raw": $CGROUP_JSON
  },
  "metrics_pipeline": "runtime -> OTLP gRPC -> otel-collector (prometheus exporter :8889) <- scrape (5s) <- prometheus"
}
EOF

# ---- latest-baseline.json (consumed by CI smoke delta comparison) -------

echo "-- writing latest-baseline.json --"
# Extract per-capability baseline p50/p95/p99 from summary.json.
extract_percentile() {
    local cap="$1" key="$2"
    jq -r ".metrics[\"http_req_duration{scenario:baseline,capability:${cap}}\"].values[\"${key}\"] // 0 | tostring" \
        "$EVIDENCE_ROOT/$EVIDENCE_DIR/summary.json"
}

cat > "$REPORTS_DIR/latest-baseline.json" <<EOF
{
  "generated_at":      "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "from_report":       "ops/slo/calibration-reports/$DATE_UTC-baseline-v$VERSION.md",
  "envelope_manifest": "ops/slo/calibration-reports/evidence/$EVIDENCE_DIR/manifest.json",
  "comparison_context": "This snapshot captures distributions measured under the 2C.2 local pinned compose envelope (2 CPU / 2 GiB, full stack with OTel collector, pgx postgres, http-upstream-mock). CI smoke runs under a different, more restrictive envelope (GHA ubuntu-latest public runner at 4 CPU / 16 GiB, but compose.ci-smoke.yaml pins limits conservatively for the private-repo floor, stripped compose without OTel collector). Absolute comparison is NOT apples-to-apples; delta thresholds in ci-smoke-comment.sh are calibrated to flag GROSS regressions only (>50% p99 drift).",
  "runner_class_baseline":       "local-pinned-compose-2cpu-2gib",
  "runner_class_smoke_expected": "github-actions-ubuntu-latest-public-4cpu-16gib",
  "core": {
    "shell.exec@v1":            { "p50_ms": $(extract_percentile "shell.exec@v1" "p(50)"),            "p95_ms": $(extract_percentile "shell.exec@v1" "p(95)"),            "p99_ms": $(extract_percentile "shell.exec@v1" "p(99)") },
    "filesystem.read_file@v1":  { "p50_ms": $(extract_percentile "filesystem.read_file@v1" "p(50)"),  "p95_ms": $(extract_percentile "filesystem.read_file@v1" "p(95)"),  "p99_ms": $(extract_percentile "filesystem.read_file@v1" "p(99)") },
    "filesystem.write_file@v1": { "p50_ms": $(extract_percentile "filesystem.write_file@v1" "p(50)"), "p95_ms": $(extract_percentile "filesystem.write_file@v1" "p(95)"), "p99_ms": $(extract_percentile "filesystem.write_file@v1" "p(99)") },
    "http.request@v1":          { "p50_ms": $(extract_percentile "http.request@v1" "p(50)"),          "p95_ms": $(extract_percentile "http.request@v1" "p(95)"),          "p99_ms": $(extract_percentile "http.request@v1" "p(99)") }
  }
}
EOF

# ---- render the report markdown -----------------------------------------

echo "-- rendering report markdown --"

# Build core tier sections + git tables + summary rows via Python-less
# templating. This is intentionally simple: the operator may hand-edit
# post-generation to refine wording in the Decision / justification
# columns. The generated draft populates everything that's derivable.

CORE_TIER_OUT=""
for cap in "${CAPABILITIES[@]}"; do
    p50=$(extract_percentile "$cap" "p(50)")
    p95=$(extract_percentile "$cap" "p(95)")
    p99=$(extract_percentile "$cap" "p(99)")
    cap_safe="${cap//@/-}"; cap_safe="${cap_safe//./-}"
    CORE_TIER_OUT+="
### $cap — core calibration

#### Baseline distribution

| metric | observed | current target | proposed target | decision | justification |
|---|---|---|---|---|---|
| p50 latency | ${p50} ms | — | — | — | — |
| p95 latency | ${p95} ms | — | — | — | — |
| p99 latency | ${p99} ms | (see ops/slo) | (operator review) | (operator review) | (operator review) |

#### Evidence sources

- \`evidence/$EVIDENCE_DIR/summary.json\` — k6 \`handleSummary\` raw
- \`evidence/$EVIDENCE_DIR/${cap_safe}-baseline-prom-instant.json\` — PromQL instant query at run-end
- \`evidence/$EVIDENCE_DIR/${cap_safe}-baseline-prom-range.json\` — PromQL query_range over run window
"
done

GIT_STATUS_OUT="(Operator to fill with p50/p95/p99 per tree + overall — see \`evidence/$EVIDENCE_DIR/git-status-smoke-split.json\`.)"
GIT_ROUGH_OUT="(Operator to fill with observed ranges per capability — see \`evidence/$EVIDENCE_DIR/git-rough-observations.json\`.)"

# Substitute template placeholders.
sed \
    -e "s|{{VERSION}}|$VERSION|g" \
    -e "s|{{DATE_UTC}}|$DATE_UTC|g" \
    -e "s|{{GIT_AUTHOR}}|$GIT_AUTHOR|g" \
    -e "s|{{GIT_SHA}}|$GIT_SHA|g" \
    -e "s|{{RUN_DURATION}}|$RUN_DURATION|g" \
    -e "s|{{HOST_MACHINE}}|$HOST_MACHINE|g" \
    -e "s|{{HOST_OS}}|$HOST_OS|g" \
    -e "s|{{DOCKER_VERSION}}|$DOCKER_VERSION|g" \
    -e "s|{{COMPOSE_VERSION}}|$COMPOSE_VERSION|g" \
    -e "s|{{HOST_CPU_COUNT}}|$HOST_CPU_COUNT|g" \
    -e "s|{{HOST_RAM}}|$HOST_RAM|g" \
    -e "s|{{RUNTIME_IMAGE}}|$RUNTIME_IMAGE|g" \
    -e "s|{{COLLECTOR_VERSION}}|$COLLECTOR_VERSION|g" \
    -e "s|{{K6_VERSION}}|$K6_VERSION|g" \
    -e "s|{{SLOTH_VERSION}}|$SLOTH_VERSION|g" \
    -e "s|{{PROMTOOL_VERSION}}|$PROMTOOL_VERSION|g" \
    -e "s|{{EVIDENCE_DIR}}|$EVIDENCE_DIR|g" \
    "$TEMPLATE" > "$REPORT_FILE"

# Replace multi-line placeholders via awk. Values pass through ENVIRON[]
# rather than `-v` — awk's `-v` assignment does NOT support embedded
# newlines (it emits "awk: newline in string" and exits 2). Tier tables,
# git section tables, and the cgroup JSON block are all multi-line.
export CORE_TIER_OUT GIT_STATUS_OUT GIT_ROUGH_OUT CGROUP_JSON
awk '
    /\{\{CORE_TIER_SECTIONS\}\}/  { print ENVIRON["CORE_TIER_OUT"]; next }
    /\{\{GIT_STATUS_TABLES\}\}/   { print ENVIRON["GIT_STATUS_OUT"]; next }
    /\{\{GIT_ROUGH_TABLES\}\}/    { print ENVIRON["GIT_ROUGH_OUT"]; next }
    /\{\{CGROUP_VERIFICATION\}\}/ { print ENVIRON["CGROUP_JSON"]; next }
    { print }
' "$REPORT_FILE" > "${REPORT_FILE}.tmp" && mv "${REPORT_FILE}.tmp" "$REPORT_FILE"

# Remaining placeholders become operator TODOs:
sed -i.bak 's|{{SUMMARY_ROWS}}|(operator fills after reviewing evidence)|;
            s|{{COUNT_HIGH}}|?|;
            s|{{COUNT_SMOKE}}|?|;
            s|{{COUNT_ROUGH}}|?|' "$REPORT_FILE"
rm -f "${REPORT_FILE}.bak"

echo ""
echo "== generate-report.sh complete =="
echo "Report:          $REPORT_FILE"
echo "Evidence dir:    $EVIDENCE_ROOT/$EVIDENCE_DIR/"
echo "latest-baseline: $REPORTS_DIR/latest-baseline.json"
echo ""
echo "Next: review the evidence, hand-edit the core tier tables' proposed"
echo "targets + decisions + justifications in the report, hand-edit"
echo "ops/slo/*.yaml accordingly, then commit both together."
