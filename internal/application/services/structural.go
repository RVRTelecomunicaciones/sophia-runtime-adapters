package services

// structural.go — pure helpers extracted from execute_service.go so each
// can be unit-tested in isolation. None of these functions depend on
// ExecuteService fields; they are free functions or free types.

import (
	"context"
	"errors"
	"runtime/debug"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
	domainservices "github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound"
)

// ---------------------------------------------------------------------------
// Internal raw-outcome types (application-layer only; not registered with
// ResultNormalizer — dispatched directly by the Execute switch, Option A).
// ---------------------------------------------------------------------------

// panicRaw implements domainservices.AdapterRawOutcome and carries a recovered
// panic value plus truncated stack for diagnostics.
type panicRaw struct {
	recoveredValue any
	stack          []byte
	structuralErr  error
}

func (*panicRaw) IsAdapterRawOutcome() {}

// Compile-time: panicRaw satisfies AdapterRawOutcome.
var _ domainservices.AdapterRawOutcome = (*panicRaw)(nil)

type ctxKind int

const (
	ctxKindTimeout   ctxKind = iota
	ctxKindCancelled ctxKind = iota
)

// ctxRaw implements domainservices.AdapterRawOutcome and carries a context
// error with a pre-classified kind (timeout vs cancelled).
type ctxRaw struct {
	kind   ctxKind
	ctxErr error
}

func (*ctxRaw) IsAdapterRawOutcome() {}

// Compile-time: ctxRaw satisfies AdapterRawOutcome.
var _ domainservices.AdapterRawOutcome = (*ctxRaw)(nil)

// ---------------------------------------------------------------------------
// ctxOutcomeFor
// ---------------------------------------------------------------------------

// ctxOutcomeFor translates a context error into an AdapterRawOutcome that
// carries the right ErrorClass. Overrides the adapter's raw when the
// caller's ctx is the source of truth.
//
// Returns a *ctxRaw for context.DeadlineExceeded or context.Canceled.
// For any other error (or a nil error) it returns fallback unchanged.
func ctxOutcomeFor(ctxErr error, fallback domainservices.AdapterRawOutcome) domainservices.AdapterRawOutcome {
	switch {
	case errors.Is(ctxErr, context.DeadlineExceeded):
		return &ctxRaw{kind: ctxKindTimeout, ctxErr: ctxErr}
	case errors.Is(ctxErr, context.Canceled):
		return &ctxRaw{kind: ctxKindCancelled, ctxErr: ctxErr}
	}
	return fallback
}

// ---------------------------------------------------------------------------
// SafeExecute
// ---------------------------------------------------------------------------

// SafeExecute wraps adapter.Execute with panic recovery. On panic it returns
// a (*panicRaw, nil) so the caller can build a structured failure receipt.
// On a structural Go error it returns (nil, err). On success returns the raw
// outcome and a nil error.
//
// It is a free function (not a method on ExecuteService) so it can be tested
// in isolation without wiring a full service.
func SafeExecute(
	ctx context.Context,
	a outbound.Adapter,
	cap valueobjects.Capability,
	p valueobjects.Payload,
) (raw domainservices.AdapterRawOutcome, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			raw = &panicRaw{
				recoveredValue: rec,
				stack:          truncateStack(debug.Stack(), 4096),
			}
			err = nil
		}
	}()
	return a.Execute(ctx, cap, p)
}

// ---------------------------------------------------------------------------
// mustBuildTimings
// ---------------------------------------------------------------------------

// mustBuildTimings constructs a Timings value directly using exact timestamps
// from the flow rather than "now" at each mark point. All times are
// normalized to UTC.
//
// NOTE: This helper does NOT enforce the temporal monotonicity invariant
// (submitted ≤ started ≤ completed). That invariant is enforced by
// TimingsBuilder.Build(). All callers in ExecuteService pass already-ordered
// times from the clock, so out-of-order inputs are not expected. If a caller
// passes out-of-order times the resulting Timings struct will be technically
// inconsistent but mustBuildTimings will still return it — the inconsistency
// will surface later if the Timings are validated by other code.
func mustBuildTimings(submitted, started, completed time.Time, persisted *time.Time) entities.Timings {
	t := entities.Timings{
		SubmittedAt: submitted.UTC(),
		StartedAt:   started.UTC(),
		CompletedAt: completed.UTC(),
	}
	if persisted != nil {
		utc := persisted.UTC()
		t.PersistedAt = &utc
	}
	return t
}

// ---------------------------------------------------------------------------
// truncateStack
// ---------------------------------------------------------------------------

// truncateStack returns a defensive copy of b limited to max bytes.
// Returns nil when b is nil. Always returns a new slice independent of b so
// caller mutations to b cannot affect the returned value.
func truncateStack(b []byte, max int) []byte {
	if b == nil {
		return nil
	}
	n := len(b)
	if n > max {
		n = max
	}
	cp := make([]byte, n)
	copy(cp, b[:n])
	return cp
}

// ---------------------------------------------------------------------------
// minDuration
// ---------------------------------------------------------------------------

// minDuration returns the minimum of non-zero durations. Zero or negative
// values are treated as "no cap from this source" and ignored. Returns 0
// when all inputs are ≤ 0 (or no inputs are supplied).
func minDuration(ds ...time.Duration) time.Duration {
	var min time.Duration
	for _, d := range ds {
		if d <= 0 {
			continue
		}
		if min == 0 || d < min {
			min = d
		}
	}
	return min
}
