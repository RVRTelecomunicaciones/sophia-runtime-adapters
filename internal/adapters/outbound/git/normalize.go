package git

import (
	"context"
	"errors"
	"fmt"
	"os"
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
func (a *Adapter) NormalizeStatus(_ valueobjects.Capability, raw services.AdapterRawOutcome, clk shared.Clock) (entities.ExecutionResult, error) {
	r, ok := raw.(*statusRaw)
	if !ok {
		return entities.ExecutionResult{}, fmt.Errorf("git.NormalizeStatus: unexpected raw type %T", raw)
	}
	now := clk.Now()

	// Priority 1: ctx error.
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

	// Priority 2: validation.
	if r.validation != "" {
		return entities.NewExecutionResult(
			valueobjects.StatusFailure, valueobjects.HintNonRetryable,
			valueobjects.ErrValidationFailure, r.validation,
			nil, nil, nil, nil, nil, 0, 0, msToDuration(r.durationMs), now,
		)
	}

	// Priority 3: external (go-git) error.
	if r.runErr != nil {
		return entities.NewExecutionResult(
			valueobjects.StatusFailure, valueobjects.HintUnknown,
			valueobjects.ErrExternalFailure, r.runErr.Error(),
			nil, nil, nil, nil, nil, 0, 0, msToDuration(r.durationMs), now,
		)
	}

	// Success — pack metadata. No artifacts (read-only).
	meta := map[string]string{
		"branch":        r.branch,
		"head":          r.head,
		"clean":         fmt.Sprintf("%t", r.clean),
		"entries_count": fmt.Sprintf("%d", len(r.entries)),
	}
	return entities.NewExecutionResult(
		valueobjects.StatusSuccess, valueobjects.HintNonRetryable,
		"", "",
		nil, nil, nil,
		nil,
		meta,
		0, 0,
		msToDuration(r.durationMs), now,
	)
}

// NormalizeClone normalizes a git.clone@v1 raw outcome with D4.6 partial
// classification: partial when fetch succeeds, checkout fails, ≥1 artifact
// present, and the destination was NOT reverted.
func (a *Adapter) NormalizeClone(cap valueobjects.Capability, raw services.AdapterRawOutcome, clk shared.Clock) (entities.ExecutionResult, error) {
	r, ok := raw.(*cloneRaw)
	if !ok {
		return entities.ExecutionResult{}, fmt.Errorf("git.NormalizeClone: unexpected raw type %T", raw)
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

	// Build artifacts when the clone produced a directory.
	var artifacts []entities.Artifact
	if r.repoPath != "" && (r.fetchOK || r.runErr != nil) {
		// Directory artifact: present whenever something was written to dest.
		if _, statErr := os.Stat(r.repoPath); statErr == nil {
			if dirArt, err := entities.NewArtifact(
				entities.ArtifactDirectory,
				"file://"+r.repoPath,
				0, "",
				map[string]string{"path": r.repoPath},
			); err == nil {
				artifacts = append(artifacts, dirArt)
			}
		}
	}
	if r.headSHA != "" {
		if commitArt, err := entities.NewArtifact(
			entities.ArtifactGitCommit,
			"git://"+r.repoPath+"#"+r.headSHA,
			0, "",
			map[string]string{"sha": r.headSHA},
		); err == nil {
			artifacts = append(artifacts, commitArt)
		}
	}
	if r.refResolved != "" {
		if refArt, err := entities.NewArtifact(
			entities.ArtifactGitRef,
			"git://"+r.repoPath+"@"+r.refResolved,
			0, "",
			map[string]string{"ref": r.refResolved},
		); err == nil {
			artifacts = append(artifacts, refArt)
		}
	}

	// Apply D4.6 4-AND partial classification.
	classification := entities.PartialClassification{
		AllowsPartial:      cap.AllowsPartial(),
		HasArtifacts:       len(artifacts) > 0,
		HasUncompletedStep: !r.checkoutOK,
		Reverted:           false, // go-git clone does not roll back on checkout failure
	}
	status := classification.Classify()

	meta := map[string]string{
		"fetch_ok":    fmt.Sprintf("%t", r.fetchOK),
		"checkout_ok": fmt.Sprintf("%t", r.checkoutOK),
	}
	if r.repoPath != "" {
		meta["repo_path"] = r.repoPath
	}
	if r.refResolved != "" {
		meta["ref_resolved"] = r.refResolved
	}
	if r.headSHA != "" {
		meta["head_sha"] = r.headSHA
	}

	switch status {
	case valueobjects.StatusSuccess:
		return entities.NewExecutionResult(
			valueobjects.StatusSuccess, valueobjects.HintNonRetryable,
			"", "",
			nil, nil, nil,
			artifacts, meta,
			0, 0, msToDuration(r.durationMs), now,
		)
	case valueobjects.StatusPartial:
		errMsg := "clone partial: fetch succeeded but checkout failed"
		if r.runErr != nil {
			errMsg += " — " + r.runErr.Error()
		}
		return entities.NewExecutionResult(
			valueobjects.StatusPartial, valueobjects.HintUnknown,
			valueobjects.ErrExternalFailure, errMsg,
			nil, nil, nil,
			artifacts, meta,
			0, 0, msToDuration(r.durationMs), now,
		)
	default:
		// Failure — either runErr without artifacts, or total clone failure.
		errMsg := "clone failed"
		if r.runErr != nil {
			errMsg = r.runErr.Error()
		}
		return entities.NewExecutionResult(
			valueobjects.StatusFailure, valueobjects.HintUnknown,
			valueobjects.ErrExternalFailure, errMsg,
			nil, nil, nil,
			artifacts, meta,
			0, 0, msToDuration(r.durationMs), now,
		)
	}
}

// NormalizeDiff normalizes a git.diff@v1 raw outcome.
func (a *Adapter) NormalizeDiff(_ valueobjects.Capability, raw services.AdapterRawOutcome, clk shared.Clock) (entities.ExecutionResult, error) {
	r, ok := raw.(*diffRaw)
	if !ok {
		return entities.ExecutionResult{}, fmt.Errorf("git.NormalizeDiff: unexpected raw type %T", raw)
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

	// Success: patch in stdout_ref, no artifacts (read-only).
	stdout, _ := entities.NewStreamRef(r.patch, int64(r.totalBytes), a.cfg.InlineStreamLimit)
	meta := map[string]string{
		"truncated":   fmt.Sprintf("%t", r.truncated),
		"total_bytes": fmt.Sprintf("%d", r.totalBytes),
	}
	return entities.NewExecutionResult(
		valueobjects.StatusSuccess, valueobjects.HintNonRetryable,
		"", "",
		&stdout, nil, nil,
		nil, // no artifacts (read-only)
		meta,
		int64(r.totalBytes), 0,
		msToDuration(r.durationMs), now,
	)
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
