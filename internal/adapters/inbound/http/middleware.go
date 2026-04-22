package http

import (
	"fmt"
	stdlog "log"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/oklog/ulid/v2"

	"github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/obs/log"
)

// panicRecoverer recovers from panics in downstream handlers and
// renders a stable 500 HTTPError envelope. The recovered value and
// a truncated stack are logged to the default logger.
//
// This complements the adapter-layer safeExecute (application/services)
// which catches panics inside outbound adapters. The two recover rings
// are independent: outbound panics never reach here because
// ExecuteService already converts them to receipts.
func panicRecoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				stdlog.Printf("http: panic recovered: %v\nstack:\n%s", rec, debug.Stack())
				writeHTTPError(w, http.StatusInternalServerError,
					"adapter_internal_error",
					fmt.Sprintf("panic recovered: %v", rec))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// requestIDHeader attaches a request-id to the response. chi's
// middleware.RequestID populates ctx; this middleware propagates to the
// X-Request-Id response header so operators can correlate client-side
// logs with server-side traces.
func requestIDHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id := r.Header.Get("X-Request-Id"); id != "" {
			w.Header().Set("X-Request-Id", id)
		}
		next.ServeHTTP(w, r)
	})
}

// LoggerMiddleware binds a request-scoped logger to ctx, enriched with
// correlation_id. Correlation id sources, in order:
//
//  1. X-Correlation-Id request header (trusted, pass-through).
//  2. A generated ULID when absent (written back into the request
//     headers so downstream handlers can read it too).
//
// A nil root falls back to a Nop logger so the never-nil ctx invariant
// holds regardless of how the middleware is wired in bootstrap.
//
// Registration order in router.go must be:
//
//	chimw.RequestID → LoggerMiddleware → requestIDHeader → panicRecoverer
//
// LoggerMiddleware runs AFTER chimw.RequestID (to inherit the request-id
// context) and BEFORE panicRecoverer (so recovered panics have a logger
// already bound).
//
// Spec §5.5.
func LoggerMiddleware(root *log.Logger) func(http.Handler) http.Handler {
	if root == nil {
		root = log.NewNop()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			corr := r.Header.Get("X-Correlation-Id")
			if corr == "" {
				corr = ulid.Make().String()
				r.Header.Set("X-Correlation-Id", corr)
			}
			reqLogger := root.With(slog.String("correlation_id", corr))
			ctx := log.ContextWith(r.Context(), reqLogger)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
