// Package config holds the Phase 1 runtime configuration. Config is
// built from environment variables at startup (load.go) and consumed
// by internal/bootstrap/wire.go — the single composition point.
//
// The defaults mirror spec §9.8 exactly. Override via environment:
//
//	RUNTIME_HTTP_ADDR                  (default ":8080")
//	RUNTIME_MAX_TIMEOUT_BUDGET         (default "30m")
//	RUNTIME_MAX_PAYLOAD_BYTES          (default "1048576")
//	RUNTIME_INLINE_STREAM_LIMIT        (default "16384")
//	RUNTIME_IDEMPOTENCY_WINDOW         (default "24h")
//	RUNTIME_SHUTDOWN_GRACE_PERIOD      (default "30s")
//	RUNTIME_MAX_CONCURRENT_EXECUTIONS  (default "64")
//	RUNTIME_ALLOWED_COMMANDS_PATH      (colon-separated, default "/usr/bin:/bin")
//	RUNTIME_ALLOWED_WORKING_DIRS       (colon-separated, default "$HOME")
//	RUNTIME_ALLOWED_FILESYSTEM_ROOTS   (colon-separated, default "$HOME")
//	RUNTIME_HTTP_ALLOW_PRIVATE_NETWORKS (default "false")
//	RUNTIME_HTTP_HOST_ALLOWLIST        (comma-separated, default "")
//	RUNTIME_POSTGRES_DSN               (required; no default)
//	RUNTIME_VERSION                    (default "0.1.0")
//	RUNTIME_HOSTNAME                   (default from os.Hostname)
//	RUNTIME_PROVENANCE_SOURCE          (default "http"; one of governance|sdk|http|cli|test)
//
//	OTEL_ENABLED                       (default "false")
//	OTEL_SERVICE_NAME                  (default "runtime-adapters")
//	OTEL_EXPORTER_OTLP_ENDPOINT        (no default; required if OTEL_ENABLED=true)
//	OTEL_TRACES_SAMPLER                (default "parentbased_traceidratio")
//	OTEL_TRACES_SAMPLER_ARG            (default "0.1")
package config

import (
	"fmt"
	"time"
)

// Config bundles all runtime settings. Never construct directly —
// call Load() to build from env + defaults with validation.
type Config struct {
	// HTTP server
	HTTPAddr string

	// Runtime caps + windows
	MaxTimeoutBudget        time.Duration
	MaxPayloadBytes         int
	InlineStreamLimit       int
	IdempotencyWindow       time.Duration
	ShutdownGracePeriod     time.Duration
	MaxConcurrentExecutions int

	// Shell adapter
	AllowedCommandsPath []string
	AllowedWorkingDirs  []string

	// Filesystem adapter
	AllowedFilesystemRoots []string

	// HTTP adapter
	HTTPAllowPrivateNetworks bool
	HTTPHostAllowlist        []string

	// Persistence
	PostgresDSN string

	// Identity / provenance
	RuntimeVersion   string
	Hostname         string
	ProvenanceSource string // one of governance|sdk|http|cli|test

	// OTel
	OTelEnabled          bool
	OTelServiceName      string
	OTelExporterEndpoint string
	OTelTracesSampler    string
	OTelTracesSamplerArg float64
}

// Validate enforces domain rules on a constructed Config. Load() calls
// this before returning. Rerun after any mutation (though production
// code should never mutate Config after Load).
func (c Config) Validate() error {
	if c.HTTPAddr == "" {
		return fmt.Errorf("HTTPAddr must not be empty")
	}
	if c.MaxTimeoutBudget <= 0 {
		return fmt.Errorf("MaxTimeoutBudget must be > 0, got %v", c.MaxTimeoutBudget)
	}
	if c.MaxPayloadBytes <= 0 {
		return fmt.Errorf("MaxPayloadBytes must be > 0, got %d", c.MaxPayloadBytes)
	}
	if c.InlineStreamLimit <= 0 {
		return fmt.Errorf("InlineStreamLimit must be > 0, got %d", c.InlineStreamLimit)
	}
	if c.IdempotencyWindow <= 0 {
		return fmt.Errorf("IdempotencyWindow must be > 0, got %v", c.IdempotencyWindow)
	}
	if c.ShutdownGracePeriod <= 0 {
		return fmt.Errorf("ShutdownGracePeriod must be > 0, got %v", c.ShutdownGracePeriod)
	}
	if c.MaxConcurrentExecutions <= 0 {
		return fmt.Errorf("MaxConcurrentExecutions must be > 0, got %d", c.MaxConcurrentExecutions)
	}
	if len(c.AllowedCommandsPath) == 0 {
		return fmt.Errorf("AllowedCommandsPath must be non-empty")
	}
	if len(c.AllowedWorkingDirs) == 0 {
		return fmt.Errorf("AllowedWorkingDirs must be non-empty")
	}
	if len(c.AllowedFilesystemRoots) == 0 {
		return fmt.Errorf("AllowedFilesystemRoots must be non-empty")
	}
	if c.PostgresDSN == "" {
		return fmt.Errorf("PostgresDSN is required (set RUNTIME_POSTGRES_DSN)")
	}
	if c.RuntimeVersion == "" {
		return fmt.Errorf("RuntimeVersion must not be empty")
	}
	if c.Hostname == "" {
		return fmt.Errorf("Hostname must not be empty")
	}
	if !validProvenanceSource(c.ProvenanceSource) {
		return fmt.Errorf("ProvenanceSource %q invalid (must be one of governance|sdk|http|cli|test)", c.ProvenanceSource)
	}
	if c.OTelEnabled && c.OTelExporterEndpoint == "" {
		return fmt.Errorf("OTelExporterEndpoint is required when OTelEnabled=true")
	}
	if c.OTelTracesSamplerArg < 0 || c.OTelTracesSamplerArg > 1 {
		return fmt.Errorf("OTelTracesSamplerArg must be in [0,1], got %v", c.OTelTracesSamplerArg)
	}
	return nil
}

func validProvenanceSource(s string) bool {
	switch s {
	case "governance", "sdk", "http", "cli", "test":
		return true
	}
	return false
}
