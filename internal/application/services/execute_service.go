package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
	domainservices "github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
	"github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/obs"
	obslog "github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/obs/log"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/inbound"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound"
)

// Compile-time check: ExecuteService satisfies inbound.RuntimeService.
var _ inbound.RuntimeService = (*ExecuteService)(nil)

// ExecuteService implements inbound.RuntimeService via the §6.2
// 11-step flow. It is the single choreographer of execution in Phase 1
// — everything outside (HTTP handlers, SDK, persistence, adapters) is
// a dependency wired at bootstrap.
//
// Instances are safe for concurrent use. All dependencies are required
// — NewExecuteService validates that none are nil.
type ExecuteService struct {
	adapters    map[string]outbound.Adapter // keyed by AdapterID.String()
	registry    *valueobjects.CapabilityRegistry
	metrics     *obs.Registry // optional; nil-safe (production wiring always non-nil)
	normalizer  *domainservices.ResultNormalizer
	receipts    outbound.ReceiptRepository
	idempotency outbound.IdempotencyStore
	limiter     *ConcurrencyLimiter
	clock       shared.Clock
	idGen       entities.IDGenerator
	maxTimeout  time.Duration
	idempWindow time.Duration
	provenance  entities.Provenance // baseline; every receipt carries a copy
}

// ExecuteServiceConfig bundles the constructor arguments so callers
// (bootstrap/wire.go) construct by name rather than positional args.
//
// Metrics is optional (nil-safe). Production wiring (bootstrap) always
// passes a non-nil *obs.Registry; tests may leave it nil — every emission
// site guards against nil. The field is named Metrics (not Registry) to
// avoid collision with the Registry field that holds the capability catalog.
type ExecuteServiceConfig struct {
	Adapters    map[string]outbound.Adapter
	Registry    *valueobjects.CapabilityRegistry
	Metrics     *obs.Registry
	Normalizer  *domainservices.ResultNormalizer
	Receipts    outbound.ReceiptRepository
	Idempotency outbound.IdempotencyStore
	Limiter     *ConcurrencyLimiter
	Clock       shared.Clock
	IDGen       entities.IDGenerator
	MaxTimeout  time.Duration
	IdempWindow time.Duration
	Provenance  entities.Provenance
}

// NewExecuteService validates config and returns a ready service.
// Returns an error when any required dependency is nil or invalid.
func NewExecuteService(cfg ExecuteServiceConfig) (*ExecuteService, error) {
	if cfg.Adapters == nil {
		return nil, fmt.Errorf("ExecuteServiceConfig.Adapters is required")
	}
	if cfg.Registry == nil {
		return nil, fmt.Errorf("ExecuteServiceConfig.Registry is required")
	}
	if cfg.Normalizer == nil {
		return nil, fmt.Errorf("ExecuteServiceConfig.Normalizer is required")
	}
	if cfg.Receipts == nil {
		return nil, fmt.Errorf("ExecuteServiceConfig.Receipts is required")
	}
	if cfg.Idempotency == nil {
		return nil, fmt.Errorf("ExecuteServiceConfig.Idempotency is required")
	}
	if cfg.Limiter == nil {
		return nil, fmt.Errorf("ExecuteServiceConfig.Limiter is required")
	}
	if cfg.Clock == nil {
		return nil, fmt.Errorf("ExecuteServiceConfig.Clock is required")
	}
	if cfg.IDGen == nil {
		return nil, fmt.Errorf("ExecuteServiceConfig.IDGen is required")
	}
	if cfg.MaxTimeout < 0 {
		return nil, fmt.Errorf("ExecuteServiceConfig.MaxTimeout must be ≥ 0 (0 = unlimited)")
	}
	if cfg.IdempWindow <= 0 {
		return nil, fmt.Errorf("ExecuteServiceConfig.IdempWindow must be > 0")
	}
	if cfg.Provenance.Source == "" {
		return nil, fmt.Errorf("ExecuteServiceConfig.Provenance is required (zero value)")
	}
	return &ExecuteService{
		adapters:    cfg.Adapters,
		registry:    cfg.Registry,
		metrics:     cfg.Metrics, // optional — nil is valid in tests
		normalizer:  cfg.Normalizer,
		receipts:    cfg.Receipts,
		idempotency: cfg.Idempotency,
		limiter:     cfg.Limiter,
		clock:       cfg.Clock,
		idGen:       cfg.IDGen,
		maxTimeout:  cfg.MaxTimeout,
		idempWindow: cfg.IdempWindow,
		provenance:  cfg.Provenance,
	}, nil
}

