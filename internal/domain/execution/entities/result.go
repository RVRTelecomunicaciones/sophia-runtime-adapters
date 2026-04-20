package entities

import (
	"fmt"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
)

const (
	// MaxErrorMessageBytes bounds the error_message size (§4.3). Messages
	// longer than this are truncated silently; adapters are responsible
	// for not including PII.
	MaxErrorMessageBytes = 4 * 1024
	// MaxAdapterMetaBytes bounds the total byte length of adapter_meta
	// values (I18 / A4.2). Measured on values only, not keys.
	MaxAdapterMetaBytes = 8 * 1024
)

// ExecutionResult is the normalized, adapter-agnostic output of a
// capability execution. It is produced by the domain's ResultNormalizer
// from adapter-specific raw outcomes; downstream (application,
// persistence, inbound) only ever sees this type.
//
// Consistency invariants (§5.9):
//   - success ⇒ error_class is empty AND error_message is empty
//   - timeout ⇒ error_class is ErrTimeout
//   - cancelled ⇒ error_class is ErrCancelled
//   - failure or partial ⇒ error_class is any of the remaining 7 values
//     AND error_message is non-empty
//
// All fields are exported so that the normalizer (in a different package)
// can construct instances directly; NewExecutionResult still validates
// invariants before returning.
type ExecutionResult struct {
	Status       valueobjects.ExecutionStatus `json:"status"`
	Retryable    valueobjects.RetryHint       `json:"retryable"`
	ErrorClass   valueobjects.ErrorClass      `json:"error_class,omitempty"`
	ErrorMessage string                       `json:"error_message,omitempty"`
	StdoutRef    *StreamRef                   `json:"stdout_ref,omitempty"`
	StderrRef    *StreamRef                   `json:"stderr_ref,omitempty"`
	ExitCode     *int                         `json:"exit_code,omitempty"`
	Artifacts    []Artifact                   `json:"artifacts"`
	AdapterMeta  map[string]string            `json:"adapter_meta"`
	BytesRead    int64                        `json:"bytes_read"`
	BytesWritten int64                        `json:"bytes_written"`
	DurationMs   int64                        `json:"duration_ms"`
	CompletedAt  time.Time                    `json:"completed_at"`
}

// NewExecutionResult validates the arguments against §5.9 consistency
// invariants and returns an ExecutionResult. ErrorMessage is silently
// truncated to MaxErrorMessageBytes. AdapterMeta total value bytes is
// validated against MaxAdapterMetaBytes.
//
// Parameters:
//   - status, retry, errClass, errMsg: see §5.9.
//   - stdoutRef, stderrRef: optional, may be nil (e.g. for read-only capabilities).
//   - exitCode: optional, set for shell capabilities; nil otherwise.
//   - artifacts: may be empty; nil is converted to empty slice.
//   - adapterMeta: may be empty; nil is converted to empty map; total ≤ 8 KiB.
//   - bytesRead/bytesWritten: 0 if N/A.
//   - duration, completedAt: required.
func NewExecutionResult(
	status valueobjects.ExecutionStatus,
	retry valueobjects.RetryHint,
	errClass valueobjects.ErrorClass,
	errMsg string,
	stdoutRef, stderrRef *StreamRef,
	exitCode *int,
	artifacts []Artifact,
	adapterMeta map[string]string,
	bytesRead, bytesWritten int64,
	duration time.Duration,
	completedAt time.Time,
) (ExecutionResult, error) {
	// Invariant checks first.
	switch status {
	case valueobjects.StatusSuccess:
		if errClass != "" || errMsg != "" {
			return ExecutionResult{}, fmt.Errorf("status=success must not carry error_class (%q) or error_message", errClass)
		}
	case valueobjects.StatusTimeout:
		if errClass != valueobjects.ErrTimeout {
			return ExecutionResult{}, fmt.Errorf("status=timeout must carry error_class=timeout, got %q", errClass)
		}
	case valueobjects.StatusCancelled:
		if errClass != valueobjects.ErrCancelled {
			return ExecutionResult{}, fmt.Errorf("status=cancelled must carry error_class=cancelled, got %q", errClass)
		}
	case valueobjects.StatusFailure, valueobjects.StatusPartial:
		if errClass == "" {
			return ExecutionResult{}, fmt.Errorf("status=%s must carry a non-empty error_class", status)
		}
		if errMsg == "" {
			return ExecutionResult{}, fmt.Errorf("status=%s must carry a non-empty error_message", status)
		}
	default:
		return ExecutionResult{}, fmt.Errorf("invalid execution_status %q", status)
	}

	// RetryHint must be a valid enum value.
	switch retry {
	case valueobjects.HintRetryable, valueobjects.HintNonRetryable, valueobjects.HintUnknown:
	default:
		return ExecutionResult{}, fmt.Errorf("invalid retry_hint %q", retry)
	}

	// Truncate error_message if too long.
	if len(errMsg) > MaxErrorMessageBytes {
		errMsg = errMsg[:MaxErrorMessageBytes]
	}

	// Validate adapter_meta total bytes.
	if adapterMeta == nil {
		adapterMeta = map[string]string{}
	}
	total := 0
	for _, v := range adapterMeta {
		total += len(v)
	}
	if total > MaxAdapterMetaBytes {
		return ExecutionResult{}, fmt.Errorf("adapter_meta total value bytes %d exceeds max %d", total, MaxAdapterMetaBytes)
	}

	// bytesRead / bytesWritten must not be negative.
	if bytesRead < 0 {
		return ExecutionResult{}, fmt.Errorf("bytes_read must be ≥ 0, got %d", bytesRead)
	}
	if bytesWritten < 0 {
		return ExecutionResult{}, fmt.Errorf("bytes_written must be ≥ 0, got %d", bytesWritten)
	}

	if duration < 0 {
		return ExecutionResult{}, fmt.Errorf("duration must be ≥ 0, got %v", duration)
	}

	if artifacts == nil {
		artifacts = []Artifact{}
	}

	// Defensive copy of adapter_meta and artifacts so caller mutations
	// can't affect the result.
	metaCopy := make(map[string]string, len(adapterMeta))
	for k, v := range adapterMeta {
		metaCopy[k] = v
	}
	artifactsCopy := make([]Artifact, len(artifacts))
	copy(artifactsCopy, artifacts)

	return ExecutionResult{
		Status:       status,
		Retryable:    retry,
		ErrorClass:   errClass,
		ErrorMessage: errMsg,
		StdoutRef:    stdoutRef,
		StderrRef:    stderrRef,
		ExitCode:     exitCode,
		Artifacts:    artifactsCopy,
		AdapterMeta:  metaCopy,
		BytesRead:    bytesRead,
		BytesWritten: bytesWritten,
		DurationMs:   duration.Milliseconds(),
		CompletedAt:  completedAt.UTC(),
	}, nil
}

// StripStreams returns a copy of the result with StdoutRef and StderrRef
// set to nil. Used by GetReceipt with include_streams=false (UC3 / §6.4)
// for lightweight audit fetches.
func (r ExecutionResult) StripStreams() ExecutionResult {
	r.StdoutRef = nil
	r.StderrRef = nil
	return r
}
