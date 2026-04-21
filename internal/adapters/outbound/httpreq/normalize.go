package httpreq

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
)

// Normalize converts an httpRaw into a domain ExecutionResult per §8.5
// status mapping rules.
func (a *Adapter) Normalize(_ valueobjects.Capability, raw services.AdapterRawOutcome, clk shared.Clock) (entities.ExecutionResult, error) {
	r, ok := raw.(*httpRaw)
	if !ok {
		return entities.ExecutionResult{}, fmt.Errorf("httpreq.Normalize: unexpected raw type %T", raw)
	}
	now := clk.Now()

	if r.ctxErr != nil {
		if errors.Is(r.ctxErr, context.DeadlineExceeded) {
			return entities.NewExecutionResult(
				valueobjects.StatusTimeout, valueobjects.HintRetryable,
				valueobjects.ErrTimeout, r.ctxErr.Error(),
				nil, nil, nil, nil, nil, 0, 0, msToDuration(r.durationMs), now,
			)
		}
		return entities.NewExecutionResult(
			valueobjects.StatusCancelled, valueobjects.HintNonRetryable,
			valueobjects.ErrCancelled, r.ctxErr.Error(),
			nil, nil, nil, nil, nil, 0, 0, msToDuration(r.durationMs), now,
		)
	}
	if r.validation != "" {
		return entities.NewExecutionResult(
			valueobjects.StatusFailure, valueobjects.HintNonRetryable,
			valueobjects.ErrValidationFailure, r.validation,
			nil, nil, nil, nil, nil, 0, 0, msToDuration(r.durationMs), now,
		)
	}
	if r.runErr != nil {
		return entities.NewExecutionResult(
			valueobjects.StatusFailure, valueobjects.HintUnknown,
			valueobjects.ErrExternalFailure, r.runErr.Error(),
			nil, nil, nil, nil, nil, 0, 0, msToDuration(r.durationMs), now,
		)
	}

	stdout, _ := entities.NewStreamRef(r.body, r.totalBytes, a.cfg.InlineStreamLimit)

	meta := map[string]string{
		"status_code":    strconv.Itoa(r.statusCode),
		"body_truncated": fmt.Sprintf("%t", r.bodyTruncated),
		"total_bytes":    fmt.Sprintf("%d", r.totalBytes),
		"redirect_count": fmt.Sprintf("%d", r.redirectCount),
	}
	for k, v := range r.headers {
		// Prefix to avoid clashing with our own meta keys.
		meta["header."+k] = v
	}

	// Status classification (§8.5).
	success := false
	if len(r.expectedStatus) > 0 {
		for _, s := range r.expectedStatus {
			if s == r.statusCode {
				success = true
				break
			}
		}
	} else {
		success = r.statusCode >= 200 && r.statusCode < 300
	}

	if success {
		return entities.NewExecutionResult(
			valueobjects.StatusSuccess, valueobjects.HintNonRetryable,
			"", "",
			&stdout, nil, nil, nil, meta,
			r.totalBytes, 0, msToDuration(r.durationMs), now,
		)
	}

	// Failure: classify retry hint by status class.
	hint := valueobjects.HintUnknown
	switch {
	case r.statusCode >= 500 && r.statusCode < 600:
		hint = valueobjects.HintRetryable
	case r.statusCode >= 400 && r.statusCode < 500:
		hint = valueobjects.HintUnknown
	}
	errMsg := fmt.Sprintf("unexpected status %d", r.statusCode)
	if len(r.expectedStatus) > 0 {
		errMsg += fmt.Sprintf(" (expected %v)", r.expectedStatus)
	}
	return entities.NewExecutionResult(
		valueobjects.StatusFailure, hint,
		valueobjects.ErrExternalFailure, errMsg,
		&stdout, nil, nil, nil, meta,
		r.totalBytes, 0, msToDuration(r.durationMs), now,
	)
}

func msToDuration(ms int64) time.Duration { return time.Duration(ms) * time.Millisecond }
