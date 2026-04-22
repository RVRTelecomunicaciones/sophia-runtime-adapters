package log

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
)

func TestLevelFor_Status(t *testing.T) {
	type tc struct {
		status   valueobjects.ExecutionStatus
		errClass valueobjects.ErrorClass
		want     slog.Level
	}
	cases := []tc{
		// success + cancelled → INFO regardless of errClass
		{valueobjects.StatusSuccess, "", slog.LevelInfo},
		{valueobjects.StatusCancelled, "", slog.LevelInfo},

		// timeout + partial → WARN regardless of errClass
		{valueobjects.StatusTimeout, "", slog.LevelWarn},
		{valueobjects.StatusPartial, "", slog.LevelWarn},

		// failure with caller-fault ErrorClasses → INFO
		{valueobjects.StatusFailure, valueobjects.ErrValidationFailure, slog.LevelInfo},
		{valueobjects.StatusFailure, valueobjects.ErrCapabilityUnknown, slog.LevelInfo},
		{valueobjects.StatusFailure, valueobjects.ErrPayloadSchemaFailure, slog.LevelInfo},
		{valueobjects.StatusFailure, valueobjects.ErrPreconditionFailure, slog.LevelInfo},

		// failure with transient ErrorClasses → WARN
		{valueobjects.StatusFailure, valueobjects.ErrExternalFailure, slog.LevelWarn},
		{valueobjects.StatusFailure, valueobjects.ErrInterrupted, slog.LevelWarn},

		// failure with invariant-violation ErrorClasses → ERROR
		{valueobjects.StatusFailure, valueobjects.ErrAdapterInternalError, slog.LevelError},
		{valueobjects.StatusFailure, valueobjects.ErrNormalizationFailure, slog.LevelError},

		// Defensive: ErrTimeout / ErrCancelled as ErrorClass under StatusFailure
		// are unspecced in §5.4. They fall through to the outer default → ERROR.
		// These cases exist to pin the current behavior and prevent silent drift
		// if someone later adds these values to the sub-switch without updating
		// this table.
		{valueobjects.StatusFailure, valueobjects.ErrTimeout, slog.LevelError},
		{valueobjects.StatusFailure, valueobjects.ErrCancelled, slog.LevelError},

		// defensive: unknown status escalates to ERROR
		{valueobjects.ExecutionStatus("impossible"), "", slog.LevelError},
	}
	for _, c := range cases {
		got := LevelFor(c.status, c.errClass)
		require.Equal(t, c.want, got,
			"status=%s errClass=%s", c.status, c.errClass)
	}
}
