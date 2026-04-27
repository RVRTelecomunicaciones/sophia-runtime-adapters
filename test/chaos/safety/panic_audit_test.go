//go:build integration

// Package safety contains Bundle 4 invariant tests for the runtime-adapters.
// Task 4.1 — panic recoverer mount audit — verifies that R4 holds across
// every panic-prone call path defined in spec §10.1.
//
// The four locations under test:
//   - adapter-execute: panic inside outbound.Adapter.Execute
//   - normalizer-fault: panic inside ResultNormalizer closure
//   - persist-fault: panic inside ReceiptRepository.Save
//   - idempotency-replay: panic inside IdempotencyStore.Lookup
//
// For each location the test asserts:
//  1. Caller receives a Go error or a receipt with failure classification;
//     no panic propagated to caller, no hang.
//  2. Receipt classification: status=failure / error_class=adapter_internal_error /
//     retry_hint=non_retryable (when a receipt is produced).
//  3. Log entry at ERROR level with the §5.3 contractual fields plus
//     panic_location field (D2C3.16, log-only, not on receipt).
//  4. runtime_adapters.adapter.panics counter incremented.
//
// Tag: -tags=integration (mirrors the B3 chaos integration tests).
package safety

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/sophia-ecosystem/runtime-adapters/internal/application/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
	domainservices "github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
	"github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/obs"
	obslog "github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/obs/log"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound/testdoubles"
)

// ---------------------------------------------------------------------------
// deterministic ULID pool
// ---------------------------------------------------------------------------

const (
	uid01 = "01H8XXXXXXXXXXXXXXXXXXX001"
	uid02 = "01H8XXXXXXXXXXXXXXXXXXX002"
	uid03 = "01H8XXXXXXXXXXXXXXXXXXX003"
	uid04 = "01H8XXXXXXXXXXXXXXXXXXX004"
	uid05 = "01H8XXXXXXXXXXXXXXXXXXX005"
	uid06 = "01H8XXXXXXXXXXXXXXXXXXX006"
	uid07 = "01H8XXXXXXXXXXXXXXXXXXX007"
	uid08 = "01H8XXXXXXXXXXXXXXXXXXX008"
	uid09 = "01H8XXXXXXXXXXXXXXXXXXX009"
	uid10 = "01H8XXXXXXXXXXXXXXXXXXX010"
	uid11 = "01H8XXXXXXXXXXXXXXXXXXX011"
	uid12 = "01H8XXXXXXXXXXXXXXXXXXX012"
	uid13 = "01H8XXXXXXXXXXXXXXXXXXX013"
)

// ---------------------------------------------------------------------------
// Panicking test doubles — all build-tagged (integration), never in prod.
// ---------------------------------------------------------------------------

// panicAdapterStub panics unconditionally in Execute (adapter-execute path).
type panicAdapterStub struct {
	id   valueobjects.AdapterID
	caps []valueobjects.Capability
}

func (p *panicAdapterStub) ID() valueobjects.AdapterID              { return p.id }
func (p *panicAdapterStub) Capabilities() []valueobjects.Capability { return p.caps }
func (p *panicAdapterStub) Execute(_ context.Context, _ valueobjects.Capability, _ valueobjects.Payload) (domainservices.AdapterRawOutcome, error) {
	panic("simulated adapter-execute panic")
}

var _ outbound.Adapter = (*panicAdapterStub)(nil)

// successRawStub is a raw type accepted by the default normalizer in auditApp.
type successRawStub struct{}

func (*successRawStub) IsAdapterRawOutcome() {}

// successAdapterStub returns successRawStub — adapter succeeds, normalizer runs.
type successAdapterStub struct {
	id   valueobjects.AdapterID
	caps []valueobjects.Capability
}

func (a *successAdapterStub) ID() valueobjects.AdapterID              { return a.id }
func (a *successAdapterStub) Capabilities() []valueobjects.Capability { return a.caps }
func (a *successAdapterStub) Execute(_ context.Context, _ valueobjects.Capability, _ valueobjects.Payload) (domainservices.AdapterRawOutcome, error) {
	return &successRawStub{}, nil
}

