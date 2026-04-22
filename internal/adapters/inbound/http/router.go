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
// T52 (execute) and T53 (capabilities + receipts) provide the real
// handler implementations wired below.
func NewRouter(svc inbound.RuntimeService, query inbound.QueryService, logger *log.Logger) http.Handler {
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

	// Health check (useful for ops from day one — not in spec but
	// trivial and non-controversial).
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	return r
}
