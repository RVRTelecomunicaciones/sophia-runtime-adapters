// Package inbound defines the inbound port interfaces — the entry
// points into the runtime from HTTP handlers, the in-process Go SDK,
// or any future transport. Concrete implementations live under
// internal/application/services/ (Bundle 3 later tasks).
package inbound

import (
	"context"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
)

// RuntimeService is the primary inbound port — a single Execute method
// that accepts a typed ExecutionRequest and returns an ExecutionReceipt
// (A7.1 — receipt is the central artifact; returning result separately
// would duplicate). Errors are reserved for STRUCTURAL failures
// (concurrency limit exceeded, persistence failure); business outcomes
// (success / failure / timeout / cancelled / partial) all travel inside
// the returned ExecutionReceipt.
type RuntimeService interface {
	Execute(ctx context.Context, req entities.ExecutionRequest) (entities.ExecutionReceipt, error)
}
