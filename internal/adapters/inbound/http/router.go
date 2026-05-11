package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/obs/log"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/inbound"
)

// NewRouter builds the Phase 1 HTTP router. svc and query are the
// inbound ports; both must be non-nil. logger is the root logger
// bound into the request context by LoggerMiddleware; a nil logger
// falls back to log.NewNop() (never-nil invariant).
//
// readiness is OPTIONAL — when non-nil, GET /readyz is mounted and
// probes the dependency with a 2s timeout (P1.4 / ADR-0005). When nil,
// the route is omitted entirely, returning 404. /healthz (liveness)
// remains always-on and dependency-free.
//
// T52 (execute) and T53 (capabilities + receipts) provide the real
// handler implementations wired below.
func NewRouter(svc inbound.RuntimeService, query inbound.QueryService, logger *log.Logger, readiness Readiness) http.Handler {
	if svc == nil {
		panic("http.NewRouter: RuntimeService is required")
	}
	if query == nil {
		panic("http.NewRouter: QueryService is required")
	}
	if logger == nil {
		logger = log.NewNop()
	}

	r := chi.NewRouter()

	// Middleware chain (spec §5.5):
	//   chimw.RequestID → LoggerMiddleware → requestIDHeader → panicRecoverer
	// LoggerMiddleware runs AFTER chimw.RequestID (to inherit the request-id
	// ctx) and BEFORE panicRecoverer (so recovered panics have a logger
	// already bound in ctx).
	r.Use(chimw.RequestID)
	r.Use(LoggerMiddleware(logger))
	r.Use(requestIDHeader)
	r.Use(panicRecoverer)

	// API v1 group.
	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/execute", NewExecuteHandler(svc, ExecuteHandlerConfig{}))
		r.Get("/capabilities", NewCapabilitiesHandler(query))
		r.Get("/receipts/{id}", NewReceiptsHandler(query))
	})

	// Liveness probe (useful for ops from day one — not in spec but
	// trivial and non-controversial). Dependency-free by design: a
	// 200 here means "the process is up and routing", nothing more.
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// Readiness probe (P1.4 / ADR-0005). Mounted only when a probe
	// was wired by the caller — keeps the contract test suite and
	// SDK-equivalence tests free from DB-readiness coupling.
	// Convention: -z suffix to mirror /healthz.
	if readiness != nil {
		r.Method(http.MethodGet, "/readyz", NewReadyzHandler(readiness))
	}

	return r
}
