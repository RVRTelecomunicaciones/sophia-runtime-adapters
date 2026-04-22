package log

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// Coverage-focused tests to hit defensive and trivial branches that the
// behavioral tests don't exercise. Lives in its own file to keep the
// intent-driven tests (logger_test, levels_test, ctx_test, config_test)
// readable.

func TestNew_HappyPath_TextAndJSON(t *testing.T) {
	for _, cfg := range []Config{
		{Format: "text", Level: "info"},
		{Format: "json", Level: "debug"},
		{Format: "text", Level: "warn", AddSource: true},
		{Format: "json", Level: "error"},
	} {
		lg, err := New(cfg)
		require.NoError(t, err, "cfg=%+v", cfg)
		require.NotNil(t, lg)
	}
}

func TestLogger_DebugAndWarnEmit(t *testing.T) {
	lg, h := newMemLogger()
	lg.Debug(context.Background(), "debug msg")
	lg.Warn(context.Background(), "warn msg")
	recs := *h.records
	require.Len(t, recs, 2)
	require.Equal(t, "debug msg", recs[0].Message)
	require.Equal(t, "warn msg", recs[1].Message)
}

func TestContextWith_NilLoggerReplacedByNop(t *testing.T) {
	ctx := ContextWith(context.Background(), nil)
	got := FromContext(ctx)
	require.NotNil(t, got, "ContextWith(nil) must still yield a usable logger")
	// The Nop path is safe to emit:
	got.Info(ctx, "silent")
}
