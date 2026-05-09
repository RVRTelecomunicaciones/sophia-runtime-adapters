package log_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	loglocal "github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/obs/log"
)

func newJSONHandler(buf *bytes.Buffer) slog.Handler {
	return slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
}

func TestOTelContextHandler_NoSpan_NoFieldsAdded(t *testing.T) {
	var buf bytes.Buffer
	inner := newJSONHandler(&buf)
	h := loglocal.NewOTelContextHandler(inner)
	logger := slog.New(h)

	logger.InfoContext(context.Background(), "no-span")

	var rec map[string]any
	if err := json.Unmarshal(bytes.TrimRight(buf.Bytes(), "\n"), &rec); err != nil {
		t.Fatalf("unmarshal: %v (raw=%q)", err, buf.String())
	}
	if _, ok := rec["trace_id"]; ok {
		t.Errorf("trace_id must NOT be present without an active span; got record %s", buf.String())
	}
	if _, ok := rec["span_id"]; ok {
		t.Errorf("span_id must NOT be present without an active span; got record %s", buf.String())
	}
}

func TestOTelContextHandler_ActiveSpan_TraceIDAndSpanIDAdded(t *testing.T) {
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(tracetest.NewInMemoryExporter())))
	tracer := tp.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test-span")
	defer span.End()

	var buf bytes.Buffer
	inner := newJSONHandler(&buf)
	h := loglocal.NewOTelContextHandler(inner)
	logger := slog.New(h)

	logger.InfoContext(ctx, "with-span")

	var rec map[string]any
	if err := json.Unmarshal(bytes.TrimRight(buf.Bytes(), "\n"), &rec); err != nil {
		t.Fatalf("unmarshal: %v (raw=%q)", err, buf.String())
	}

	tid, ok := rec["trace_id"].(string)
	if !ok || len(tid) != 32 {
		t.Errorf("trace_id: want 32-char hex string, got %v (record=%s)", rec["trace_id"], buf.String())
	}
	sid, ok := rec["span_id"].(string)
	if !ok || len(sid) != 16 {
		t.Errorf("span_id: want 16-char hex string, got %v (record=%s)", rec["span_id"], buf.String())
	}
	if !strings.Contains(tid, span.SpanContext().TraceID().String()) {
		t.Errorf("trace_id mismatch: got %q, want %q", tid, span.SpanContext().TraceID().String())
	}
}

func TestOTelContextHandler_DelegatesToInnerHandler(t *testing.T) {
	var buf bytes.Buffer
	inner := newJSONHandler(&buf)
	h := loglocal.NewOTelContextHandler(inner)
	logger := slog.New(h)

	logger.InfoContext(context.Background(), "msg-test", slog.String("custom", "value"))

	if !strings.Contains(buf.String(), `"msg":"msg-test"`) {
		t.Errorf("inner handler must emit msg field; got %s", buf.String())
	}
	if !strings.Contains(buf.String(), `"custom":"value"`) {
		t.Errorf("inner handler must emit custom attrs; got %s", buf.String())
	}
}
