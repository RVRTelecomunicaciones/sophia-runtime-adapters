package git

import (
	"context"
	"fmt"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound"
)

// AdapterIDString is the spec-mandated external AdapterID for the
// git adapter (§5.4).
const AdapterIDString = "git"

// Adapter implements outbound.Adapter for the 4 git capabilities
// (status / clone / diff / commit). Dispatching happens by capability
// name; the per-capability implementations live in T35..T38 files
// (status.go, clone.go, diff.go, commit.go) and are wired here as
// methods on Adapter.
type Adapter struct {
	id           valueobjects.AdapterID
	capabilities []valueobjects.Capability
	cfg          Config
	clk          shared.Clock // injected for testability of duration measurement
}

// Compile-time interface satisfaction.
var _ outbound.Adapter = (*Adapter)(nil)

// NewAdapter constructs the adapter. Returns an error if the config is
// invalid (empty allowlist, non-positive InlineStreamLimit, nil Clock).
func NewAdapter(cfg Config, clk shared.Clock) (*Adapter, error) {
	if cfg.InlineStreamLimit <= 0 {
		return nil, fmt.Errorf("git.Config.InlineStreamLimit must be > 0, got %d", cfg.InlineStreamLimit)
	}
	if len(cfg.AllowedWorkingDirs) == 0 {
		return nil, fmt.Errorf("git.Config.AllowedWorkingDirs must be non-empty")
	}
	if clk == nil {
		return nil, fmt.Errorf("git.NewAdapter: Clock is required")
	}
	id, err := valueobjects.NewAdapterID(AdapterIDString)
	if err != nil {
		return nil, fmt.Errorf("construct AdapterID: %w", err)
	}
	mk := func(name string, partial bool, def time.Duration) (valueobjects.Capability, error) {
		c, e := valueobjects.NewCapability(id, name, "v1", partial, def)
		return c, e
	}
	caps := make([]valueobjects.Capability, 0, 4)
	for _, e := range []struct {
		name    string
		partial bool
		def     time.Duration
	}{
		{"status", false, defaultStatusTimeout},
		{"clone", true, defaultCloneTimeout},
		{"diff", false, defaultDiffTimeout},
		{"commit", false, defaultCommitTimeout},
	} {
		c, err := mk(e.name, e.partial, e.def)
		if err != nil {
			return nil, fmt.Errorf("construct %q capability: %w", e.name, err)
		}
		caps = append(caps, c)
	}
	return &Adapter{id: id, capabilities: caps, cfg: cfg, clk: clk}, nil
}

// ID returns the adapter's external identifier.
func (a *Adapter) ID() valueobjects.AdapterID { return a.id }

// Capabilities returns the capability set this adapter supports.
func (a *Adapter) Capabilities() []valueobjects.Capability {
	out := make([]valueobjects.Capability, len(a.capabilities))
	copy(out, a.capabilities)
	return out
}

// Execute dispatches by capability name. Per-capability methods are
// implemented in dedicated files (status.go, clone.go, diff.go,
// commit.go) per T35..T38. T34 scaffolds them as "not implemented"
// stubs returning the per-capability raw with notImpl=true.
func (a *Adapter) Execute(ctx context.Context, cap valueobjects.Capability, payload valueobjects.Payload) (services.AdapterRawOutcome, error) {
	if err := ctx.Err(); err != nil {
		// Honor cancelled ctx by returning the canonical-typed raw with
		// ctxErr set, so the per-capability normalizer can produce a
		// cancelled/timeout result. Use per-cap raws so normalizer can
		// type-assert correctly.
		switch cap.Name() {
		case "status":
			return &statusRaw{ctxErr: err}, nil
		case "clone":
			return &cloneRaw{ctxErr: err}, nil
		case "diff":
			return &diffRaw{ctxErr: err}, nil
		case "commit":
			return &commitRaw{ctxErr: err}, nil
		default:
			return &statusRaw{ctxErr: err}, nil
		}
	}
	switch cap.Name() {
	case "status":
		return a.status(ctx, cap, payload)
	case "clone":
		return a.clone(ctx, cap, payload)
	case "diff":
		return a.diff(ctx, cap, payload)
	case "commit":
		return a.commit(ctx, cap, payload)
	default:
		return &statusRaw{validation: fmt.Sprintf("unknown capability for git adapter: %s", cap.Canonical())}, nil
	}
}
