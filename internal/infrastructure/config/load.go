package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/chaos"
)

// EnvSource abstracts env-var lookup for testability. os.LookupEnv is
// the production implementation.
type EnvSource interface {
	Lookup(key string) (string, bool)
}

// OSEnv reads from os.Getenv / os.LookupEnv.
type OSEnv struct{}

// Lookup returns the env var value and whether it was set.
func (OSEnv) Lookup(key string) (string, bool) { return os.LookupEnv(key) }

// MapEnv is a test-only EnvSource backed by a map.
type MapEnv map[string]string

// Lookup returns the value and presence flag for the given key.
func (m MapEnv) Lookup(key string) (string, bool) {
	v, ok := m[key]
	return v, ok
}

// Load reads env vars from src and returns a validated Config. Returns
// an error on parse or validation failure. Pass OSEnv{} in production.
//
// Malformed env values (e.g. a non-numeric string for an integer field)
// are treated as unset and the default is applied. Call Validate() on
// the returned Config to surface configuration errors; do not rely on
// strict parse errors from Load itself.
func Load(src EnvSource) (Config, error) {
	if src == nil {
		src = OSEnv{}
	}
	home, _ := os.UserHomeDir()
	hostname, _ := os.Hostname()

	c := Config{
		HTTPAddr:                 lookupString(src, "RUNTIME_HTTP_ADDR", ":8080"),
		MaxTimeoutBudget:         lookupDuration(src, "RUNTIME_MAX_TIMEOUT_BUDGET", 30*time.Minute),
		MaxPayloadBytes:          lookupInt(src, "RUNTIME_MAX_PAYLOAD_BYTES", 1<<20),
		InlineStreamLimit:        lookupInt(src, "RUNTIME_INLINE_STREAM_LIMIT", 16*1024),
		IdempotencyWindow:        lookupDuration(src, "RUNTIME_IDEMPOTENCY_WINDOW", 24*time.Hour),
		ShutdownGracePeriod:      lookupDuration(src, "RUNTIME_SHUTDOWN_GRACE_PERIOD", 30*time.Second),
		MaxConcurrentExecutions:  lookupInt(src, "RUNTIME_MAX_CONCURRENT_EXECUTIONS", 64),
		AllowedCommandsPath:      lookupList(src, "RUNTIME_ALLOWED_COMMANDS_PATH", ":", []string{"/usr/bin", "/bin"}),
		AllowedWorkingDirs:       lookupList(src, "RUNTIME_ALLOWED_WORKING_DIRS", ":", []string{home}),
		AllowedFilesystemRoots:   lookupList(src, "RUNTIME_ALLOWED_FILESYSTEM_ROOTS", ":", []string{home}),
		HTTPAllowPrivateNetworks: lookupBool(src, "RUNTIME_HTTP_ALLOW_PRIVATE_NETWORKS", false),
		HTTPHostAllowlist:        lookupList(src, "RUNTIME_HTTP_HOST_ALLOWLIST", ",", nil),
		PostgresDSN:              lookupString(src, "RUNTIME_POSTGRES_DSN", ""),
		RuntimeVersion:           lookupString(src, "RUNTIME_VERSION", "0.1.0"),
		Hostname:                 lookupString(src, "RUNTIME_HOSTNAME", hostname),
		ProvenanceSource:         lookupString(src, "RUNTIME_PROVENANCE_SOURCE", "http"),
		OTelEnabled:              lookupBool(src, "OTEL_ENABLED", false),
		OTelServiceName:          lookupString(src, "OTEL_SERVICE_NAME", "runtime-adapters"),
		OTelExporterEndpoint:     lookupString(src, "OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		OTelTracesSampler:        lookupString(src, "OTEL_TRACES_SAMPLER", "parentbased_traceidratio"),
		OTelTracesSamplerArg:     lookupFloat(src, "OTEL_TRACES_SAMPLER_ARG", 0.1),
		Env:                      lookupRuntimeEnv(src),
		Chaos: chaos.ChaosConfigEnv{
			Enabled:     lookupBool(src, "RUNTIME_CHAOS_ENABLED", false),
			ProfilePath: lookupString(src, "RUNTIME_CHAOS_PROFILE", ""),
		},
	}

	if err := c.Validate(); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	return c, nil
}

func lookupString(src EnvSource, key, def string) string {
	if v, ok := src.Lookup(key); ok && v != "" {
		return v
	}
	return def
}

func lookupInt(src EnvSource, key string, def int) int {
	if v, ok := src.Lookup(key); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func lookupDuration(src EnvSource, key string, def time.Duration) time.Duration {
	if v, ok := src.Lookup(key); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func lookupBool(src EnvSource, key string, def bool) bool {
	if v, ok := src.Lookup(key); ok && v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

func lookupFloat(src EnvSource, key string, def float64) float64 {
	if v, ok := src.Lookup(key); ok && v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func lookupList(src EnvSource, key, sep string, def []string) []string {
	v, ok := src.Lookup(key)
	if !ok || v == "" {
		return def
	}
	parts := strings.Split(v, sep)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return def
	}
	return out
}

// lookupRuntimeEnv reads RUNTIME_ENV and returns the value as-is (defaulting
// to "development" when unset). Enum validation is performed by Validate() so
// that unknown values are rejected at startup per R10 with a clear error.
func lookupRuntimeEnv(src EnvSource) string {
	if v, ok := src.Lookup("RUNTIME_ENV"); ok && v != "" {
		return v
	}
	return "development"
}
