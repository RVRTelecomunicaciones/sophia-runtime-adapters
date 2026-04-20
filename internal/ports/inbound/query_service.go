package inbound

import (
	"context"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
)

// CapabilityFilter optionally narrows ListCapabilities to a single
// adapter. Zero value (nil AdapterID) returns the full catalog.
type CapabilityFilter struct {
	AdapterID *valueobjects.AdapterID
}

// ListCapabilitiesResponse is returned by QueryService.ListCapabilities.
// The fields are emitted as snake_case on wire (inbound HTTP handler
// is responsible for marshaling). schema_version is stamped "v1" per
// D4.11 / I20.
type ListCapabilitiesResponse struct {
	Capabilities   []valueobjects.Capability `json:"capabilities"`
	RuntimeVersion string                    `json:"runtime_version"`
	SchemaVersion  string                    `json:"schema_version"`
}

// GetReceiptOptions tunes GetReceipt. IncludeStreams=false strips
// stdout_ref/stderr_ref from the returned receipt (UC3 / §6.4 /
// lightweight audit fetches). Default (zero value) is IncludeStreams=false;
// inbound HTTP handlers flip it to true unless the client explicitly
// opts out via the include_streams query parameter.
//
// Note: the DEFAULT depends on the inbound layer's interpretation. The
// domain/application contract simply honors whatever flag it receives.
type GetReceiptOptions struct {
	IncludeStreams bool
}

// QueryService is the read-side inbound port — catalog introspection
// and receipt retrieval (UC2, UC3). No side effects; safe for
// concurrent use.
type QueryService interface {
	// ListCapabilities returns the full or filtered capability catalog.
	// Never errors in Phase 1 (registry is in-memory, built at bootstrap).
	// Phase 2 may introduce transient errors (registry reloads, etc.);
	// the interface returns error for forward compatibility.
	ListCapabilities(ctx context.Context, filter CapabilityFilter) (ListCapabilitiesResponse, error)

	// GetReceipt retrieves a persisted receipt by ID. Returns the
	// underlying ReceiptRepository's error (wrapped ErrReceiptNotFound
	// for misses) and honors GetReceiptOptions.IncludeStreams.
	GetReceipt(ctx context.Context, id shared.ReceiptID, opts GetReceiptOptions) (entities.ExecutionReceipt, error)
}
