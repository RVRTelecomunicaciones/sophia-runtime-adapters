package log

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
)

// Logger is the contract-bound wrapper around slog.Logger. All emission
// points in the runtime flow through this type.
type Logger struct {
	inner *slog.Logger
}

// New builds a Logger from Config. cfg is validated; returns error on
// invalid enum values (R10).
func New(cfg Config) (*Logger, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("log.New: %w", err)
	}
	level, err := parseLevel(cfg.Level)
	if err != nil {
		return nil, fmt.Errorf("log.New: %w", err)
	}
	opts := &slog.HandlerOptions{
		Level:     level,
		AddSource: cfg.AddSource,
	}
	var h slog.Handler
	switch cfg.Format {
	case "json":
		h = slog.NewJSONHandler(os.Stdout, opts)
	case "text":
		h = slog.NewTextHandler(os.Stdout, opts)
	default:
		// already caught by Validate; defensive
		return nil, fmt.Errorf("log.New: unreachable: format %q", cfg.Format)
	}
	return &Logger{inner: slog.New(h)}, nil
}

// NewNop returns a Logger whose output is discarded. Used as the never-nil
// default in FromContext.
func NewNop() *Logger {
	return &Logger{inner: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

// NewWithHandler builds a Logger from an explicit slog.Handler. Primarily
// useful in tests that need to inspect emitted records; production code
// should use New(Config). A nil handler yields a Nop logger.
func NewWithHandler(h slog.Handler) *Logger {
	if h == nil {
		return NewNop()
	}
	return &Logger{inner: slog.New(h)}
}

// With returns a derived Logger with additional attributes.
func (l *Logger) With(attrs ...slog.Attr) *Logger {
	args := make([]any, 0, len(attrs))
	for _, a := range attrs {
		args = append(args, a)
	}
	return &Logger{inner: l.inner.With(args...)}
}

// Debug emits at DEBUG level.
func (l *Logger) Debug(ctx context.Context, msg string, attrs ...slog.Attr) {
	l.emit(ctx, slog.LevelDebug, msg, attrs)
}

// Info emits at INFO level.
func (l *Logger) Info(ctx context.Context, msg string, attrs ...slog.Attr) {
	l.emit(ctx, slog.LevelInfo, msg, attrs)
}

// Warn emits at WARN level.
func (l *Logger) Warn(ctx context.Context, msg string, attrs ...slog.Attr) {
	l.emit(ctx, slog.LevelWarn, msg, attrs)
}

// Error emits at ERROR level.
func (l *Logger) Error(ctx context.Context, msg string, attrs ...slog.Attr) {
	l.emit(ctx, slog.LevelError, msg, attrs)
}

func (l *Logger) emit(ctx context.Context, lvl slog.Level, msg string, attrs []slog.Attr) {
	if !l.inner.Enabled(ctx, lvl) {
		return
	}
	args := make([]any, 0, len(attrs))
	for _, a := range attrs {
		args = append(args, a)
	}
	l.inner.Log(ctx, lvl, msg, args...)
}

func parseLevel(s string) (slog.Level, error) {
	switch s {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unknown level %q", s)
	}
}
