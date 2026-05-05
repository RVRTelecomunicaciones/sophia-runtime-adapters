package entities_test

import (
	"encoding/json"
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

func mustCapability(t *testing.T) valueobjects.Capability {
	t.Helper()
	aid, err := valueobjects.NewAdapterID("shell")
	if err != nil {
		t.Fatalf("mustCapability NewAdapterID: %v", err)
	}
	cap, err := valueobjects.NewCapability(aid, "exec", "v1", false, 5*time.Second)
	if err != nil {
		t.Fatalf("mustCapability NewCapability: %v", err)
	}
	return cap
}

// ─── ULIDGen tests ────────────────────────────────────────────────────────────

func TestULIDGen_ProducesValid26CharULIDs(t *testing.T) {
	gen := entities.ULIDGen{}
	seen := make(map[string]struct{}, 100)

	for i := range 100 {
		s := gen.New()
		if len(s) != 26 {
			t.Errorf("call %d: expected 26-char ULID, got %q (len=%d)", i, s, len(s))
		}
		// Validate via shared.NewHandleID — same regex the production code uses.
		if _, err := shared.NewHandleID(s); err != nil {
			t.Errorf("call %d: NewHandleID(%q) failed: %v", i, s, err)
		}
		if _, dup := seen[s]; dup {
			t.Errorf("call %d: duplicate ULID %q", i, s)
		}
		seen[s] = struct{}{}
	}
}

func TestULIDGen_SeededReaderProducesConsistentFormat(t *testing.T) {
	// Use a seeded math/rand/v2 source for reproducibility.
	// We cannot use ulid.Monotonic here without importing ulid in the test package,
	// so we just verify format and that re-seeding produces the same entropy.
	//
	// ULID layout (26 chars total, Crockford base-32):
	//   chars 0..9   — millisecond timestamp from ulid.Now() (NOT deterministic
	//                  by design — its purpose is k-sortability, not clock semantics).
	//   chars 10..25 — 80-bit randomness drawn from Entropy.
	//
	// We compare ONLY the entropy portion: same seed must produce the same
	// entropy regardless of when the calls land relative to the ms boundary.
	// Comparing the full 26 chars used to flake on slow CI runners when the
	// two New() calls straddled a millisecond.
	src := rand.NewChaCha8([32]byte{1, 2, 3})
	gen := entities.ULIDGen{Entropy: src}

	first := gen.New()
	if len(first) != 26 {
		t.Fatalf("seeded ULIDGen: expected 26-char ULID, got %q", first)
	}
	if _, err := shared.NewHandleID(first); err != nil {
		t.Fatalf("seeded ULIDGen: NewHandleID(%q) failed: %v", first, err)
	}

	// Re-seed with same key — expect same entropy portion.
	src2 := rand.NewChaCha8([32]byte{1, 2, 3})
	gen2 := entities.ULIDGen{Entropy: src2}
	second := gen2.New()

	if first[10:] != second[10:] {
		t.Errorf("seeded ULIDGen: same seed produced different entropy: %q vs %q (entropy %q vs %q)",
			first, second, first[10:], second[10:])
	}
}

// ─── NewExecutionHandle happy path ────────────────────────────────────────────

func TestNewExecutionHandle_HappyPath(t *testing.T) {
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	clk := fakeClock(now)
	cid := mustCorrelationID(t, validULID)
	cap := mustCapability(t)
	gen := &entities.FakeIDGen{IDs: []string{validULID}}

	h, err := entities.NewExecutionHandle(gen, cid, cap, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if h.HandleID().String() != validULID {
		t.Errorf("HandleID = %q, want %q", h.HandleID().String(), validULID)
	}
	if h.CorrelationID().String() != cid.String() {
		t.Errorf("CorrelationID = %q, want %q", h.CorrelationID().String(), cid.String())
	}
	if h.AdapterID().String() != cap.AdapterID().String() {
		t.Errorf("AdapterID = %q, want %q", h.AdapterID().String(), cap.AdapterID().String())
	}
	if h.Capability().Canonical() != cap.Canonical() {
		t.Errorf("Capability = %q, want %q", h.Capability().Canonical(), cap.Canonical())
	}
	if !h.StartedAt().Equal(now) {
		t.Errorf("StartedAt = %v, want %v", h.StartedAt(), now)
	}
}

// ─── Guard clauses ────────────────────────────────────────────────────────────

func TestNewExecutionHandle_NilGeneratorRejected(t *testing.T) {
	cid := mustCorrelationID(t, validULID)
	cap := mustCapability(t)
	clk := fakeClock(time.Now())

	_, err := entities.NewExecutionHandle(nil, cid, cap, clk)
	if err == nil {
		t.Fatal("expected error for nil generator, got nil")
	}
	if !strings.Contains(err.Error(), "IDGenerator") {
		t.Errorf("error %q should mention IDGenerator", err.Error())
	}
}

func TestNewExecutionHandle_NilClockRejected(t *testing.T) {
	cid := mustCorrelationID(t, validULID)
	cap := mustCapability(t)
	gen := &entities.FakeIDGen{IDs: []string{validULID}}

	_, err := entities.NewExecutionHandle(gen, cid, cap, nil)
	if err == nil {
		t.Fatal("expected error for nil clock, got nil")
	}
	if !strings.Contains(err.Error(), "Clock") {
		t.Errorf("error %q should mention Clock", err.Error())
	}
}

func TestNewExecutionHandle_InvalidGeneratorOutput(t *testing.T) {
	// gen.New() returns a non-ULID string → NewHandleID must reject it.
	gen := &entities.FakeIDGen{IDs: []string{"not-a-ulid"}}
	cid := mustCorrelationID(t, validULID)
	cap := mustCapability(t)
	clk := fakeClock(time.Now())

	_, err := entities.NewExecutionHandle(gen, cid, cap, clk)
	if err == nil {
		t.Fatal("expected error for invalid generator output, got nil")
	}
	if !strings.Contains(err.Error(), "generate handle_id") {
		t.Errorf("error %q should mention generate handle_id", err.Error())
	}
}

func TestNewExecutionHandle_ZeroCorrelationIDRejected(t *testing.T) {
	gen := &entities.FakeIDGen{IDs: []string{validULID}}
	cap := mustCapability(t)
	clk := fakeClock(time.Now())

	_, err := entities.NewExecutionHandle(gen, shared.CorrelationID{}, cap, clk)
	if err == nil {
		t.Fatal("expected error for zero correlation_id, got nil")
	}
	if !strings.Contains(err.Error(), "correlation_id") {
		t.Errorf("error %q should mention correlation_id", err.Error())
	}
}

func TestNewExecutionHandle_ZeroCapabilityRejected(t *testing.T) {
	gen := &entities.FakeIDGen{IDs: []string{validULID}}
	cid := mustCorrelationID(t, validULID)
	clk := fakeClock(time.Now())

	_, err := entities.NewExecutionHandle(gen, cid, valueobjects.Capability{}, clk)
	if err == nil {
		t.Fatal("expected error for zero capability, got nil")
	}
	if !strings.Contains(err.Error(), "capability") {
		t.Errorf("error %q should mention capability", err.Error())
	}
}

// ─── Getters ──────────────────────────────────────────────────────────────────

func TestExecutionHandle_Getters(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := fakeClock(now)
	cid := mustCorrelationID(t, validULID)
	cap := mustCapability(t)
	gen := &entities.FakeIDGen{IDs: []string{validULID}}

	h, err := entities.NewExecutionHandle(gen, cid, cap, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		name string
		got  interface{}
		want interface{}
	}{
		{"HandleID", h.HandleID().String(), validULID},
		{"CorrelationID", h.CorrelationID().String(), cid.String()},
		{"AdapterID", h.AdapterID().String(), "shell"},
		{"Capability.Canonical", h.Capability().Canonical(), "shell.exec@v1"},
		{"StartedAt", h.StartedAt(), now},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !reflect.DeepEqual(tt.got, tt.want) {
				t.Errorf("got %v, want %v", tt.got, tt.want)
			}
		})
	}
}

