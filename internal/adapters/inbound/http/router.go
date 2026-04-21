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
// T52 (execute) and T53 (capabilities + receipts) provide the real
// handler implementations wired below.
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
