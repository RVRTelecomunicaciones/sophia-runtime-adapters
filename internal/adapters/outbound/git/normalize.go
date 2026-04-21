package git

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
)

// NormalizeStatus / NormalizeClone / NormalizeDiff / NormalizeCommit are
// the registered closures consumed by services.ResultNormalizer. T34
// scaffolds them with notImpl + ctx handling; T35..T38 add the real
// per-capability normalization.

// NormalizeStatus normalizes a git.status@v1 raw outcome.
func (a *Adapter) NormalizeStatus(cap valueobjects.Capability, raw services.AdapterRawOutcome, clk shared.Clock) (entities.ExecutionResult, error) {
	r, ok := raw.(*statusRaw)
	if !ok {
		return entities.ExecutionResult{}, fmt.Errorf("git.NormalizeStatus: unexpected raw type %T", raw)
	}
	return a.normalizePlaceholder(r.ctxErr, r.notImpl, r.validation, r.durationMs, clk)
}

// NormalizeClone normalizes a git.clone@v1 raw outcome.
func (a *Adapter) NormalizeClone(cap valueobjects.Capability, raw services.AdapterRawOutcome, clk shared.Clock) (entities.ExecutionResult, error) {
	r, ok := raw.(*cloneRaw)
	if !ok {
		return entities.ExecutionResult{}, fmt.Errorf("git.NormalizeClone: unexpected raw type %T", raw)
	}
	return a.normalizePlaceholder(r.ctxErr, r.notImpl, r.validation, r.durationMs, clk)
}

// NormalizeDiff normalizes a git.diff@v1 raw outcome.
func (a *Adapter) NormalizeDiff(cap valueobjects.Capability, raw services.AdapterRawOutcome, clk shared.Clock) (entities.ExecutionResult, error) {
	r, ok := raw.(*diffRaw)
	if !ok {
		return entities.ExecutionResult{}, fmt.Errorf("git.NormalizeDiff: unexpected raw type %T", raw)
	}
	return a.normalizePlaceholder(r.ctxErr, r.notImpl, r.validation, r.durationMs, clk)
}

// NormalizeCommit normalizes a git.commit@v1 raw outcome.
func (a *Adapter) NormalizeCommit(cap valueobjects.Capability, raw services.AdapterRawOutcome, clk shared.Clock) (entities.ExecutionResult, error) {
	r, ok := raw.(*commitRaw)
	if !ok {
		return entities.ExecutionResult{}, fmt.Errorf("git.NormalizeCommit: unexpected raw type %T", raw)
	}
	return a.normalizePlaceholder(r.ctxErr, r.notImpl, r.validation, r.durationMs, clk)
}

// normalizePlaceholder is the shared stub logic used by all four normalizers
// until T35..T38 replace them with real per-capability normalization.
func (a *Adapter) normalizePlaceholder(ctxErr error, notImpl bool, validation string, durationMs int64, clk shared.Clock) (entities.ExecutionResult, error) {
	now := clk.Now()
	dur := msToDuration(durationMs)

	if ctxErr != nil {
		if errors.Is(ctxErr, context.DeadlineExceeded) {
			return entities.NewExecutionResult(
				valueobjects.StatusTimeout, valueobjects.HintRetryable,
				valueobjects.ErrTimeout, ctxErr.Error(),
				nil, nil, nil, nil, nil, 0, 0, dur, now,
			)
		}
		return entities.NewExecutionResult(
			valueobjects.StatusCancelled, valueobjects.HintNonRetryable,
			valueobjects.ErrCancelled, ctxErr.Error(),
			nil, nil, nil, nil, nil, 0, 0, dur, now,
		)
	}
	if validation != "" {
		return entities.NewExecutionResult(
			valueobjects.StatusFailure, valueobjects.HintNonRetryable,
			valueobjects.ErrValidationFailure, validation,
			nil, nil, nil, nil, nil, 0, 0, dur, now,
		)
	}
	if notImpl {
		return entities.NewExecutionResult(
			valueobjects.StatusFailure, valueobjects.HintNonRetryable,
			valueobjects.ErrAdapterInternalError, "git adapter capability is not implemented yet (T34 scaffolding)",
			nil, nil, nil, nil, nil, 0, 0, dur, now,
		)
	}
	return entities.NewExecutionResult(
		valueobjects.StatusFailure, valueobjects.HintNonRetryable,
		valueobjects.ErrAdapterInternalError, "git adapter normalize: unhandled raw state",
		nil, nil, nil, nil, nil, 0, 0, dur, now,
	)
}

func msToDuration(ms int64) time.Duration { return time.Duration(ms) * time.Millisecond }
