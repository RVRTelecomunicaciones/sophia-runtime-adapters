package log

import (
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
