package filesystem

import (
	"context"
	"fmt"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound"
)

// AdapterIDString is the spec-mandated external AdapterID for the
// filesystem adapter (§8.4).
const AdapterIDString = "filesystem"

// Adapter implements outbound.Adapter for the 2 filesystem capabilities
// (read_file / write_file). Dispatching happens by capability name; the
// per-capability implementations live in read.go and write.go.
type Adapter struct {
	id           valueobjects.AdapterID
	capabilities []valueobjects.Capability
	cfg          Config
	clk          shared.Clock
}

// Compile-time interface satisfaction.
var _ outbound.Adapter = (*Adapter)(nil)

// NewAdapter constructs the adapter. Returns an error if the config is
// invalid (empty allowlist, non-positive InlineStreamLimit, nil Clock).
func NewAdapter(cfg Config, clk shared.Clock) (*Adapter, error) {
	if cfg.InlineStreamLimit <= 0 {
		return nil, fmt.Errorf("filesystem.Config.InlineStreamLimit must be > 0")
	}
	if len(cfg.AllowedFilesystemRoots) == 0 {
		return nil, fmt.Errorf("filesystem.Config.AllowedFilesystemRoots must be non-empty")
	}
	if clk == nil {
		return nil, fmt.Errorf("filesystem.NewAdapter: Clock is required")
	}
	id, err := valueobjects.NewAdapterID(AdapterIDString)
	if err != nil {
		return nil, fmt.Errorf("AdapterID: %w", err)
	}
	readCap, err := valueobjects.NewCapability(id, "read_file", "v1", false, defaultReadTimeout)
	if err != nil {
		return nil, fmt.Errorf("read_file capability: %w", err)
	}
	writeCap, err := valueobjects.NewCapability(id, "write_file", "v1", false, defaultWriteTimeout)
	if err != nil {
		return nil, fmt.Errorf("write_file capability: %w", err)
	}
	return &Adapter{
		id:           id,
		capabilities: []valueobjects.Capability{readCap, writeCap},
		cfg:          cfg,
		clk:          clk,
	}, nil
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
// implemented in dedicated files (read.go, write.go).
func (a *Adapter) Execute(ctx context.Context, cap valueobjects.Capability, payload valueobjects.Payload) (services.AdapterRawOutcome, error) {
	if err := ctx.Err(); err != nil {
		switch cap.Name() {
		case "read_file":
			return &readRaw{ctxErr: err}, nil
		case "write_file":
			return &writeRaw{ctxErr: err}, nil
		default:
			return &readRaw{ctxErr: err}, nil
		}
	}
	switch cap.Name() {
	case "read_file":
		return a.readFile(ctx, payload)
	case "write_file":
		return a.writeFile(ctx, payload)
	default:
		return &readRaw{validation: fmt.Sprintf("unknown capability for filesystem adapter: %s", cap.Canonical())}, nil
	}
}
