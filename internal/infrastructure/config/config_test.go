package config

import (
	"testing"
	"time"
)

// validBase returns a fully-valid Config with all required fields set.
// Tests mutate individual fields to exercise each validation branch.
func validBase() Config {
	return Config{
		HTTPAddr:                 ":8080",
		MaxTimeoutBudget:         30 * time.Minute,
		MaxPayloadBytes:          1 << 20,
		InlineStreamLimit:        16 * 1024,
		IdempotencyWindow:        24 * time.Hour,
		ShutdownGracePeriod:      30 * time.Second,
		MaxConcurrentExecutions:  64,
		AllowedCommandsPath:      []string{"/usr/bin", "/bin"},
		AllowedWorkingDirs:       []string{"/home/user"},
		AllowedFilesystemRoots:   []string{"/home/user"},
		PostgresDSN:              "postgres://user:pass@localhost/db",
		RuntimeVersion:           "0.1.0",
		Hostname:                 "test-host",
		ProvenanceSource:         "http",
		OTelEnabled:              false,
		OTelServiceName:          "runtime-adapters",
		OTelExporterEndpoint:     "",
		OTelTracesSampler:        "parentbased_traceidratio",
		OTelTracesSamplerArg:     0.1,
	}
}

func TestConfig_Validate_HappyPath(t *testing.T) {
	c := validBase()
	if err := c.Validate(); err != nil {
		t.Fatalf("expected nil error for valid config, got: %v", err)
	}
}

func TestConfig_Validate_ProvenanceSources(t *testing.T) {
	for _, src := range []string{"governance", "sdk", "http", "cli", "test"} {
		c := validBase()
		c.ProvenanceSource = src
		if err := c.Validate(); err != nil {
			t.Errorf("ProvenanceSource %q should be valid, got: %v", src, err)
		}
	}
}

func TestConfig_Validate_OTelEnabledWithEndpoint(t *testing.T) {
	c := validBase()
	c.OTelEnabled = true
	c.OTelExporterEndpoint = "http://otel-collector:4317"
	if err := c.Validate(); err != nil {
		t.Fatalf("OTelEnabled=true with endpoint set should be valid, got: %v", err)
	}
}

func TestConfig_Validate_SamplerArgBoundaries(t *testing.T) {
	tests := []struct {
		name    string
		arg     float64
		wantErr bool
	}{
		{"zero", 0.0, false},
		{"one", 1.0, false},
		{"mid", 0.5, false},
		{"negative", -0.001, true},
		{"over one", 1.001, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validBase()
			c.OTelTracesSamplerArg = tt.arg
			err := c.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("arg=%v: wantErr=%v, got err=%v", tt.arg, tt.wantErr, err)
			}
		})
	}
}

func TestConfig_Validate_FieldErrors(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{
			name:   "empty HTTPAddr",
			mutate: func(c *Config) { c.HTTPAddr = "" },
		},
		{
			name:   "zero MaxTimeoutBudget",
			mutate: func(c *Config) { c.MaxTimeoutBudget = 0 },
		},
		{
			name:   "negative MaxTimeoutBudget",
			mutate: func(c *Config) { c.MaxTimeoutBudget = -1 * time.Second },
		},
		{
			name:   "zero MaxPayloadBytes",
			mutate: func(c *Config) { c.MaxPayloadBytes = 0 },
		},
		{
			name:   "negative MaxPayloadBytes",
			mutate: func(c *Config) { c.MaxPayloadBytes = -1 },
		},
		{
			name:   "zero InlineStreamLimit",
			mutate: func(c *Config) { c.InlineStreamLimit = 0 },
		},
		{
			name:   "zero IdempotencyWindow",
			mutate: func(c *Config) { c.IdempotencyWindow = 0 },
		},
		{
			name:   "zero ShutdownGracePeriod",
			mutate: func(c *Config) { c.ShutdownGracePeriod = 0 },
		},
		{
			name:   "zero MaxConcurrentExecutions",
			mutate: func(c *Config) { c.MaxConcurrentExecutions = 0 },
		},
		{
			name:   "empty AllowedCommandsPath",
			mutate: func(c *Config) { c.AllowedCommandsPath = nil },
		},
		{
			name:   "empty AllowedWorkingDirs",
			mutate: func(c *Config) { c.AllowedWorkingDirs = []string{} },
		},
		{
			name:   "empty AllowedFilesystemRoots",
			mutate: func(c *Config) { c.AllowedFilesystemRoots = nil },
		},
		{
			name:   "empty PostgresDSN",
			mutate: func(c *Config) { c.PostgresDSN = "" },
		},
		{
			name:   "empty RuntimeVersion",
			mutate: func(c *Config) { c.RuntimeVersion = "" },
		},
		{
			name:   "empty Hostname",
			mutate: func(c *Config) { c.Hostname = "" },
		},
		{
			name:   "invalid ProvenanceSource",
			mutate: func(c *Config) { c.ProvenanceSource = "invalid" },
		},
		{
			name: "OTelEnabled without endpoint",
			mutate: func(c *Config) {
				c.OTelEnabled = true
				c.OTelExporterEndpoint = ""
			},
		},
		{
			name:   "OTelTracesSamplerArg negative",
			mutate: func(c *Config) { c.OTelTracesSamplerArg = -0.5 },
		},
		{
			name:   "OTelTracesSamplerArg > 1",
			mutate: func(c *Config) { c.OTelTracesSamplerArg = 1.5 },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validBase()
			tt.mutate(&c)
			if err := c.Validate(); err == nil {
				t.Errorf("expected validation error for %q, got nil", tt.name)
			}
		})
	}
}
