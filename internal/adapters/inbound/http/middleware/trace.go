// Package middleware contains chi-compatible HTTP middlewares for the
// runtime-adapters inbound transport that are independent from the chi-
// owned set in ../middleware.go (panic recovery, request-id echo,
// logger binding).
//
// trace.go implements W3C traceparent propagation bridged into the OTEL
// SDK (ADR-0005 P2.2e — Phase B for runtime-adapters). Unlike the
// orchestator counterpart which carries its own domain.Trace value
// object, runtime-adapters already has OTEL fully wired (Phase 2C.1) so
// the middleware leverages OTEL's TextMapPropagator directly. This
// keeps the trace_id flowing into every existing span without
// introducing a parallel trace abstraction.
package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// TraceHeader is the canonical W3C trace-context header name.
const TraceHeader = "Traceparent"

// requestIDHeader is the fallback correlation header name used when
// Traceparent is absent. Lower-case-r form matches the chi default.
const requestIDHeader = "X-Request-Id"

// TraceW3C returns chi-compatible middleware that bridges inbound W3C
// Traceparent headers into the OTEL request context. Resolution order:
//
//  1. Traceparent header → OTEL TextMapPropagator.Extract.
//     Resulting ctx already carries the remote SpanContext, so every
//     downstream span (and every slog record passing through
//     OTelContextHandler) inherits the orchestator-supplied trace_id.
//
//  2. X-Request-Id header → deterministically derive a trace_id from
//     the value (UUID-style 32 hex direct, otherwise XOR-fold of raw
//     bytes), generate a fresh span_id, install as remote SpanContext.
//
//  3. Neither header present → generate a fresh trace_id + span_id,
//     install as remote SpanContext.
//
// In all three branches the resolved traceparent is echoed back on the
// response via the same propagator before the next handler runs, so
// callers can correlate retries even when they did not supply a header.
//
// randReader is the random source for fallback / fresh paths. Production
// callers MUST pass crypto/rand.Reader. Tests use a deterministic
// io.Reader (R12 injectable-randomness pattern, mirrors orchestator's
// trace.New).
//
// logger receives one WARN per malformed Traceparent so operators can
// catch mis-configured callers. nil falls back to slog.Default().
//
// Register this middleware as the FIRST entry in the chain — before the
// existing logger / panic-recovery middlewares — so that:
//   - log records emitted by downstream middlewares already carry
//     trace_id + span_id (via OTelContextHandler in obs/log).
//   - any span created downstream inherits the remote parent.
func TraceW3C(randReader io.Reader, logger *slog.Logger) func(http.Handler) http.Handler {
	if randReader == nil {
		randReader = rand.Reader
	}
	if logger == nil {
		logger = slog.Default()
	}
	prop := propagation.TraceContext{}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := resolveTraceContext(r, prop, randReader, logger)

			// Echo Traceparent so callers can correlate across retries
			// and follow redirects. We always echo: if extraction
			// succeeded, the echoed value matches the inbound header;
			// otherwise we expose the freshly-generated trace.
			//
			// Inject uses the W3C propagator so the format is
			// guaranteed-canonical and round-trips through Parse.
			prop.Inject(ctx, propagation.HeaderCarrier(w.Header()))

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// resolveTraceContext returns a ctx that carries a remote SpanContext
// suitable for the OTEL slog handler and for downstream span creation.
//
// The function is total: it always returns a ctx with a valid remote
// span context, regardless of header presence or shape. Malformed
// Traceparent values are logged at WARN and treated like "no header".
func resolveTraceContext(r *http.Request, prop propagation.TraceContext, randReader io.Reader, logger *slog.Logger) context.Context {
	ctx := r.Context()

	if raw := r.Header.Get(TraceHeader); raw != "" {
		extracted := prop.Extract(ctx, propagation.HeaderCarrier(r.Header))
		sc := trace.SpanContextFromContext(extracted)
		if sc.IsValid() {
			return extracted
		}
		// Header was present but the OTEL parser refused it. Emit a
		// warning so operators notice mis-configured callers; we do
		// NOT reject the request because malformed traceparent is a
		// caller-side mistake, not a security event.
		logger.LogAttrs(ctx, slog.LevelWarn, "trace: malformed Traceparent header, generating fresh trace",
			slog.String("raw_header", raw),
		)
	}

	if rid := r.Header.Get(requestIDHeader); rid != "" {
		if sc, err := spanContextFromRequestID(rid, randReader); err == nil {
			return trace.ContextWithRemoteSpanContext(ctx, sc)
		}
	}

	sc, err := freshSpanContext(randReader)
	if err != nil {
		// Last resort: rand source is broken. Returning the original
		// ctx means downstream code runs without a trace_id, which is
		// degraded but still functional — preferred over rejecting
		// the request.
		logger.LogAttrs(ctx, slog.LevelError, "trace: failed to generate fresh trace context",
			slog.String("error", err.Error()),
		)
		return ctx
	}
	return trace.ContextWithRemoteSpanContext(ctx, sc)
}

// freshSpanContext generates a brand-new SpanContext with random
// trace_id + span_id, marked sampled and remote so that
// propagation.TraceContext.Inject emits a well-formed Traceparent.
func freshSpanContext(randReader io.Reader) (trace.SpanContext, error) {
	var tid trace.TraceID
	if _, err := io.ReadFull(randReader, tid[:]); err != nil {
		return trace.SpanContext{}, err
	}
	var sid trace.SpanID
	if _, err := io.ReadFull(randReader, sid[:]); err != nil {
		return trace.SpanContext{}, err
	}
	return trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    tid,
		SpanID:     sid,
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	}), nil
}

// spanContextFromRequestID derives a SpanContext from an X-Request-Id
// header so that every service correlating on the same request-id
// yields the same trace_id. Strategy mirrors orchestator's
// trace.FromRequestID:
//
//   - 32 hex chars after dash-strip → used directly as trace_id;
//   - otherwise → 16-byte XOR fold of raw bytes (deterministic, non-
//     cryptographic — goal is stable correlation, not secrecy).
//
// span_id is always freshly random; SHA-stable span ids would force
// the runtime to claim it is the same span as the upstream, which it
// is not.
func spanContextFromRequestID(rid string, randReader io.Reader) (trace.SpanContext, error) {
	tidBytes, err := traceIDFromRequestID(rid)
	if err != nil {
		return trace.SpanContext{}, err
	}
	var tid trace.TraceID
	copy(tid[:], tidBytes)

	var sid trace.SpanID
	if _, err := io.ReadFull(randReader, sid[:]); err != nil {
		return trace.SpanContext{}, err
	}
	return trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    tid,
		SpanID:     sid,
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	}), nil
}

// traceIDFromRequestID returns 16 bytes of trace_id derived from the
// supplied X-Request-Id. Returns an error if the result would be the
// invalid all-zero trace_id.
func traceIDFromRequestID(rid string) ([]byte, error) {
	stripped := strings.ToLower(strings.ReplaceAll(rid, "-", ""))
	if len(stripped) == 32 {
		if decoded, err := hex.DecodeString(stripped); err == nil && !allZero(decoded) {
			return decoded, nil
		}
	}
	out := make([]byte, 16)
	for i, b := range []byte(rid) {
		out[i%16] ^= b
	}
	if allZero(out) {
		// Defensively avoid the invalid all-zero trace_id. Setting the
		// LSB matches orchestator's fallback in trace.FromRequestID.
		out[15] = 0x01
	}
	if allZero(out) {
		return nil, errors.New("trace: derived trace_id is zero")
	}
	return out, nil
}

func allZero(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}
