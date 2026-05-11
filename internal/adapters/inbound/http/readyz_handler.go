package http

import (
	"context"
	"net/http"
	"time"
)

// Readiness probes a dependency for readiness. *pgxpool.Pool satisfies
// this interface via its Ping(ctx) error method, so the bootstrap layer
// passes the pool directly without any adapter shim.
//
// Kept as a tiny interface (one method) for two reasons:
//   1. Hexagonal hygiene — the inbound HTTP layer must not import a
//      concrete pgx type.
//   2. Tests in this package can substitute an in-memory probe (success
//      / failure / slow) without touching real Postgres.
type Readiness interface {
	Ping(ctx context.Context) error
}

// defaultReadyTimeout caps the DB probe to bound /readyz latency under
// degraded DB conditions. Kept small on purpose — readiness is a hot
// path for orchestrators (k8s, compose healthcheck) and a slow probe
// just causes thrashing on real outages.
const defaultReadyTimeout = 2 * time.Second

// readyResponse is the JSON envelope returned by /readyz.
// Status:
//   - "ready"     — all checks ok (HTTP 200)
//   - "degraded"  — at least one check failed (HTTP 503)
type readyResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

// NewReadyzHandler returns an http.Handler that probes readiness with
// a 2s timeout. probe is required (panics on nil) — callers that lack
// a probe should not mount the route.
//
// Response contract:
//   - 200 + {"status":"ready","checks":{"db":"ok"}}        when probe ok
//   - 503 + {"status":"degraded","checks":{"db":"<err>"}}  when probe fails
//
// Notes:
//   - No metrics are emitted. /healthz emits none either (router.go);
//     adding HTTP-route labels would breach R16 (bounded cardinality)
//     and the instrument catalog in obs/metrics.go does not declare any
//     HTTP-route instrument.
//   - The handler does not log on success (avoid drowning the log on
//     1Hz probe traffic). Failures rely on the caller observing the
//     503 + body; logging is left to higher-level middleware.
func NewReadyzHandler(probe Readiness) http.Handler {
	if probe == nil {
		panic("http.NewReadyzHandler: Readiness probe is required")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), defaultReadyTimeout)
		defer cancel()

		if err := probe.Ping(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, readyResponse{
				Status: "degraded",
				Checks: map[string]string{"db": err.Error()},
			})
			return
		}
		writeJSON(w, http.StatusOK, readyResponse{
			Status: "ready",
			Checks: map[string]string{"db": "ok"},
		})
	})
}
