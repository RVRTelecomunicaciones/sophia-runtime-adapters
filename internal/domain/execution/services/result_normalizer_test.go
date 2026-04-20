package services

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
)

// ---------------------------------------------------------------------------
// Test doubles
// ---------------------------------------------------------------------------

// stubRaw is a test-only AdapterRawOutcome implementation in this package.
type stubRaw struct{ value string }

func (stubRaw) IsAdapterRawOutcome() {}

// buildCap constructs a valid Capability for tests.
func buildCap(t *testing.T, adapterID, name, version string) valueobjects.Capability {
	t.Helper()
	aid, err := valueobjects.NewAdapterID(adapterID)
	if err != nil {
		t.Fatalf("NewAdapterID(%q): %v", adapterID, err)
	}
	cap, err := valueobjects.NewCapability(aid, name, version, false, time.Second)
	if err != nil {
		t.Fatalf("NewCapability: %v", err)
	}
	return cap
}

// successResult builds a minimal valid success ExecutionResult for use as
// normalizer closure return values.
func successResult(t *testing.T, clk shared.Clock) entities.ExecutionResult {
	t.Helper()
	r, err := entities.NewExecutionResult(
		valueobjects.StatusSuccess,
		valueobjects.HintNonRetryable,
		"", "",
		nil, nil, nil,
		nil, nil,
		0, 0,
		10*time.Millisecond,
		clk.Now(),
	)
	if err != nil {
		t.Fatalf("NewExecutionResult: %v", err)
	}
	return r
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// 1. NewResultNormalizer panics on non-positive inlineLimit.
func TestNewResultNormalizer_PanicsOnNonPositiveLimit(t *testing.T) {
	cases := []struct {
		name  string
		limit int
	}{
		{"zero", 0},
		{"negative", -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("expected panic for limit=%d, got none", tc.limit)
				}
				msg, ok := r.(string)
				if !ok {
					t.Fatalf("expected string panic, got %T: %v", r, r)
				}
				if !strings.Contains(msg, "inlineLimit must be > 0") {
					t.Errorf("unexpected panic message: %q", msg)
				}
			}()
			NewResultNormalizer(tc.limit)
		})
	}
}

// 2. InlineLimit returns the configured value.
func TestResultNormalizer_InlineLimit(t *testing.T) {
	n := NewResultNormalizer(16384)
	if got := n.InlineLimit(); got != 16384 {
		t.Errorf("InlineLimit() = %d, want 16384", got)
	}
}

// 3. Register happy path — first registration succeeds.
func TestResultNormalizer_Register_HappyPath(t *testing.T) {
	n := NewResultNormalizer(1024)
	cap := buildCap(t, "shell", "exec", "v1")
	fn := func(c valueobjects.Capability, r AdapterRawOutcome, clk shared.Clock) (entities.ExecutionResult, error) {
		return successResult(t, clk), nil
	}
	if err := n.Register(cap.Canonical(), fn); err != nil {
		t.Fatalf("Register returned unexpected error: %v", err)
	}
}

// 4. Register rejects empty canonical.
func TestResultNormalizer_Register_RejectsEmptyCanonical(t *testing.T) {
	n := NewResultNormalizer(1024)
	fn := func(c valueobjects.Capability, r AdapterRawOutcome, clk shared.Clock) (entities.ExecutionResult, error) {
		return entities.ExecutionResult{}, nil
	}
	err := n.Register("", fn)
	if err == nil {
		t.Fatal("expected error for empty canonical, got nil")
	}
	if !strings.Contains(err.Error(), "canonical must not be empty") {
		t.Errorf("unexpected error message: %q", err.Error())
	}
}

// 5. Register rejects nil fn.
func TestResultNormalizer_Register_RejectsNilFn(t *testing.T) {
	n := NewResultNormalizer(1024)
	err := n.Register("shell.exec@v1", nil)
	if err == nil {
		t.Fatal("expected error for nil fn, got nil")
	}
	if !strings.Contains(err.Error(), "must not be nil") {
		t.Errorf("unexpected error message: %q", err.Error())
	}
}

