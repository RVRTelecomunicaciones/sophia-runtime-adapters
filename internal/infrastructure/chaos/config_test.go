package chaos

import (
	"strings"
	"testing"
)

// TestLoadConfig_DisabledByDefault verifies that Enabled=false returns a
// disabled Config without error, with Env set to the supplied runtimeEnv.
func TestLoadConfig_DisabledByDefault(t *testing.T) {
	cfg, err := LoadConfig(ChaosConfigEnv{Enabled: false}, "development", testCatalog())
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil Config")
	}
	if cfg.Enabled {
		t.Error("Config.Enabled: got true, want false")
	}
	if cfg.Env != "development" {
		t.Errorf("Config.Env: got %q, want %q", cfg.Env, "development")
	}
}

// TestLoadConfig_DisabledStillReturnsEnv verifies that when disabled the
// Config.Env field is set from runtimeEnv even though chaos is off.
func TestLoadConfig_DisabledStillReturnsEnv(t *testing.T) {
	cfg, err := LoadConfig(ChaosConfigEnv{Enabled: false}, "staging", testCatalog())
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if cfg.Env != "staging" {
		t.Errorf("Config.Env: got %q, want %q", cfg.Env, "staging")
	}
}

// TestLoadConfig_RejectsProductionWhenEnabled verifies that enabling chaos in a
// production environment is rejected (R17 fail-closed).
func TestLoadConfig_RejectsProductionWhenEnabled(t *testing.T) {
	cfg, err := LoadConfig(
		ChaosConfigEnv{Enabled: true, ProfilePath: "internal/infrastructure/chaos/testdata/minimal-enabled.yaml"},
		"production",
		testCatalog(),
	)
	if err == nil {
		t.Fatal("expected error for production + enabled, got nil")
	}
	if cfg != nil {
		t.Errorf("expected nil Config on error, got: %+v", cfg)
	}
	if !strings.Contains(err.Error(), "production") {
		t.Errorf("error should mention %q, got: %v", "production", err)
	}
}

// TestLoadConfig_AllowsProductionWhenDisabled verifies that production env is
// not rejected when chaos is disabled — production infra is the common case.
func TestLoadConfig_AllowsProductionWhenDisabled(t *testing.T) {
	cfg, err := LoadConfig(ChaosConfigEnv{Enabled: false}, "production", testCatalog())
	if err != nil {
		t.Fatalf("expected nil error for disabled+production, got: %v", err)
	}
	if cfg.Enabled {
		t.Error("Config.Enabled: got true, want false")
	}
}

// TestLoadConfig_RequiresProfilePathWhenEnabled verifies that enabling chaos
// without a profile path is rejected.
func TestLoadConfig_RequiresProfilePathWhenEnabled(t *testing.T) {
	cfg, err := LoadConfig(
		ChaosConfigEnv{Enabled: true, ProfilePath: ""},
		"development",
		testCatalog(),
	)
	if err == nil {
		t.Fatal("expected error for enabled+empty ProfilePath, got nil")
	}
	if cfg != nil {
		t.Errorf("expected nil Config on error, got: %+v", cfg)
	}
	if !strings.Contains(err.Error(), "RUNTIME_CHAOS_PROFILE") {
		t.Errorf("error should mention RUNTIME_CHAOS_PROFILE, got: %v", err)
	}
}

// TestLoadConfig_HappyPath verifies the successful case: enabled + valid path +
// development env → returns Config with Enabled=true and a loaded Profile.
func TestLoadConfig_HappyPath(t *testing.T) {
	cfg, err := LoadConfig(
		ChaosConfigEnv{
			Enabled:     true,
			ProfilePath: "internal/infrastructure/chaos/testdata/minimal-enabled.yaml",
		},
		"development",
		testCatalog(),
	)
	if err != nil {
		t.Fatalf("expected nil error on happy path, got: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil Config")
	}
	if !cfg.Enabled {
		t.Error("Config.Enabled: got false, want true")
	}
	if cfg.Profile == nil {
		t.Error("Config.Profile: got nil, want non-nil loaded profile")
	}
	if cfg.Env != "development" {
		t.Errorf("Config.Env: got %q, want %q", cfg.Env, "development")
	}
	if cfg.Profile.Name != "minimal-enabled" {
		t.Errorf("Profile.Name: got %q, want %q", cfg.Profile.Name, "minimal-enabled")
	}
}

