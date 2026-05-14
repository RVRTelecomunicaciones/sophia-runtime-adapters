package http

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/stretchr/testify/require"

	"github.com/sophia-ecosystem/runtime-adapters/internal/adapters/inbound/http/middleware"
	"github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/obs/log"
)

// TestTraceW3C_BridgesIntoSlogViaOTelContextHandler is the contract
// test for P2.2e: when an inbound request carries a Traceparent, every
// slog record emitted with that request's ctx must contain trace_id +
// span_id matching the orchestator-supplied values. This proves the
// full chain TraceW3C → ctx → OTelContextHandler works end-to-end.
//
// Failure here means the bridge is broken: spans / logs would NOT
// correlate across services and ADR-0005 §P2.2e is violated.
func TestTraceW3C_BridgesIntoSlogViaOTelContextHandler(t *testing.T) {
	const inboundTrace = "4bf92f3577b34da6a3ce929d0e0e4736"
	const inboundSpan = "00f067aa0ba902b7"
	traceparent := "00-" + inboundTrace + "-" + inboundSpan + "-01"

	// Wire a real Logger backed by OTelContextHandler so trace_id /
	// span_id injection is exercised — not stubbed.
	var buf bytes.Buffer
	jsonHandler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	otelHandler := log.NewOTelContextHandler(jsonHandler)
	root := log.NewWithHandler(otelHandler)

	emitter := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bound := log.FromContext(r.Context())
		require.NotNil(t, bound)
		bound.Info(r.Context(), "test event")
		w.WriteHeader(http.StatusOK)
	})

	router := chi.NewRouter()
	router.Use(middleware.TraceW3C(rand.Reader, nil))
	router.Use(chimw.RequestID)
	router.Use(LoggerMiddleware(root))
	router.Handle("/probe", emitter)

	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.Header.Set("Traceparent", traceparent)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.NotEmpty(t, rec.Header().Get("Traceparent"),
		"Traceparent must be echoed on the response")

	// Decode every JSON record emitted and verify trace_id + span_id.
	var sawProbe bool
	dec := json.NewDecoder(&buf)
	for dec.More() {
		var record map[string]any
		require.NoError(t, dec.Decode(&record))
		if record["msg"] != "test event" {
			continue
		}
		sawProbe = true
		require.Equal(t, inboundTrace, record["trace_id"],
			"slog must carry the orchestator-supplied trace_id")
		require.Equal(t, inboundSpan, record["span_id"],
			"slog must carry the inbound span_id (root span until adapter starts its own)")
	}
	require.True(t, sawProbe, "test event record was never emitted — chain is broken")
}

// TestTraceW3C_BridgesGeneratedTraceIntoSlog covers the path where no
// header is supplied: the freshly-generated SpanContext must still flow
// into slog records so logs are correlatable for client-less traffic
// (cron probes, healthchecks).
func TestTraceW3C_BridgesGeneratedTraceIntoSlog(t *testing.T) {
	var buf bytes.Buffer
	otelHandler := log.NewOTelContextHandler(slog.NewJSONHandler(&buf, nil))
	root := log.NewWithHandler(otelHandler)

	emitter := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.FromContext(r.Context()).Info(r.Context(), "anonymous probe")
		w.WriteHeader(http.StatusOK)
	})

	router := chi.NewRouter()
	router.Use(middleware.TraceW3C(rand.Reader, nil))
	router.Use(LoggerMiddleware(root))
	router.Handle("/probe", emitter)

	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	dec := json.NewDecoder(&buf)
	for dec.More() {
		var record map[string]any
		require.NoError(t, dec.Decode(&record))
		if record["msg"] != "anonymous probe" {
			continue
		}
		require.NotEmpty(t, record["trace_id"],
			"generated trace_id must reach slog records via OTelContextHandler")
		require.NotEmpty(t, record["span_id"])
	}
}

// helper to silence unused-import warning when context import is dropped
// during edits; harmless.
var _ context.Context = context.Background()
