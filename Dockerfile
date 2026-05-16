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

# ---- llm-base stage (shared LLM runtime foundation) ---------------------
# Common base for all three LLM-capable targets:
#   - runtime-adapters-llm-opencode (opencode CLI dispatcher)
#   - runtime-adapters-llm-ollama   (ollama binary, daemon runs externally)
#   - runtime-adapters-llm-aider    (aider-chat via pipx)
#
# Provides: debian:12-slim + tini PID 1 + nonroot user 65532 + ca-certificates
# + git + openssh-client.
#
# Tini (PID 1) is REQUIRED across all LLM targets: LLM subprocesses fork
# child processes that may not exit cleanly; tini reaps zombies and forwards
# signals correctly. Combined with shell.exec@v1's enforced timeout_budget_ms
# this bounds the worst-case impact of any upstream subprocess hang.
FROM debian:12-slim AS llm-base
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
 && mkdir -p /home/nonroot/.config /home/nonroot/.local/share \
 && chown -R nonroot:nonroot /home/nonroot

COPY --from=build /out/runtime-adapters /usr/local/bin/runtime-adapters

ENV HOME=/home/nonroot
USER nonroot
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/runtime-adapters"]

# ---- runtime-adapters-llm-opencode runtime stage ------------------------
# debian:12-slim base with opencode CLI + git + tini for real SDD cycles.
# Coexists with the distroless `runtime-adapters` target:
#   - `runtime-adapters`             (default): minimal, no LLM dispatcher
#   - `runtime-adapters-llm-opencode` (this):   can spawn opencode subprocess
#
# Use this target when shell.exec@v1 needs to invoke opencode (the
# orchestrator's dispatcher is wired to OpenCode). For pure capability
# execution without LLM, prefer the distroless target.
#
# Permissions defaults are set via /home/nonroot/.config/opencode/opencode.json
# (mounted as a read-only secret in compose.llm.yaml) to avoid the upstream
# headless-hang behavior of the default `ask` permission level (issue #14473).
#
# See ADR-0012 for full design rationale (base image choice, version pin,
# checksum policy, multi-arch support).
FROM llm-base AS runtime-adapters-llm-opencode
RUN mkdir -p /home/nonroot/.config/opencode /home/nonroot/.local/share/opencode \
 && chown -R nonroot:nonroot /home/nonroot
COPY --from=opencode-bin /usr/local/bin/opencode /usr/local/bin/opencode

# ---- runtime-adapters-llm (alias for runtime-adapters-llm-opencode) -----
# Backward-compatible alias. Existing deployments that reference the
# `-llm` tag suffix continue to work without a values.yaml change.
# New deployments should use the explicit `-llm-opencode` suffix per
# the llm.target="opencode" Helm value.
FROM runtime-adapters-llm-opencode AS runtime-adapters-llm

