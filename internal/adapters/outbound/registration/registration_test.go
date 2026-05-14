package registration_test

import (
	"testing"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/adapters/outbound/filesystem"
	"github.com/sophia-ecosystem/runtime-adapters/internal/adapters/outbound/git"
	"github.com/sophia-ecosystem/runtime-adapters/internal/adapters/outbound/httpreq"
	"github.com/sophia-ecosystem/runtime-adapters/internal/adapters/outbound/registration"
	"github.com/sophia-ecosystem/runtime-adapters/internal/adapters/outbound/shell"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
)

// validConfig builds a minimal correct Config using the provided tmp dir.
func validConfig(tmp string) registration.Config {
	return registration.Config{
		Shell: shell.Config{
			AllowedCommandsPath: []string{"/bin", "/usr/bin"},
			AllowedWorkingDirs:  []string{tmp},
			InlineStreamLimit:   16 * 1024,
		},
		Git: git.Config{
			AllowedWorkingDirs: []string{tmp},
			SSHAgent:           false,
			InlineStreamLimit:  16 * 1024,
		},
		Filesystem: filesystem.Config{
			AllowedFilesystemRoots: []string{tmp},
			InlineStreamLimit:      16 * 1024,
		},
		HTTP: httpreq.Config{
			InlineStreamLimit: 16 * 1024,
		},
	}
}

func TestRegisterAllPhase1_HappyPath(t *testing.T) {
	tmp := t.TempDir()
	norm := services.NewResultNormalizer(16 * 1024)
	clk := &shared.FakeClock{T: time.Now()}

	adapters, err := registration.RegisterAllPhase1(norm, validConfig(tmp), clk)
	if err != nil {
		t.Fatalf("RegisterAllPhase1: %v", err)
	}
	if len(adapters) != 4 {
		t.Fatalf("expected 4 adapters, got %d", len(adapters))
	}

	// Verify each AdapterID is present.
	expected := []string{"shell", "git", "filesystem", "http"}
	for _, want := range expected {
		id, err := valueobjects.NewAdapterID(want)
		if err != nil {
			t.Fatalf("NewAdapterID(%q): %v", want, err)
		}
		if _, ok := adapters[id]; !ok {
			t.Errorf("missing adapter %q", want)
		}
	}

	// Verify all 9 canonicals registered with normalizer.
	registered := norm.Registered()
	if len(registered) != 9 {
		t.Errorf("expected 9 registered normalizers, got %d: %v", len(registered), registered)
	}
}

func TestRegisterAllPhase1_RejectsNilNormalizer(t *testing.T) {
	tmp := t.TempDir()
	clk := &shared.FakeClock{T: time.Now()}
	if _, err := registration.RegisterAllPhase1(nil, validConfig(tmp), clk); err == nil {
		t.Error("expected error for nil normalizer")
	}
}

func TestRegisterAllPhase1_RejectsNilClock(t *testing.T) {
	tmp := t.TempDir()
	norm := services.NewResultNormalizer(16 * 1024)
	if _, err := registration.RegisterAllPhase1(norm, validConfig(tmp), nil); err == nil {
		t.Error("expected error for nil clock")
	}
}

func TestRegisterAllPhase1_PropagatesAdapterError(t *testing.T) {
	tmp := t.TempDir()
	norm := services.NewResultNormalizer(16 * 1024)
	clk := &shared.FakeClock{T: time.Now()}
	cfg := validConfig(tmp)
	cfg.Shell.InlineStreamLimit = 0 // invalid — forces shell.NewAdapter to fail
	if _, err := registration.RegisterAllPhase1(norm, cfg, clk); err == nil {
		t.Error("expected error from shell adapter constructor")
	}
}

func TestRegisterAllPhase1_DuplicateNormalizerFails(t *testing.T) {
	tmp := t.TempDir()
	norm := services.NewResultNormalizer(16 * 1024)
	clk := &shared.FakeClock{T: time.Now()}

	// Pre-register one of the canonicals to force a duplicate.
	stub := func(_ valueobjects.Capability, _ services.AdapterRawOutcome, _ shared.Clock) (entities.ExecutionResult, error) {
		return entities.ExecutionResult{}, nil
	}
	if err := norm.Register("shell.exec@v1", stub); err != nil {
		t.Fatalf("pre-register stub: %v", err)
	}

	if _, err := registration.RegisterAllPhase1(norm, validConfig(tmp), clk); err == nil {
		t.Error("expected error on duplicate normalizer registration")
	}
}

func TestVerifyCoversPhase1Catalog_Green(t *testing.T) {
	tmp := t.TempDir()
	norm := services.NewResultNormalizer(16 * 1024)
	clk := &shared.FakeClock{T: time.Now()}
	if _, err := registration.RegisterAllPhase1(norm, validConfig(tmp), clk); err != nil {
		t.Fatal(err)
	}
	if err := registration.VerifyCoversPhase1Catalog(norm); err != nil {
		t.Errorf("VerifyCoversPhase1Catalog: %v", err)
	}
}

func TestVerifyCoversPhase1Catalog_MissingNormalizer(t *testing.T) {
	norm := services.NewResultNormalizer(16 * 1024)
	// Don't register anything — every catalog entry should be missing.
	if err := registration.VerifyCoversPhase1Catalog(norm); err == nil {
		t.Error("expected error when no normalizers registered")
	}
}