var _ outbound.Adapter = (*successAdapterStub)(nil)

// panickingReceiptStore panics unconditionally in Save (persist-fault path).
type panickingReceiptStore struct{}

func (s *panickingReceiptStore) Save(_ context.Context, _ entities.ExecutionReceipt) (entities.ExecutionReceipt, error) {
	panic("simulated persist-fault panic")
}
func (s *panickingReceiptStore) FindByID(_ context.Context, _ shared.ReceiptID) (entities.ExecutionReceipt, error) {
	return entities.ExecutionReceipt{}, outbound.ErrReceiptNotFound
}

var _ outbound.ReceiptRepository = (*panickingReceiptStore)(nil)

// panickingIdempotencyStore panics unconditionally in Lookup
// (idempotency-replay path).
type panickingIdempotencyStore struct{}

func (s *panickingIdempotencyStore) Lookup(_ context.Context, _ shared.IdempotencyKey) (shared.ReceiptID, bool, error) {
	panic("simulated idempotency-replay panic")
}
func (s *panickingIdempotencyStore) Record(_ context.Context, _ shared.IdempotencyKey, _ shared.ReceiptID, _ time.Duration) error {
	return nil
}

var _ outbound.IdempotencyStore = (*panickingIdempotencyStore)(nil)

// ---------------------------------------------------------------------------
// In-memory slog handler for log capture
// ---------------------------------------------------------------------------

// capturedLog holds one recorded log entry.
type capturedLog struct {
	Level   slog.Level
	Message string
	Fields  map[string]any
}

// logStore is the shared, thread-safe backing store for memHandler.
// All derived handlers (created via WithAttrs/WithGroup) point to the same
// logStore so that entries written through any derived handler are visible
// to the caller who holds the original memHandler.
type logStore struct {
	mu      sync.Mutex
	records []capturedLog
}

func (s *logStore) append(l capturedLog) {
	s.mu.Lock()
	s.records = append(s.records, l)
	s.mu.Unlock()
}

func (s *logStore) all() []capturedLog {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]capturedLog, len(s.records))
	copy(cp, s.records)
	return cp
}

// memHandler is a slog.Handler backed by a shared logStore.
// WithAttrs creates a child handler with additional pre-bound attributes;
// both parent and child share the same logStore so records are visible
// to the original handler regardless of how many With calls are chained.
type memHandler struct {
	store *logStore
	level slog.Level
	attrs []slog.Attr // pre-bound attributes (from WithAttrs chain)
}

func newMemHandler(level slog.Level) *memHandler {
	return &memHandler{
		store: &logStore{},
		level: level,
	}
}

func (h *memHandler) Enabled(_ context.Context, l slog.Level) bool { return l >= h.level }

func (h *memHandler) Handle(_ context.Context, r slog.Record) error {
	fields := make(map[string]any)
	// Pre-bound attrs from the WithAttrs chain.
	for _, a := range h.attrs {
		if a.Key != "" {
			fields[a.Key] = a.Value.Any()
		}
	}
	// Per-record attrs.
	r.Attrs(func(a slog.Attr) bool {
		if a.Key != "" {
			fields[a.Key] = a.Value.Any()
		}
		return true
	})
	h.store.append(capturedLog{
		Level:   r.Level,
		Message: r.Message,
		Fields:  fields,
	})
	return nil
}

func (h *memHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	child := &memHandler{
		store: h.store, // shared store — child writes are visible to parent
		level: h.level,
	}
	child.attrs = append(child.attrs, h.attrs...)
	child.attrs = append(child.attrs, attrs...)
	return child
}

func (h *memHandler) WithGroup(_ string) slog.Handler {
	// Groups not needed for these tests — return a child that shares the store.
	return &memHandler{store: h.store, level: h.level, attrs: h.attrs}
}

// all returns a snapshot of all captured records (visible regardless of
// which derived handler wrote them).
func (h *memHandler) all() []capturedLog {
	return h.store.all()
}

