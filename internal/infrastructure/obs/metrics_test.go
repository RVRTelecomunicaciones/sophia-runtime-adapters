package obs_test

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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

// TestDurationBuckets_CoverSlothThresholds asserts every `le="<value>"`
// referenced in ops/slo/*.yaml latency SLIs maps to a boundary in
// obs.DurationBuckets. Prevents silent drift between the SLI SLO
// thresholds and the histogram bucket shape — if someone adds a 45s
// latency threshold in a Sloth spec, DurationBuckets must grow or the
// quantile computation becomes imprecise.
//
// Re-enabled in Bundle 4 T31 (previously skipped in Bundle 2 T13).
// Spec §6.3 "Histogram bucket configuration" + §7.3.
func TestDurationBuckets_CoverSlothThresholds(t *testing.T) {
	seen := map[float64]bool{}
	for _, b := range obs.DurationBuckets {
		seen[b] = true
	}

	// Walk up from this package to the repo root, then descend into ops/slo.
	repoRoot := findRepoRoot(t)
	matches, err := filepath.Glob(filepath.Join(repoRoot, "ops", "slo", "*.yaml"))
	require.NoError(t, err)
	require.NotEmpty(t, matches,
		"no ops/slo/*.yaml specs found — Bundle 4 authored them; this test should not have been re-enabled without them")

	reLE := regexp.MustCompile(`le="([0-9.]+)"`)

	for _, f := range matches {
		data, err := os.ReadFile(f)
		require.NoError(t, err, "read %s", f)

		for _, match := range reLE.FindAllStringSubmatch(string(data), -1) {
			val, err := strconv.ParseFloat(match[1], 64)
			require.NoError(t, err, "parse %q in %s", match[1], f)

			require.True(t, seen[val],
				"bucket %v referenced via le=%q in %s is not in obs.DurationBuckets %v — add it or change the spec threshold",
				val, match[1], f, obs.DurationBuckets)
		}
	}
}

// findRepoRoot walks up the filesystem until it finds a directory that
// contains go.mod, returning that path. Fails the test if not found
// within 10 levels.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	dir := cwd
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not find repo root (go.mod) starting from %s", cwd)
	return ""
}
