// Package obs — pgxpool collector for Phase 2C.4 / E.
//
// Wires pgxpool.Pool.Stat() snapshots to OTel observable instruments
// under the runtime_adapters.pgx_pool.* namespace. Six metrics, zero
// labels (R16 cardinality bounded), all observed in ONE callback so
// they share a single Stat() snapshot per export tick (no torn read).
package obs

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
)

// PoolStatSnapshot is the project-internal value type for one
// point-in-time view of pgxpool stats. Decouples the collector from
// pgx-specific *pgxpool.Stat (which has unexported fields and cannot
// be constructed in tests). Field types match what the OTel
// instruments accept (int64). The two Count fields are CUMULATIVE
// MONOTONIC values direct from pgxpool — NOT deltas. The OTel SDK
// and Prometheus rate() at query time compute rates from these
// cumulatives.
//
// See spec §3 P3 + D2C4E.5 for the cumulative-vs-delta contract.
type PoolStatSnapshot struct {
	IdleConns         int64
	MaxConns          int64
	TotalConns        int64
	AcquiredConns     int64
	AcquireCount      int64 // cumulative monotonic
	EmptyAcquireCount int64 // cumulative monotonic
}

// PoolStatProvider returns a snapshot. Production wires this against
// pgxpool.Pool.Stat() via SnapshotFromPgx; tests wire a closure
// returning fixed values.
type PoolStatProvider func() PoolStatSnapshot

// PgxPoolCollector wires PoolStatSnapshot values to OTel observable
// instruments. Six metrics, zero labels. All observed in ONE callback
// so they share a single Stat() snapshot per export tick (P2 — no
// torn read across the 6 fields).
type PgxPoolCollector struct {
	snapshotFn        PoolStatProvider
	idleConns         metric.Int64ObservableGauge
	maxConns          metric.Int64ObservableGauge
	totalConns        metric.Int64ObservableGauge
	acquiredConns     metric.Int64ObservableGauge
	acquireCount      metric.Int64ObservableCounter
	emptyAcquireCount metric.Int64ObservableCounter
	registration      metric.Registration
}

// NewPgxPoolCollector constructs and registers the collector against the
// supplied meter. Each instrument's name maps directly to a field on
// PoolStatSnapshot — see the inline mapping below.
func NewPgxPoolCollector(meter metric.Meter, snapshotFn PoolStatProvider) (*PgxPoolCollector, error) {
	if snapshotFn == nil {
		return nil, fmt.Errorf("pgxpool collector: snapshotFn must not be nil")
	}
	c := &PgxPoolCollector{snapshotFn: snapshotFn}

	var err error
	// ---- Gauges (current state, no _total suffix in Prom) ----
	if c.idleConns, err = meter.Int64ObservableGauge(
		"runtime_adapters.pgx_pool.idle_conns",
		metric.WithDescription("pgxpool.Stat().IdleConns — connections currently idle in the pool."),
		metric.WithUnit("{connections}"),
	); err != nil {
		return nil, fmt.Errorf("idle_conns: %w", err)
	}
	if c.maxConns, err = meter.Int64ObservableGauge(
		"runtime_adapters.pgx_pool.max_conns",
		metric.WithDescription("pgxpool.Stat().MaxConns — pool capacity ceiling."),
		metric.WithUnit("{connections}"),
	); err != nil {
		return nil, fmt.Errorf("max_conns: %w", err)
	}
	if c.totalConns, err = meter.Int64ObservableGauge(
		"runtime_adapters.pgx_pool.total_conns",
		metric.WithDescription("pgxpool.Stat().TotalConns — connections currently in the pool (idle + acquired + constructing)."),
		metric.WithUnit("{connections}"),
	); err != nil {
		return nil, fmt.Errorf("total_conns: %w", err)
	}
	if c.acquiredConns, err = meter.Int64ObservableGauge(
		"runtime_adapters.pgx_pool.acquired_conns",
		metric.WithDescription("pgxpool.Stat().AcquiredConns — connections currently in use by callers."),
		metric.WithUnit("{connections}"),
	); err != nil {
		return nil, fmt.Errorf("acquired_conns: %w", err)
	}
	// ---- Counters (cumulative monotonic — Prom appends _total) ----
	if c.acquireCount, err = meter.Int64ObservableCounter(
		"runtime_adapters.pgx_pool.acquire_count",
		metric.WithDescription("pgxpool.Stat().AcquireCount — cumulative successful acquires since pool startup."),
		metric.WithUnit("{acquires}"),
	); err != nil {
		return nil, fmt.Errorf("acquire_count: %w", err)
	}
	if c.emptyAcquireCount, err = meter.Int64ObservableCounter(
		"runtime_adapters.pgx_pool.empty_acquire_count",
		metric.WithDescription("pgxpool.Stat().EmptyAcquireCount — cumulative acquires that found the pool empty (saturation marker)."),
		metric.WithUnit("{acquires}"),
	); err != nil {
		return nil, fmt.Errorf("empty_acquire_count: %w", err)
	}

	// One callback observes all 6 from a single Stat() snapshot (P2).
	c.registration, err = meter.RegisterCallback(c.observe,
		c.idleConns, c.maxConns, c.totalConns, c.acquiredConns,
		c.acquireCount, c.emptyAcquireCount,
	)
	if err != nil {
		return nil, fmt.Errorf("register callback: %w", err)
	}
	return c, nil
}

// observe is the registered callback. ONE Stat() snapshot per call;
// all 6 instruments observed from the same view (P2). Errors are
// silently dropped — a Stat() call never errors today (it returns a
// value type), but the OTel API requires we return error. If the
// snapshotFn ever panics (e.g., pool closed mid-call), the SDK
// recovers; we don't trap here because there's no useful action.
func (c *PgxPoolCollector) observe(_ context.Context, o metric.Observer) error {
	s := c.snapshotFn()
	o.ObserveInt64(c.idleConns, s.IdleConns)
	o.ObserveInt64(c.maxConns, s.MaxConns)
	o.ObserveInt64(c.totalConns, s.TotalConns)
	o.ObserveInt64(c.acquiredConns, s.AcquiredConns)
	o.ObserveInt64(c.acquireCount, s.AcquireCount)
	o.ObserveInt64(c.emptyAcquireCount, s.EmptyAcquireCount)
	return nil
}

// Close unregisters the callback. Safe to call multiple times.
// Bootstrap calls this before pool.Close() so the SDK does not retain
// a callback that closes over a torn pool.
func (c *PgxPoolCollector) Close() error {
	if c.registration == nil {
		return nil
	}
	err := c.registration.Unregister()
	c.registration = nil
	return err
}

// SnapshotFromPgx is the production wrapper: convert *pgxpool.Stat
// to PoolStatSnapshot. Cumulative counter fields are read directly
// (no delta computation here — see PoolStatSnapshot doc + spec §3 P3).
// Gauge fields are int32 in pgx; widened to int64 for OTel.
func SnapshotFromPgx(s *pgxpool.Stat) PoolStatSnapshot {
	return PoolStatSnapshot{
		IdleConns:         int64(s.IdleConns()),
		MaxConns:          int64(s.MaxConns()),
		TotalConns:        int64(s.TotalConns()),
		AcquiredConns:     int64(s.AcquiredConns()),
		AcquireCount:      s.AcquireCount(),
		EmptyAcquireCount: s.EmptyAcquireCount(),
	}
}
