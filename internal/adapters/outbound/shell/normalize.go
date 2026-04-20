package shell

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

// Normalize is the per-capability normalizer closure that the adapter
// registers with services.ResultNormalizer at bootstrap. Signature
// matches services.AdapterNormalizer.
func (a *Adapter) Normalize(cap valueobjects.Capability, raw services.AdapterRawOutcome, clk shared.Clock) (entities.ExecutionResult, error) {
	r, ok := raw.(*execRaw)
	if !ok {
		return entities.ExecutionResult{}, fmt.Errorf("shell.Normalize: unexpected raw type %T", raw)
	}
	now := clk.Now()

	// Priority 1: ctx error classifies the outcome.
	if r.ctxErr != nil {
		if errors.Is(r.ctxErr, context.DeadlineExceeded) {
			return entities.NewExecutionResult(
				valueobjects.StatusTimeout, valueobjects.HintRetryable,
				valueobjects.ErrTimeout, r.ctxErr.Error(),
				streamRefOrNil(r.stdout, a.cfg.InlineStreamLimit),
				streamRefOrNil(r.stderr, a.cfg.InlineStreamLimit),
				ptrInt(r.exitCode),
				nil, nil, // no artifacts (D4.8)
				int64(len(r.stdout)), int64(len(r.stderr)),
				msToDuration(r.durationMs), now,
			)
		}
		return entities.NewExecutionResult(
			valueobjects.StatusCancelled, valueobjects.HintNonRetryable,
			valueobjects.ErrCancelled, r.ctxErr.Error(),
			streamRefOrNil(r.stdout, a.cfg.InlineStreamLimit),
			streamRefOrNil(r.stderr, a.cfg.InlineStreamLimit),
			ptrInt(r.exitCode),
			nil, nil,
			int64(len(r.stdout)), int64(len(r.stderr)),
			msToDuration(r.durationMs), now,
		)
	}

	// Priority 2: validation failure.
	if r.validation != "" {
		return entities.NewExecutionResult(
			valueobjects.StatusFailure, valueobjects.HintNonRetryable,
			valueobjects.ErrValidationFailure, r.validation,
			nil, nil, nil, nil, nil, 0, 0, msToDuration(r.durationMs), now,
		)
	}

	// Priority 3: exit classification against exit_success.
	success := false
	for _, code := range r.exitSuccess {
		if code == r.exitCode {
			success = true
			break
		}
	}
	stdout := streamRefOrNil(r.stdout, a.cfg.InlineStreamLimit)
	stderr := streamRefOrNil(r.stderr, a.cfg.InlineStreamLimit)
	if success {
		return entities.NewExecutionResult(
			valueobjects.StatusSuccess, valueobjects.HintNonRetryable,
			"", "",
			stdout, stderr, ptrInt(r.exitCode),
			nil, nil,
			int64(len(r.stdout)), int64(len(r.stderr)),
			msToDuration(r.durationMs), now,
		)
	}
	return entities.NewExecutionResult(
		valueobjects.StatusFailure, valueobjects.HintUnknown,
		valueobjects.ErrExternalFailure,
		fmt.Sprintf("exit code %d not in exit_success list %v", r.exitCode, r.exitSuccess),
		stdout, stderr, ptrInt(r.exitCode),
		nil, nil,
		int64(len(r.stdout)), int64(len(r.stderr)),
		msToDuration(r.durationMs), now,
	)
}

func streamRefOrNil(data []byte, limit int) *entities.StreamRef {
	if limit <= 0 {
		return nil
	}
	if len(data) == 0 {
		// Empty stream: emit an inline empty StreamRef for observability.
		empty, _ := entities.NewStreamRef(nil, 0, limit)
		return &empty
	}
	s, err := entities.NewStreamRef(data, int64(len(data)), limit)
	if err != nil {
		return nil
	}
	return &s
}

func ptrInt(n int) *int { return &n }

func msToDuration(ms int64) time.Duration { return time.Duration(ms) * time.Millisecond }
