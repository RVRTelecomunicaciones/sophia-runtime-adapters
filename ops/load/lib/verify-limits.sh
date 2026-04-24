#!/usr/bin/env bash
# ops/load/lib/verify-limits.sh
#
# Asserts that the measurement envelope's cgroup limits were actually
# applied to the runtime-adapters container. D2C2.4 + §6.3.
#
# Expected: NanoCpus=2000000000 (2.0 CPU), Memory=2147483648 (2 GiB).
# Any deviation aborts with a clear error BEFORE k6 starts — a bad
# envelope invalidates measurements, catch it early.
#
# Usage: invoked from `make load-baseline` after compose up --wait.

set -euo pipefail

CONTAINER="${1:-runtime-load-runtime-adapters-1}"

if ! docker inspect "$CONTAINER" >/dev/null 2>&1; then
    echo "ERROR: container $CONTAINER not found. Is compose up?" >&2
    echo "  Try: docker compose -f ops/local/compose.yaml up -d --wait" >&2
    exit 1
fi

LIMITS_JSON="$(docker inspect "$CONTAINER" | jq '.[0].HostConfig | {NanoCpus, Memory, MemoryReservation}')"

echo "=== Runtime envelope (actual, from docker inspect) ==="
echo "$LIMITS_JSON"

NANO_CPUS=$(echo "$LIMITS_JSON" | jq -r '.NanoCpus')
MEMORY=$(echo "$LIMITS_JSON" | jq -r '.Memory')

EXPECTED_NANO_CPUS=2000000000      # 2.0 CPU
EXPECTED_MEMORY=2147483648         # 2 GiB

FAIL=0
if [[ "$NANO_CPUS" != "$EXPECTED_NANO_CPUS" ]]; then
    echo "FAIL: NanoCpus=$NANO_CPUS, expected=$EXPECTED_NANO_CPUS (2.0 CPU)" >&2
    FAIL=1
fi
if [[ "$MEMORY" != "$EXPECTED_MEMORY" ]]; then
    echo "FAIL: Memory=$MEMORY, expected=$EXPECTED_MEMORY (2 GiB)" >&2
    FAIL=1
fi

if [[ "$FAIL" == "1" ]]; then
    echo "" >&2
    echo "Envelope mismatch — targets calibrated from this run would carry a wrong envelope declaration." >&2
    echo "Check ops/local/compose.yaml cpus: and mem_limit: for runtime-adapters." >&2
    exit 1
fi

echo "OK: envelope limits match expected (2 CPU / 2 GiB)"
