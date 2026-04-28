#!/usr/bin/env bash
# ops/chaos/scripts/dump.sh
#
# Diagnostic dump for chaos test sessions.
# Captures Prometheus query state, Alertmanager live alerts, receiver-stub
# /inspect payload, and runtime container logs into a local directory.
#
# Usage:
#   ./ops/chaos/scripts/dump.sh
#   OUT_DIR=/tmp/my-dump ./ops/chaos/scripts/dump.sh
#
# Requires the chaos compose stack to be running (make chaos-up).
# NOTE: do not `set -e` — we want the script to keep dumping even when one
# probe returns a non-2xx (otherwise a missing endpoint would skip subsequent
# probes and obscure unrelated state). Each curl uses `|| true` to swallow
# errors locally.
set -uo pipefail
OUT="${OUT_DIR:-./chaos-dump}"
mkdir -p "$OUT"

probe() {
  local label="$1"
  local url="$2"
  local out_file="$3"
  echo "==> $label ($url)"
  curl -sf --max-time 5 "$url" > "$OUT/$out_file" || {
    echo "    (probe failed: $url)" > "$OUT/$out_file"
  }
}

probe "Prometheus targets"          "http://localhost:9090/api/v1/targets"          prom-targets.json
probe "Prometheus rules"            "http://localhost:9090/api/v1/rules"            prom-rules.json
probe "Prometheus runtime info"     "http://localhost:9090/api/v1/status/runtimeinfo" prom-runtime.json
probe "Prometheus alertmanagers"    "http://localhost:9090/api/v1/alertmanagers"    prom-alertmanagers.json
probe "Prometheus query execution_total" \
                                    "http://localhost:9090/api/v1/query?query=runtime_adapters_execution_total" \
                                    prom-execution-total.json
probe "Prometheus query SLI ratio_rate1m" \
                                    "http://localhost:9090/api/v1/query?query=slo:sli_error:ratio_rate1m" \
                                    prom-sli-rate1m.json
probe "Prometheus active alerts"    "http://localhost:9090/api/v1/alerts"           prom-active-alerts.json
probe "Alertmanager alerts"         "http://localhost:9093/api/v2/alerts"           am-alerts.json
probe "Alertmanager status"         "http://localhost:9093/api/v2/status"           am-status.json
probe "Receiver-stub /inspect"      "http://localhost:8088/inspect"                 receiver-inspect.json

echo "==> Prometheus container logs (last 200 lines)"
docker logs --tail 200 "$(docker ps -qf name=prometheus | head -n1)" > "$OUT/prometheus-logs.txt" 2>&1 || true

echo "==> Alertmanager container logs (last 200 lines)"
docker logs --tail 200 "$(docker ps -qf name=alertmanager | head -n1)" > "$OUT/alertmanager-logs.txt" 2>&1 || true

echo "==> Runtime container logs (last 500 lines)"
docker logs --tail 500 "$(docker ps -qf name=runtime-adapters | head -n1)" > "$OUT/runtime-logs.txt" 2>&1 || true

echo "Dump written to $OUT/"
