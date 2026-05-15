# syntax=docker/dockerfile:1.7

# ---- build stage (shared) ------------------------------------------------
# One build stage compiles ALL binaries; per-binary runtime stages select
# the artifact via --target. Per D2C4AB.6.
FROM golang:1.26.2-alpine AS build
WORKDIR /src

# Cache module downloads separately from source compilation.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 + trimpath + stripped symbols → static binaries fit for
# distroless/static. `-s -w` removes symbol + DWARF info; `-trimpath`
# removes absolute paths from the binary so reproducibility improves.
RUN CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/runtime-adapters ./cmd/runtime-adapters

RUN CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/linear-webhook-adapter ./cmd/linear-webhook-adapter

RUN CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/grafana-annotations-webhook ./cmd/grafana-annotations-webhook

# ---- runtime-adapters runtime stage --------------------------------------
# Distroless/static: ~2 MiB base, no shell, no package manager, nonroot user.
FROM gcr.io/distroless/static:nonroot AS runtime-adapters

COPY --from=build /out/runtime-adapters /runtime-adapters

# Binary listens on :8080 per cmd/runtime-adapters config default; the
# compose stack uses service-level `cpus:` and `mem_limit:` to enforce
# the measurement envelope, so nothing else is declared here.
ENTRYPOINT ["/runtime-adapters"]

# ---- linear-webhook-adapter runtime stage --------------------------------
# Same distroless base as runtime-adapters. Listens on :9095. Per D2C4AB.4
# the adapter has independent lifecycle from runtime-adapters — separate
# container, separate restart, separate health.
FROM gcr.io/distroless/static:nonroot AS linear-webhook-adapter

COPY --from=build /out/linear-webhook-adapter /linear-webhook-adapter

ENTRYPOINT ["/linear-webhook-adapter"]

# ---- grafana-annotations-webhook runtime stage ----------------------------
# Phase 2C.4 / D — Bundle 2. Same distroless base as runtime-adapters and
# linear-webhook-adapter. Listens on :9096. Per D2C4D.7 the adapter has
# independent lifecycle from runtime-adapters AND linear-webhook —
# separate container, separate restart, separate health. The shared
# `build` stage compiles all three binaries; the runtime image carries
# only the static binary, no toolchain.
FROM gcr.io/distroless/static:nonroot AS grafana-annotations-webhook

COPY --from=build /out/grafana-annotations-webhook /grafana-annotations-webhook

ENTRYPOINT ["/grafana-annotations-webhook"]

# ---- opencode-bin stage (LLM-capable target prerequisite) ---------------
# Downloads opencode CLI standalone tarball from upstream release.
# Pinned by OPENCODE_VERSION (override via --build-arg). The tarball is
# a bun-compiled standalone binary (~120 MB extracted), needs no Bun
# nor Node at runtime. See ADR-0009.
#
# Per the 2026-05-15 research, the linux releases ship CLI tarballs
# (NOT only desktop .deb/.AppImage). Asset naming pattern:
#   amd64 → opencode-linux-x64-baseline.tar.gz   (no AVX2 required)
#   arm64 → opencode-linux-arm64.tar.gz
# Baseline is preferred for amd64 to maximize host-CPU compatibility.
#
# No upstream checksums published (verified via gh release view); HTTPS
# to the GitHub CDN is the single point of trust. Documented in ADR-0009.
FROM debian:12-slim AS opencode-bin
# ARG inside the stage carries the default; the value can still be
# overridden via `docker buildx build --build-arg OPENCODE_VERSION=...`.
# A bare `ARG OPENCODE_VERSION` (without default) does NOT inherit the
# global ARG declared before the first FROM — that scope only feeds
# `FROM image:${VAR}` substitutions, not in-stage RUN expansions.
ARG OPENCODE_VERSION=1.14.48
ARG TARGETARCH
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl tar \
 && rm -rf /var/lib/apt/lists/* \
 && case "${TARGETARCH}" in \
      amd64) ASSET="opencode-linux-x64-baseline.tar.gz" ;; \
      arm64) ASSET="opencode-linux-arm64.tar.gz" ;; \
      *) echo "unsupported TARGETARCH: ${TARGETARCH}" >&2; exit 1 ;; \
    esac \
 && mkdir -p /tmp/opencode-dl \
 && curl -fsSL \
      "https://github.com/anomalyco/opencode/releases/download/v${OPENCODE_VERSION}/${ASSET}" \
      -o /tmp/opencode-dl/opencode.tar.gz \
 && tar -xzf /tmp/opencode-dl/opencode.tar.gz -C /tmp/opencode-dl \
 && install -m 0755 /tmp/opencode-dl/opencode /usr/local/bin/opencode \
 && rm -rf /tmp/opencode-dl \
 && /usr/local/bin/opencode --version

# ---- runtime-adapters-llm runtime stage ---------------------------------
# debian:12-slim base with opencode CLI + git + tini for cycles SDD reales.
# Coexists with the distroless `runtime-adapters` target above:
#   - `runtime-adapters` (default): minimal, no LLM dispatcher possible
#   - `runtime-adapters-llm` (this): can spawn opencode subprocess
#
# Use this target when shell.exec@v1 needs to invoke opencode (i.e. the
# orchestrator's dispatcher is wired to OpenCode). For pure capability
# execution without LLM, prefer the distroless target.
#
# Tini (PID 1) is REQUIRED here per upstream bug anomalyco/opencode#17516
# (opencode run hangs after tool calls); tini reaps zombies and forwards
# signals cleanly. Combined with shell.exec@v1's enforced timeout this
# bounds the worst-case impact of the bug.
#
# Permissions defaults are set via /home/nonroot/.config/opencode/opencode.json
# (mounted as a read-only secret in compose.llm.yaml) to avoid the upstream
# headless-hang behavior of the default `ask` permission level (issue #14473).
#
# See ADR-0009 for full design rationale (base image choice, version pin,
# checksum policy, multi-arch support).
FROM debian:12-slim AS runtime-adapters-llm
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
        ca-certificates \
        git \
        openssh-client \
        tini \
 && rm -rf /var/lib/apt/lists/* \
 && groupadd --system --gid 65532 nonroot \
 && useradd --system --uid 65532 --gid 65532 \
        --home-dir /home/nonroot --create-home \
        --shell /bin/bash nonroot \
 && mkdir -p /home/nonroot/.config/opencode /home/nonroot/.local/share/opencode \
 && chown -R nonroot:nonroot /home/nonroot
COPY --from=opencode-bin /usr/local/bin/opencode /usr/local/bin/opencode
COPY --from=build /out/runtime-adapters /usr/local/bin/runtime-adapters
USER nonroot
ENV HOME=/home/nonroot
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/runtime-adapters"]