// ─── MarshalJSON ──────────────────────────────────────────────────────────────

func TestExecutionHandle_MarshalJSON(t *testing.T) {
	now := time.Date(2026, 4, 19, 12, 30, 45, 123000000, time.UTC)
	clk := fakeClock(now)
	cid := mustCorrelationID(t, validULID)
	cap := mustCapability(t)
	gen := &entities.FakeIDGen{IDs: []string{validULID}}

	h, err := entities.NewExecutionHandle(gen, cid, cap, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	b, err := json.Marshal(h)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}

	var got map[string]interface{}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal for inspection: %v", err)
	}

	// Verify all expected snake_case keys are present.
	for _, key := range []string{"handle_id", "correlation_id", "adapter_id", "capability", "started_at"} {
		if _, ok := got[key]; !ok {
			t.Errorf("missing key %q in JSON output: %s", key, string(b))
		}
	}

	// Capability must be the canonical string, not a nested object.
	capVal, _ := got["capability"].(string)
	if capVal != "shell.exec@v1" {
		t.Errorf("capability = %q, want %q", capVal, "shell.exec@v1")
	}

	// handle_id must match what we injected.
	if got["handle_id"] != validULID {
		t.Errorf("handle_id = %v, want %q", got["handle_id"], validULID)
	}

	// started_at must be the millisecond-precision UTC ISO format.
	wantStartedAt := "2026-04-19T12:30:45.123Z"
	if got["started_at"] != wantStartedAt {
		t.Errorf("started_at = %v, want %q", got["started_at"], wantStartedAt)
	}

	// correlation_id matches.
	if got["correlation_id"] != cid.String() {
		t.Errorf("correlation_id = %v, want %q", got["correlation_id"], cid.String())
	}

	// adapter_id matches.
	if got["adapter_id"] != "shell" {
		t.Errorf("adapter_id = %v, want %q", got["adapter_id"], "shell")
	}
}

