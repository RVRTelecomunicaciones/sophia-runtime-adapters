package log

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

// memHandler collects records for assertion.
type memHandler struct {
	records *[]slog.Record
	attrs   []slog.Attr
}

func (h *memHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *memHandler) Handle(_ context.Context, r slog.Record) error {
	if len(h.attrs) > 0 {
		r.AddAttrs(h.attrs...)
	}
	*h.records = append(*h.records, r)
	return nil
}
func (h *memHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	merged := make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	merged = append(merged, h.attrs...)
	merged = append(merged, attrs...)
	return &memHandler{records: h.records, attrs: merged}
}
func (h *memHandler) WithGroup(name string) slog.Handler { return h }

func newMemLogger() (*Logger, *memHandler) {
	recs := make([]slog.Record, 0)
	h := &memHandler{records: &recs}
	return &Logger{inner: slog.New(h)}, h
}

func TestLogger_EmitsInfo(t *testing.T) {
	lg, h := newMemLogger()
	lg.Info(context.Background(), "hello", slog.String("k", "v"))
	require.Len(t, *h.records, 1)
	require.Equal(t, slog.LevelInfo, (*h.records)[0].Level)
	require.Equal(t, "hello", (*h.records)[0].Message)
}

func TestLogger_EmitsError(t *testing.T) {
	lg, h := newMemLogger()
	lg.Error(context.Background(), "oops")
	require.Len(t, *h.records, 1)
	require.Equal(t, slog.LevelError, (*h.records)[0].Level)
}

func TestLogger_WithDerivesChild(t *testing.T) {
	lg, h := newMemLogger()
	child := lg.With(slog.String("capability", "shell.exec@v1"))
	child.Info(context.Background(), "exec")
	require.Len(t, *h.records, 1)

	found := false
	(*h.records)[0].Attrs(func(a slog.Attr) bool {
		if a.Key == "capability" && a.Value.String() == "shell.exec@v1" {
			found = true
		}
		return true
	})
	require.True(t, found, "child logger should carry With attrs")
}

func TestNewNop_DoesNotPanic(t *testing.T) {
	lg := NewNop()
	require.NotNil(t, lg)
	lg.Info(context.Background(), "silent")
	lg.Error(context.Background(), "silent")
}

func TestNew_BadConfigReturnsError(t *testing.T) {
	_, err := New(Config{Format: "xml", Level: "info"})
	require.Error(t, err)
}
