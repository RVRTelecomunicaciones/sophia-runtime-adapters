package entities_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
)

// newFakeClock returns a FakeClock seeded at a deterministic base time (UTC).
func newFakeClock() *shared.FakeClock {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return &shared.FakeClock{T: base}
}

// ---------------------------------------------------------------------------
// Happy path
// ---------------------------------------------------------------------------

func TestTimingsBuilder_HappyPath_AllFourFields(t *testing.T) {
	clk := newFakeClock()

	submitted := clk.T
	bld := entities.NewTimingsBuilder(clk).MarkSubmitted()

	clk.Advance(10 * time.Millisecond)
	started := clk.T
	bld.MarkStarted()

	clk.Advance(50 * time.Millisecond)
	completed := clk.T
	bld.MarkCompleted()

	clk.Advance(5 * time.Millisecond)
	persisted := clk.T
	bld.MarkPersisted()

	tim, err := bld.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !tim.SubmittedAt.Equal(submitted) {
		t.Errorf("submitted_at: got %v, want %v", tim.SubmittedAt, submitted)
	}
	if !tim.StartedAt.Equal(started) {
		t.Errorf("started_at: got %v, want %v", tim.StartedAt, started)
	}
	if !tim.CompletedAt.Equal(completed) {
		t.Errorf("completed_at: got %v, want %v", tim.CompletedAt, completed)
	}
	if tim.PersistedAt == nil {
		t.Fatal("expected persisted_at to be set, got nil")
	}
	if !tim.PersistedAt.Equal(persisted) {
		t.Errorf("persisted_at: got %v, want %v", tim.PersistedAt, persisted)
	}
}

func TestTimingsBuilder_ChainSyntax(t *testing.T) {
	clk := newFakeClock()
	clk.Advance(10 * time.Millisecond) // started > submitted by construction

	// Chain: submit → advance → start → advance → complete
	clk2 := newFakeClock()
	tim, err := func() (entities.Timings, error) {
		b := entities.NewTimingsBuilder(clk2)
		b.MarkSubmitted()
		clk2.Advance(10 * time.Millisecond)
		b.MarkStarted()
		clk2.Advance(50 * time.Millisecond)
		b.MarkCompleted()
		return b.Build()
	}()
	if err != nil {
		t.Fatalf("chain build failed: %v", err)
	}
	if tim.PersistedAt != nil {
		t.Error("expected nil persisted_at when MarkPersisted not called")
	}
}

func TestTimingsBuilder_WithoutPersisted_PersistedAtNil(t *testing.T) {
	clk := newFakeClock()
	bld := entities.NewTimingsBuilder(clk)
	bld.MarkSubmitted()
	clk.Advance(5 * time.Millisecond)
	bld.MarkStarted()
	clk.Advance(5 * time.Millisecond)
	bld.MarkCompleted()

	tim, err := bld.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tim.PersistedAt != nil {
		t.Errorf("expected PersistedAt nil, got %v", tim.PersistedAt)
	}
}

// ---------------------------------------------------------------------------
// Missing timestamp errors
// ---------------------------------------------------------------------------

func TestTimingsBuilder_MissingSubmitted(t *testing.T) {
	clk := newFakeClock()
	bld := entities.NewTimingsBuilder(clk)
	// deliberately skip MarkSubmitted
	clk.Advance(5 * time.Millisecond)
	bld.MarkStarted()
	clk.Advance(5 * time.Millisecond)
	bld.MarkCompleted()

	_, err := bld.Build()
	if err == nil {
		t.Fatal("expected error for missing submitted_at, got nil")
	}
	if !strings.Contains(err.Error(), "submitted_at") {
		t.Errorf("expected error mentioning submitted_at, got: %v", err)
	}
}

func TestTimingsBuilder_MissingStarted(t *testing.T) {
	clk := newFakeClock()
	bld := entities.NewTimingsBuilder(clk)
	bld.MarkSubmitted()
	// deliberately skip MarkStarted
	clk.Advance(10 * time.Millisecond)
	bld.MarkCompleted()

	_, err := bld.Build()
	if err == nil {
		t.Fatal("expected error for missing started_at, got nil")
	}
	if !strings.Contains(err.Error(), "started_at") {
		t.Errorf("expected error mentioning started_at, got: %v", err)
	}
}