// ─── JSON round-trip (Added in Bundle 5 T48 for persistence round-trip support) ─

// TestExecutionHandle_JSONRoundTrip verifies Marshal → Unmarshal yields
// a handle with equal public state.
func TestExecutionHandle_JSONRoundTrip(t *testing.T) {
	now := time.Date(2026, 4, 19, 12, 30, 45, 0, time.UTC)
	clk := fakeClock(now)
	cid := mustCorrelationID(t, validULID)
	cap := mustCapability(t)
	gen := &entities.FakeIDGen{IDs: []string{validULID}}

	original, err := entities.NewExecutionHandle(gen, cid, cap, clk)
	if err != nil {
		t.Fatalf("NewExecutionHandle: %v", err)
	}

	b, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}

	var restored entities.ExecutionHandle
	if err := json.Unmarshal(b, &restored); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}

	if restored.HandleID().String() != original.HandleID().String() {
		t.Errorf("HandleID: got %q, want %q", restored.HandleID(), original.HandleID())
	}
	if restored.CorrelationID().String() != original.CorrelationID().String() {
		t.Errorf("CorrelationID: got %q, want %q", restored.CorrelationID(), original.CorrelationID())
	}
	if restored.AdapterID().String() != original.AdapterID().String() {
		t.Errorf("AdapterID: got %q, want %q", restored.AdapterID(), original.AdapterID())
	}
	if restored.Capability().Canonical() != original.Capability().Canonical() {
		t.Errorf("Capability.Canonical: got %q, want %q", restored.Capability().Canonical(), original.Capability().Canonical())
	}
	if !restored.StartedAt().Equal(original.StartedAt()) {
		t.Errorf("StartedAt: got %v, want %v", restored.StartedAt(), original.StartedAt())
	}
}

// ─── FakeIDGen exhaustion ─────────────────────────────────────────────────────

func TestFakeIDGen_PanicsWhenExhausted(t *testing.T) {
	gen := &entities.FakeIDGen{IDs: []string{validULID}}
	_ = gen.New() // consume the only ID

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when FakeIDGen exhausted, got none")
		}
	}()
	_ = gen.New() // should panic
}