# ---- ollama-bin stage ---------------------------------------------------
# Fetches the official ollama static binary for the target architecture.
# The install script from ollama.com/download is the upstream-recommended
# installation method; we extract only the binary placement from it and
# do NOT start the daemon here. The operator is expected to run the ollama
# daemon externally (ollama/ollama image or host binary) and expose it at
# OLLAMA_HOST. The adapter hits that endpoint; no GPU drivers needed in
# this image.
#
# Install approach: the official install script places the binary at
# /usr/local/bin/ollama. We replicate that placement directly by
# downloading the prebuilt binary from the GitHub release assets —
# same source the install script uses — avoiding the need to execute
# the script in the build context. Asset naming:
#   amd64 → ollama-linux-amd64.tgz   (inside: bin/ollama)
#   arm64 → ollama-linux-arm64.tgz
FROM debian:12-slim AS ollama-bin
# Default pinned here (inside the stage) so it takes effect without
# --build-arg. Pattern matches the opencode-bin stage above.
ARG OLLAMA_VERSION=0.9.0
ARG TARGETARCH
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl \
 && rm -rf /var/lib/apt/lists/* \
 && case "${TARGETARCH}" in \
      amd64) ASSET="ollama-linux-amd64.tgz" ;; \
      arm64) ASSET="ollama-linux-arm64.tgz" ;; \
      *) echo "unsupported TARGETARCH: ${TARGETARCH}" >&2; exit 1 ;; \
    esac \
 && mkdir -p /tmp/ollama-dl \
 && curl -fsSL \
      "https://github.com/ollama/ollama/releases/download/v${OLLAMA_VERSION}/${ASSET}" \
      -o /tmp/ollama-dl/ollama.tgz \
 && tar -xzf /tmp/ollama-dl/ollama.tgz -C /tmp/ollama-dl \
 && install -m 0755 /tmp/ollama-dl/bin/ollama /usr/local/bin/ollama \
 && rm -rf /tmp/ollama-dl \
 && /usr/local/bin/ollama --version

# ---- runtime-adapters-llm-ollama runtime stage --------------------------
# LLM target for ollama-backed dispatch. Bundles the ollama BINARY only —
# NOT the daemon. The operator runs the ollama daemon externally (e.g.
# ollama/ollama:latest Kubernetes Deployment or a host process with GPU
# access) and exposes it at OLLAMA_HOST. This image hits that endpoint
# via shell.exec@v1 → `ollama run <model>` or via the Ollama HTTP API
# directly from Go code.
#
# Why binary-only (no daemon)?
#   - GPU drivers, CUDA/ROCm libs, and large model weights live outside
#     this container by design — putting them here would balloon the image
#     to tens of GB and break the single-responsibility principle.
#   - The operator manages model availability and GPU scheduling on the
#     daemon side; the adapter only needs the CLI binary to interact.
#
# OLLAMA_HOST default: http://ollama.ollama.svc.cluster.local:11434
# Override via the Helm chart (llm.target="ollama" → env var injected).
FROM llm-base AS runtime-adapters-llm-ollama
COPY --from=ollama-bin /usr/local/bin/ollama /usr/local/bin/ollama

# ---- aider-bin stage ----------------------------------------------------
# Installs aider-chat via pipx so Python dependencies are fully isolated
# from the system Python. pipx creates a dedicated virtualenv per tool in
# /root/.local/pipx/venvs/aider-chat/ and symlinks the binary into
# /root/.local/bin/aider (or with --home the pipx home directory).
#
# We install as root in this intermediate stage so that we can then copy
# the entire pipx virtualenv tree into the final llm-base image and place
# the binary at /usr/local/bin/aider (required: allowedCommandsPath is
# /usr/local/bin:/usr/bin:/bin in both compose and Helm).
#
# pipx vs pip:
#   - pip install --system-site-packages pollutes the system Python which
#     risks breakage if any other package later pip-installs conflicting
#     deps; pipx strictly isolates per-tool.
#   - The resulting venv is self-contained and copyable as-is; no pip or
#     Python toolchain is needed in the final runtime image beyond the
#     venv's own interpreter.
FROM debian:12-slim AS aider-bin
ARG AIDER_VERSION=0.82.2
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        python3 \
        python3-venv \
        pipx \
 && rm -rf /var/lib/apt/lists/* \
 && PIPX_HOME=/opt/pipx PIPX_BIN_DIR=/usr/local/bin \
    pipx install "aider-chat==${AIDER_VERSION}" \
 && /usr/local/bin/aider --version

# ---- runtime-adapters-llm-aider runtime stage ---------------------------
# LLM target for aider-backed dispatch. aider-chat lands at
# /usr/local/bin/aider (from the pipx install above copied via the
# aider-bin stage), satisfying the allowedCommandsPath whitelist
# (/usr/local/bin:/usr/bin:/bin) without any config change.
#
# aider does NOT bundle its own API keys — the operator MUST supply:
#   ANTHROPIC_API_KEY  — for Claude models (recommended)
#   OPENAI_API_KEY     — for GPT-4 models
# via the Helm chart (llm.target="aider" → secrets.aiderEnv Secret).
#
# The Python venv (created by pipx in aider-bin) is self-contained and
# does not require pip/pipx/python3 to be present in the runtime image —
# the venv ships its own Python interpreter. We install python3 and
# python3-venv via apt for libpython3.X.so (the shared library the venv
# interpreter links to at runtime), then clean up the Python dev tooling.
FROM llm-base AS runtime-adapters-llm-aider
# python3 provides the shared library (libpython3.11.so.1.0 on debian:12)
# that the pipx-created venv interpreter links against at runtime. Without
# it the aider binary crashes with "error while loading shared libraries".
USER root
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
        python3 \
 && rm -rf /var/lib/apt/lists/*
COPY --from=aider-bin /opt/pipx /opt/pipx
COPY --from=aider-bin /usr/local/bin/aider /usr/local/bin/aider
USER nonroot
