// Package registration wires the Phase 1 outbound adapters into a single
// registry consumed by internal/bootstrap/wire.go (Bundle 7) and
// ExecuteService (Bundle 3). RegisterAllPhase1 is the SOLE place where
// concrete adapters are instantiated together.
package registration

import (
	"fmt"

	"github.com/sophia-ecosystem/runtime-adapters/internal/adapters/outbound/filesystem"
	"github.com/sophia-ecosystem/runtime-adapters/internal/adapters/outbound/git"
	"github.com/sophia-ecosystem/runtime-adapters/internal/adapters/outbound/httpreq"
	"github.com/sophia-ecosystem/runtime-adapters/internal/adapters/outbound/shell"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound"
)

// Config bundles the per-adapter Configs so bootstrap can construct all 4
// adapters with one call. Each sub-config is the adapter's own Config type.
type Config struct {
	Shell      shell.Config
	Git        git.Config
	Filesystem filesystem.Config
	HTTP       httpreq.Config
}

// RegisterAllPhase1 instantiates the 4 Phase 1 adapters, registers each
// adapter's per-capability normalizer with the shared ResultNormalizer,
// and returns a map keyed by AdapterID for ExecuteService consumption.
//
// Returns an error if:
//   - The normalizer is nil.
//   - The clock is nil.
//   - Any adapter constructor rejects its config (allowlist empty,
//     InlineStreamLimit ≤ 0, etc.).
//   - A normalizer registration fails (e.g. the canonical was already
//     registered by an earlier call — this is a programming error and
//     warrants a startup abort).
//
// Caller (wire.go) is responsible for verifying that the resulting
// adapter set covers vo.NewPhase1Capabilities() — the dedicated test
// VerifyCoversPhase1Catalog does this in CI.
func RegisterAllPhase1(
	normalizer *services.ResultNormalizer,
	cfg Config,
	clk shared.Clock,
) (map[valueobjects.AdapterID]outbound.Adapter, error) {
	if normalizer == nil {
		return nil, fmt.Errorf("RegisterAllPhase1: normalizer is required")
	}
	if clk == nil {
		return nil, fmt.Errorf("RegisterAllPhase1: clock is required")
	}

	// Construct adapters.
	shellAdapter, err := shell.NewAdapter(cfg.Shell)
	if err != nil {
		return nil, fmt.Errorf("shell adapter: %w", err)
	}
	gitAdapter, err := git.NewAdapter(cfg.Git, clk)
	if err != nil {
		return nil, fmt.Errorf("git adapter: %w", err)
	}
	fsAdapter, err := filesystem.NewAdapter(cfg.Filesystem, clk)
	if err != nil {
		return nil, fmt.Errorf("filesystem adapter: %w", err)
	}
	httpAdapter, err := httpreq.NewAdapter(cfg.HTTP, clk)
	if err != nil {
		return nil, fmt.Errorf("http adapter: %w", err)
	}

	// Register per-capability normalizers. Each Register call must
	// succeed; duplicates indicate misconfiguration.
	registrations := []struct {
		canonical string
		fn        services.AdapterNormalizer
	}{
		{"shell.exec@v1", shellAdapter.Normalize},
		{"git.status@v1", gitAdapter.NormalizeStatus},
		{"git.clone@v1", gitAdapter.NormalizeClone},
		{"git.diff@v1", gitAdapter.NormalizeDiff},
		{"git.commit@v1", gitAdapter.NormalizeCommit},
		{"filesystem.read_file@v1", fsAdapter.NormalizeRead},
		{"filesystem.write_file@v1", fsAdapter.NormalizeWrite},
		{"http.request@v1", httpAdapter.Normalize},
	}
	for _, r := range registrations {
		if err := normalizer.Register(r.canonical, r.fn); err != nil {
			return nil, fmt.Errorf("register %q: %w", r.canonical, err)
		}
	}

	return map[valueobjects.AdapterID]outbound.Adapter{
		shellAdapter.ID(): shellAdapter,
		gitAdapter.ID():   gitAdapter,
		fsAdapter.ID():    fsAdapter,
		httpAdapter.ID():  httpAdapter,
	}, nil
}

// VerifyCoversPhase1Catalog returns an error if the registered normalizers
// don't cover every canonical from vo.NewPhase1Capabilities. Useful at
// startup or in CI to catch drift between the catalog and wiring.
func VerifyCoversPhase1Catalog(normalizer *services.ResultNormalizer) error {
	caps, err := valueobjects.NewPhase1Capabilities()
	if err != nil {
		return fmt.Errorf("Phase 1 catalog: %w", err)
	}
	registered := map[string]bool{}
	for _, c := range normalizer.Registered() {
		registered[c] = true
	}
	for _, c := range caps {
		if !registered[c.Canonical()] {
			return fmt.Errorf("Phase 1 catalog mismatch: capability %q has no registered normalizer", c.Canonical())
		}
	}
	return nil
}
