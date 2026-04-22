package obs_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"

	"github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/obs"
)

// Meter used across tests. No-op provider is active because SetupOTel
// is never called here — safe for all assertions.
func testMeter(name string) (r *obs.Registry) {
	meter := otel.Meter(name)
	reg, err := obs.NewRegistry(meter)
	if err != nil {
		panic(err)
	}
	return reg
}

// TestNewRegistry_BuildsAllInstruments verifies NewRegistry populates
// every field of Registry.
func TestNewRegistry_BuildsAllInstruments(t *testing.T) {
	r := testMeter("test.builds")
	require.NotNil(t, r.ExecutionTotal)
	require.NotNil(t, r.ExecutionDuration)
	require.NotNil(t, r.ExecutionActive)
	require.NotNil(t, r.ConcurrencyRejects)
	require.NotNil(t, r.AdapterPanics)
	require.NotNil(t, r.ReceiptPersistFails)
	require.NotNil(t, r.IdempotencyReplays)
	require.NotNil(t, r.PoolAcquireDuration)
	require.NotNil(t, r.OtelExporterQueueSize)
	require.NotNil(t, r.MigrateFailures)
	require.NotNil(t, r.PartialSignal)
}

// TestNewRegistry_InstrumentsCallable verifies each instrument accepts
// emission calls without panicking (no-op path).
func TestNewRegistry_InstrumentsCallable(t *testing.T) {
	ctx := context.Background()
	r := testMeter("test.callable")

	r.ExecutionTotal.Add(ctx, 1)
	r.ExecutionDuration.Record(ctx, 0.42)
	r.ExecutionActive.Add(ctx, 1)
	r.ConcurrencyRejects.Add(ctx, 1)
	r.AdapterPanics.Add(ctx, 1)
	r.ReceiptPersistFails.Add(ctx, 1)
	r.IdempotencyReplays.Add(ctx, 1)
	r.PoolAcquireDuration.Record(ctx, 0.07)
	r.OtelExporterQueueSize.Record(ctx, 42)
	r.MigrateFailures.Add(ctx, 0)
	r.PartialSignal.Add(ctx, 1)
}

// TestRecordExecution_SuccessOnlyDuration verifies execution.duration
// observations only happen when status=success (§6.3 invariant). With the
// no-op provider we can't assert on recorded values directly — but we CAN
// verify the helper doesn't panic for any status. A stronger assertion
// against a manual metric reader lands in Bundle 3 integration tests.
func TestRecordExecution_DoesNotPanicAcrossStatuses(t *testing.T) {
	ctx := context.Background()
	r := testMeter("test.record")
	for _, status := range []string{"success", "failure", "timeout", "cancelled", "partial"} {
		r.RecordExecution(ctx, "shell.exec@v1", status, 0.123)
	}
}

// TestMetricContract_LabelBlacklist enforces R16: high-cardinality labels
// may not appear on any declared instrument.
func TestMetricContract_LabelBlacklist(t *testing.T) {
	blacklist := map[string]bool{
		"error_class":    true,
		"receipt_id":     true,
		"handle_id":      true,
		"correlation_id": true,
		"trace_id":       true,
		"retry_hint":     true,
	}
	for _, inst := range obs.InstrumentCatalog() {
		for _, lbl := range inst.Labels {
			require.False(t, blacklist[lbl],
				"instrument %q has blacklisted label %q (R16 violation)",
				inst.Name, lbl)
		}
	}
}

// TestMetricContract_LabelWhitelist is the converse: labels on declared
// instruments must come from the allowed whitelist. Additive safety net
// — if someone adds a new label type, this trips.
func TestMetricContract_LabelWhitelist(t *testing.T) {
	whitelist := map[string]bool{
		"capability": true,
		"adapter":    true,
		"status":     true,
		"signal":     true,
	}
	for _, inst := range obs.InstrumentCatalog() {
		for _, lbl := range inst.Labels {
			require.True(t, whitelist[lbl],
				"instrument %q has label %q not in whitelist (R16)",
				inst.Name, lbl)
		}
	}
}

// TestMetricContract_CardinalityBudget — per-instrument product of
// Bounded label-value counts must stay within Phase 1 budget.
func TestMetricContract_CardinalityBudget(t *testing.T) {
	const phase1MaxSeriesPerInstrument = 200

	for _, inst := range obs.InstrumentCatalog() {
		if inst.Bounded == nil {
			continue
		}
		product := 1
		for _, n := range inst.Bounded {
			product *= n
		}
		require.LessOrEqual(t, product, phase1MaxSeriesPerInstrument,
			"instrument %q cardinality %d exceeds Phase1 budget %d",
			inst.Name, product, phase1MaxSeriesPerInstrument)
	}
}

// TestMetricContract_UniqueNames asserts the catalog has no duplicate
// instrument names.
func TestMetricContract_UniqueNames(t *testing.T) {
	seen := map[string]bool{}
	for _, inst := range obs.InstrumentCatalog() {
		require.False(t, seen[inst.Name], "duplicate instrument name %q", inst.Name)
		seen[inst.Name] = true
	}
	require.Len(t, seen, 11, "expected exactly 11 instruments in §6.3 catalog")
}

// TestDurationBuckets_CoverSlothThresholds — skipped; re-enabled in
// Bundle 4 T31 once ops/slo/*.yaml exists. See spec §7.3 provisional
// targets: 0.5, 1, 2, 3, 5, 10, 30 must all map to a bucket boundary.
func TestDurationBuckets_CoverSlothThresholds(t *testing.T) {
	t.Skip("re-enabled in Bundle 4 T31 once ops/slo/*.yaml exists")
}
