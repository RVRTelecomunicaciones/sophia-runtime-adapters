package obs

import (
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// Registry holds the named instruments per spec §9.7. Construct via
// NewRegistry; pass to application + adapter code that needs to emit
// metrics. Thread-safe (underlying OTel instruments are safe for
// concurrent use).
//
// Counters:
//
//	execute_attempted_total{capability}
//	execute_timeout_total{capability}
//	execute_cancelled_total{capability}
//	execute_panics_recovered_total{adapter}
//	idempotency_hit_total
//	idempotency_miss_total
//
// Histograms:
//
//	execute_duration_ms{capability}
//	persist_duration_ms
//	adapter_execute_duration_ms{adapter}
//
// Gauges (observable counters):
//
//	bytes_read_total{capability}
//	bytes_written_total{capability}
type Registry struct {
	ExecuteAttempted         metric.Int64Counter
	ExecuteTimeout           metric.Int64Counter
	ExecuteCancelled         metric.Int64Counter
	ExecutePanicsRecovered   metric.Int64Counter
	IdempotencyHit           metric.Int64Counter
	IdempotencyMiss          metric.Int64Counter
	ExecuteDurationMs        metric.Int64Histogram
	PersistDurationMs        metric.Int64Histogram
	AdapterExecuteDurationMs metric.Int64Histogram
	BytesRead                metric.Int64Counter
	BytesWritten             metric.Int64Counter
}

// NewRegistry builds a Registry using the global MeterProvider. Call
// AFTER SetupOTel — otherwise the no-op provider is used and all
// instruments are silent (safe for local/test runs).
func NewRegistry(instrumentationName string) (*Registry, error) {
	meter := otel.Meter(instrumentationName)

	attempted, err := meter.Int64Counter("execute_attempted_total",
		metric.WithDescription("Count of Execute calls attempted (per capability)."))
	if err != nil {
		return nil, fmt.Errorf("execute_attempted_total: %w", err)
	}
	timeout, err := meter.Int64Counter("execute_timeout_total",
		metric.WithDescription("Count of Execute calls that ended in timeout."))
	if err != nil {
		return nil, fmt.Errorf("execute_timeout_total: %w", err)
	}
	cancelled, err := meter.Int64Counter("execute_cancelled_total",
		metric.WithDescription("Count of Execute calls that ended in cancellation."))
	if err != nil {
		return nil, fmt.Errorf("execute_cancelled_total: %w", err)
	}
	panics, err := meter.Int64Counter("execute_panics_recovered_total",
		metric.WithDescription("Count of adapter panics recovered by the runtime."))
	if err != nil {
		return nil, fmt.Errorf("execute_panics_recovered_total: %w", err)
	}
	idemHit, err := meter.Int64Counter("idempotency_hit_total",
		metric.WithDescription("Idempotency cache hits."))
	if err != nil {
		return nil, fmt.Errorf("idempotency_hit_total: %w", err)
	}
	idemMiss, err := meter.Int64Counter("idempotency_miss_total",
		metric.WithDescription("Idempotency cache misses."))
	if err != nil {
		return nil, fmt.Errorf("idempotency_miss_total: %w", err)
	}
	execDur, err := meter.Int64Histogram("execute_duration_ms",
		metric.WithDescription("End-to-end Execute duration in ms (per capability)."),
		metric.WithUnit("ms"))
	if err != nil {
		return nil, fmt.Errorf("execute_duration_ms: %w", err)
	}
	persistDur, err := meter.Int64Histogram("persist_duration_ms",
		metric.WithDescription("Receipt persistence duration in ms."),
		metric.WithUnit("ms"))
	if err != nil {
		return nil, fmt.Errorf("persist_duration_ms: %w", err)
	}
	adapterDur, err := meter.Int64Histogram("adapter_execute_duration_ms",
		metric.WithDescription("Per-adapter Execute call duration in ms."),
		metric.WithUnit("ms"))
	if err != nil {
		return nil, fmt.Errorf("adapter_execute_duration_ms: %w", err)
	}
	bytesRead, err := meter.Int64Counter("bytes_read_total",
		metric.WithDescription("Total bytes read by capabilities."),
		metric.WithUnit("By"))
	if err != nil {
		return nil, fmt.Errorf("bytes_read_total: %w", err)
	}
	bytesWritten, err := meter.Int64Counter("bytes_written_total",
		metric.WithDescription("Total bytes written by capabilities."),
		metric.WithUnit("By"))
	if err != nil {
		return nil, fmt.Errorf("bytes_written_total: %w", err)
	}

	return &Registry{
		ExecuteAttempted:         attempted,
		ExecuteTimeout:           timeout,
		ExecuteCancelled:         cancelled,
		ExecutePanicsRecovered:   panics,
		IdempotencyHit:           idemHit,
		IdempotencyMiss:          idemMiss,
		ExecuteDurationMs:        execDur,
		PersistDurationMs:        persistDur,
		AdapterExecuteDurationMs: adapterDur,
		BytesRead:                bytesRead,
		BytesWritten:             bytesWritten,
	}, nil
}
