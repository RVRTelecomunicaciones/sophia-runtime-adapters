package obs_test

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/obs"
)

// TestNewRegistry_AllInstrumentsNonNil verifies that NewRegistry
// returns a non-nil *Registry with all 11 instruments populated.
// Uses the global no-op provider (OTelEnabled=false, so SetupOTel was
// never called in this test — the SDK default no-op provider is active).
func TestNewRegistry_AllInstrumentsNonNil(t *testing.T) {
	t.Parallel()

	reg, err := obs.NewRegistry("test.instrumentation")
	if err != nil {
		t.Fatalf("NewRegistry returned error: %v", err)
	}
	if reg == nil {
		t.Fatal("NewRegistry returned nil registry")
	}

	// Counters
	if reg.ExecuteAttempted == nil {
		t.Error("ExecuteAttempted is nil")
	}
	if reg.ExecuteTimeout == nil {
		t.Error("ExecuteTimeout is nil")
	}
	if reg.ExecuteCancelled == nil {
		t.Error("ExecuteCancelled is nil")
	}
	if reg.ExecutePanicsRecovered == nil {
		t.Error("ExecutePanicsRecovered is nil")
	}
	if reg.IdempotencyHit == nil {
		t.Error("IdempotencyHit is nil")
	}
	if reg.IdempotencyMiss == nil {
		t.Error("IdempotencyMiss is nil")
	}

	// Histograms
	if reg.ExecuteDurationMs == nil {
		t.Error("ExecuteDurationMs is nil")
	}
	if reg.PersistDurationMs == nil {
		t.Error("PersistDurationMs is nil")
	}
	if reg.AdapterExecuteDurationMs == nil {
		t.Error("AdapterExecuteDurationMs is nil")
	}

	// Byte counters
	if reg.BytesRead == nil {
		t.Error("BytesRead is nil")
	}
	if reg.BytesWritten == nil {
		t.Error("BytesWritten is nil")
	}
}

// TestNewRegistry_InstrumentsCallable verifies each instrument can be
// called without panicking (no-op path; no values are actually exported).
func TestNewRegistry_InstrumentsCallable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	reg, err := obs.NewRegistry("test.callable")
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	capAttr := metric.WithAttributes(attribute.String("capability", "filesystem.read"))
	adapterAttr := metric.WithAttributes(attribute.String("adapter", "filesystem"))

	// Counters — Add must not panic.
	reg.ExecuteAttempted.Add(ctx, 1, capAttr)
	reg.ExecuteTimeout.Add(ctx, 1, capAttr)
	reg.ExecuteCancelled.Add(ctx, 1, capAttr)
	reg.ExecutePanicsRecovered.Add(ctx, 1, adapterAttr)
	reg.IdempotencyHit.Add(ctx, 1)
	reg.IdempotencyMiss.Add(ctx, 1)
	reg.BytesRead.Add(ctx, 512, capAttr)
	reg.BytesWritten.Add(ctx, 128, capAttr)

	// Histograms — Record must not panic.
	reg.ExecuteDurationMs.Record(ctx, 42, capAttr)
	reg.PersistDurationMs.Record(ctx, 7)
	reg.AdapterExecuteDurationMs.Record(ctx, 15, adapterAttr)
}
