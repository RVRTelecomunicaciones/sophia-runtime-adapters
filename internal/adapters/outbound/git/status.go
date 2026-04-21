package git

import (
	"context"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
)

// status implements git.status@v1 — see T35 for the real implementation.
func (a *Adapter) status(_ context.Context, _ valueobjects.Capability, _ valueobjects.Payload) (services.AdapterRawOutcome, error) {
	return &statusRaw{notImpl: true}, nil
}
