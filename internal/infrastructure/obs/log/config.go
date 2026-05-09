package log

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// Config tunes the Logger. Zero values fall back to Phase 2C.1 defaults
// (FORMAT=text, LEVEL=info, ADD_SOURCE=false). Spec §5.6.
type Config struct {
	Format     string // "text" | "json"
	Level      string // "debug" | "info" | "warn" | "error"
	AddSource  bool
	MirrorPath string // absolute path; empty → stdout only (R10 pure)
}

// LoadConfig reads RUNTIME_LOG_* env vars. Missing or empty → defaults.
// Invalid values cause Validate to return error.
func LoadConfig() (Config, error) {
	cfg := Config{
		Format: envOr("RUNTIME_LOG_FORMAT", "text"),
		Level:  envOr("RUNTIME_LOG_LEVEL", "info"),
	}
	raw := os.Getenv("RUNTIME_LOG_ADD_SOURCE")
	if raw == "" {
		cfg.AddSource = false
	} else {
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("RUNTIME_LOG_ADD_SOURCE: %w", err)
		}
		cfg.AddSource = b
	}
	cfg.MirrorPath = os.Getenv("RUNTIME_LOG_MIRROR_PATH")
	return cfg, cfg.Validate()
}

// Validate enforces strict enum membership per R10.
func (c Config) Validate() error {
	switch c.Format {
	case "text", "json":
	default:
		return fmt.Errorf("RUNTIME_LOG_FORMAT must be text|json, got %q", c.Format)
	}
	switch c.Level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("RUNTIME_LOG_LEVEL must be debug|info|warn|error, got %q", c.Level)
	}
	if c.MirrorPath != "" {
		if !filepath.IsAbs(c.MirrorPath) {
			return fmt.Errorf("RUNTIME_LOG_MIRROR_PATH must be an absolute path, got %q", c.MirrorPath)
		}
		parent := filepath.Dir(c.MirrorPath)
		f, err := os.CreateTemp(parent, ".runtime-log-mirror-check-")
		if err != nil {
			return fmt.Errorf("RUNTIME_LOG_MIRROR_PATH parent %q is not writable: %w", parent, err)
		}
		_ = f.Close()
		_ = os.Remove(f.Name())
	}
	return nil
}

func envOr(key, def string) string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v
}
