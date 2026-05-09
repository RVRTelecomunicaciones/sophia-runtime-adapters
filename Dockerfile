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
