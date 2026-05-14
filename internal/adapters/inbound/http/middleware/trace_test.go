package middleware

import (
	"bytes"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
)

// deterministicReader yields a repeating byte pattern so trace_id /
// span_id generation in tests is reproducible. R12 injectable-randomness
// pattern, mirrors orchestator's domain/trace tests.
func deterministicReader(seed byte) io.Reader {
	buf := make([]byte, 1024)
	for i := range buf {
		buf[i] = seed + byte(i%17)
	}
	return bytes.NewReader(buf)
}

// captureHandler stores the inbound ctx so tests can inspect the
// SpanContext that the middleware installed.
func captureHandler(captured *trace.SpanContext) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*captured = trace.SpanContextFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
}

func TestTraceW3C_ExtractsTraceparentIntoOTelContext(t *testing.T) {
	const inboundTrace = "4bf92f3577b34da6a3ce929d0e0e4736"
	const inboundSpan = "00f067aa0ba902b7"
	traceparent := "00-" + inboundTrace + "-" + inboundSpan + "-01"

	var got trace.SpanContext
	mw := TraceW3C(deterministicReader(0x42), slog.Default())
	h := mw(captureHandler(&got))

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	req.Header.Set("Traceparent", traceparent)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, got.IsValid(), "ctx must carry a valid SpanContext")
	require.Equal(t, inboundTrace, got.TraceID().String(),
		"OTEL trace_id must match the orchestator-supplied trace_id (the bridge contract)")
	require.Equal(t, inboundSpan, got.SpanID().String())
	require.True(t, got.IsSampled(), "sampled flag must round-trip")
	require.True(t, got.IsRemote(), "extracted span must be marked remote")

	// Echoed Traceparent must round-trip the same trace_id.
	echoed := rec.Header().Get("Traceparent")
	require.True(t, strings.Contains(echoed, inboundTrace),
		"response Traceparent %q must contain inbound trace_id %q", echoed, inboundTrace)
}

func TestTraceW3C_GeneratesFreshWhenNoHeaders(t *testing.T) {
	var got trace.SpanContext
	mw := TraceW3C(deterministicReader(0x42), slog.Default())
	h := mw(captureHandler(&got))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.True(t, got.IsValid(), "fresh span context must be valid")
	require.NotEqual(t, strings.Repeat("0", 32), got.TraceID().String(),
		"generated trace_id must not be all zeros")
	require.NotEqual(t, strings.Repeat("0", 16), got.SpanID().String(),
		"generated span_id must not be all zeros")

	// Determinism: same seed → same trace_id.
	expectedTID := make([]byte, 16)
	for i := range expectedTID {
		expectedTID[i] = 0x42 + byte(i%17)
	}
	require.Equal(t, hex.EncodeToString(expectedTID), got.TraceID().String(),
		"deterministic reader must yield deterministic trace_id")

	require.NotEmpty(t, rec.Header().Get("Traceparent"),
		"response must echo freshly-generated Traceparent")
}

func TestTraceW3C_XRequestIDFallback_32HexDirect(t *testing.T) {
	const reqID = "deadbeefdeadbeefdeadbeefdeadbeef"

	var got trace.SpanContext
	mw := TraceW3C(deterministicReader(0x42), slog.Default())
	h := mw(captureHandler(&got))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-Id", reqID)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.True(t, got.IsValid())
	require.Equal(t, reqID, got.TraceID().String(),
		"32-hex X-Request-Id must map directly to trace_id (deterministic correlation)")
}

func TestTraceW3C_XRequestIDFallback_UUIDStripDashes(t *testing.T) {
	// 8-4-4-4-12 UUID form → strip dashes → 32 hex.
	const reqID = "12345678-1234-1234-1234-123456789012"
	const wantTrace = "12345678123412341234123456789012"

	var got trace.SpanContext
	mw := TraceW3C(deterministicReader(0x42), slog.Default())
	h := mw(captureHandler(&got))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-Id", reqID)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.True(t, got.IsValid())
	require.Equal(t, wantTrace, got.TraceID().String())
}

func TestTraceW3C_XRequestIDFallback_ArbitraryString(t *testing.T) {
	// Non-hex, non-UUID → XOR-fold path. Must produce a stable, non-zero
	// trace_id for the same input across requests.
	const reqID = "my-custom-correlation-id-xyz"

	var first, second trace.SpanContext
	mw := TraceW3C(deterministicReader(0x42), slog.Default())

	h1 := mw(captureHandler(&first))
	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	req1.Header.Set("X-Request-Id", reqID)
	h1.ServeHTTP(httptest.NewRecorder(), req1)

	h2 := mw(captureHandler(&second))
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.Header.Set("X-Request-Id", reqID)
	h2.ServeHTTP(httptest.NewRecorder(), req2)

	require.True(t, first.IsValid())
	require.True(t, second.IsValid())
	require.Equal(t, first.TraceID().String(), second.TraceID().String(),
		"same X-Request-Id must produce the same trace_id across calls")
	require.NotEqual(t, strings.Repeat("0", 32), first.TraceID().String())
}

func TestTraceW3C_MalformedTraceparentFallsBackToFresh(t *testing.T) {
	var got trace.SpanContext
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	mw := TraceW3C(deterministicReader(0x42), logger)
	h := mw(captureHandler(&got))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Traceparent", "garbage-not-a-traceparent")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.True(t, got.IsValid(),
		"malformed Traceparent must NOT block the request — fresh trace generated instead")
	require.NotEmpty(t, rec.Header().Get("Traceparent"))
	require.Contains(t, buf.String(), "malformed",
		"malformed Traceparent must produce a WARN log so operators can spot misconfig")
}

func TestTraceW3C_TraceparentTakesPriorityOverXRequestID(t *testing.T) {
	const inboundTrace = "4bf92f3577b34da6a3ce929d0e0e4736"
	traceparent := "00-" + inboundTrace + "-00f067aa0ba902b7-01"

	var got trace.SpanContext
	mw := TraceW3C(deterministicReader(0x42), slog.Default())
	h := mw(captureHandler(&got))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Traceparent", traceparent)
	req.Header.Set("X-Request-Id", "deadbeefdeadbeefdeadbeefdeadbeef")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, inboundTrace, got.TraceID().String(),
		"Traceparent must win when both headers are present")
}

func TestTraceW3C_NilLoggerFallsBackToDefault(t *testing.T) {
	mw := TraceW3C(deterministicReader(0x42), nil)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	require.NotPanics(t, func() {
		h.ServeHTTP(rec, req)
	}, "nil logger must fall back silently to slog.Default()")
}

func TestTraceW3C_NilRandReaderFallsBackToCryptoRand(t *testing.T) {
	mw := TraceW3C(nil, slog.Default())
	var got trace.SpanContext
	h := mw(captureHandler(&got))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.True(t, got.IsValid(),
		"nil rand reader must fall back to crypto/rand.Reader and still produce a valid span")
}

func TestTraceIDFromRequestID_EdgeCases(t *testing.T) {
	t.Run("all-zero hex rejected", func(t *testing.T) {
		_, err := traceIDFromRequestID("00000000000000000000000000000000")
		// Falls through to XOR-fold which yields all-zero too; defensive
		// branch sets LSB to 0x01.
		require.NoError(t, err)
	})
	t.Run("empty string", func(t *testing.T) {
		// XOR-fold of empty is all-zero → defensive LSB → non-zero.
		out, err := traceIDFromRequestID("")
		require.NoError(t, err)
		require.False(t, allZero(out))
	})
}
