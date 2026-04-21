package git

import (
	"context"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
)

// clone implements git.clone@v1 — see T36 for the real implementation.
func (a *Adapter) clone(_ context.Context, _ valueobjects.Capability, _ valueobjects.Payload) (services.AdapterRawOutcome, error) {
	return &cloneRaw{notImpl: true}, nil
}
