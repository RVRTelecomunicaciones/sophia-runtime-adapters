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
set -euo pipefail
OUT="${OUT_DIR:-./chaos-dump}"
mkdir -p "$OUT"

echo "==> Prometheus targets"
curl -sf http://localhost:9090/api/v1/targets | tee "$OUT/prom-targets.json" > /dev/null

echo "==> Prometheus query: execution.total"
curl -sf "http://localhost:9090/api/v1/query?query=runtime_adapters_execution_total" \
  | tee "$OUT/prom-execution-total.json" > /dev/null

echo "==> Active alerts (Prometheus)"
curl -sf http://localhost:9090/api/v1/alerts | tee "$OUT/prom-active-alerts.json" > /dev/null

echo "==> Alertmanager state"
curl -sf http://localhost:9093/api/v2/alerts | tee "$OUT/am-alerts.json" > /dev/null

echo "==> Receiver-stub /inspect"
curl -sf http://localhost:8088/inspect | tee "$OUT/receiver-inspect.json" > /dev/null

echo "==> Runtime container logs (last 500 lines)"
docker logs --tail 500 "$(docker ps -qf name=runtime-adapters)" 2>&1 \
  | tee "$OUT/runtime-logs.txt" > /dev/null

echo "Dump written to $OUT/"
