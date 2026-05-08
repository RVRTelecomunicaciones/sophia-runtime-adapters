package log

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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

func TestNewWithHandler_RoundTrip(t *testing.T) {
	recs := make([]slog.Record, 0)
	h := &memHandler{records: &recs}
	lg := NewWithHandler(h)
	lg.Info(context.Background(), "via-with-handler")
	require.Len(t, *h.records, 1)
	require.Equal(t, "via-with-handler", (*h.records)[0].Message)
}

func TestNewWithHandler_NilYieldsNop(t *testing.T) {
	lg := NewWithHandler(nil)
	require.NotNil(t, lg)
	// Does not panic; output discarded.
	lg.Info(context.Background(), "silent")
	lg.Error(context.Background(), "silent-err")
}

func TestLogger_MirrorPathEmpty_StdoutOnly(t *testing.T) {
	cfg := Config{Format: "json", Level: "info", MirrorPath: ""}
	lg, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if lg == nil {
		t.Fatal("logger is nil")
	}
	lg.Info(context.Background(), "test")
}

func TestLogger_MirrorPathSet_FileGetsContent(t *testing.T) {
	dir := t.TempDir()
	mirrorPath := filepath.Join(dir, "runtime.jsonl")
	cfg := Config{Format: "json", Level: "info", MirrorPath: mirrorPath}
	lg, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	lg.Info(context.Background(), "msg-marker")

	data, err := os.ReadFile(mirrorPath)
	if err != nil {
		t.Fatalf("read mirror: %v", err)
	}
	if !strings.Contains(string(data), "msg-marker") {
		t.Errorf("mirror does NOT contain emitted msg; content=%s", data)
	}
}

func TestLogger_MirrorPathInvalidAtStartup_Errors(t *testing.T) {
	cfg := Config{Format: "json", Level: "info", MirrorPath: "/nonexistent-dir-xyz/runtime.jsonl"}
	_, err := New(cfg)
	if err == nil {
		t.Fatal("New: expected error for non-writable mirror path, got nil")
	}
}
