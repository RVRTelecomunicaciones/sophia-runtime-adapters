//go:build integration

// Package integration contains per-PR chaos integration tests (Bundle 3).
// They exercise each of the 6 CI fault profiles from Bundle 2 end-to-end
// within a single Go process — no compose stack required.
//
// Pattern:
//   - A testcontainers Postgres is started per sub-test.
//   - An OTel ManualReader-backed MeterProvider is installed as the global
//     before calling bootstrap.BuildRuntime so metrics are capturable.
//   - bootstrap.BuildRuntime composes the full runtime with chaos enabled.
//   - sdk.NewClient wraps the runtime's inbound services for SDK-peer access.
//   - After execution, MetricSnapshotter reads from the ManualReader.
//
// Tag: -tags=integration  (never runs in the default `go test ./...`).
package integration

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sophia-ecosystem/runtime-adapters/internal/adapters/inbound/sdk"
	"github.com/sophia-ecosystem/runtime-adapters/internal/bootstrap"
	"github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/chaos"
	"github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/config"
)

// ChaosApp bundles the SDK client, a receipt counter, and the metric
// snapshotter returned by ChaosTestApp.
type ChaosApp struct {
	// SDK is the in-process SDK client wired to the chaos runtime's services.
	SDK *sdk.Client
	// Receipts provides test-side access to the execution_receipts table.
	Receipts *ReceiptHelper
	// SnapshotMetrics reads the OTel ManualReader and returns a typed snapshot.
	SnapshotMetrics func(t *testing.T) *MetricSnapshot
}

// ChaosTestApp wires a full runtime app with chaos enabled for the given
// profileRel path (relative to the repo root) and returns a ChaosApp plus
// a teardown function. Tests MUST defer teardown.
//
// OTel setup: installs a sdkmetric.ManualReader as the global MeterProvider
// BEFORE calling bootstrap.BuildRuntime. Since SetupOTel with OTelEnabled=false
// does not replace the global provider (see otel.go), the ManualReader provider
// survives through the entire bootstrap. obs.NewRegistry binds all instruments
// to it, making them capturable via reader.Collect.
//
// bootstrap.Runtime now exposes ExecSvc and QuerySvc (added in B3 Task 3.1),
// which enables direct SDK peer construction without going through HTTP.
func ChaosTestApp(t *testing.T, profileRel string) (*ChaosApp, func()) {
	t.Helper()

	// 1. OTel: install a ManualReader-backed MeterProvider as the global.
	//    Must happen BEFORE BuildRuntime so obs.NewRegistry binds instruments to it.
	//    SetupOTel with OTelEnabled=false leaves the global provider intact (see otel.go).
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)

	// 2. Postgres testcontainer.
	dsn, pgTeardown := startPGForChaos(t)

	// 3. Config — chaos enabled, profile path set, allowlists include /tmp.
	cfg := config.Config{
		HTTPAddr:                 "127.0.0.1:0",
		MaxTimeoutBudget:         30 * time.Minute,
		MaxPayloadBytes:          1 << 20,
		InlineStreamLimit:        16 * 1024,
		IdempotencyWindow:        24 * time.Hour,
		ShutdownGracePeriod:      15 * time.Second,
		MaxConcurrentExecutions:  64,
		AllowedCommandsPath:      []string{"/usr/bin", "/bin"},
		AllowedWorkingDirs:       []string{"/tmp"},
		AllowedFilesystemRoots:   []string{"/tmp"},
		HTTPAllowPrivateNetworks: true,
		PostgresDSN:              dsn,
		RuntimeVersion:           "0.1.0-chaos-test",
		Hostname:                 "chaos-test",
		ProvenanceSource:         "test",
		// OTelEnabled=false: SetupOTel skips replacing the global provider,
		// leaving the ManualReader-backed MeterProvider in place.
		OTelEnabled: false,
		Env:         "development",
		Chaos: chaos.ChaosConfigEnv{
			Enabled:     true,
			ProfilePath: profileRel,
		},
	}

	// 4. BuildRuntime — composes the full app, runs migrations, wraps
	//    adapters and receipt store with chaos. Now exposes ExecSvc/QuerySvc.
	rt, err := bootstrap.BuildRuntime(context.Background(), cfg)
	if err != nil {
		pgTeardown()
		t.Fatalf("BuildRuntime with chaos: %v", err)
	}

	// 5. SDK client — uses the runtime's inbound services directly (in-process).
	sdkClient, err := sdk.NewClient(rt.ExecSvc, rt.QuerySvc, sdk.ClientConfig{
		MaxTimeoutBudget: 30 * time.Minute,
	})
	if err != nil {
		_ = rt.Shutdown(context.Background())
		pgTeardown()
		t.Fatalf("sdk.NewClient: %v", err)
	}

	// 6. Test-side pool for CountAll queries (same DSN, separate pool).
	//    BuildRuntime already ran migrations on this DSN.
	pool := openPool(t, dsn)

	receipts := &ReceiptHelper{pool: pool}

	snapshotFn := func(t *testing.T) *MetricSnapshot {
		t.Helper()
		var rm metricdata.ResourceMetrics
		if err := reader.Collect(context.Background(), &rm); err != nil {
			t.Fatalf("OTel ManualReader.Collect: %v", err)
		}
		return &MetricSnapshot{rm: rm}
	}

	teardown := func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = rt.Shutdown(shutCtx)
		pool.Close()
		pgTeardown()
		// Restore the global MeterProvider to a no-op so subsequent sub-tests
		// don't share this reader's state.
		otel.SetMeterProvider(metricnoop.NewMeterProvider())
	}

	return &ChaosApp{
		SDK:             sdkClient,
		Receipts:        receipts,
		SnapshotMetrics: snapshotFn,
	}, teardown
}

// ReceiptHelper wraps a pgxpool to provide test-side queries against
// execution_receipts without importing the pg adapter package.
type ReceiptHelper struct {
	pool *pgxpool.Pool
}

// CountAll returns the total number of rows in execution_receipts.
func (r *ReceiptHelper) CountAll(ctx context.Context) (int64, error) {
	const q = `SELECT COUNT(*) FROM execution_receipts`
	var n int64
	row := r.pool.QueryRow(ctx, q)
	if err := row.Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// MetricSnapshot is a typed view into a collected OTel ResourceMetrics.
type MetricSnapshot struct {
	rm metricdata.ResourceMetrics
}

// ExecutionTotal returns the sum of runtime_adapters.execution.total data
// points that match both the given capability label and status label.
// Returns 0 if no matching data point is found.
func (s *MetricSnapshot) ExecutionTotal(capability, status string) int64 {
	for _, sm := range s.rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "runtime_adapters.execution.total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				capVal, capOK := dp.Attributes.Value("capability")
				statusVal, statusOK := dp.Attributes.Value("status")
				if capOK && statusOK &&
					capVal.AsString() == capability &&
					statusVal.AsString() == status {
					return dp.Value
				}
			}
		}
	}
	return 0
}

// PersistFailures returns the current value of the
// runtime_adapters.receipt.persist.failures counter. Returns 0 if not found.
func (s *MetricSnapshot) PersistFailures() int64 {
	for _, sm := range s.rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "runtime_adapters.receipt.persist.failures" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			var total int64
			for _, dp := range sum.DataPoints {
				total += dp.Value
			}
			return total
		}
	}
	return 0
}
