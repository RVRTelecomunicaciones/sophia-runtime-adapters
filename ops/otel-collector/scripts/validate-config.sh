#!/usr/bin/env bash
# ops/otel-collector/scripts/validate-config.sh
#
# Validates ops/otel-collector/config.yaml against the SAME collector
# image tag that compose runs (D2C2.15 — no drift between validated
# config and runtime collector). Version pinned via
# ops/otel-collector/.collector-version.
#
# Exit 0 on valid; non-zero on any error.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COLLECTOR_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

VERSION_FILE="$COLLECTOR_DIR/.collector-version"
CONFIG_FILE="$COLLECTOR_DIR/config.yaml"

if [[ ! -f "$VERSION_FILE" ]]; then
    echo "ERROR: missing $VERSION_FILE" >&2
    exit 1
fi
if [[ ! -f "$CONFIG_FILE" ]]; then
    echo "ERROR: missing $CONFIG_FILE" >&2
    exit 1
fi

VERSION="$(tr -d '[:space:]' < "$VERSION_FILE")"
IMAGE="otel/opentelemetry-collector-contrib:$VERSION"

echo "Validating $CONFIG_FILE against $IMAGE ..."

# `validate` is a subcommand on newer collector images; use `--dry-run` or
# equivalent verification for older versions. For 0.106+, the validate
# subcommand is first-class.
docker run --rm \
    -v "$CONFIG_FILE:/etc/otel/config.yaml:ro" \
    "$IMAGE" \
    validate --config=/etc/otel/config.yaml

echo "OK: collector config valid under $IMAGE"
