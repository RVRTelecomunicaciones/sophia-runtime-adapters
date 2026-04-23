# syntax=docker/dockerfile:1.7

# ---- build stage ---------------------------------------------------------
FROM golang:1.26.2-alpine AS build
WORKDIR /src

# Cache module downloads separately from source compilation.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 + trimpath + stripped symbols → static binary fit for
# distroless/static. `-s -w` removes symbol + DWARF info; `-trimpath`
# removes absolute paths from the binary so reproducibility improves.
RUN CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/runtime-adapters ./cmd/runtime-adapters

# ---- runtime stage -------------------------------------------------------
# Distroless/static: ~2 MiB base, no shell, no package manager, nonroot user.
# Production-realistic shape; also what 2C.4 will ship.
FROM gcr.io/distroless/static:nonroot

COPY --from=build /out/runtime-adapters /runtime-adapters

# Binary listens on :8080 per cmd/runtime-adapters config default; the
# compose stack uses service-level `cpus:` and `mem_limit:` to enforce
# the measurement envelope, so nothing else is declared here.
ENTRYPOINT ["/runtime-adapters"]
