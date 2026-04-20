package inbound_test

import (
	"context"
	"testing"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/inbound"
)

type stubRuntime struct{}

func (stubRuntime) Execute(ctx context.Context, req entities.ExecutionRequest) (entities.ExecutionReceipt, error) {
	return entities.ExecutionReceipt{}, nil
}

func TestRuntimeServiceInterface(t *testing.T) {
	var _ inbound.RuntimeService = stubRuntime{}
}
