package entities

import (
	"fmt"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
)

// Timings captures the lifecycle timestamps of an ExecutionReceipt. All
// timestamps are UTC with millisecond precision. Temporal invariants
// (submitted ≤ started ≤ completed; persisted ≥ completed when set) are
// enforced by the builder — direct construction of an inconsistent
// Timings literal is still possible but discouraged; prefer the builder.
type Timings struct {
	SubmittedAt time.Time  `json:"submitted_at"`
	StartedAt   time.Time  `json:"started_at"`
	CompletedAt time.Time  `json:"completed_at"`
	PersistedAt *time.Time `json:"persisted_at,omitempty"`
}

// TimingsBuilder constructs Timings step by step, capturing the current
// time (from the injected Clock) at each Mark* call. Build returns an
// error if the recorded timestamps violate the temporal invariant.
type TimingsBuilder struct {
	clk shared.Clock
	t   Timings
}

// NewTimingsBuilder creates a builder that uses clk.Now() for each Mark.
func NewTimingsBuilder(clk shared.Clock) *TimingsBuilder {
	return &TimingsBuilder{clk: clk}
}

// MarkSubmitted records the submission timestamp.
func (b *TimingsBuilder) MarkSubmitted() *TimingsBuilder {
	b.t.SubmittedAt = b.clk.Now()
	return b
}

// MarkStarted records the start-of-execution timestamp.
func (b *TimingsBuilder) MarkStarted() *TimingsBuilder {
	b.t.StartedAt = b.clk.Now()
	return b
}

// MarkCompleted records the completion timestamp.
func (b *TimingsBuilder) MarkCompleted() *TimingsBuilder {
	b.t.CompletedAt = b.clk.Now()
	return b
}

// MarkPersisted records the persistence timestamp (optional).
func (b *TimingsBuilder) MarkPersisted() *TimingsBuilder {
	now := b.clk.Now()
	b.t.PersistedAt = &now
	return b
}

// Build returns the Timings if the temporal invariant holds. Invariant:
// submitted ≤ started ≤ completed; persisted ≥ completed when set.
// Returns an error with a specific message identifying which edge failed.
func (b *TimingsBuilder) Build() (Timings, error) {
	if b.t.SubmittedAt.IsZero() {
		return Timings{}, fmt.Errorf("timings: submitted_at not set")
	}
	if b.t.StartedAt.IsZero() {
		return Timings{}, fmt.Errorf("timings: started_at not set")
	}
	if b.t.CompletedAt.IsZero() {
		return Timings{}, fmt.Errorf("timings: completed_at not set")
	}
	if b.t.SubmittedAt.After(b.t.StartedAt) {
		return Timings{}, fmt.Errorf("timings invariant: submitted_at (%v) > started_at (%v)", b.t.SubmittedAt, b.t.StartedAt)
	}
	if b.t.StartedAt.After(b.t.CompletedAt) {
		return Timings{}, fmt.Errorf("timings invariant: started_at (%v) > completed_at (%v)", b.t.StartedAt, b.t.CompletedAt)
	}
	if b.t.PersistedAt != nil && b.t.PersistedAt.Before(b.t.CompletedAt) {
		return Timings{}, fmt.Errorf("timings invariant: persisted_at (%v) < completed_at (%v)", b.t.PersistedAt, b.t.CompletedAt)
	}
	return b.t, nil
}