// TestLoadConfig_StagingEnvAllowed verifies that staging environment allows
// chaos to be enabled — only production is forbidden.
func TestLoadConfig_StagingEnvAllowed(t *testing.T) {
	cfg, err := LoadConfig(
		ChaosConfigEnv{
			Enabled:     true,
			ProfilePath: "internal/infrastructure/chaos/testdata/minimal-enabled.yaml",
		},
		"staging",
		testCatalog(),
	)
	if err != nil {
		t.Fatalf("staging env should allow chaos, got error: %v", err)
	}
	if !cfg.Enabled {
		t.Error("Config.Enabled: got false, want true")
	}
	if cfg.Env != "staging" {
		t.Errorf("Config.Env: got %q, want %q", cfg.Env, "staging")
	}
}

// TestLoadConfig_DelegatesProfileLoadErrors verifies that a parse-time error
// from LoadProfile (e.g. invalid-fault.yaml has an unknown fault kind) bubbles
// through unchanged.
func TestLoadConfig_DelegatesProfileLoadErrors(t *testing.T) {
	cfg, err := LoadConfig(
		ChaosConfigEnv{
			Enabled:     true,
			ProfilePath: "internal/infrastructure/chaos/testdata/invalid-fault.yaml",
		},
		"development",
		testCatalog(),
	)
	if err == nil {
		t.Fatal("expected error from invalid-fault profile, got nil")
	}
	if cfg != nil {
		t.Errorf("expected nil Config on error, got: %+v", cfg)
	}
	// The error originates from parseProfile (fault-kind validation).
	msg := err.Error()
	if !strings.Contains(msg, "fault kind") && !strings.Contains(msg, "not_a_real_fault") {
		t.Errorf("error should mention fault kind or not_a_real_fault, got: %v", err)
	}
}

// TestLoadConfig_DelegatesAllowlistErrors verifies that a path outside the
// allowlist is rejected by LoadProfile and the error bubbles through.
func TestLoadConfig_DelegatesAllowlistErrors(t *testing.T) {
	cfg, err := LoadConfig(
		ChaosConfigEnv{
			Enabled:     true,
			ProfilePath: "/tmp/not-in-allowlist.yaml",
		},
		"development",
		testCatalog(),
	)
	if err == nil {
		t.Fatal("expected allowlist error, got nil")
	}
	if cfg != nil {
		t.Errorf("expected nil Config on error, got: %+v", cfg)
	}
	if !strings.Contains(err.Error(), "allowlist") {
		t.Errorf("error should mention allowlist, got: %v", err)
	}
}

// TestLoadConfig_NilProfileOnError verifies that every error path returns a nil
// *Config — callers must not partially use a failed Config.
func TestLoadConfig_NilProfileOnError(t *testing.T) {
	cases := []struct {
		name string
		env  ChaosConfigEnv
		renv string
	}{
		{
			name: "production rejected",
			env:  ChaosConfigEnv{Enabled: true, ProfilePath: "internal/infrastructure/chaos/testdata/minimal-enabled.yaml"},
			renv: "production",
		},
		{
			name: "empty profile path",
			env:  ChaosConfigEnv{Enabled: true, ProfilePath: ""},
			renv: "development",
		},
		{
			name: "invalid profile",
			env:  ChaosConfigEnv{Enabled: true, ProfilePath: "internal/infrastructure/chaos/testdata/invalid-fault.yaml"},
			renv: "development",
		},
		{
			name: "outside allowlist",
			env:  ChaosConfigEnv{Enabled: true, ProfilePath: "/tmp/outside.yaml"},
			renv: "development",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := LoadConfig(tc.env, tc.renv, testCatalog())
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if cfg != nil {
				t.Errorf("expected nil *Config on error, got: %+v", cfg)
			}
		})
	}
}
