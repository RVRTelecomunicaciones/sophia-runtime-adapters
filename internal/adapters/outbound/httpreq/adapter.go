package httpreq

import (
	"context"
	"fmt"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound"
)

// AdapterIDString is "http" — the EXTERNAL AdapterID (Gap-1: spec
// §5.4 mandates "http"; the Go package name "httpreq" is internal).
const AdapterIDString = "http"

// Adapter implements outbound.Adapter for the http.request@v1 capability.
type Adapter struct {
	id           valueobjects.AdapterID
	capabilities []valueobjects.Capability
	cfg          Config
	clk          shared.Clock
}

// Compile-time interface satisfaction.
var _ outbound.Adapter = (*Adapter)(nil)

// NewAdapter constructs the HTTP adapter. Returns an error if config is
// invalid (non-positive InlineStreamLimit, nil Clock).
func NewAdapter(cfg Config, clk shared.Clock) (*Adapter, error) {
	if cfg.InlineStreamLimit <= 0 {
		return nil, fmt.Errorf("httpreq.Config.InlineStreamLimit must be > 0")
	}
	if clk == nil {
		return nil, fmt.Errorf("httpreq.NewAdapter: Clock is required")
	}
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = defaultMaxBodyBytes
	}
	if cfg.MaxRedirects <= 0 {
		cfg.MaxRedirects = defaultMaxRedirects
	}
	id, err := valueobjects.NewAdapterID(AdapterIDString)
	if err != nil {
		return nil, fmt.Errorf("AdapterID: %w", err)
	}
	cap, err := valueobjects.NewCapability(id, "request", "v1", false, 15*time.Second)
	if err != nil {
		return nil, fmt.Errorf("http.request@v1 capability: %w", err)
	}
	return &Adapter{
		id:           id,
		capabilities: []valueobjects.Capability{cap},
		cfg:          cfg,
		clk:          clk,
	}, nil
}

// ID returns the adapter's external identifier ("http").
func (a *Adapter) ID() valueobjects.AdapterID { return a.id }

// Capabilities returns the capability set this adapter supports.
func (a *Adapter) Capabilities() []valueobjects.Capability {
	out := make([]valueobjects.Capability, len(a.capabilities))
	copy(out, a.capabilities)
	return out
}

// Execute dispatches by capability canonical. Returns (raw, nil) for all
// business-level failures (D7.4); structural errors return (nil, err).
func (a *Adapter) Execute(ctx context.Context, cap valueobjects.Capability, payload valueobjects.Payload) (services.AdapterRawOutcome, error) {
	if err := ctx.Err(); err != nil {
		return &httpRaw{ctxErr: err}, nil
	}
	if cap.Canonical() != "http.request@v1" {
		return &httpRaw{validation: fmt.Sprintf("unknown capability: %s", cap.Canonical())}, nil
	}
	return a.request(ctx, payload)
}
