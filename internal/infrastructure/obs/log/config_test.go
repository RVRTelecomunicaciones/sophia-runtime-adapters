package log

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigLoad_Defaults(t *testing.T) {
	t.Setenv("RUNTIME_LOG_FORMAT", "")
	t.Setenv("RUNTIME_LOG_LEVEL", "")
	t.Setenv("RUNTIME_LOG_ADD_SOURCE", "")

	cfg, err := LoadConfig()
	require.NoError(t, err)
	require.Equal(t, "text", cfg.Format)
	require.Equal(t, "info", cfg.Level)
	require.False(t, cfg.AddSource)
}

func TestConfigLoad_Explicit(t *testing.T) {
	t.Setenv("RUNTIME_LOG_FORMAT", "json")
	t.Setenv("RUNTIME_LOG_LEVEL", "debug")
	t.Setenv("RUNTIME_LOG_ADD_SOURCE", "true")

	cfg, err := LoadConfig()
	require.NoError(t, err)
	require.Equal(t, "json", cfg.Format)
	require.Equal(t, "debug", cfg.Level)
	require.True(t, cfg.AddSource)
}

func TestConfigValidate_RejectsBadFormat(t *testing.T) {
	cfg := Config{Format: "xml", Level: "info"}
	require.ErrorContains(t, cfg.Validate(), "RUNTIME_LOG_FORMAT")
}

func TestConfigValidate_RejectsBadLevel(t *testing.T) {
	cfg := Config{Format: "text", Level: "trace"}
	require.ErrorContains(t, cfg.Validate(), "RUNTIME_LOG_LEVEL")
}

func TestConfig_MirrorPath_DefaultEmpty(t *testing.T) {
	t.Setenv("RUNTIME_LOG_MIRROR_PATH", "")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.MirrorPath != "" {
		t.Errorf("MirrorPath: got %q, want empty (R10 pure)", cfg.MirrorPath)
	}
}

func TestConfig_MirrorPath_AbsolutePathRequired(t *testing.T) {
	t.Setenv("RUNTIME_LOG_MIRROR_PATH", "relative/path.jsonl")
	_, err := LoadConfig()
	if err == nil {
		t.Fatal("LoadConfig: expected error for relative path, got nil")
	}
	if !strings.Contains(err.Error(), "absolute") {
		t.Errorf("error must mention 'absolute': got %v", err)
	}
}

func TestConfig_MirrorPath_ParentMustBeWritable(t *testing.T) {
	t.Setenv("RUNTIME_LOG_MIRROR_PATH", "/proc/this-cannot-be-written/runtime.jsonl")
	_, err := LoadConfig()
	if err == nil {
		t.Fatal("LoadConfig: expected error for non-writable parent, got nil")
	}
}

func TestConfig_MirrorPath_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "runtime.jsonl")
	t.Setenv("RUNTIME_LOG_MIRROR_PATH", path)
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.MirrorPath != path {
		t.Errorf("MirrorPath: got %q, want %q", cfg.MirrorPath, path)
	}
}
