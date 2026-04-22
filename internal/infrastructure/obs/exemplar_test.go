package obs_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/exemplar"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/obs"
)

// TestRecordExecution_AttachesTraceIDExemplar verifies that execution.duration
// observations carry an exemplar with the active span's trace_id when a span
// is active on the ctx (§6.5 + A2C1.10).
//
// Setup mirrors production (obs.SetupOTel):
//   - MeterProvider with WithExemplarFilter(exemplar.AlwaysOnFilter) so
//     capture is deterministic regardless of trace sampling.
//   - A View that drops receipt_id from aggregation (so it survives as an
//     exemplar attribute but doesn't inflate metric series — R16).
//   - TracerProvider with AlwaysSample() so the started span is sampled.
//
// Assertions:
//   - The execution.duration histogram has a data point.
//   - The data point's Attributes contain capability=shell.exec@v1 and do
//     NOT contain receipt_id (the View filtered it out).
//   - At least one exemplar is attached with a non-empty TraceID matching
//     the span's trace_id.
//   - Best-effort: if receipt_id appears on the exemplar's FilteredAttributes,
//     it matches the one we passed (logged as info; not required because the
//     SDK may or may not retain filtered attributes on exemplars depending on
//     reservoir choice).
func TestRecordExecution_AttachesTraceIDExemplar(t *testing.T) {
	// Meter provider — manual reader + AlwaysOnFilter + receipt_id-drop view.
	reader := sdkmetric.NewManualReader()
	dropReceiptIDView := sdkmetric.NewView(
		sdkmetric.Instrument{Name: "runtime_adapters.execution.duration"},
		sdkmetric.Stream{
			AttributeFilter: attribute.NewDenyKeysFilter("receipt_id"),
		},
	)
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(reader),
		sdkmetric.WithExemplarFilter(exemplar.AlwaysOnFilter),
		sdkmetric.WithView(dropReceiptIDView),
	)
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	meter := mp.Meter("runtime-adapters-test")
	reg, err := obs.NewRegistry(meter)
	require.NoError(t, err)

	// Tracer provider — AlwaysSample so the span gets sampled and its IDs
	// surface on exemplars.
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("runtime-adapters-test")

	ctx, span := tracer.Start(context.Background(), "test.exec")
	spanCtx := span.SpanContext()
	require.True(t, spanCtx.IsValid(), "span context must be valid")
	require.True(t, spanCtx.IsSampled(), "span must be sampled")

	const (
		receiptID = "01HZZZZZZZZZZZZZZZZZZZZZZZ"
		capName   = "shell.exec@v1"
	)
	reg.RecordExecution(ctx, capName, "success", receiptID, 1.23)
	span.End()

	// Collect.
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))

	// Find runtime_adapters.execution.duration.
	var hist *metricdata.Histogram[float64]
	for i := range rm.ScopeMetrics {
		for j := range rm.ScopeMetrics[i].Metrics {
			m := rm.ScopeMetrics[i].Metrics[j]
			if m.Name != "runtime_adapters.execution.duration" {
				continue
			}
			h, ok := m.Data.(metricdata.Histogram[float64])
			require.True(t, ok, "execution.duration must be Histogram[float64], got %T", m.Data)
			hist = &h
		}
	}
	require.NotNil(t, hist, "execution.duration histogram not found in ScopeMetrics")
	require.NotEmpty(t, hist.DataPoints, "execution.duration has no data points")

	dp := hist.DataPoints[0]

	// R16: capability present, receipt_id stripped from aggregation.
	gotCap, capOK := dp.Attributes.Value("capability")
	require.True(t, capOK, "expected capability attribute on data point")
	require.Equal(t, capName, gotCap.AsString())

	_, ridOK := dp.Attributes.Value("receipt_id")
	require.False(t, ridOK,
		"receipt_id must NOT appear on data point attributes (View should drop it)")

	// Exemplar assertions (§6.5 mandatory-when-span-active for trace_id).
	require.NotEmpty(t, dp.Exemplars,
		"expected at least one exemplar on execution.duration data point")

	var wantTID [16]byte = spanCtx.TraceID()
	foundMatch := false
	for _, ex := range dp.Exemplars {
		if bytes.Equal(ex.TraceID, wantTID[:]) {
			foundMatch = true
			break
		}
	}
	require.True(t, foundMatch,
		"no exemplar matched active trace_id %x; got exemplars=%d",
		wantTID[:], len(dp.Exemplars))

	// Best-effort: log whether receipt_id surfaced on the exemplar's
	// FilteredAttributes. This is NOT a hard assertion because the SDK
	// reservoir policy governs filtered-attribute retention.
	for _, ex := range dp.Exemplars {
		for _, kv := range ex.FilteredAttributes {
			if string(kv.Key) == "receipt_id" {
				t.Logf("exemplar carried receipt_id=%s on FilteredAttributes", kv.Value.AsString())
			}
		}
	}
}
