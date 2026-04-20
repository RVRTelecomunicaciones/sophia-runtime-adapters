package entities

import "github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"

// PartialClassification encodes the four signals used to classify a
// multi-step outcome per spec §4.5 / D4.6 / I6. An outcome is `partial`
// if and only if ALL of:
//   - AllowsPartial: the capability declared multi-step semantics.
//   - HasArtifacts: at least one durable side effect was applied.
//   - HasUncompletedStep: at least one declared step did not complete.
//   - !Reverted: the adapter could not roll back the completed steps.
//
// Otherwise Classify returns `success` (when nothing was left
// incomplete) or `failure` (everything else).
type PartialClassification struct {
	AllowsPartial      bool
	HasArtifacts       bool
	HasUncompletedStep bool
	Reverted           bool
}

// Classify returns the ExecutionStatus (success, failure, or partial)
// implied by the four signals. Callers remain responsible for choosing
// between `failure`, `timeout`, and `cancelled` when a context-driven
// outcome is in play — this helper covers only the §4.5 rule.
func (p PartialClassification) Classify() valueobjects.ExecutionStatus {
	if !p.AllowsPartial {
		return valueobjects.StatusFailure
	}
	if !p.HasUncompletedStep {
		return valueobjects.StatusSuccess
	}
	if !p.HasArtifacts {
		return valueobjects.StatusFailure
	}
	if p.Reverted {
		return valueobjects.StatusFailure
	}
	return valueobjects.StatusPartial
}
