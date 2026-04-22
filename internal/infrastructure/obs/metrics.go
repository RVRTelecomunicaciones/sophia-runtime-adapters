// Package obs holds the observability primitives for runtime-adapters.
// The metric contract — instrument names, types, units, label sets —
// is defined exclusively in this file (invariant I23). Operational
// components (collector, scraper) MUST NOT rename or drop these.
//
// See docs/metrics.md for the public catalog and
// docs/superpowers/specs/2026-04-21-phase-2c-observability-slos-design.md
// §6.3 for authoritative naming and semantics.
package obs

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Instrument is the declarative entry for the metric contract. Used by
// TestMetricContract_* to enforce label blacklist (R16) and per-instrument
// cardinality budget.
type Instrument struct {
	Name    string
	Type    string // "counter" | "histogram" | "gauge" | "updowncounter"
	Unit    string
	Labels  []string
	Bounded map[string]int // optional — for cardinality budget test
}

// InstrumentCatalog returns the authoritative set of declared instruments.
// Must match the instruments constructed in NewRegistry exactly.
// See §6.3 for semantics, R16 for label discipline, I23 for contract.
func InstrumentCatalog() []Instrument {
	return []Instrument{
		{Name: "runtime_adapters.execution.total", Type: "counter", Unit: "{executions}",
			Labels:  []string{"capability", "status"},
			Bounded: map[string]int{"capability": 8, "status": 5}},
		{Name: "runtime_adapters.execution.duration", Type: "histogram", Unit: "s",
			Labels:  []string{"capability"},
			Bounded: map[string]int{"capability": 8}},
		{Name: "runtime_adapters.execution.active", Type: "updowncounter", Unit: "{executions}",
			Labels:  []string{"capability"},
			Bounded: map[string]int{"capability": 8}},
		{Name: "runtime_adapters.concurrency.rejects", Type: "counter", Unit: "{rejects}",
			Labels: nil},
		{Name: "runtime_adapters.adapter.panics", Type: "counter", Unit: "{panics}",
			Labels:  []string{"adapter"},
			Bounded: map[string]int{"adapter": 4}},
		{Name: "runtime_adapters.receipt.persist.failures", Type: "counter", Unit: "{failures}",
			Labels: nil},
		{Name: "runtime_adapters.idempotency.replays", Type: "counter", Unit: "{replays}",
			Labels:  []string{"capability"},
			Bounded: map[string]int{"capability": 8}},
		{Name: "runtime_adapters.pool.connections.acquired.duration", Type: "histogram", Unit: "s",
			Labels: nil},
		{Name: "runtime_adapters.otel.exporter.queue.size", Type: "gauge", Unit: "{items}",
			Labels: []string{"signal"}},
		{Name: "runtime_adapters.migrate.failures", Type: "counter", Unit: "{failures}",
			Labels: nil},
		{Name: "runtime_adapters.partial.signal", Type: "counter", Unit: "{partials}",
			Labels:  []string{"capability"},
			Bounded: map[string]int{"capability": 8}},
	}
}

// DurationBuckets is the pinned bucket boundary set for
// runtime_adapters.execution.duration. MUST include every threshold
// referenced in ops/slo/*.yaml (§7.3 provisional targets: 0.5, 1, 2, 3,
// 5, 10, 30). Extras round out the distribution for dashboards.
// TestDurationBuckets_CoverSlothThresholds (Bundle 4) enforces the
// Sloth ↔ bucket cross-check.
var DurationBuckets = []float64{0.1, 0.25, 0.5, 1, 2, 3, 5, 10, 30, 60}

// Registry holds the 11 Phase 2C.1 instruments. Construct via NewRegistry;
// pass to ExecuteService and adapters that need to emit metrics.
type Registry struct {
	ExecutionTotal        metric.Int64Counter
	ExecutionDuration     metric.Float64Histogram
	ExecutionActive       metric.Int64UpDownCounter
	ConcurrencyRejects    metric.Int64Counter
	AdapterPanics         metric.Int64Counter
	ReceiptPersistFails   metric.Int64Counter
	IdempotencyReplays    metric.Int64Counter
	PoolAcquireDuration   metric.Float64Histogram
	OtelExporterQueueSize metric.Int64Gauge
	MigrateFailures       metric.Int64Counter
	PartialSignal         metric.Int64Counter
}

