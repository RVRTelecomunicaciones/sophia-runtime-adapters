package http

import (
	"fmt"
	"log"
	"net/http"
	"runtime/debug"
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
				log.Printf("http: panic recovered: %v\nstack:\n%s", rec, debug.Stack())
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
