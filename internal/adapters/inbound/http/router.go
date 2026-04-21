package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/inbound"
)

// NewRouter builds the Phase 1 HTTP router. svc and query are the
// inbound ports; both must be non-nil.
//
// The three routes are registered as STUBS in T51 — T52 (execute),
// T53 (capabilities + receipts) replace the stub bodies with real
// handlers.
func NewRouter(svc inbound.RuntimeService, query inbound.QueryService) http.Handler {
	if svc == nil {
		panic("http.NewRouter: RuntimeService is required")
	}
	if query == nil {
		panic("http.NewRouter: QueryService is required")
	}

	r := chi.NewRouter()

	// Middleware chain: request-id → logger → panicRecoverer → handler.
	r.Use(chimw.RequestID)
	r.Use(requestIDHeader)
	r.Use(panicRecoverer)

	// API v1 group.
	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/execute", stubNotImplemented("execute"))
		r.Get("/capabilities", stubNotImplemented("capabilities"))
		r.Get("/receipts/{id}", stubNotImplemented("receipts"))
	})

	// Health check (useful for ops from day one — not in spec but
	// trivial and non-controversial).
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	return r
}

// stubNotImplemented returns a 501 handler that marks the route as
// pending (T52/T53 replace these).
func stubNotImplemented(route string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeHTTPError(w, http.StatusNotImplemented,
			"adapter_internal_error",
			"handler pending for route: "+route)
	}
}
