package services

// structural_test.go — exhaustive unit tests for the pure helpers in
// structural.go. Tests live in the same package (white-box) so they can
// reach unexported types (panicRaw, ctxRaw, ctxKind, ctxOutcomeFor,
// truncateStack, minDuration, mustBuildTimings) directly.

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	domainservices "github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound"
)

// ---------------------------------------------------------------------------
// panicRaw / ctxRaw — interface satisfaction + zero-value
// ---------------------------------------------------------------------------

func TestPanicRaw_SatisfiesAdapterRawOutcome(t *testing.T) {
	// compile-time assertion already exists in structural.go; this test
	// demonstrates it at runtime too.
	var _ domainservices.AdapterRawOutcome = (*panicRaw)(nil)
	// Call the marker method so the coverage tool counts it.
	p := &panicRaw{}
	p.IsAdapterRawOutcome()
}

func TestPanicRaw_ZeroValue(t *testing.T) {
	var p panicRaw
	if p.recoveredValue != nil {
		t.Errorf("zero panicRaw.recoveredValue: want nil, got %v", p.recoveredValue)
	}
	if p.stack != nil {
		t.Errorf("zero panicRaw.stack: want nil, got %v", p.stack)
	}
	if p.structuralErr != nil {
		t.Errorf("zero panicRaw.structuralErr: want nil, got %v", p.structuralErr)
	}
}

func TestCtxRaw_SatisfiesAdapterRawOutcome(t *testing.T) {
	var _ domainservices.AdapterRawOutcome = (*ctxRaw)(nil)
	// Call the marker method so the coverage tool counts it.
	c := &ctxRaw{}
	c.IsAdapterRawOutcome()
}

// ---------------------------------------------------------------------------
// ctxOutcomeFor
// ---------------------------------------------------------------------------

func TestCtxOutcomeFor_DeadlineExceeded_ReturnsCtxKindTimeout(t *testing.T) {
	result := ctxOutcomeFor(context.DeadlineExceeded, nil)
	cr, ok := result.(*ctxRaw)
	if !ok {
		t.Fatalf("expected *ctxRaw, got %T", result)
	}
	if cr.kind != ctxKindTimeout {
		t.Errorf("kind = %v, want ctxKindTimeout", cr.kind)
	}
	if !errors.Is(cr.ctxErr, context.DeadlineExceeded) {
		t.Errorf("ctxErr = %v, want context.DeadlineExceeded", cr.ctxErr)
	}
}

func TestCtxOutcomeFor_Canceled_ReturnsCtxKindCancelled(t *testing.T) {
	result := ctxOutcomeFor(context.Canceled, nil)
	cr, ok := result.(*ctxRaw)
	if !ok {
		t.Fatalf("expected *ctxRaw, got %T", result)
	}
	if cr.kind != ctxKindCancelled {
		t.Errorf("kind = %v, want ctxKindCancelled", cr.kind)
	}
	if !errors.Is(cr.ctxErr, context.Canceled) {
		t.Errorf("ctxErr = %v, want context.Canceled", cr.ctxErr)
	}
}

func TestCtxOutcomeFor_OtherError_ReturnsFallback(t *testing.T) {
	sentinel := &panicRaw{recoveredValue: "sentinel"}
	result := ctxOutcomeFor(errors.New("synthetic"), sentinel)
	if result != sentinel {
		t.Errorf("expected fallback sentinel, got %v", result)
	}
}

func TestCtxOutcomeFor_NilError_ReturnsFallback(t *testing.T) {
	sentinel := &panicRaw{recoveredValue: "sentinel"}
	result := ctxOutcomeFor(nil, sentinel)
	if result != sentinel {
		t.Errorf("expected fallback sentinel for nil err, got %v", result)
	}
}

