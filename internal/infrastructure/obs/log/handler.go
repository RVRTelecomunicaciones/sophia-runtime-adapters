package log

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// OTelContextHandler wraps an inner slog.Handler and, on each Handle
// call, extracts the active OTel SpanContext from ctx. When the span
// context is valid, it adds `trace_id` and `span_id` string attributes
// to the record before delegating. When there is no active span, the
// record is delegated unchanged.
//
// Per D2C4D.9 (spec §8): runtime emits OTel log signal carrying
// trace_id + span_id at minimum. This handler is the single insertion
// point for those fields; callers continue to use the standard
// logger.{Info,Warn,Error}Context API and the trace context flows
// from request middleware via ctx.
type OTelContextHandler struct {
	inner slog.Handler
}

// NewOTelContextHandler returns a handler that wraps inner and injects
// trace_id + span_id from the ctx SpanContext when present. inner must
// be non-nil; passing nil panics — callers should always layer this
// over a real handler such as slog.NewJSONHandler.
func NewOTelContextHandler(inner slog.Handler) *OTelContextHandler {
	if inner == nil {
		panic("log.NewOTelContextHandler: inner handler is nil")
	}
	return &OTelContextHandler{inner: inner}
}

// Enabled delegates to the inner handler.
func (h *OTelContextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

// Handle injects trace_id + span_id when ctx carries an active span,
// then delegates to the inner handler.
func (h *OTelContextHandler) Handle(ctx context.Context, r slog.Record) error {
	sc := trace.SpanContextFromContext(ctx)
	if sc.IsValid() {
		r.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}
	return h.inner.Handle(ctx, r)
}

// WithAttrs returns a new OTelContextHandler whose inner handler has
// the given attrs bound, preserving the trace context injection.
func (h *OTelContextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &OTelContextHandler{inner: h.inner.WithAttrs(attrs)}
}

// WithGroup returns a new OTelContextHandler whose inner handler is in
// a named group, preserving the trace context injection.
func (h *OTelContextHandler) WithGroup(name string) slog.Handler {
	return &OTelContextHandler{inner: h.inner.WithGroup(name)}
}