func TestTimingsBuilder_MissingCompleted(t *testing.T) {
	clk := newFakeClock()
	bld := entities.NewTimingsBuilder(clk)
	bld.MarkSubmitted()
	clk.Advance(10 * time.Millisecond)
	bld.MarkStarted()
	// deliberately skip MarkCompleted

	_, err := bld.Build()
	if err == nil {
		t.Fatal("expected error for missing completed_at, got nil")
	}
	if !strings.Contains(err.Error(), "completed_at") {
		t.Errorf("expected error mentioning completed_at, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Temporal invariant violations (clock goes backwards between Mark* calls)
// ---------------------------------------------------------------------------

func TestTimingsBuilder_SubmittedAfterStarted_ViolatesInvariant(t *testing.T) {
	clk := newFakeClock()
	bld := entities.NewTimingsBuilder(clk)

	// Advance clock so submitted is at t+10ms
	clk.Advance(10 * time.Millisecond)
	bld.MarkSubmitted()

	// Wind clock back so started is at t+0 (before submitted)
	clk.T = clk.T.Add(-10 * time.Millisecond)
	bld.MarkStarted()

	clk.Advance(50 * time.Millisecond)
	bld.MarkCompleted()

	_, err := bld.Build()
	if err == nil {
		t.Fatal("expected invariant error for submitted_at > started_at, got nil")
	}
	if !strings.Contains(err.Error(), "submitted_at") || !strings.Contains(err.Error(), "started_at") {
		t.Errorf("expected error mentioning submitted_at and started_at, got: %v", err)
	}
}

func TestTimingsBuilder_StartedAfterCompleted_ViolatesInvariant(t *testing.T) {
	clk := newFakeClock()
	bld := entities.NewTimingsBuilder(clk)

	bld.MarkSubmitted()

	// Advance to a future time for started
	clk.Advance(50 * time.Millisecond)
	bld.MarkStarted()

	// Wind clock back so completed is before started
	clk.T = clk.T.Add(-20 * time.Millisecond)
	bld.MarkCompleted()

	_, err := bld.Build()
	if err == nil {
		t.Fatal("expected invariant error for started_at > completed_at, got nil")
	}
	if !strings.Contains(err.Error(), "started_at") || !strings.Contains(err.Error(), "completed_at") {
		t.Errorf("expected error mentioning started_at and completed_at, got: %v", err)
	}
}

func TestTimingsBuilder_PersistedBeforeCompleted_ViolatesInvariant(t *testing.T) {
	clk := newFakeClock()
	bld := entities.NewTimingsBuilder(clk)

	bld.MarkSubmitted()
	clk.Advance(10 * time.Millisecond)
	bld.MarkStarted()
	clk.Advance(50 * time.Millisecond)
	bld.MarkCompleted()

	// Wind clock back so persisted is before completed
	clk.T = clk.T.Add(-20 * time.Millisecond)
	bld.MarkPersisted()

	_, err := bld.Build()
	if err == nil {
		t.Fatal("expected invariant error for persisted_at < completed_at, got nil")
	}
	if !strings.Contains(err.Error(), "persisted_at") || !strings.Contains(err.Error(), "completed_at") {
		t.Errorf("expected error mentioning persisted_at and completed_at, got: %v", err)
	}
}

func TestTimingsBuilder_PersistedEqualsCompleted_OK(t *testing.T) {
	clk := newFakeClock()
	bld := entities.NewTimingsBuilder(clk)

	bld.MarkSubmitted()
	clk.Advance(10 * time.Millisecond)
	bld.MarkStarted()
	clk.Advance(50 * time.Millisecond)

	// completed and persisted at the exact same instant
	completedTime := clk.T
	bld.MarkCompleted()
	// do not advance — persisted == completed
	clk.T = completedTime
	bld.MarkPersisted()

	tim, err := bld.Build()
	if err != nil {
		t.Fatalf("unexpected error for persisted_at == completed_at: %v", err)
	}
	if tim.PersistedAt == nil {
		t.Fatal("expected persisted_at to be set")
	}
	if !tim.PersistedAt.Equal(tim.CompletedAt) {
		t.Errorf("persisted_at (%v) != completed_at (%v)", tim.PersistedAt, tim.CompletedAt)
	}
}

func TestTimingsBuilder_PersistedAfterCompleted_OK(t *testing.T) {
	clk := newFakeClock()
	bld := entities.NewTimingsBuilder(clk)

	bld.MarkSubmitted()
	clk.Advance(10 * time.Millisecond)
	bld.MarkStarted()
	clk.Advance(50 * time.Millisecond)
	bld.MarkCompleted()
	clk.Advance(100 * time.Millisecond)
	bld.MarkPersisted()

	_, err := bld.Build()
	if err != nil {
		t.Fatalf("unexpected error for persisted_at > completed_at: %v", err)
	}
}

// ---------------------------------------------------------------------------
// JSON
// ---------------------------------------------------------------------------

func TestTimings_JSONRoundTrip_WithPersisted(t *testing.T) {
	clk := newFakeClock()
	bld := entities.NewTimingsBuilder(clk)
	bld.MarkSubmitted()
	clk.Advance(10 * time.Millisecond)
	bld.MarkStarted()
	clk.Advance(50 * time.Millisecond)
	bld.MarkCompleted()
	clk.Advance(5 * time.Millisecond)
	bld.MarkPersisted()

	original, err := bld.Build()
	if err != nil {
		t.Fatalf("build error: %v", err)
	}

	b, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	var decoded entities.Timings
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if !decoded.SubmittedAt.Equal(original.SubmittedAt) {
		t.Errorf("submitted_at: got %v, want %v", decoded.SubmittedAt, original.SubmittedAt)
	}
	if !decoded.StartedAt.Equal(original.StartedAt) {
		t.Errorf("started_at: got %v, want %v", decoded.StartedAt, original.StartedAt)
	}
	if !decoded.CompletedAt.Equal(original.CompletedAt) {
		t.Errorf("completed_at: got %v, want %v", decoded.CompletedAt, original.CompletedAt)
	}
	if decoded.PersistedAt == nil || !decoded.PersistedAt.Equal(*original.PersistedAt) {
		t.Errorf("persisted_at: got %v, want %v", decoded.PersistedAt, original.PersistedAt)
	}
}

func TestTimings_JSONRoundTrip_WithoutPersisted(t *testing.T) {
	clk := newFakeClock()
	bld := entities.NewTimingsBuilder(clk)
	bld.MarkSubmitted()
	clk.Advance(10 * time.Millisecond)
	bld.MarkStarted()
	clk.Advance(50 * time.Millisecond)
	bld.MarkCompleted()

	original, err := bld.Build()
	if err != nil {
		t.Fatalf("build error: %v", err)
	}

	b, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	// persisted_at must not appear in the payload
	if strings.Contains(string(b), "persisted_at") {
		t.Errorf("expected persisted_at to be omitted from JSON, got: %s", b)
	}

	var decoded entities.Timings
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if decoded.PersistedAt != nil {
		t.Errorf("expected nil PersistedAt after round-trip, got %v", decoded.PersistedAt)
	}
}