// NewRegistry constructs the 11 §6.3 instruments bound to the provided
// meter. Callers typically pass otel.Meter("runtime-adapters") from
// bootstrap, after SetupOTel. Safe with the no-op provider for tests.
func NewRegistry(meter metric.Meter) (*Registry, error) {
	var r Registry
	var err error

	if r.ExecutionTotal, err = meter.Int64Counter(
		"runtime_adapters.execution.total",
		metric.WithUnit("{executions}"),
		metric.WithDescription("Total executions by capability and status."),
	); err != nil {
		return nil, fmt.Errorf("execution.total: %w", err)
	}
	if r.ExecutionDuration, err = meter.Float64Histogram(
		"runtime_adapters.execution.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Success-only execution duration per capability."),
		metric.WithExplicitBucketBoundaries(DurationBuckets...),
	); err != nil {
		return nil, fmt.Errorf("execution.duration: %w", err)
	}
	if r.ExecutionActive, err = meter.Int64UpDownCounter(
		"runtime_adapters.execution.active",
		metric.WithUnit("{executions}"),
		metric.WithDescription("In-flight executions per capability."),
	); err != nil {
		return nil, fmt.Errorf("execution.active: %w", err)
	}
	if r.ConcurrencyRejects, err = meter.Int64Counter(
		"runtime_adapters.concurrency.rejects",
		metric.WithUnit("{rejects}"),
		metric.WithDescription("Fast-reject count (A9.1)."),
	); err != nil {
		return nil, fmt.Errorf("concurrency.rejects: %w", err)
	}
	if r.AdapterPanics, err = meter.Int64Counter(
		"runtime_adapters.adapter.panics",
		metric.WithUnit("{panics}"),
		metric.WithDescription("Recovered adapter panics per adapter (R4)."),
	); err != nil {
		return nil, fmt.Errorf("adapter.panics: %w", err)
	}
	if r.ReceiptPersistFails, err = meter.Int64Counter(
		"runtime_adapters.receipt.persist.failures",
		metric.WithUnit("{failures}"),
		metric.WithDescription("Receipt persistence failures (A4.3)."),
	); err != nil {
		return nil, fmt.Errorf("receipt.persist.failures: %w", err)
	}
	if r.IdempotencyReplays, err = meter.Int64Counter(
		"runtime_adapters.idempotency.replays",
		metric.WithUnit("{replays}"),
		metric.WithDescription("Idempotency replay hits per capability (D6.4)."),
	); err != nil {
		return nil, fmt.Errorf("idempotency.replays: %w", err)
	}
	if r.PoolAcquireDuration, err = meter.Float64Histogram(
		"runtime_adapters.pool.connections.acquired.duration",
		metric.WithUnit("s"),
		metric.WithDescription("pgx pool acquire latency."),
	); err != nil {
		return nil, fmt.Errorf("pool.connections.acquired.duration: %w", err)
	}
	if r.OtelExporterQueueSize, err = meter.Int64Gauge(
		"runtime_adapters.otel.exporter.queue.size",
		metric.WithUnit("{items}"),
		metric.WithDescription("OTel exporter queue depth per signal."),
	); err != nil {
		return nil, fmt.Errorf("otel.exporter.queue.size: %w", err)
	}
	if r.MigrateFailures, err = meter.Int64Counter(
		"runtime_adapters.migrate.failures",
		metric.WithUnit("{failures}"),
		metric.WithDescription("Bootstrap migration failures."),
	); err != nil {
		return nil, fmt.Errorf("migrate.failures: %w", err)
	}
	if r.PartialSignal, err = meter.Int64Counter(
		"runtime_adapters.partial.signal",
		metric.WithUnit("{partials}"),
		metric.WithDescription("Secondary simplified partial-outcome signal."),
	); err != nil {
		return nil, fmt.Errorf("partial.signal: %w", err)
	}

	return &r, nil
}

// RecordExecution is the sole emission helper for execution.total,
// execution.duration (success-only per §6.3), and partial.signal
// (secondary simplified signal when status=partial). Used by
// ExecuteService at Step 11 in Bundle 3.
//
// §6.5 + A2C1.10: when a span is active on ctx, the SDK automatically
// attaches {trace_id, span_id} as exemplars on the execution.duration
// observation. receiptID is attached as an observation attribute; a View
// in SetupOTel drops it from the aggregation key so the attribute
// survives as an exemplar tag (FilteredAttributes) without inflating
// metric cardinality (R16). receiptID is best-effort — when empty, no
// attribute is attached and the observation still proceeds. Exemplar
// attachment itself never fails: the SDK exposes no error path.
func (r *Registry) RecordExecution(
	ctx context.Context,
	capability, status, receiptID string,
	durationSec float64,
) {
	attrs := attribute.NewSet(
		attribute.String("capability", capability),
		attribute.String("status", status),
	)
	r.ExecutionTotal.Add(ctx, 1, metric.WithAttributeSet(attrs))
	if status == "success" {
		// Include receipt_id as an observation attribute so the SDK can
		// carry it on exemplars (FilteredAttributes). The SetupOTel View
		// strips it from the aggregation key to keep cardinality bounded.
		durAttrs := []attribute.KeyValue{
			attribute.String("capability", capability),
		}
		if receiptID != "" {
			durAttrs = append(durAttrs, attribute.String("receipt_id", receiptID))
		}
		r.ExecutionDuration.Record(ctx, durationSec, metric.WithAttributes(durAttrs...))
	}
	if status == "partial" {
		capAttr := attribute.NewSet(attribute.String("capability", capability))
		r.PartialSignal.Add(ctx, 1, metric.WithAttributeSet(capAttr))
	}
}