// 6. Register rejects duplicate canonical.
func TestResultNormalizer_Register_RejectsDuplicate(t *testing.T) {
	n := NewResultNormalizer(1024)
	fn := func(c valueobjects.Capability, r AdapterRawOutcome, clk shared.Clock) (entities.ExecutionResult, error) {
		return entities.ExecutionResult{}, nil
	}
	if err := n.Register("shell.exec@v1", fn); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	err := n.Register("shell.exec@v1", fn)
	if err == nil {
		t.Fatal("expected error for duplicate registration, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate normalizer registration") {
		t.Errorf("unexpected error message: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "shell.exec@v1") {
		t.Errorf("error should include canonical key, got: %q", err.Error())
	}
}

// 7. Normalize dispatches to the right closure.
func TestResultNormalizer_Normalize_DispatchesToRightClosure(t *testing.T) {
	n := NewResultNormalizer(1024)
	clk := &shared.FakeClock{T: time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)}

	capA := buildCap(t, "shell", "exec", "v1")
	capB := buildCap(t, "git", "status", "v1")

	sentinelA := "result-from-shell"
	sentinelB := "result-from-git"

	fnA := func(c valueobjects.Capability, r AdapterRawOutcome, clk shared.Clock) (entities.ExecutionResult, error) {
		res := successResult(t, clk)
		res.AdapterMeta = map[string]string{"sentinel": sentinelA}
		return res, nil
	}
	fnB := func(c valueobjects.Capability, r AdapterRawOutcome, clk shared.Clock) (entities.ExecutionResult, error) {
		res := successResult(t, clk)
		res.AdapterMeta = map[string]string{"sentinel": sentinelB}
		return res, nil
	}

	if err := n.Register(capA.Canonical(), fnA); err != nil {
		t.Fatalf("Register A: %v", err)
	}
	if err := n.Register(capB.Canonical(), fnB); err != nil {
		t.Fatalf("Register B: %v", err)
	}

	raw := stubRaw{value: "test"}

	resA, err := n.Normalize(capA, raw, clk)
	if err != nil {
		t.Fatalf("Normalize(capA): %v", err)
	}
	if got := resA.AdapterMeta["sentinel"]; got != sentinelA {
		t.Errorf("capA: sentinel = %q, want %q", got, sentinelA)
	}

	resB, err := n.Normalize(capB, raw, clk)
	if err != nil {
		t.Fatalf("Normalize(capB): %v", err)
	}
	if got := resB.AdapterMeta["sentinel"]; got != sentinelB {
		t.Errorf("capB: sentinel = %q, want %q", got, sentinelB)
	}
}

// 8. Normalize returns ErrNoNormalizerRegistered (wrapped) when canonical absent.
func TestResultNormalizer_Normalize_ErrNoNormalizerRegistered(t *testing.T) {
	n := NewResultNormalizer(1024)
	clk := &shared.FakeClock{T: time.Now()}
	cap := buildCap(t, "shell", "exec", "v1")

	_, err := n.Normalize(cap, stubRaw{}, clk)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrNoNormalizerRegistered) {
		t.Errorf("errors.Is(err, ErrNoNormalizerRegistered) = false; err = %v", err)
	}
	if !strings.Contains(err.Error(), cap.Canonical()) {
		t.Errorf("error message should contain canonical %q, got: %q", cap.Canonical(), err.Error())
	}
}

// 9. Normalize returns error when clk is nil.
func TestResultNormalizer_Normalize_NilClockError(t *testing.T) {
	n := NewResultNormalizer(1024)
	cap := buildCap(t, "shell", "exec", "v1")
	fn := func(c valueobjects.Capability, r AdapterRawOutcome, clk shared.Clock) (entities.ExecutionResult, error) {
		return entities.ExecutionResult{}, nil
	}
	if err := n.Register(cap.Canonical(), fn); err != nil {
		t.Fatalf("Register: %v", err)
	}

	_, err := n.Normalize(cap, stubRaw{}, nil)
	if err == nil {
		t.Fatal("expected error for nil clock, got nil")
	}
	if !strings.Contains(err.Error(), "clock is required") {
		t.Errorf("unexpected error message: %q", err.Error())
	}
}

// 10. Registered() returns sorted canonicals.
func TestResultNormalizer_Registered_Sorted(t *testing.T) {
	n := NewResultNormalizer(1024)
	fn := func(c valueobjects.Capability, r AdapterRawOutcome, clk shared.Clock) (entities.ExecutionResult, error) {
		return entities.ExecutionResult{}, nil
	}

	canonicals := []string{
		"shell.exec@v1",
		"git.status@v1",
		"filesystem.read@v1",
	}
	for _, c := range canonicals {
		if err := n.Register(c, fn); err != nil {
			t.Fatalf("Register(%q): %v", c, err)
		}
	}

	got := n.Registered()
	if len(got) != len(canonicals) {
		t.Fatalf("len(Registered()) = %d, want %d", len(got), len(canonicals))
	}
	for i := 1; i < len(got); i++ {
		if got[i-1] > got[i] {
			t.Errorf("Registered() not sorted: got[%d]=%q > got[%d]=%q", i-1, got[i-1], i, got[i])
		}
	}
	// Verify expected sorted order explicitly.
	want := []string{
		"filesystem.read@v1",
		"git.status@v1",
		"shell.exec@v1",
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("Registered()[%d] = %q, want %q", i, got[i], w)
		}
	}
}

// 11. Concurrent Normalize calls are race-safe.
func TestResultNormalizer_ConcurrentNormalize(t *testing.T) {
	n := NewResultNormalizer(1024)
	clk := &shared.FakeClock{T: time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)}

	cap := buildCap(t, "shell", "exec", "v1")
	fn := func(c valueobjects.Capability, r AdapterRawOutcome, clk shared.Clock) (entities.ExecutionResult, error) {
		return successResult(t, clk), nil
	}
	if err := n.Register(cap.Canonical(), fn); err != nil {
		t.Fatalf("Register: %v", err)
	}

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errs := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_, err := n.Normalize(cap, stubRaw{value: "concurrent"}, clk)
			if err != nil {
				errs <- err
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("Normalize error in goroutine: %v", err)
	}
}