func TestCtxOutcomeFor_NilError_NilFallback_ReturnsNil(t *testing.T) {
	result := ctxOutcomeFor(nil, nil)
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

// ---------------------------------------------------------------------------
// SafeExecute
// ---------------------------------------------------------------------------

// nopAdapter is a minimal outbound.Adapter for testing SafeExecute directly.
type nopAdapter struct {
	fn func(ctx context.Context, cap valueobjects.Capability, p valueobjects.Payload) (domainservices.AdapterRawOutcome, error)
}

func (a *nopAdapter) ID() valueobjects.AdapterID              { return valueobjects.AdapterID{} }
func (a *nopAdapter) Capabilities() []valueobjects.Capability { return nil }
func (a *nopAdapter) Execute(ctx context.Context, cap valueobjects.Capability, p valueobjects.Payload) (domainservices.AdapterRawOutcome, error) {
	return a.fn(ctx, cap, p)
}

var _ outbound.Adapter = (*nopAdapter)(nil)

// testRaw is a minimal raw outcome used only in SafeExecute tests.
type testRaw struct{}

func (*testRaw) IsAdapterRawOutcome() {}

func TestSafeExecute_HappyPath_ReturnsRawNilErr(t *testing.T) {
	want := &testRaw{}
	a := &nopAdapter{fn: func(_ context.Context, _ valueobjects.Capability, _ valueobjects.Payload) (domainservices.AdapterRawOutcome, error) {
		return want, nil
	}}
	raw, err := SafeExecute(context.Background(), a, valueobjects.Capability{}, valueobjects.Payload{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if raw != want {
		t.Errorf("raw = %v, want %v", raw, want)
	}
}

func TestSafeExecute_AdapterReturnsError_PropagatesError(t *testing.T) {
	sentinel := errors.New("adapter error")
	a := &nopAdapter{fn: func(_ context.Context, _ valueobjects.Capability, _ valueobjects.Payload) (domainservices.AdapterRawOutcome, error) {
		return nil, sentinel
	}}
	raw, err := SafeExecute(context.Background(), a, valueobjects.Capability{}, valueobjects.Payload{})
	if raw != nil {
		t.Errorf("expected nil raw on error, got %v", raw)
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got %v", err)
	}
}

func TestSafeExecute_AdapterPanicsWithString_ReturnsPanicRaw(t *testing.T) {
	a := &nopAdapter{fn: func(_ context.Context, _ valueobjects.Capability, _ valueobjects.Payload) (domainservices.AdapterRawOutcome, error) {
		panic("boom")
	}}
	raw, err := SafeExecute(context.Background(), a, valueobjects.Capability{}, valueobjects.Payload{})
	if err != nil {
		t.Fatalf("expected nil error after panic recovery, got %v", err)
	}
	pr, ok := raw.(*panicRaw)
	if !ok {
		t.Fatalf("expected *panicRaw, got %T", raw)
	}
	if pr.recoveredValue != "boom" {
		t.Errorf("recoveredValue = %v, want \"boom\"", pr.recoveredValue)
	}
	if len(pr.stack) == 0 {
		t.Error("expected non-empty stack in panicRaw")
	}
}

func TestSafeExecute_AdapterPanicsWithError_RecoveredValueIsError(t *testing.T) {
	panicErr := errors.New("panicked error")
	a := &nopAdapter{fn: func(_ context.Context, _ valueobjects.Capability, _ valueobjects.Payload) (domainservices.AdapterRawOutcome, error) {
		panic(panicErr)
	}}
	raw, err := SafeExecute(context.Background(), a, valueobjects.Capability{}, valueobjects.Payload{})
	if err != nil {
		t.Fatalf("expected nil error after panic recovery, got %v", err)
	}
	pr, ok := raw.(*panicRaw)
	if !ok {
		t.Fatalf("expected *panicRaw, got %T", raw)
	}
	if !errors.Is(pr.recoveredValue.(error), panicErr) {
		t.Errorf("recoveredValue = %v, want %v", pr.recoveredValue, panicErr)
	}
	if len(pr.stack) == 0 {
		t.Error("expected non-empty stack in panicRaw")
	}
}

func TestSafeExecute_CancelledCtx_AdapterStillCalled(t *testing.T) {
	// Context cancellation before the call does not prevent the adapter
	// from being invoked — it's the adapter's responsibility to check ctx.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	called := false
	want := &testRaw{}
	a := &nopAdapter{fn: func(ctx context.Context, _ valueobjects.Capability, _ valueobjects.Payload) (domainservices.AdapterRawOutcome, error) {
		called = true
		return want, nil
	}}
	raw, err := SafeExecute(ctx, a, valueobjects.Capability{}, valueobjects.Payload{})
	if !called {
		t.Error("expected adapter to be called even with cancelled ctx")
	}
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if raw != want {
		t.Errorf("raw = %v, want %v", raw, want)
	}
}

// ---------------------------------------------------------------------------
// minDuration
// ---------------------------------------------------------------------------

func TestMinDuration_Empty_ReturnsZero(t *testing.T) {
	if got := minDuration(); got != 0 {
		t.Errorf("minDuration() = %v, want 0", got)
	}
}

func TestMinDuration_SinglePositive_ReturnsThat(t *testing.T) {
	if got := minDuration(5 * time.Second); got != 5*time.Second {
		t.Errorf("minDuration(5s) = %v, want 5s", got)
	}
}

func TestMinDuration_MultiplePositive_ReturnsSmallest(t *testing.T) {
	got := minDuration(5*time.Second, 3*time.Second, 10*time.Second)
	if got != 3*time.Second {
		t.Errorf("got %v, want 3s", got)
	}
}

func TestMinDuration_SkipsZero(t *testing.T) {
	got := minDuration(5*time.Second, 0, 2*time.Second)
	if got != 2*time.Second {
		t.Errorf("got %v, want 2s (zero ignored)", got)
	}
}

func TestMinDuration_SkipsNegative(t *testing.T) {
	got := minDuration(5*time.Second, -1*time.Second, 2*time.Second)
	if got != 2*time.Second {
		t.Errorf("got %v, want 2s (negative ignored)", got)
	}
}

func TestMinDuration_AllZero_ReturnsZero(t *testing.T) {
	got := minDuration(0, 0, 0)
	if got != 0 {
		t.Errorf("got %v, want 0 (all zero)", got)
	}
}

func TestMinDuration_AllNegative_ReturnsZero(t *testing.T) {
	got := minDuration(-1*time.Second, -2*time.Second)
	if got != 0 {
		t.Errorf("got %v, want 0 (all negative)", got)
	}
}

// ---------------------------------------------------------------------------
// truncateStack
// ---------------------------------------------------------------------------

func TestTruncateStack_Nil_ReturnsNil(t *testing.T) {
	result := truncateStack(nil, 100)
	if result != nil {
		t.Errorf("truncateStack(nil, 100) = %v, want nil", result)
	}
}

func TestTruncateStack_ShortInput_ReturnsCopy(t *testing.T) {
	input := []byte("short")
	result := truncateStack(input, 100)
	if !bytes.Equal(result, input) {
		t.Errorf("truncateStack short: got %q, want %q", result, input)
	}
}

func TestTruncateStack_ExactlyAtLimit_ReturnsCopy(t *testing.T) {
	input := []byte("abcd")
	result := truncateStack(input, 4)
	if !bytes.Equal(result, input) {
		t.Errorf("truncateStack at-limit: got %q, want %q", result, input)
	}
}

func TestTruncateStack_LongInput_TruncatesToMax(t *testing.T) {
	input := []byte("long stack trace...")
	result := truncateStack(input, 4)
	if !bytes.Equal(result, []byte("long")) {
		t.Errorf("truncateStack truncated: got %q, want %q", result, "long")
	}
}

func TestTruncateStack_DefensiveCopy_MutatingInputDoesNotChangeOutput(t *testing.T) {
	input := []byte("hello")
	result := truncateStack(input, 100)
	// Mutate the original.
	input[0] = 'X'
	if result[0] == 'X' {
		t.Error("truncateStack output shares memory with input (not a defensive copy)")
	}
}

func TestTruncateStack_LargeInput_ReturnsExactlyMaxBytes(t *testing.T) {
	big := make([]byte, 10*1024) // 10 KB
	for i := range big {
		big[i] = byte(i % 256)
	}
	result := truncateStack(big, 4096)
	if len(result) != 4096 {
		t.Errorf("len(result) = %d, want 4096", len(result))
	}
	// Verify content matches the first 4096 bytes of input.
	if !bytes.Equal(result, big[:4096]) {
		t.Error("truncated content does not match first 4096 bytes of input")
	}
}

func TestTruncateStack_DefensiveCopy_MutatingOutputDoesNotChangeInput(t *testing.T) {
	input := []byte("world")
	result := truncateStack(input, 100)
	result[0] = 'Z'
	if input[0] == 'Z' {
		t.Error("mutating truncateStack output changed input (not a defensive copy)")
	}
}

// ---------------------------------------------------------------------------
// mustBuildTimings
// ---------------------------------------------------------------------------

func TestMustBuildTimings_HappyPath_AllUTC(t *testing.T) {
	loc := time.FixedZone("UTC+5", 5*60*60)
	submitted := time.Date(2026, 1, 1, 10, 0, 0, 0, loc) // not UTC
	started := time.Date(2026, 1, 1, 10, 0, 1, 0, loc)
	completed := time.Date(2026, 1, 1, 10, 0, 2, 0, loc)

	timings := mustBuildTimings(submitted, started, completed, nil)

	if timings.SubmittedAt.Location() != time.UTC {
		t.Errorf("SubmittedAt not UTC: %v", timings.SubmittedAt.Location())
	}
	if timings.StartedAt.Location() != time.UTC {
		t.Errorf("StartedAt not UTC: %v", timings.StartedAt.Location())
	}
	if timings.CompletedAt.Location() != time.UTC {
		t.Errorf("CompletedAt not UTC: %v", timings.CompletedAt.Location())
	}
	if timings.PersistedAt != nil {
		t.Errorf("PersistedAt: want nil, got %v", timings.PersistedAt)
	}

	// Values must be the UTC equivalents of the inputs.
	if !timings.SubmittedAt.Equal(submitted.UTC()) {
		t.Errorf("SubmittedAt = %v, want %v", timings.SubmittedAt, submitted.UTC())
	}
}

func TestMustBuildTimings_WithPersisted_PersistedAtIsUTCCopy(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	persistedSrc := now.Add(10 * time.Millisecond)

	timings := mustBuildTimings(now, now, now, &persistedSrc)

	if timings.PersistedAt == nil {
		t.Fatal("PersistedAt is nil, want non-nil")
	}
	if !timings.PersistedAt.Equal(persistedSrc.UTC()) {
		t.Errorf("PersistedAt = %v, want %v", *timings.PersistedAt, persistedSrc.UTC())
	}
	if timings.PersistedAt.Location() != time.UTC {
		t.Errorf("PersistedAt not UTC: %v", timings.PersistedAt.Location())
	}
	// Must be a copy — mutating persistedSrc should not change the stored pointer.
	orig := persistedSrc
	persistedSrc = persistedSrc.Add(time.Hour)
	if !timings.PersistedAt.Equal(orig.UTC()) {
		t.Error("PersistedAt aliased the caller's variable — not a copy")
	}
}

func TestMustBuildTimings_OutOfOrderInputs_NoError(t *testing.T) {
	// mustBuildTimings does NOT enforce monotonicity. Passing out-of-order
	// times is caller's responsibility. This test documents the current
	// (intentional) behavior: the helper just stores what it is given.
	future := time.Date(2026, 1, 1, 12, 1, 0, 0, time.UTC)
	past := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	// submitted > completed intentionally.
	timings := mustBuildTimings(future, past, past, nil)
	if !timings.SubmittedAt.Equal(future.UTC()) {
		t.Errorf("SubmittedAt = %v, want %v", timings.SubmittedAt, future)
	}
}
