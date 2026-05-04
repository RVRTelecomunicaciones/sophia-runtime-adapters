package bootstrap

import (
	"errors"
	"strings"
	"testing"

	"github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/config"
)

// TestAdapterConfig_TranslatesCorrectly verifies that adapterConfig maps
// every relevant top-level Config field to the correct adapter sub-config.
// No Docker or external resources required.
func TestAdapterConfig_TranslatesCorrectly(t *testing.T) {
	cfg := config.Config{
		AllowedCommandsPath:      []string{"/usr/bin"},
		AllowedWorkingDirs:       []string{"/tmp"},
		AllowedFilesystemRoots:   []string{"/tmp"},
		InlineStreamLimit:        16 * 1024,
		HTTPAllowPrivateNetworks: true,
		HTTPHostAllowlist:        []string{"example.com"},
	}
	ac := adapterConfig(cfg)

	// Shell
	if len(ac.Shell.AllowedCommandsPath) != 1 || ac.Shell.AllowedCommandsPath[0] != "/usr/bin" {
		t.Errorf("shell AllowedCommandsPath: got %v", ac.Shell.AllowedCommandsPath)
	}
	if ac.Shell.InlineStreamLimit != 16*1024 {
		t.Errorf("shell InlineStreamLimit: got %d, want %d", ac.Shell.InlineStreamLimit, 16*1024)
	}

	// Git
	if len(ac.Git.AllowedWorkingDirs) != 1 || ac.Git.AllowedWorkingDirs[0] != "/tmp" {
		t.Errorf("git AllowedWorkingDirs: got %v", ac.Git.AllowedWorkingDirs)
	}
	if !ac.Git.SSHAgent {
		t.Errorf("git SSHAgent: expected true (Phase 1 default), got false")
	}
	if ac.Git.InlineStreamLimit != 16*1024 {
		t.Errorf("git InlineStreamLimit: got %d, want %d", ac.Git.InlineStreamLimit, 16*1024)
	}

	// Filesystem
	if len(ac.Filesystem.AllowedFilesystemRoots) != 1 || ac.Filesystem.AllowedFilesystemRoots[0] != "/tmp" {
		t.Errorf("filesystem AllowedFilesystemRoots: got %v", ac.Filesystem.AllowedFilesystemRoots)
	}
	if ac.Filesystem.InlineStreamLimit != 16*1024 {
		t.Errorf("filesystem InlineStreamLimit: got %d, want %d", ac.Filesystem.InlineStreamLimit, 16*1024)
	}

	// HTTP
	if !ac.HTTP.AllowPrivateNetworks {
		t.Errorf("http AllowPrivateNetworks: expected true, got false")
	}
	if len(ac.HTTP.HostAllowlist) != 1 || ac.HTTP.HostAllowlist[0] != "example.com" {
		t.Errorf("http HostAllowlist: got %v", ac.HTTP.HostAllowlist)
	}
	if ac.HTTP.InlineStreamLimit != 16*1024 {
		t.Errorf("http InlineStreamLimit: got %d, want %d", ac.HTTP.InlineStreamLimit, 16*1024)
	}
}

// TestRuntime_AbortsInCIWithoutTestTenant verifies the Layer 3
// runtime-side mode lock per spec §9.3 + D2C4AB.16. Aborts startup
// if CI=true && RUNTIME_TENANT != "test".
//
// Uses the helper enforceRuntimeModeLock so the test does NOT
// require a real Postgres + OTel stack — pure env-var logic.
func TestRuntime_AbortsInCIWithoutTestTenant(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr bool
	}{
		{"local + non-test tenant: ok", map[string]string{"CI": "", "RUNTIME_TENANT": "prod"}, false},
		{"CI + test tenant: ok", map[string]string{"CI": "true", "RUNTIME_TENANT": "test"}, false},
		{"CI + non-test tenant: ABORT", map[string]string{"CI": "true", "RUNTIME_TENANT": "prod"}, true},
		{"CI + missing tenant: ABORT", map[string]string{"CI": "true"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := enforceRuntimeModeLock(func(k string) string { return tt.env[k] })
			if (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.Is(err, errRuntimeModeLock) {
					t.Errorf("err must wrap errRuntimeModeLock, got %v", err)
				}
				if !strings.Contains(err.Error(), "RUNTIME_TENANT") {
					t.Errorf("err must mention RUNTIME_TENANT, got %v", err)
				}
			}
		})
	}
}
