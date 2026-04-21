// Package sdk exposes an in-process Go client for the runtime. It is a
// thin wrapper around the inbound RuntimeService + QueryService
// interfaces — no I/O, no business logic. Use this when the runtime is
// co-located with the caller (e.g. governance during the Phase 1
// pilot); for network calls from a separate process, implement an HTTP
// client against the spec §6.1 endpoints.
//
// SDK input types (ExecuteInput, ListCapabilitiesFilter, GetReceiptOptions)
// are plain Go structs. They are converted to domain value objects via
// the NewXxx constructors inside each method; validation errors surface
// as Go errors on the caller's side.
package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/inbound"
)

// Client is the in-proc SDK handle. Construct via NewClient.
type Client struct {
	runtime inbound.RuntimeService
	query   inbound.QueryService
	clock   shared.Clock
	maxTB   time.Duration // max timeout budget — defaults to 30 min if zero
}

// ClientConfig bundles constructor options. All fields optional; zero
// values resolve to sane defaults.
type ClientConfig struct {
	Clock            shared.Clock
	MaxTimeoutBudget time.Duration
	MaxPayloadBytes  int
}

// NewClient returns an SDK client wrapping the given inbound services.
// svc and query must be non-nil.
func NewClient(svc inbound.RuntimeService, query inbound.QueryService, cfg ClientConfig) (*Client, error) {
	if svc == nil {
		return nil, fmt.Errorf("sdk.NewClient: RuntimeService is required")
	}
	if query == nil {
		return nil, fmt.Errorf("sdk.NewClient: QueryService is required")
	}
	clk := cfg.Clock
	if clk == nil {
		clk = shared.RealClock{}
	}
	maxTB := cfg.MaxTimeoutBudget
	if maxTB == 0 {
		maxTB = 30 * time.Minute
	}
	return &Client{runtime: svc, query: query, clock: clk, maxTB: maxTB}, nil
}

// ExecuteInput is the plain-struct input for Execute. All fields mirror
// spec §4.1 for the request envelope. ContentType defaults to
// "application/json" if empty.
type ExecuteInput struct {
	CorrelationID     string
	AdapterID         string
	CapabilityName    string
	CapabilityVersion string
	ContentType       string          // defaults to "application/json"
	Payload           json.RawMessage
	TimeoutBudgetMs   int64
	IdempotencyKey    string // optional
	ActorID           string // optional
	TaskID            string // optional
	WorkflowRunID     string // optional
	RetryAttempt      int    // optional
}

// Execute dispatches an ExecutionRequest built from in. Validation
// errors (invalid IDs, bad capability name, etc.) are returned as Go
// errors; business outcomes (timeout, failure, cancel, partial) are
// carried inside the returned ExecutionReceipt.
func (c *Client) Execute(ctx context.Context, in ExecuteInput) (entities.ExecutionReceipt, error) {
	cid, err := shared.NewCorrelationID(in.CorrelationID)
	if err != nil {
		return entities.ExecutionReceipt{}, fmt.Errorf("correlation_id: %w", err)
	}
	aid, err := valueobjects.NewAdapterID(in.AdapterID)
	if err != nil {
		return entities.ExecutionReceipt{}, fmt.Errorf("adapter_id: %w", err)
	}
	ct := in.ContentType
	if ct == "" {
		ct = valueobjects.ContentTypeJSON
	}
	maxPayload := 1 << 20 // 1 MiB matching HTTP handler default
	pl, err := valueobjects.NewPayload(ct, in.Payload, maxPayload)
	if err != nil {
		return entities.ExecutionReceipt{}, fmt.Errorf("payload: %w", err)
	}
	tb, err := valueobjects.NewTimeoutBudget(in.TimeoutBudgetMs, c.maxTB)
	if err != nil {
		return entities.ExecutionReceipt{}, fmt.Errorf("timeout_budget: %w", err)
	}
	reqIn := entities.ExecutionRequestInput{
		CorrelationID:     cid,
		AdapterID:         aid,
		CapabilityName:    in.CapabilityName,
		CapabilityVersion: in.CapabilityVersion,
		Payload:           pl,
		TimeoutBudget:     tb,
		ActorID:           in.ActorID,
		TaskID:            in.TaskID,
		WorkflowRunID:     in.WorkflowRunID,
		RetryAttempt:      in.RetryAttempt,
	}
	if in.IdempotencyKey != "" {
		ik, err := shared.NewIdempotencyKey(in.IdempotencyKey)
		if err != nil {
			return entities.ExecutionReceipt{}, fmt.Errorf("idempotency_key: %w", err)
		}
		reqIn.IdempotencyKey = &ik
	}
	req, err := entities.NewExecutionRequest(reqIn, c.clock)
	if err != nil {
		return entities.ExecutionReceipt{}, fmt.Errorf("build request: %w", err)
	}
	return c.runtime.Execute(ctx, req)
}

// ListCapabilitiesFilter mirrors inbound.CapabilityFilter with a plain
// string for ergonomics. Empty AdapterID means "no filter".
type ListCapabilitiesFilter struct {
	AdapterID string
}

// ListCapabilities returns the catalog, optionally filtered by adapter_id.
func (c *Client) ListCapabilities(ctx context.Context, f ListCapabilitiesFilter) (inbound.ListCapabilitiesResponse, error) {
	var filter inbound.CapabilityFilter
	if f.AdapterID != "" {
		aid, err := valueobjects.NewAdapterID(f.AdapterID)
		if err != nil {
			return inbound.ListCapabilitiesResponse{}, fmt.Errorf("adapter_id: %w", err)
		}
		filter.AdapterID = &aid
	}
	return c.query.ListCapabilities(ctx, filter)
}

// GetReceiptOptions mirrors inbound.GetReceiptOptions.
type GetReceiptOptions struct {
	IncludeStreams bool
}

// GetReceipt retrieves a receipt by ID string. Default IncludeStreams
// is true (spec §6.4); callers must explicitly pass a GetReceiptOptions
// with IncludeStreams=false to strip.
func (c *Client) GetReceipt(ctx context.Context, id string, opts GetReceiptOptions) (entities.ExecutionReceipt, error) {
	rid, err := shared.NewReceiptID(id)
	if err != nil {
		return entities.ExecutionReceipt{}, fmt.Errorf("receipt_id: %w", err)
	}
	return c.query.GetReceipt(ctx, rid, inbound.GetReceiptOptions{IncludeStreams: opts.IncludeStreams})
}
