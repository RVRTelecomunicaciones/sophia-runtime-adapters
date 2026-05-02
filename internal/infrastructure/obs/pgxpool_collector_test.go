package obs_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/obs"
)

// TestPgxPoolCollector_AllSixMetricsObservedFromSingleSnapshot verifies
// the contract from spec §3 P2: one Stat() snapshot per callback,
// all 6 metrics emitted with the snapshot's field values.
//
// Spec: docs/superpowers/specs/2026-05-01-phase-2c.4-e-pgx-pool-collector-design.md §4.4
func TestPgxPoolCollector_AllSixMetricsObservedFromSingleSnapshot(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := mp.Meter("test.pgxpool")

	snap := obs.PoolStatSnapshot{
		IdleConns:         3,
		MaxConns:          10,
		TotalConns:        7,
		AcquiredConns:     4,
		AcquireCount:      100,
		EmptyAcquireCount: 5,
	}
	collector, err := obs.NewPgxPoolCollector(meter, func() obs.PoolStatSnapshot { return snap })
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, collector.Close()) })

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))

	got := readAllInt64Metrics(t, rm)

	expected := map[string]int64{
		"runtime_adapters.pgx_pool.idle_conns":          3,
		"runtime_adapters.pgx_pool.max_conns":           10,
		"runtime_adapters.pgx_pool.total_conns":         7,
		"runtime_adapters.pgx_pool.acquired_conns":      4,
		"runtime_adapters.pgx_pool.acquire_count":       100,
		"runtime_adapters.pgx_pool.empty_acquire_count": 5,
	}
	for name, want := range expected {
		require.Equalf(t, want, got[name], "metric %s value mismatch", name)
	}
}

// TestPgxPoolCollector_CounterIsCumulativeMonotonicNotDelta verifies P3 +
// D2C4E.5 + A2C4E.2: when AcquireCount in the snapshot grows from 100
// to 150 between two collects, the OTel counter reports 150 (cumulative)
// — NOT 50 (the delta). Prom rate() at query time computes rates from
// cumulatives; emitting deltas here would double-count.
//
// Spec: docs/superpowers/specs/2026-05-01-phase-2c.4-e-pgx-pool-collector-design.md §3 P3
func TestPgxPoolCollector_CounterIsCumulativeMonotonicNotDelta(t *testing.T) {
	var current obs.PoolStatSnapshot
	snapFn := func() obs.PoolStatSnapshot { return current }

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	collector, err := obs.NewPgxPoolCollector(mp.Meter("test.pgxpool"), snapFn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, collector.Close()) })

	// First collect: counter at 100.
	current = obs.PoolStatSnapshot{AcquireCount: 100}
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	got := readAllInt64Metrics(t, rm)
	require.Equal(t, int64(100), got["runtime_adapters.pgx_pool.acquire_count"],
		"first collect: AcquireCount expected 100, got %d", got["runtime_adapters.pgx_pool.acquire_count"])

	// Second collect: counter advanced to 150.
	current = obs.PoolStatSnapshot{AcquireCount: 150}
	require.NoError(t, reader.Collect(context.Background(), &rm))
	got = readAllInt64Metrics(t, rm)
	// Expect 150 (cumulative), NOT 50 (delta).
	require.Equalf(t, int64(150), got["runtime_adapters.pgx_pool.acquire_count"],
		"second collect: counter must be cumulative monotonic (got %d, expected 150). "+
			"If this is 50, the collector is computing deltas — pgxpool.Stat() returns "+
			"cumulative monotonic values direct from pgx; pass them through unchanged. "+
			"See spec §3 P3.",
		got["runtime_adapters.pgx_pool.acquire_count"])
}

// readAllInt64Metrics walks ResourceMetrics and extracts every Int64
// gauge or sum data point keyed by metric name. Returns 0 for absent
// metrics so test failure messages stay clean.
func readAllInt64Metrics(t *testing.T, rm metricdata.ResourceMetrics) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch d := m.Data.(type) {
			case metricdata.Gauge[int64]:
				for _, dp := range d.DataPoints {
					out[m.Name] = dp.Value
				}
			case metricdata.Sum[int64]:
				for _, dp := range d.DataPoints {
					out[m.Name] = dp.Value
				}
			}
		}
	}
	return out
}
