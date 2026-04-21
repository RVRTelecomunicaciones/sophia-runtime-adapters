package git

import (
	"context"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
)

// commit implements git.commit@v1 — see T38 for the real implementation.
func (a *Adapter) commit(_ context.Context, _ valueobjects.Capability, _ valueobjects.Payload) (services.AdapterRawOutcome, error) {
	return &commitRaw{notImpl: true}, nil
}