// Execute runs the §6.2 11-step UC1 flow.
//
// Structural errors (concurrency limit, persistence failure) are returned
// as a Go error with a zero receipt. Business outcomes (success / failure /
// timeout / cancelled / partial) are always persisted and returned inside
// the receipt with a nil error. Receipt and error are mutually exclusive
// (A4.3).
func (s *ExecuteService) Execute(ctx context.Context, req entities.ExecutionRequest) (entities.ExecutionReceipt, error) {
	// Enrich the request-scoped logger with correlation_id (§5.3). This
	// field is carried forward through every subsequent enrichment and
	// appears on the final "execution complete" emit at step 11.
	logger := obslog.FromContext(ctx).With(
		slog.String("correlation_id", req.CorrelationID().String()),
	)
	ctx = obslog.ContextWith(ctx, logger)

	// Step 0: concurrency limiter (A9.1 fast-reject). On rejection we bump
	// the ConcurrencyRejects counter (§6.3) on the caller's ctx before
	// returning — the execution never starts so no receipt/handle exists.
	if err := s.limiter.TryAcquire(); err != nil {
		if s.metrics != nil && errors.Is(err, ErrTooManyExecutions) {
			s.metrics.ConcurrencyRejects.Add(ctx, 1)
		}
		return entities.ExecutionReceipt{}, err
	}
	defer s.limiter.Release()

	// Step 2: idempotency lookup.
	// A panic during Lookup cannot be treated as a cache-miss and allowed
	// to proceed — that risks duplicate side effects (D6.4). Instead
	// convert to a structural failure receipt and return it. Log at ERROR
	// with panic_location=idempotency_replay.
	if key, ok := req.IdempotencyKey(); ok {
		type lookupResult struct {
			rid   shared.ReceiptID
			found bool
			err   error
		}
		var lr lookupResult
		var lookupPanicked bool
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					lookupPanicked = true
					obslog.FromContext(ctx).Error(ctx, "idempotency lookup panic recovered",
						slog.String("panic_location", "idempotency_replay"),
						slog.String("panic_value", fmt.Sprintf("%v", rec)),
					)
				}
			}()
			lr.rid, lr.found, lr.err = s.idempotency.Lookup(ctx, key)
		}()
		if lookupPanicked {
			return s.persistStructural(ctx, req, valueobjects.ErrAdapterInternalError, "idempotency replay panic recovered")
		}
		if lr.err == nil && lr.found {
			// Replay-everything: return the cached receipt (success or failure alike).
			return s.receipts.FindByID(ctx, lr.rid)
		}
	}

	// Step 3: capability resolution.
	cap, err := s.registry.Lookup(req.CapabilityCanonical())
	if err != nil {
		return s.persistStructural(ctx, req, valueobjects.ErrCapabilityUnknown, err.Error())
	}

	// Enrich logger with capability + adapter (§5.3: always present from
	// step 3 onward).
	logger = logger.With(
		slog.String("capability", cap.Canonical()),
		slog.String("adapter", cap.AdapterID().String()),
	)
	ctx = obslog.ContextWith(ctx, logger)

	// Step 4: timeout resolution.
	effective := minDuration(req.TimeoutBudget().Duration(), cap.DefaultTimeout(), s.maxTimeout)
	if effective <= 0 {
		return s.persistStructural(ctx, req, valueobjects.ErrValidationFailure, "effective timeout budget ≤ 0")
	}

	// Step 5: handle generation.
	handle, err := entities.NewExecutionHandle(s.idGen, req.CorrelationID(), cap, s.clock)
	if err != nil {
		return s.persistStructural(ctx, req, valueobjects.ErrAdapterInternalError, "handle generation: "+err.Error())
	}

	// Enrich logger with handle_id (§5.3: always present from step 5 onward).
	logger = logger.With(slog.String("handle_id", handle.HandleID().String()))
	ctx = obslog.ContextWith(ctx, logger)

	// Step 6: ctx setup.
	execCtx, cancel := context.WithTimeout(ctx, effective)
	defer cancel()

	// Step 7: adapter dispatch with panic recovery.
	adapter, ok := s.adapters[cap.AdapterID().String()]
	if !ok {
		return s.persistStructural(ctx, req, valueobjects.ErrCapabilityUnknown, "no adapter registered for "+cap.AdapterID().String())
	}

	// Bracket the dispatch with ExecutionActive ±1 (§6.3). The defer guards
	// against the (recovered) panic path — SafeExecute recovers, but this
	// discipline costs nothing and keeps the invariant visible.
	if s.metrics != nil {
		capAttr := attribute.NewSet(attribute.String("capability", cap.Canonical()))
		s.metrics.ExecutionActive.Add(execCtx, 1, metric.WithAttributeSet(capAttr))
		defer s.metrics.ExecutionActive.Add(execCtx, -1, metric.WithAttributeSet(capAttr))
	}

	tStart := s.clock.Now()
	raw, adapterErr := SafeExecute(execCtx, adapter, cap, req.Payload())
	tEnd := s.clock.Now()

	// After the adapter returns, we may need to persist even when ctx (or
	// execCtx) is cancelled. Use a fresh background context for all
	// post-adapter I/O so cancellation of the caller's ctx doesn't block
	// receipt persistence. This is per A4.3: side effects (persistence)
	// must be attempted regardless of caller lifecycle.
	persistCtx := context.Background()

	// Step 7b: classify ctx outcome.
	if execCtx.Err() != nil {
		raw = ctxOutcomeFor(execCtx.Err(), raw)
		adapterErr = nil
	}
	if adapterErr != nil && raw == nil {
		// Structural adapter error with no raw: synthesize panicRaw.
		raw = &panicRaw{recoveredValue: nil, structuralErr: adapterErr}
	}

	// Step 7c: wire adapter.panics counter and panic_location log for
	// the adapter-execute site (D2C3.16). The counter is wired here
	// (not inside SafeExecute) so we avoid threading *obs.Registry
	// through the free function — the panicRaw type-check is sufficient.
	// Non-adapter panics (normalizer, persist, idempotency) do NOT
	// increment this counter per D2C3.16 ("no fingir métrica
	// semánticamente falsa").
	if pr, ok := raw.(*panicRaw); ok && pr.structuralErr == nil {
		if s.metrics != nil {
			s.metrics.AdapterPanics.Add(ctx, 1,
				metric.WithAttributes(attribute.String("adapter", cap.AdapterID().String())))
		}
		obslog.FromContext(ctx).Error(ctx, "adapter panic recovered",
			slog.String("panic_location", "adapter_execute"),
			slog.String("panic_value", fmt.Sprintf("%v", pr.recoveredValue)),
		)
	}

	// Step 8 (Option A): dispatch ctx-special and panic-special raws
	// directly; otherwise delegate to the registered normalizer.
	var result entities.ExecutionResult
	dur := tEnd.Sub(tStart)

	switch r := raw.(type) {
	case *ctxRaw:
		if r.kind == ctxKindTimeout {
			result, err = entities.NewExecutionResult(
				valueobjects.StatusTimeout, valueobjects.HintRetryable,
				valueobjects.ErrTimeout, r.ctxErr.Error(),
				nil, nil, nil, nil, nil, 0, 0, dur, tEnd,
			)
		} else {
			result, err = entities.NewExecutionResult(
				valueobjects.StatusCancelled, valueobjects.HintNonRetryable,
				valueobjects.ErrCancelled, r.ctxErr.Error(),
				nil, nil, nil, nil, nil, 0, 0, dur, tEnd,
			)
		}
		if err != nil {
			return entities.ExecutionReceipt{}, fmt.Errorf("build ctx result: %w", err)
		}
	case *panicRaw:
		msg := fmt.Sprintf("panic recovered: %v", r.recoveredValue)
		if r.structuralErr != nil {
			msg = "adapter structural error: " + r.structuralErr.Error()
		}
		adapterMeta := map[string]string{}
		if len(r.stack) > 0 {
			adapterMeta["panic.stack"] = string(r.stack)
		}
		result, err = entities.NewExecutionResult(
			valueobjects.StatusFailure, valueobjects.HintNonRetryable,
			valueobjects.ErrAdapterInternalError, msg,
			nil, nil, nil, nil, adapterMeta, 0, 0, dur, tEnd,
		)
		if err != nil {
			return entities.ExecutionReceipt{}, fmt.Errorf("build panic result: %w", err)
		}
	default:
		var nErr error
		result, nErr = func() (r entities.ExecutionResult, e error) {
			defer func() {
				if rec := recover(); rec != nil {
					obslog.FromContext(ctx).Error(ctx, "normalizer panic recovered",
						slog.String("panic_location", "normalizer"),
						slog.String("panic_value", fmt.Sprintf("%v", rec)),
					)
					var buildErr error
					r, buildErr = entities.NewExecutionResult(
						valueobjects.StatusFailure, valueobjects.HintNonRetryable,
						valueobjects.ErrNormalizationFailure,
						fmt.Sprintf("normalizer panic: %v", rec),
						nil, nil, nil, nil, nil, 0, 0, dur, tEnd,
					)
					if buildErr != nil {
						e = fmt.Errorf("build normalizer-panic result: %w", buildErr)
					}
				}
			}()
			return s.normalizer.Normalize(cap, raw, s.clock)
		}()
		if nErr != nil {
			result, err = entities.NewExecutionResult(
				valueobjects.StatusFailure, valueobjects.HintNonRetryable,
				valueobjects.ErrNormalizationFailure, nErr.Error(),
				nil, nil, nil, nil, nil, 0, 0, dur, tEnd,
			)
			if err != nil {
				return entities.ExecutionReceipt{}, fmt.Errorf("build normalization-failure result: %w", err)
			}
		}
	}

	// Step 9: assemble receipt.
	timings := mustBuildTimings(req.SubmittedAt(), tStart, tEnd, nil)
	receipt, err := entities.NewExecutionReceipt(s.idGen, req, handle, result, s.provenance, timings, s.clock)
	if err != nil {
		return entities.ExecutionReceipt{}, fmt.Errorf("receipt assembly: %w", err)
	}

	// Step 10: persist (use persistCtx so caller cancellation doesn't block
	// the audit write — per A4.3 the receipt is the central artifact).
	//
	// The inner closure carries a defer/recover for the panic case (R4).
	// On panic: logs at ERROR with panic_location=persist, increments
	// receipt.persist.failures, returns the same error shape as the normal
	// persist-failure path so the caller always gets a consistent 5xx.
	// Per A4.3: do NOT fake persistence; return error to caller.
	var persistPanicHandled bool
	saved, perr := func() (entities.ExecutionReceipt, error) {
		defer func() {
			if rec := recover(); rec != nil {
				persistPanicHandled = true
				obslog.FromContext(ctx).Error(ctx, "persist receipt",
					slog.String("panic_location", "persist"),
					slog.String("panic_value", fmt.Sprintf("%v", rec)),
				)
				if s.metrics != nil {
					s.metrics.ReceiptPersistFails.Add(persistCtx, 1)
				}
			}
		}()
		return s.receipts.Save(persistCtx, receipt)
	}()
	if perr != nil || persistPanicHandled {
		// §5.4 orthogonal ERROR-always: persistence failure MUST emit even
		// when the underlying execution succeeded — the persist error masks
		// the side effect and the receipt is unrecoverable. The enriched
		// logger on ctx already carries correlation_id / capability /
		// adapter / handle_id from steps 0/3/5.
		//
		// runtime_adapters.receipt.persist.failures (§6.3 instrument 6 /
		// A4.3 instrumentation): incremented on every persist failure
		// regardless of which path (happy path here; persistStructural
		// path symmetrically below). The metric is the operational
		// signal a persist outage is occurring; the log is the
		// incident-level enrichment.
		//
		// Note: the panic-recovery path already logged + incremented above;
		// the non-panic error path logs + increments here.
		if !persistPanicHandled {
			if s.metrics != nil {
				s.metrics.ReceiptPersistFails.Add(persistCtx, 1)
			}
			obslog.FromContext(ctx).Error(ctx, "persist receipt",
				slog.String("error", perr.Error()),
			)
		}
		if persistPanicHandled {
			return entities.ExecutionReceipt{}, fmt.Errorf("persistence failed; side effect may have occurred: panic at persist site")
		}
		return entities.ExecutionReceipt{}, fmt.Errorf("persistence failed; side effect may have occurred: %w", perr)
	}

	// Step 10b: record idempotency key (best-effort; ignore errors).
	// A panic during Record is also swallowed — the receipt is already
	// persisted and observable; idempotency-record is best-effort by
	// design. Log only with panic_location=idempotency_record.
	if key, ok := req.IdempotencyKey(); ok {
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					obslog.FromContext(ctx).Error(ctx, "idempotency record panic recovered",
						slog.String("panic_location", "idempotency_record"),
						slog.String("panic_value", fmt.Sprintf("%v", rec)),
					)
				}
			}()
			_ = s.idempotency.Record(persistCtx, key, saved.ReceiptID(), s.idempWindow)
		}()
	}

	// Step 11: final emit — "execution complete" with all §5.3 contract
	// fields. Level is chosen by LevelFor(status, errClass) per §5.4.
	// Metrics recorded here so execution.total / execution.duration /
	// partial.signal are all emitted from a single choke-point before
	// the final log (§6.3).
	if s.metrics != nil {
		dur := time.Duration(saved.Result().DurationMs) * time.Millisecond
		// §6.5 + A2C1.10: pass receipt_id so the SDK can carry it as an
		// exemplar tag; the SetupOTel View drops it from aggregation so
		// cardinality stays bounded (R16).
		s.metrics.RecordExecution(ctx,
			cap.Canonical(),
			saved.Result().Status.String(),
			saved.ReceiptID().String(),
			dur.Seconds(),
		)
	}
	emitExecutionComplete(ctx, logger, saved)

	return saved, nil
}

