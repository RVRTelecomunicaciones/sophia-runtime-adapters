package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

// minimalMap returns a MapEnv with only the required key set.
func minimalMap() MapEnv {
	return MapEnv{
		"RUNTIME_POSTGRES_DSN": "postgres://user:pass@localhost/db",
	}
}

// TestLoad_Defaults verifies that when only the required RUNTIME_POSTGRES_DSN
// is set, all other fields receive their spec §9.8 default values.
func TestLoad_Defaults(t *testing.T) {
	home, _ := os.UserHomeDir()
	hostname, _ := os.Hostname()

	c, err := Load(minimalMap())
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	tests := []struct {
		name string
		got  interface{}
		want interface{}
	}{
		{"HTTPAddr", c.HTTPAddr, ":8080"},
		{"MaxTimeoutBudget", c.MaxTimeoutBudget, 30 * time.Minute},
		{"MaxPayloadBytes", c.MaxPayloadBytes, 1 << 20},
		{"InlineStreamLimit", c.InlineStreamLimit, 16 * 1024},
		{"IdempotencyWindow", c.IdempotencyWindow, 24 * time.Hour},
		{"ShutdownGracePeriod", c.ShutdownGracePeriod, 30 * time.Second},
		{"MaxConcurrentExecutions", c.MaxConcurrentExecutions, 64},
		{"HTTPAllowPrivateNetworks", c.HTTPAllowPrivateNetworks, false},
		{"PostgresDSN", c.PostgresDSN, "postgres://user:pass@localhost/db"},
		{"RuntimeVersion", c.RuntimeVersion, "0.1.0"},
		{"Hostname", c.Hostname, hostname},
		{"ProvenanceSource", c.ProvenanceSource, "http"},
		{"OTelEnabled", c.OTelEnabled, false},
		{"OTelServiceName", c.OTelServiceName, "runtime-adapters"},
		{"OTelExporterEndpoint", c.OTelExporterEndpoint, ""},
		{"OTelTracesSampler", c.OTelTracesSampler, "parentbased_traceidratio"},
		{"OTelTracesSamplerArg", c.OTelTracesSamplerArg, 0.1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s: got %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}

	// AllowedCommandsPath default
	if len(c.AllowedCommandsPath) != 2 ||
		c.AllowedCommandsPath[0] != "/usr/bin" ||
		c.AllowedCommandsPath[1] != "/bin" {
		t.Errorf("AllowedCommandsPath: got %v, want [/usr/bin /bin]", c.AllowedCommandsPath)
	}

	// AllowedWorkingDirs default = $HOME
	if len(c.AllowedWorkingDirs) != 1 || c.AllowedWorkingDirs[0] != home {
		t.Errorf("AllowedWorkingDirs: got %v, want [%s]", c.AllowedWorkingDirs, home)
	}

	// AllowedFilesystemRoots default = $HOME
	if len(c.AllowedFilesystemRoots) != 1 || c.AllowedFilesystemRoots[0] != home {
		t.Errorf("AllowedFilesystemRoots: got %v, want [%s]", c.AllowedFilesystemRoots, home)
	}

	// HTTPHostAllowlist default = nil/empty
	if len(c.HTTPHostAllowlist) != 0 {
		t.Errorf("HTTPHostAllowlist: got %v, want empty", c.HTTPHostAllowlist)
	}
}

// TestLoad_Overrides verifies that every env var override is applied
// to the corresponding Config field.
func TestLoad_Overrides(t *testing.T) {
	env := MapEnv{
		"RUNTIME_HTTP_ADDR":                   ":9090",
		"RUNTIME_MAX_TIMEOUT_BUDGET":          "1h",
		"RUNTIME_MAX_PAYLOAD_BYTES":           "2097152",
		"RUNTIME_INLINE_STREAM_LIMIT":         "32768",
		"RUNTIME_IDEMPOTENCY_WINDOW":          "48h",
		"RUNTIME_SHUTDOWN_GRACE_PERIOD":       "60s",
		"RUNTIME_MAX_CONCURRENT_EXECUTIONS":   "128",
		"RUNTIME_ALLOWED_COMMANDS_PATH":       "/usr/local/bin:/usr/bin",
		"RUNTIME_ALLOWED_WORKING_DIRS":        "/tmp:/var/tmp",
		"RUNTIME_ALLOWED_FILESYSTEM_ROOTS":    "/data:/mnt",
		"RUNTIME_HTTP_ALLOW_PRIVATE_NETWORKS": "true",
		"RUNTIME_HTTP_HOST_ALLOWLIST":         "example.com,api.example.com",
		"RUNTIME_POSTGRES_DSN":                "postgres://prod:secret@db/prod",
		"RUNTIME_VERSION":                     "1.2.3",
		"RUNTIME_HOSTNAME":                    "my-host",
		"RUNTIME_PROVENANCE_SOURCE":           "governance",
		"OTEL_ENABLED":                        "true",
		"OTEL_SERVICE_NAME":                   "my-service",
		"OTEL_EXPORTER_OTLP_ENDPOINT":         "http://otel:4317",
		"OTEL_TRACES_SAMPLER":                 "always_on",
		"OTEL_TRACES_SAMPLER_ARG":             "1.0",
	}

	c, err := Load(env)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if c.HTTPAddr != ":9090" {
		t.Errorf("HTTPAddr: got %q, want %q", c.HTTPAddr, ":9090")
	}
	if c.MaxTimeoutBudget != time.Hour {
		t.Errorf("MaxTimeoutBudget: got %v, want 1h", c.MaxTimeoutBudget)
	}
	if c.MaxPayloadBytes != 2097152 {
		t.Errorf("MaxPayloadBytes: got %d, want 2097152", c.MaxPayloadBytes)
	}
	if c.InlineStreamLimit != 32768 {
		t.Errorf("InlineStreamLimit: got %d, want 32768", c.InlineStreamLimit)
	}
	if c.IdempotencyWindow != 48*time.Hour {
		t.Errorf("IdempotencyWindow: got %v, want 48h", c.IdempotencyWindow)
	}
	if c.ShutdownGracePeriod != 60*time.Second {
		t.Errorf("ShutdownGracePeriod: got %v, want 60s", c.ShutdownGracePeriod)
	}
	if c.MaxConcurrentExecutions != 128 {
		t.Errorf("MaxConcurrentExecutions: got %d, want 128", c.MaxConcurrentExecutions)
	}
	if len(c.AllowedCommandsPath) != 2 || c.AllowedCommandsPath[0] != "/usr/local/bin" {
		t.Errorf("AllowedCommandsPath: got %v", c.AllowedCommandsPath)
	}
	if len(c.AllowedWorkingDirs) != 2 || c.AllowedWorkingDirs[1] != "/var/tmp" {
		t.Errorf("AllowedWorkingDirs: got %v", c.AllowedWorkingDirs)
	}
	if len(c.AllowedFilesystemRoots) != 2 || c.AllowedFilesystemRoots[0] != "/data" {
		t.Errorf("AllowedFilesystemRoots: got %v", c.AllowedFilesystemRoots)
	}
	if !c.HTTPAllowPrivateNetworks {
		t.Error("HTTPAllowPrivateNetworks: got false, want true")
	}
	if len(c.HTTPHostAllowlist) != 2 {
		t.Errorf("HTTPHostAllowlist: got %v, want 2 entries", c.HTTPHostAllowlist)
	}
	if c.PostgresDSN != "postgres://prod:secret@db/prod" {
		t.Errorf("PostgresDSN: got %q", c.PostgresDSN)
	}
	if c.RuntimeVersion != "1.2.3" {
		t.Errorf("RuntimeVersion: got %q", c.RuntimeVersion)
	}
	if c.Hostname != "my-host" {
		t.Errorf("Hostname: got %q", c.Hostname)
	}
	if c.ProvenanceSource != "governance" {
		t.Errorf("ProvenanceSource: got %q", c.ProvenanceSource)
	}
	if !c.OTelEnabled {
		t.Error("OTelEnabled: got false, want true")
	}
	if c.OTelServiceName != "my-service" {
		t.Errorf("OTelServiceName: got %q", c.OTelServiceName)
	}
	if c.OTelExporterEndpoint != "http://otel:4317" {
		t.Errorf("OTelExporterEndpoint: got %q", c.OTelExporterEndpoint)
	}
	if c.OTelTracesSampler != "always_on" {
		t.Errorf("OTelTracesSampler: got %q", c.OTelTracesSampler)
	}
	if c.OTelTracesSamplerArg != 1.0 {
		t.Errorf("OTelTracesSamplerArg: got %v, want 1.0", c.OTelTracesSamplerArg)
	}
}

// TestLoad_MalformedIntFallsBackToDefault verifies that a non-numeric value
// for an integer field is silently ignored and the default applies.
func TestLoad_MalformedIntFallsBackToDefault(t *testing.T) {
	env := MapEnv{
		"RUNTIME_POSTGRES_DSN":      "postgres://u:p@localhost/db",
		"RUNTIME_MAX_PAYLOAD_BYTES": "not a number",
	}
	c, err := Load(env)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if c.MaxPayloadBytes != 1<<20 {
		t.Errorf("MaxPayloadBytes: got %d, want %d (default)", c.MaxPayloadBytes, 1<<20)
	}
}

// TestLoad_MissingPostgresDSN verifies that Load returns an error when the
// required RUNTIME_POSTGRES_DSN is not set.
func TestLoad_MissingPostgresDSN(t *testing.T) {
	_, err := Load(MapEnv{})
	if err == nil {
		t.Fatal("expected error when RUNTIME_POSTGRES_DSN is missing, got nil")
	}
	if !strings.Contains(err.Error(), "PostgresDSN") {
		t.Errorf("error should mention PostgresDSN, got: %v", err)
	}
}

// TestLoad_OTelEnabledWithoutEndpoint verifies that enabling OTel without
// an endpoint triggers a validation error.
func TestLoad_OTelEnabledWithoutEndpoint(t *testing.T) {
	env := MapEnv{
		"RUNTIME_POSTGRES_DSN": "postgres://u:p@localhost/db",
		"OTEL_ENABLED":         "true",
	}
	_, err := Load(env)
	if err == nil {
		t.Fatal("expected error when OTEL_ENABLED=true without endpoint, got nil")
	}
	if !strings.Contains(err.Error(), "OTelExporterEndpoint") {
		t.Errorf("error should mention OTelExporterEndpoint, got: %v", err)
	}
}

// TestLoad_OTelSamplerArgOutOfRange verifies that a sampler arg > 1 is rejected.
func TestLoad_OTelSamplerArgOutOfRange(t *testing.T) {
	env := MapEnv{
		"RUNTIME_POSTGRES_DSN":    "postgres://u:p@localhost/db",
		"OTEL_TRACES_SAMPLER_ARG": "1.5",
	}
	_, err := Load(env)
	if err == nil {
		t.Fatal("expected error for OTelTracesSamplerArg=1.5, got nil")
	}
	if !strings.Contains(err.Error(), "OTelTracesSamplerArg") {
		t.Errorf("error should mention OTelTracesSamplerArg, got: %v", err)
	}
}

// TestLoad_InvalidProvenanceSource verifies that an unrecognised source
// value triggers a validation error.
func TestLoad_InvalidProvenanceSource(t *testing.T) {
	env := MapEnv{
		"RUNTIME_POSTGRES_DSN":      "postgres://u:p@localhost/db",
		"RUNTIME_PROVENANCE_SOURCE": "unknown",
	}
	_, err := Load(env)
	if err == nil {
		t.Fatal("expected error for invalid ProvenanceSource, got nil")
	}
	if !strings.Contains(err.Error(), "ProvenanceSource") {
		t.Errorf("error should mention ProvenanceSource, got: %v", err)
	}
}

// TestLoad_NilEnvSourceUsesOSEnv verifies that passing nil falls back to the
// real OS environment (OSEnv). We set a real env var to exercise this path.
func TestLoad_NilEnvSourceUsesOSEnv(t *testing.T) {
	// Set required var in actual OS env for this test.
	t.Setenv("RUNTIME_POSTGRES_DSN", "postgres://u:p@localhost/db")

	c, err := Load(nil)
	if err != nil {
		t.Fatalf("Load(nil) returned error: %v", err)
	}
	if c.PostgresDSN != "postgres://u:p@localhost/db" {
		t.Errorf("PostgresDSN: got %q", c.PostgresDSN)
	}
}

// --- RUNTIME_ENV tests ---

// TestLoad_RuntimeEnv_Default verifies that RUNTIME_ENV defaults to "development".
func TestLoad_RuntimeEnv_Default(t *testing.T) {
	c, err := Load(minimalMap())
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if c.Env != "development" {
		t.Errorf("Env: got %q, want %q", c.Env, "development")
	}
}

// TestLoad_RuntimeEnv_ValidValues verifies all three allowed values are accepted.
func TestLoad_RuntimeEnv_ValidValues(t *testing.T) {
	for _, env := range []string{"development", "staging", "production"} {
		t.Run(env, func(t *testing.T) {
			m := minimalMap()
			m["RUNTIME_ENV"] = env
			c, err := Load(m)
			if err != nil {
				t.Fatalf("RUNTIME_ENV=%q should be valid, got error: %v", env, err)
			}
			if c.Env != env {
				t.Errorf("Env: got %q, want %q", c.Env, env)
			}
		})
	}
}

// TestLoad_RuntimeEnv_InvalidValue verifies that an unknown value is rejected
// at parse time (R10) with an error mentioning RUNTIME_ENV.
func TestLoad_RuntimeEnv_InvalidValue(t *testing.T) {
	m := minimalMap()
	m["RUNTIME_ENV"] = "qa"
	_, err := Load(m)
	if err == nil {
		t.Fatal("expected error for RUNTIME_ENV=qa, got nil")
	}
	if !strings.Contains(err.Error(), "RUNTIME_ENV") {
		t.Errorf("error should mention RUNTIME_ENV, got: %v", err)
	}
}

// --- RUNTIME_CHAOS_ENABLED tests ---

// TestLoad_ChaosEnabled_Default verifies that RUNTIME_CHAOS_ENABLED defaults to false.
func TestLoad_ChaosEnabled_Default(t *testing.T) {
	c, err := Load(minimalMap())
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if c.Chaos.Enabled {
		t.Error("Chaos.Enabled: got true, want false (default)")
	}
}

// TestLoad_ChaosEnabled_True verifies "true" is parsed as true.
func TestLoad_ChaosEnabled_True(t *testing.T) {
	m := minimalMap()
	m["RUNTIME_CHAOS_ENABLED"] = "true"
	c, err := Load(m)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !c.Chaos.Enabled {
		t.Error("Chaos.Enabled: got false, want true")
	}
}

// TestLoad_ChaosEnabled_One verifies "1" is parsed as true via strconv.ParseBool.
func TestLoad_ChaosEnabled_One(t *testing.T) {
	m := minimalMap()
	m["RUNTIME_CHAOS_ENABLED"] = "1"
	c, err := Load(m)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !c.Chaos.Enabled {
		t.Error("Chaos.Enabled: got false, want true (strconv.ParseBool accepts '1')")
	}
}

// --- RUNTIME_CHAOS_PROFILE tests ---

// TestLoad_ChaosProfile_Default verifies that RUNTIME_CHAOS_PROFILE defaults to "".
func TestLoad_ChaosProfile_Default(t *testing.T) {
	c, err := Load(minimalMap())
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if c.Chaos.ProfilePath != "" {
		t.Errorf("Chaos.ProfilePath: got %q, want %q (default empty)", c.Chaos.ProfilePath, "")
	}
}

// TestLoad_ChaosProfile_LiteralPassThrough verifies that the profile path is
// stored verbatim — path validation is intentionally deferred to LoadConfig.
func TestLoad_ChaosProfile_LiteralPassThrough(t *testing.T) {
	path := "ops/chaos/profiles/ci/my-profile.yaml"
	m := minimalMap()
	m["RUNTIME_CHAOS_PROFILE"] = path
	c, err := Load(m)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if c.Chaos.ProfilePath != path {
		t.Errorf("Chaos.ProfilePath: got %q, want %q", c.Chaos.ProfilePath, path)
	}
}

// TestLookupList_Separators verifies that both ":" and "," separators work
// correctly in lookupList.
func TestLookupList_Separators(t *testing.T) {
	tests := []struct {
		name  string
		value string
		sep   string
		want  []string
	}{
		{
			name:  "colon separator",
			value: "/usr/bin:/bin:/usr/local/bin",
			sep:   ":",
			want:  []string{"/usr/bin", "/bin", "/usr/local/bin"},
		},
		{
			name:  "comma separator",
			value: "example.com,api.example.com,cdn.example.com",
			sep:   ",",
			want:  []string{"example.com", "api.example.com", "cdn.example.com"},
		},
		{
			name:  "colon with spaces trimmed",
			value: "/a : /b : /c",
			sep:   ":",
			want:  []string{"/a", "/b", "/c"},
		},
		{
			name:  "empty string returns default",
			value: "",
			sep:   ":",
			want:  []string{"/default"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var src MapEnv
			if tt.value != "" {
				src = MapEnv{"KEY": tt.value}
			} else {
				src = MapEnv{}
			}
			got := lookupList(src, "KEY", tt.sep, []string{"/default"})
			if len(got) != len(tt.want) {
				t.Fatalf("len: got %d, want %d — got %v", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("[%d]: got %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
