package obs_test

import (
	"context"
	"testing"

	"github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/config"
	"github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/obs"
)

// minDisabledConfig returns a Config with OTelEnabled=false and all
// required fields populated so config.Validate() would pass if called.
func minDisabledConfig() config.Config {
	return config.Config{
		OTelEnabled:          false,
		OTelServiceName:      "test-service",
		OTelExporterEndpoint: "", // not required when disabled
		OTelTracesSampler:    "parentbased_traceidratio",
		OTelTracesSamplerArg: 0.1,
		RuntimeVersion:       "0.0.0",
		Hostname:             "testhost",
	}
}

// minEnabledConfig returns a Config pointing to an unreachable endpoint.
// OTLP gRPC exporters are lazy — construction succeeds; errors surface
// only during the first export attempt.
func minEnabledConfig() config.Config {
	cfg := minDisabledConfig()
	cfg.OTelEnabled = true
	cfg.OTelExporterEndpoint = "localhost:14317" // intentionally unreachable
	return cfg
}

// TestSetupOTel_Disabled verifies the no-op path returns a non-nil,
// no-error shutdown function and that calling it is clean.
func TestSetupOTel_Disabled(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	shutdown, err := obs.SetupOTel(ctx, minDisabledConfig())
	if err != nil {
		t.Fatalf("disabled SetupOTel returned unexpected error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("disabled SetupOTel returned nil shutdown")
	}
	if err := shutdown(ctx); err != nil {
		t.Fatalf("no-op shutdown returned error: %v", err)
	}
}

// TestSetupOTel_EnabledLazyExporter verifies that even with an
// unreachable endpoint the OTLP gRPC exporter construction succeeds
// (lazy connection), and a valid shutdown is returned.
func TestSetupOTel_EnabledLazyExporter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	shutdown, err := obs.SetupOTel(ctx, minEnabledConfig())
	if err != nil {
		t.Fatalf("enabled SetupOTel with lazy exporter returned unexpected error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("enabled SetupOTel returned nil shutdown")
	}

	// Shutdown with a background context — allow it to fail gracefully
	// (exporter may error on flush when no endpoint is reachable; that
	// is expected and is not what this test guards against).
	_ = shutdown(ctx)
}

// TestParseSampler covers all 6 named samplers plus the unknown-name error path.
func TestParseSampler(t *testing.T) {
	t.Parallel()

	// We test parseSampler indirectly through SetupOTel by varying the
	// OTelTracesSampler field. Each call with OTelEnabled=true constructs
	// the full provider; we only check that no error is returned for valid
	// names and that an error IS returned for an unknown name.

	cases := []struct {
		name    string
		sampler string
		arg     float64
		wantErr bool
	}{
		{"always_on", "always_on", 0, false},
		{"always_off", "always_off", 0, false},
		{"traceidratio", "traceidratio", 0.5, false},
		{"parentbased_always_on", "parentbased_always_on", 0, false},
		{"parentbased_always_off", "parentbased_always_off", 0, false},
		{"parentbased_traceidratio", "parentbased_traceidratio", 0.1, false},
		{"empty_defaults_to_parentbased", "", 0.1, false},
		{"unknown_sampler", "bogus_sampler", 0, true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			cfg := minEnabledConfig()
			cfg.OTelTracesSampler = tc.sampler
			cfg.OTelTracesSamplerArg = tc.arg

			shutdown, err := obs.SetupOTel(ctx, cfg)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for sampler %q, got nil", tc.sampler)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for sampler %q: %v", tc.sampler, err)
			}
			// Clean up providers to avoid test pollution.
			_ = shutdown(ctx)
		})
	}
}

// TestShutdown_Idempotent calls the shutdown function twice and asserts
// the second call also returns without panicking. The second call MAY
// return an error (SDK returns "already shut down") — we only guard
// against panics and verify the function is callable twice.
func TestShutdown_Idempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	shutdown, err := obs.SetupOTel(ctx, minEnabledConfig())
	if err != nil {
		t.Fatalf("SetupOTel: %v", err)
	}

	_ = shutdown(ctx) // first call
	_ = shutdown(ctx) // second call — must not panic
}
