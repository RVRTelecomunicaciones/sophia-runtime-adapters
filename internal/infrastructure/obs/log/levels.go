package log

import (
	"log/slog"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
)

// LevelFor maps (status, errClass) to the slog.Level emitted for the
// final per-execution log. See spec §5.4 for rationale.
func LevelFor(status valueobjects.ExecutionStatus, errClass valueobjects.ErrorClass) slog.Level {
	switch status {
	case valueobjects.StatusSuccess, valueobjects.StatusCancelled:
		return slog.LevelInfo
	case valueobjects.StatusTimeout, valueobjects.StatusPartial:
		return slog.LevelWarn
	case valueobjects.StatusFailure:
		switch errClass {
		case valueobjects.ErrValidationFailure,
			valueobjects.ErrCapabilityUnknown,
			valueobjects.ErrPayloadSchemaFailure,
			valueobjects.ErrPreconditionFailure:
			return slog.LevelInfo
		case valueobjects.ErrExternalFailure,
			valueobjects.ErrInterrupted:
			return slog.LevelWarn
		case valueobjects.ErrAdapterInternalError,
			valueobjects.ErrNormalizationFailure:
			return slog.LevelError
		}
	}
	return slog.LevelError
}
