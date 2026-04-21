package git

import (
	"context"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
)

// diff implements git.diff@v1 — see T37 for the real implementation.
func (a *Adapter) diff(_ context.Context, _ valueobjects.Capability, _ valueobjects.Payload) (services.AdapterRawOutcome, error) {
	return &diffRaw{notImpl: true}, nil
}