// ---------------------------------------------------------------------------
// metricSnapshot — typed view into collected ResourceMetrics.
// ---------------------------------------------------------------------------

type metricSnapshot struct {
	rm metricdata.ResourceMetrics
}

// AdapterPanics returns the sum of runtime_adapters.adapter.panics data points.
func (s *metricSnapshot) AdapterPanics() int64 {
	return s.sumInt64Counter("runtime_adapters.adapter.panics")
}

// PersistFailures returns the sum of runtime_adapters.receipt.persist.failures
// data points. B8 wires this counter at every persist-failure site
// (regular + recovered panic per B9). Used by the persist-fault audit
// to verify the integration-level metric contract end-to-end.
func (s *metricSnapshot) PersistFailures() int64 {
	return s.sumInt64Counter("runtime_adapters.receipt.persist.failures")
}

// sumInt64Counter walks the ManualReader output for an Int64 Sum
// instrument with the given name and returns the total across all
// data points. Returns 0 if the instrument is absent or empty.
func (s *metricSnapshot) sumInt64Counter(name string) int64 {
	for _, sm := range s.rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
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

// ---------------------------------------------------------------------------
// auditApp — unit-level wiring (no testcontainers required).
//
// Uses services.NewExecuteService directly with injectable doubles.
// An OTel ManualReader is installed as the global MeterProvider before
// obs.NewRegistry so all instruments bind to it and are capturable.
// A memHandler is injected into ctx via obslog.ContextWith so
// execute_service.go's obslog.FromContext(ctx) writes through it.
// ---------------------------------------------------------------------------

type auditApp struct {
	svc             *services.ExecuteService
	logHandler      *memHandler
	snapshotMetrics func(t *testing.T) *metricSnapshot
	aid             valueobjects.AdapterID
	cap             valueobjects.Capability
}

// buildCtx returns a context with the test logger bound so execute_service
// enrichments flow through the memHandler.
func (a *auditApp) buildCtx() context.Context {
	logger := obslog.NewWithHandler(a.logHandler)
	return obslog.ContextWith(context.Background(), logger)
}

// buildRequest builds an ExecutionRequest for the wired capability.
// Pass idemKey="" for no idempotency key.
func (a *auditApp) buildRequest(t *testing.T, clk shared.Clock, idemKey string) entities.ExecutionRequest {
	t.Helper()
	cid, err := shared.NewCorrelationID(uid13)
	require.NoError(t, err, "NewCorrelationID")
	pl, err := valueobjects.NewPayload(valueobjects.ContentTypeJSON, []byte(`{"cmd":"ls"}`), 0)
	require.NoError(t, err, "NewPayload")
	tb, err := valueobjects.NewTimeoutBudget(5000, 0)
	require.NoError(t, err, "NewTimeoutBudget")

	var ikPtr *shared.IdempotencyKey
	if idemKey != "" {
		ik, ikErr := shared.NewIdempotencyKey(idemKey)
		require.NoError(t, ikErr, "NewIdempotencyKey")
		ikPtr = &ik
	}

	req, err := entities.NewExecutionRequest(entities.ExecutionRequestInput{
		CorrelationID:     cid,
		AdapterID:         a.aid,
		CapabilityName:    a.cap.Name(),
		CapabilityVersion: a.cap.Version(),
		Payload:           pl,
		TimeoutBudget:     tb,
		IdempotencyKey:    ikPtr,
	}, clk)
	require.NoError(t, err, "NewExecutionRequest")
	return req
}

// auditTestAppCfg holds overridable fields for buildAuditApp.
type auditTestAppCfg struct {
	adapter      outbound.Adapter
	receipts     outbound.ReceiptRepository
	idempotency  outbound.IdempotencyStore
	normalizerFn domainservices.AdapterNormalizer
}

// buildAuditApp constructs a wired ExecuteService for the panic audit tests.
// Caller must call teardown() after the test.
func buildAuditApp(t *testing.T, cfg auditTestAppCfg) (*auditApp, func()) {
	t.Helper()

	// 1. OTel: ManualReader-backed MeterProvider as global.
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)

	metricsReg, err := obs.NewRegistry(otel.Meter("runtime-adapters"))
	require.NoError(t, err, "obs.NewRegistry")

	// 2. Clock + capability.
	clk := &shared.FakeClock{T: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	aid, err := valueobjects.NewAdapterID("shell")
	require.NoError(t, err, "NewAdapterID")
	cap, err := valueobjects.NewCapability(aid, "exec", "v1", false, 5*time.Second)
	require.NoError(t, err, "NewCapability")
	registry, err := valueobjects.NewCapabilityRegistry(cap)
	require.NoError(t, err, "NewCapabilityRegistry")

	// 3. Normalizer.
	normalizer := domainservices.NewResultNormalizer(4096)
	normFn := cfg.normalizerFn
	if normFn == nil {
		// Default: accept successRawStub, return success result.
		normFn = func(_ valueobjects.Capability, raw domainservices.AdapterRawOutcome, ck shared.Clock) (entities.ExecutionResult, error) {
			if _, ok := raw.(*successRawStub); ok {
				res, resErr := entities.NewExecutionResult(
					valueobjects.StatusSuccess, valueobjects.HintRetryable,
					"", "",
					nil, nil, nil, nil, nil, 0, 0, time.Millisecond, ck.Now(),
				)
				return res, resErr
			}
			return entities.ExecutionResult{}, fmt.Errorf("unexpected raw type %T", raw)
		}
	}
	err = normalizer.Register(cap.Canonical(), normFn)
	require.NoError(t, err, "normalizer.Register")

	// 4. Adapter (default: panicAdapterStub).
	adapter := cfg.adapter
	if adapter == nil {
		adapter = &panicAdapterStub{id: aid, caps: []valueobjects.Capability{cap}}
	}

	// 5. Receipt store (default: in-memory).
	var receipts outbound.ReceiptRepository
	if cfg.receipts != nil {
		receipts = cfg.receipts
	} else {
		receipts = testdoubles.NewInMemoryReceiptRepository(clk)
	}

	// 6. Idempotency store (default: in-memory).
	var idemp outbound.IdempotencyStore
	if cfg.idempotency != nil {
		idemp = cfg.idempotency
	} else {
		idemp = testdoubles.NewInMemoryIdempotencyStore(clk)
	}

	// 7. Provenance + IDs.
	prov, err := entities.NewProvenance(entities.ProvTest, "v0.0.1", "test-host", "runtime-v0.0.1", "")
	require.NoError(t, err, "NewProvenance")
	idGen := &entities.FakeIDGen{IDs: []string{
		uid01, uid02, uid03, uid04, uid05,
		uid06, uid07, uid08, uid09, uid10,
	}}

	// 8. ExecuteService.
	svc, err := services.NewExecuteService(services.ExecuteServiceConfig{
		Adapters:    map[string]outbound.Adapter{"shell": adapter},
		Registry:    registry,
		Metrics:     metricsReg,
		Normalizer:  normalizer,
		Receipts:    receipts,
		Idempotency: idemp,
		Limiter:     services.NewConcurrencyLimiter(10),
		Clock:       clk,
		IDGen:       idGen,
		MaxTimeout:  30 * time.Second,
		IdempWindow: 24 * time.Hour,
		Provenance:  prov,
	})
	require.NoError(t, err, "NewExecuteService")

	// 9. In-memory log handler.
	logH := newMemHandler(slog.LevelDebug)

	snapshotFn := func(t *testing.T) *metricSnapshot {
		t.Helper()
		var rm metricdata.ResourceMetrics
		if err := reader.Collect(context.Background(), &rm); err != nil {
			t.Fatalf("OTel ManualReader.Collect: %v", err)
		}
		return &metricSnapshot{rm: rm}
	}

	teardown := func() {
		otel.SetMeterProvider(metricnoop.NewMeterProvider())
	}

	return &auditApp{
		svc:             svc,
		logHandler:      logH,
		snapshotMetrics: snapshotFn,
		aid:             aid,
		cap:             cap,
	}, teardown
}

// ---------------------------------------------------------------------------
// callWithPanicGuard calls fn and recovers any panic that propagates out.
// Returns (didPanic, panicValue, returnedErr).
//
// Running Execute in the same goroutine (not spawned) ensures that the
// defer-recover captures any propagating panic before it reaches the
// test runner and crashes the process.
// ---------------------------------------------------------------------------

func callWithPanicGuard(fn func() error) (didPanic bool, panicVal any, returnedErr error) {
	defer func() {
		if r := recover(); r != nil {
			didPanic = true
			panicVal = r
		}
	}()
	returnedErr = fn()
	return
}

// ---------------------------------------------------------------------------
// assertR4Contract — common assertions for a recovered panic.
// ---------------------------------------------------------------------------

// assertR4Contract checks the R4 contract assertions for a single panic site,
// reflecting the post-B9 site-specific recovery design:
//   - adapter-execute     → receipt with AdapterInternalError; caller no error
//   - normalizer          → receipt with NormalizationFailure; caller no error
//   - persist             → caller error, NO receipt (A4.3 — no fake persistence)
//   - idempotency_replay  → structural receipt with AdapterInternalError; caller no error
//   - idempotency_record  → request succeeds; only log captures the recovery
//
// expectReceiptProduced: true when the panic produces a receipt and caller
// receives no error. false when caller receives a Go error and no receipt
// is observable (persist-fault).
//
// expectedErrorClass: only consulted when expectReceiptProduced is true.
//
// adapterPanicMetricExpected: true only at the adapter-execute site, which
// owns the runtime_adapters.adapter.panics counter (per B9: counter is
// adapter-only; non-adapter panics are diagnosed via panic_location logs).
//
// panicLocation: the expected value of the D2C3.16 log field. Required
// (B9 wired panic_location at every recovery site).
func assertR4Contract(
	t *testing.T,
	app *auditApp,
	receipt entities.ExecutionReceipt,
	callerErr error,
	snap *metricSnapshot,
	expectReceiptProduced bool,
	expectedErrorClass valueobjects.ErrorClass,
	adapterPanicMetricExpected bool,
	panicLocation string,
) {
	t.Helper()

	if expectReceiptProduced {
		require.NoError(t, callerErr,
			"R4: caller must receive no error when panic is recovered into a receipt")
		require.Equal(t, valueobjects.StatusFailure.String(), receipt.Result().Status.String(),
			"R4: status must be failure")
		require.Equal(t, expectedErrorClass.String(), receipt.Result().ErrorClass.String(),
			"R4: error_class must be %s", expectedErrorClass.String())
		require.Equal(t, valueobjects.HintNonRetryable.String(), receipt.Result().Retryable.String(),
			"R4: retry_hint must be non_retryable")
	} else {
		require.Error(t, callerErr,
			"R4: caller must receive a Go error when persist panic is recovered (A4.3 — no fake receipt)")
	}

	// Log assertion — at least one ERROR level entry must exist.
	allLogs := app.logHandler.all()
	var errorLog *capturedLog
	for i := range allLogs {
		if allLogs[i].Level == slog.LevelError {
			errorLog = &allLogs[i]
			break
		}
	}
	require.NotNilf(t, errorLog,
		"R4 log: expected at least one ERROR-level log entry; got %d records total", len(allLogs))

	// D2C3.16: panic_location is the load-bearing field for diagnosability.
	require.Contains(t, errorLog.Fields, "panic_location",
		"R4 log: ERROR log must contain 'panic_location' field (D2C3.16)")
	require.Equal(t, panicLocation, fmt.Sprintf("%v", errorLog.Fields["panic_location"]),
		"R4 log: panic_location must identify the fault site (D2C3.16)")

	// §5.3 fields (status, error_class, etc.) live on the FINAL
	// "execution complete" log emitted by ExecuteService at flow end.
	// The panic-recovery log emitted at the recovery site itself comes
	// EARLIER in the flow (before the receipt is built or persisted),
	// so it does not carry the §5.3 fields. The 2C.1 logger contract
	// tests in internal/infrastructure/obs/log/ already verify the
	// §5.3 fields appear on the execution-complete log; this audit
	// test does not duplicate that assertion. R4 + D2C3.16 are the
	// load-bearing contracts for this site.

	// Metric: runtime_adapters.adapter.panics is wired only at the adapter
	// site (B9: per-site instrumentation, not a global runtime panic counter).
	panicsCount := snap.AdapterPanics()
	if adapterPanicMetricExpected {
		require.GreaterOrEqual(t, panicsCount, int64(1),
			"R4 metric: runtime_adapters.adapter.panics must be >= 1 at adapter-execute")
	} else {
		require.Equal(t, int64(0), panicsCount,
			"R4 metric: runtime_adapters.adapter.panics must NOT increment for non-adapter sites "+
				"(panic_location=%q); use panic_location log field for diagnostics",
			panicLocation)
	}

	// Metric: runtime_adapters.receipt.persist.failures wires at the
	// persist site (B8 + B9). The persist-fault audit closes the
	// integration-level contract by asserting the counter increments.
	if panicLocation == "persist" {
		require.GreaterOrEqual(t, snap.PersistFailures(), int64(1),
			"R4 metric: runtime_adapters.receipt.persist.failures must be >= 1 at persist site")
	}
}

// ---------------------------------------------------------------------------
// TestPanicAudit — 4-location R4 invariant audit (spec §10.1 / B4 Task 4.1)
// ---------------------------------------------------------------------------

// TestPanicAudit_AdapterExecute verifies R4 at the adapter-execute site.
// SafeExecute (structural.go) wraps the adapter's Execute call with a
// defer-recover, converting the panic into a panicRaw. ExecuteService then
// builds a failure/adapter_internal_error/non_retryable receipt.
//
// This location is expected to PASS because SafeExecute already exists.
func TestPanicAudit_AdapterExecute(t *testing.T) {
	app, teardown := buildAuditApp(t, auditTestAppCfg{
		adapter: &panicAdapterStub{},
	})
	// Re-build so aid/cap are set on app before creating the adapter.
	teardown()

	app, teardown = buildAuditApp(t, auditTestAppCfg{
		adapter: &panicAdapterStub{
			id:   app.aid,
			caps: []valueobjects.Capability{app.cap},
		},
	})
	defer teardown()

	clk := &shared.FakeClock{T: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	req := app.buildRequest(t, clk, "")
	ctx := app.buildCtx()

	// First: detect whether the panic propagates to the caller.
	didPanic, panicVal, _ := callWithPanicGuard(func() error {
		_, err := app.svc.Execute(ctx, req)
		return err
	})
	if didPanic {
		t.Fatalf("BLOCKED: R4 gap at adapter-execute — panic propagated to caller: %v\n"+
			"Expected SafeExecute (internal/application/services/structural.go) to recover.\n"+
			"No AdapterInternalError receipt was produced.",
			panicVal)
	}

	// Second pass to get the receipt for assertions (fresh app so IDs/log are clean).
	app2, td2 := buildAuditApp(t, auditTestAppCfg{
		adapter: &panicAdapterStub{
			id:   app.aid,
			caps: []valueobjects.Capability{app.cap},
		},
	})
	defer td2()

	req2 := app2.buildRequest(t, &shared.FakeClock{T: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}, "")
	ctx2 := app2.buildCtx()
	receipt, execErr := app2.svc.Execute(ctx2, req2)
	snap := app2.snapshotMetrics(t)

	assertR4Contract(t, app2, receipt, execErr, snap,
		true,                                 // panic recovered into receipt (SafeExecute exists)
		valueobjects.ErrAdapterInternalError, // adapter panic → AdapterInternalError
		true,                                 // adapter.panics counter wired at adapter-execute (B9)
		"adapter_execute",
	)
}

// TestPanicAudit_NormalizerFault verifies R4 at the normalizer-fault site.
// The adapter returns successRawStub; the normalizer closure panics during
// Normalize. If no recovery exists, the panic propagates to the caller —
// this is an R4 gap.
//
// Location: execute_service.go, default branch:
//
//	result, nErr = s.normalizer.Normalize(cap, raw, s.clock)
func TestPanicAudit_NormalizerFault(t *testing.T) {
	// Get aid/cap from a preliminary build.
	tmp, td := buildAuditApp(t, auditTestAppCfg{})
	aid, cap := tmp.aid, tmp.cap
	td()

	panicNorm := func(_ valueobjects.Capability, _ domainservices.AdapterRawOutcome, _ shared.Clock) (entities.ExecutionResult, error) {
		panic("simulated normalizer-fault panic")
	}

	app, teardown := buildAuditApp(t, auditTestAppCfg{
		adapter:      &successAdapterStub{id: aid, caps: []valueobjects.Capability{cap}},
		normalizerFn: panicNorm,
	})
	defer teardown()

	clk := &shared.FakeClock{T: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	req := app.buildRequest(t, clk, "")
	ctx := app.buildCtx()

	didPanic, panicVal, _ := callWithPanicGuard(func() error {
		_, err := app.svc.Execute(ctx, req)
		return err
	})
	if didPanic {
		t.Fatalf("BLOCKED: R4 gap at normalizer-fault — panic propagated to caller: %v\n"+
			"No recovery wraps s.normalizer.Normalize() in execute_service.go.\n"+
			"The panic at the normalizer site produces no AdapterInternalError receipt.\n"+
			"File: internal/application/services/execute_service.go\n"+
			"       (default branch: result, nErr = s.normalizer.Normalize(cap, raw, s.clock))\n"+
			"Fix: add defer/recover around the Normalize call; on panic build panicRaw\n"+
			"     and produce receipt with status=failure/error_class=adapter_internal_error.",
			panicVal)
	}

	// If we reach here, recovery exists — assert the full contract.
	app2, td2 := buildAuditApp(t, auditTestAppCfg{
		adapter:      &successAdapterStub{id: aid, caps: []valueobjects.Capability{cap}},
		normalizerFn: panicNorm,
	})
	defer td2()

	req2 := app2.buildRequest(t, &shared.FakeClock{T: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}, "")
	ctx2 := app2.buildCtx()
	receipt, execErr := app2.svc.Execute(ctx2, req2)
	snap := app2.snapshotMetrics(t)

	assertR4Contract(t, app2, receipt, execErr, snap,
		true,                                 // expect receipt produced (B9 normalizer recovery → synthetic result → receipt)
		valueobjects.ErrNormalizationFailure, // normalizer panic → NormalizationFailure (B9 design)
		false,                                // adapter.panics not wired at non-adapter sites (B9: log-only diagnostics)
		"normalizer",
	)
}

// TestPanicAudit_PersistFault verifies R4 at the persist-fault site.
// The adapter succeeds, normalizer succeeds, but ReceiptRepository.Save panics.
// If no recovery exists, the panic propagates — R4 gap.
//
// Location: execute_service.go, Step 10:
//
//	saved, perr := s.receipts.Save(persistCtx, receipt)
func TestPanicAudit_PersistFault(t *testing.T) {
	tmp, td := buildAuditApp(t, auditTestAppCfg{})
	aid, cap := tmp.aid, tmp.cap
	td()

	app, teardown := buildAuditApp(t, auditTestAppCfg{
		adapter:  &successAdapterStub{id: aid, caps: []valueobjects.Capability{cap}},
		receipts: &panickingReceiptStore{},
	})
	defer teardown()

	clk := &shared.FakeClock{T: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	req := app.buildRequest(t, clk, "")
	ctx := app.buildCtx()

	didPanic, panicVal, callerErr := callWithPanicGuard(func() error {
		_, err := app.svc.Execute(ctx, req)
		return err
	})
	if didPanic {
		t.Fatalf("BLOCKED: R4 gap at persist-fault — panic propagated to caller: %v\n"+
			"No recovery wraps s.receipts.Save() in execute_service.go (Step 10).\n"+
			"A panic at the persistence site crashes the caller instead of producing\n"+
			"a structured error or AdapterInternalError receipt.\n"+
			"File: internal/application/services/execute_service.go\n"+
			"       (Step 10: saved, perr := s.receipts.Save(persistCtx, receipt))\n"+
			"Fix: add defer/recover around the Save call; on panic return a structured\n"+
			"     Go error (caller receives error, not a crash). A4.3 still applies.",
			panicVal)
	}

	// If we reach here, recovery exists.
	snap := app.snapshotMetrics(t)

	assertR4Contract(t, app, entities.ExecutionReceipt{}, callerErr, snap,
		false,                                // persist panic surfaces as Go error; A4.3 — no fake receipt
		valueobjects.ErrAdapterInternalError, // unused when expectReceiptProduced=false
		false,                                // adapter.panics not wired at persist site
		"persist",
	)
}

// TestPanicAudit_IdempotencyReplay verifies R4 at the idempotency-replay site.
// A request with an idempotency key triggers Lookup; the panicking store panics
// inside Lookup. If no recovery exists, the panic propagates — R4 gap.
//
// Location: execute_service.go, Step 2:
//
//	rid, found, err := s.idempotency.Lookup(ctx, key)
func TestPanicAudit_IdempotencyReplay(t *testing.T) {
	tmp, td := buildAuditApp(t, auditTestAppCfg{})
	aid, cap := tmp.aid, tmp.cap
	td()

	app, teardown := buildAuditApp(t, auditTestAppCfg{
		adapter:     &successAdapterStub{id: aid, caps: []valueobjects.Capability{cap}},
		idempotency: &panickingIdempotencyStore{},
	})
	defer teardown()

	clk := &shared.FakeClock{T: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	// Pass an idempotency key to trigger the Lookup call path.
	req := app.buildRequest(t, clk, uid11)
	ctx := app.buildCtx()

	didPanic, panicVal, _ := callWithPanicGuard(func() error {
		_, err := app.svc.Execute(ctx, req)
		return err
	})
	if didPanic {
		t.Fatalf("BLOCKED: R4 gap at idempotency-replay — panic propagated to caller: %v\n"+
			"No recovery wraps s.idempotency.Lookup() in execute_service.go (Step 2).\n"+
			"A panic at the idempotency Lookup site crashes the caller instead of producing\n"+
			"a structured error or AdapterInternalError receipt.\n"+
			"File: internal/application/services/execute_service.go\n"+
			"       (Step 2: rid, found, err := s.idempotency.Lookup(ctx, key))\n"+
			"Fix: add defer/recover around the Lookup call; on panic treat as Lookup miss\n"+
			"     (proceed to execution) OR return a structured Go error.",
			panicVal)
	}

	// Second pass for clean assertion state. B9 design: idempotency.Lookup
	// panic bails to persistStructural, producing a structural failure
	// receipt with error_class=adapter_internal_error. The caller does NOT
	// receive a Go error — the receipt carries the classification.
	app2, td2 := buildAuditApp(t, auditTestAppCfg{
		adapter:     &successAdapterStub{id: aid, caps: []valueobjects.Capability{cap}},
		idempotency: &panickingIdempotencyStore{},
	})
	defer td2()

	req2 := app2.buildRequest(t, &shared.FakeClock{T: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}, uid11)
	ctx2 := app2.buildCtx()
	receipt, execErr := app2.svc.Execute(ctx2, req2)
	snap := app2.snapshotMetrics(t)

	assertR4Contract(t, app2, receipt, execErr, snap,
		true,                                 // B9: idempotency Lookup panic → persistStructural → structural receipt
		valueobjects.ErrAdapterInternalError, // idempotency-replay → AdapterInternalError (B9 design)
		false,                                // adapter.panics not wired at idempotency site
		"idempotency_replay",
	)
}
