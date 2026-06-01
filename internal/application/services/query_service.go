package services

import (
	"context"
	"fmt"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/inbound"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound"
)

// QueryServiceImpl implements inbound.QueryService — the read-side
// operations (UC2 ListCapabilities, UC3 GetReceipt). No side effects;
// safe for concurrent use.
type QueryServiceImpl struct {
	registry       *valueobjects.CapabilityRegistry
	receipts       outbound.ReceiptRepository
	runtimeVersion string
}

// QueryServiceConfig bundles the constructor arguments.
type QueryServiceConfig struct {
	Registry       *valueobjects.CapabilityRegistry
	Receipts       outbound.ReceiptRepository
	RuntimeVersion string
}

// NewQueryService validates config and returns a ready service.
func NewQueryService(cfg QueryServiceConfig) (*QueryServiceImpl, error) {
	if cfg.Registry == nil {
		return nil, fmt.Errorf("registry is required")
	}
	if cfg.Receipts == nil {
		return nil, fmt.Errorf("receipts is required")
	}
	if cfg.RuntimeVersion == "" {
		return nil, fmt.Errorf("RuntimeVersion is required")
	}
	return &QueryServiceImpl{
		registry:       cfg.Registry,
		receipts:       cfg.Receipts,
		runtimeVersion: cfg.RuntimeVersion,
	}, nil
}

// ListCapabilities returns the full or filtered catalog.
// Phase 1 never errors — the registry is in-memory, constructed once
// at bootstrap. ctx is honored for cancellation only.
func (q *QueryServiceImpl) ListCapabilities(ctx context.Context, filter inbound.CapabilityFilter) (inbound.ListCapabilitiesResponse, error) {
	if err := ctx.Err(); err != nil {
		return inbound.ListCapabilitiesResponse{}, err
	}
	var caps []valueobjects.Capability
	if filter.AdapterID != nil {
		caps = q.registry.FilterByAdapter(*filter.AdapterID)
	} else {
		caps = q.registry.List()
	}
	return inbound.ListCapabilitiesResponse{
		Capabilities:   caps,
		RuntimeVersion: q.runtimeVersion,
		SchemaVersion:  "v1",
	}, nil
}

// GetReceipt retrieves a receipt by ID. When opts.IncludeStreams is
// false, stdout_ref and stderr_ref are stripped via
// ExecutionReceipt.StripStreams. Returns the underlying repository
// error on miss (wrapped ErrReceiptNotFound) or transient I/O.
func (q *QueryServiceImpl) GetReceipt(ctx context.Context, id shared.ReceiptID, opts inbound.GetReceiptOptions) (entities.ExecutionReceipt, error) {
	if err := ctx.Err(); err != nil {
		return entities.ExecutionReceipt{}, err
	}
	r, err := q.receipts.FindByID(ctx, id)
	if err != nil {
		return entities.ExecutionReceipt{}, err
	}
	if !opts.IncludeStreams {
		r = r.StripStreams()
	}
	return r, nil
}

// Compile-time interface satisfaction.
var _ inbound.QueryService = (*QueryServiceImpl)(nil)
