package filesystem

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

// NormalizeRead normalizes a filesystem.read_file@v1 raw outcome.
func (a *Adapter) NormalizeRead(_ valueobjects.Capability, raw services.AdapterRawOutcome, clk shared.Clock) (entities.ExecutionResult, error) {
	r, ok := raw.(*readRaw)
	if !ok {
		return entities.ExecutionResult{}, fmt.Errorf("filesystem.NormalizeRead: unexpected raw type %T", raw)
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

	// Success — file content in stdout_ref, no artifacts (read-only).
	stdout, _ := entities.NewStreamRef(r.data, r.totalBytes, a.cfg.InlineStreamLimit)
	meta := map[string]string{
		"truncated":   fmt.Sprintf("%t", r.truncated),
		"total_bytes": fmt.Sprintf("%d", r.totalBytes),
	}
	return entities.NewExecutionResult(
		valueobjects.StatusSuccess, valueobjects.HintNonRetryable,
		"", "",
		&stdout, nil, nil,
		nil, meta,
		r.totalBytes, 0,
		msToDuration(r.durationMs), now,
	)
}

// NormalizeWrite normalizes a filesystem.write_file@v1 raw outcome.
func (a *Adapter) NormalizeWrite(_ valueobjects.Capability, raw services.AdapterRawOutcome, clk shared.Clock) (entities.ExecutionResult, error) {
	r, ok := raw.(*writeRaw)
	if !ok {
		return entities.ExecutionResult{}, fmt.Errorf("filesystem.NormalizeWrite: unexpected raw type %T", raw)
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

	// Success: file artifact with uri/checksum/mode.
	var artifacts []entities.Artifact
	if art, err := entities.NewArtifact(
		entities.ArtifactFile,
		"file://"+r.resolvedPath,
		r.bytesWritten, r.checksumHex,
		map[string]string{
			"mode": fmt.Sprintf("%o", r.mode),
			"path": r.resolvedPath,
		},
	); err == nil {
		artifacts = append(artifacts, art)
	}
	meta := map[string]string{
		"path":          r.resolvedPath,
		"bytes_written": fmt.Sprintf("%d", r.bytesWritten),
	}
	return entities.NewExecutionResult(
		valueobjects.StatusSuccess, valueobjects.HintNonRetryable,
		"", "",
		nil, nil, nil,
		artifacts, meta,
		0, r.bytesWritten,
		msToDuration(r.durationMs), now,
	)
}

func msToDuration(ms int64) time.Duration { return time.Duration(ms) * time.Millisecond }