// emitExecutionComplete writes the single per-execution "execution complete"
// log record with the contract fields of §5.3. The level is chosen by
// LevelFor(status, errClass) per §5.4. error_class is emitted only when
// status != success (§5.3 "when `status != success`").
func emitExecutionComplete(ctx context.Context, logger *obslog.Logger, r entities.ExecutionReceipt) {
	result := r.Result()
	attrs := []slog.Attr{
		slog.String("status", result.Status.String()),
		slog.String("receipt_id", r.ReceiptID().String()),
		slog.Int64("duration_ms", result.DurationMs),
	}
	if result.Status != valueobjects.StatusSuccess {
		attrs = append(attrs, slog.String("error_class", result.ErrorClass.String()))
	}
	level := obslog.LevelFor(result.Status, result.ErrorClass)
	switch level {
	case slog.LevelInfo:
		logger.Info(ctx, "execution complete", attrs...)
	case slog.LevelWarn:
		logger.Warn(ctx, "execution complete", attrs...)
	case slog.LevelError:
		logger.Error(ctx, "execution complete", attrs...)
	default:
		logger.Debug(ctx, "execution complete", attrs...)
	}
}

// persistStructural builds a receipt for a pre-adapter failure (capability
// unknown, timeout ≤ 0, handle generation, etc.), persists it, and returns
// it. On persistence error returns the wrapped error per A4.3.
func (s *ExecuteService) persistStructural(
	ctx context.Context,
	req entities.ExecutionRequest,
	errClass valueobjects.ErrorClass,
	errMsg string,
) (entities.ExecutionReceipt, error) {
	now := s.clock.Now()
	result, err := entities.NewExecutionResult(
		valueobjects.StatusFailure, errClass.DefaultRetryHint(), errClass, errMsg,
		nil, nil, nil, nil, nil, 0, 0, 0, now,
	)
	if err != nil {
		return entities.ExecutionReceipt{}, fmt.Errorf("persistStructural: build result: %w", err)
	}

	// A zero-handle is not accepted by NewExecutionReceipt, so we build a
	// minimal pseudo-handle using the request's adapter+capability info.
	pseudoAID, _ := valueobjects.NewAdapterID(req.AdapterID().String())
	pseudoCap, capErr := valueobjects.NewCapability(pseudoAID, req.CapabilityName(), req.CapabilityVersion(), false, time.Second)
	if capErr != nil {
		// Fallback: use a generic capability name that always passes validation.
		pseudoCap, _ = valueobjects.NewCapability(pseudoAID, "unknown.capability", "v0", false, time.Second)
	}
	handle, herr := entities.NewExecutionHandle(s.idGen, req.CorrelationID(), pseudoCap, s.clock)
	if herr != nil {
		return entities.ExecutionReceipt{}, fmt.Errorf("persistStructural: build handle: %w", herr)
	}

	timings := mustBuildTimings(req.SubmittedAt(), now, now, nil)
	receipt, err := entities.NewExecutionReceipt(s.idGen, req, handle, result, s.provenance, timings, s.clock)
	if err != nil {
		return entities.ExecutionReceipt{}, fmt.Errorf("persistStructural: build receipt: %w", err)
	}
	var structuralPersistPanicHandled bool
	saved, err := func() (entities.ExecutionReceipt, error) {
		defer func() {
			if rec := recover(); rec != nil {
				structuralPersistPanicHandled = true
				obslog.FromContext(ctx).Error(ctx, "persist receipt",
					slog.String("panic_location", "persist"),
					slog.String("panic_value", fmt.Sprintf("%v", rec)),
				)
				if s.metrics != nil {
					s.metrics.ReceiptPersistFails.Add(ctx, 1)
				}
			}
		}()
		return s.receipts.Save(ctx, receipt)
	}()
	if err != nil || structuralPersistPanicHandled {
		// §5.4 orthogonal ERROR-always (persistStructural path). The caller
		// enriched the logger with correlation_id before entering
		// persistStructural; capability / adapter / handle_id may or may
		// not be bound depending on which step failed upstream.
		//
		// runtime_adapters.receipt.persist.failures: same instrumentation
		// as the happy path; persist failures from the structural path
		// are operationally indistinguishable from happy-path persist
		// failures and must contribute to the same counter.
		if !structuralPersistPanicHandled {
			if s.metrics != nil {
				s.metrics.ReceiptPersistFails.Add(ctx, 1)
			}
			obslog.FromContext(ctx).Error(ctx, "persist receipt",
				slog.String("error", err.Error()),
			)
		}
		if structuralPersistPanicHandled {
			return entities.ExecutionReceipt{}, fmt.Errorf("persistence failed; side effect may have occurred: panic at persist site")
		}
		return entities.ExecutionReceipt{}, fmt.Errorf("persistence failed; side effect may have occurred: %w", err)
	}

	// Record metrics for the structural path. Duration is 0 for pre-adapter
	// failures (no adapter ran). RecordExecution still increments
	// execution.total{capability, status=failure}; success-only histograms
	// and partial.signal are no-ops here by design.
	//
	// §6.5 + A2C1.10: the saved receipt_id is still the right exemplar tag
	// here even though the structural path doesn't record execution.duration
	// (status=failure). Kept symmetric with the happy path for consistency.
	if s.metrics != nil {
		dur := time.Duration(saved.Result().DurationMs) * time.Millisecond
		s.metrics.RecordExecution(ctx,
			pseudoCap.Canonical(),
			saved.Result().Status.String(),
			saved.ReceiptID().String(),
			dur.Seconds(),
		)
	}

	// Final emit for the pre-adapter structural-failure path. The logger
	// is whatever was bound to ctx by the caller (Execute enriches with
	// correlation_id before invoking persistStructural; capability/handle
	// may or may not be bound depending on which step failed).
	emitExecutionComplete(ctx, obslog.FromContext(ctx), saved)

	return saved, nil
}
