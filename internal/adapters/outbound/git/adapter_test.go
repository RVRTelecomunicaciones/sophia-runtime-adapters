package git

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTestAdapter(t *testing.T) *Adapter {
	t.Helper()
	a, err := NewAdapter(Config{
		AllowedWorkingDirs: []string{os.TempDir()},
		SSHAgent:           false,
		InlineStreamLimit:  16 * 1024,
	}, &shared.FakeClock{T: time.Now()})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	return a
}

func capByName(t *testing.T, a *Adapter, name string) valueobjects.Capability {
	t.Helper()
	for _, c := range a.Capabilities() {
		if c.Name() == name {
			return c
		}
	}
	t.Fatalf("capability %q not found", name)
	panic("unreachable")
}

// ---------------------------------------------------------------------------
// Constructor validation
// ---------------------------------------------------------------------------

func TestNewAdapter_RejectsZeroInlineStreamLimit(t *testing.T) {
	_, err := NewAdapter(Config{
		AllowedWorkingDirs: []string{os.TempDir()},
		InlineStreamLimit:  0,
	}, &shared.FakeClock{T: time.Now()})
	if err == nil {
		t.Fatal("expected error for InlineStreamLimit=0, got nil")
	}
}

func TestNewAdapter_RejectsNegativeInlineStreamLimit(t *testing.T) {
	_, err := NewAdapter(Config{
		AllowedWorkingDirs: []string{os.TempDir()},
		InlineStreamLimit:  -1,
	}, &shared.FakeClock{T: time.Now()})
	if err == nil {
		t.Fatal("expected error for InlineStreamLimit=-1, got nil")
	}
}

func TestNewAdapter_RejectsEmptyAllowedWorkingDirs(t *testing.T) {
	_, err := NewAdapter(Config{
		AllowedWorkingDirs: []string{},
		InlineStreamLimit:  4096,
	}, &shared.FakeClock{T: time.Now()})
	if err == nil {
		t.Fatal("expected error for empty AllowedWorkingDirs, got nil")
	}
}

func TestNewAdapter_RejectsNilClock(t *testing.T) {
	_, err := NewAdapter(Config{
		AllowedWorkingDirs: []string{os.TempDir()},
		InlineStreamLimit:  4096,
	}, nil)
	if err == nil {
		t.Fatal("expected error for nil Clock, got nil")
	}
}

// ---------------------------------------------------------------------------
// ID and Capabilities
// ---------------------------------------------------------------------------

func TestAdapter_IDReturnsGit(t *testing.T) {
	a := newTestAdapter(t)
	if got := a.ID().String(); got != "git" {
		t.Errorf("ID() = %q, want %q", got, "git")
	}
}

func TestAdapter_CapabilitiesReturns4(t *testing.T) {
	a := newTestAdapter(t)
	caps := a.Capabilities()
	if len(caps) != 4 {
		t.Fatalf("len(Capabilities()) = %d, want 4", len(caps))
	}
}

func TestAdapter_CapabilitiesHaveCorrectCanonicals(t *testing.T) {
	a := newTestAdapter(t)
	want := []string{
		"git.status@v1",
		"git.clone@v1",
		"git.diff@v1",
		"git.commit@v1",
	}
	got := map[string]bool{}
	for _, c := range a.Capabilities() {
		got[c.Canonical()] = true
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("missing capability %q", w)
		}
	}
}

func TestAdapter_OnlyCloneAllowsPartial(t *testing.T) {
	a := newTestAdapter(t)
	for _, c := range a.Capabilities() {
		if c.Name() == "clone" {
			if !c.AllowsPartial() {
				t.Error("git.clone@v1 must AllowsPartial=true")
			}
		} else {
			if c.AllowsPartial() {
				t.Errorf("git.%s@v1 must AllowsPartial=false", c.Name())
			}
		}
	}
}

func TestAdapter_CapabilityDefaultTimeouts(t *testing.T) {
	a := newTestAdapter(t)
	wantTimeouts := map[string]time.Duration{
		"status": defaultStatusTimeout,
		"clone":  defaultCloneTimeout,
		"diff":   defaultDiffTimeout,
		"commit": defaultCommitTimeout,
	}
	for _, c := range a.Capabilities() {
		want, ok := wantTimeouts[c.Name()]
		if !ok {
			t.Errorf("unexpected capability %q", c.Name())
			continue
		}
		if c.DefaultTimeout() != want {
			t.Errorf("%s default timeout = %v, want %v", c.Name(), c.DefaultTimeout(), want)
		}
	}
}

func TestAdapter_CapabilitiesIsStable(t *testing.T) {
	a := newTestAdapter(t)
	first := a.Capabilities()
	second := a.Capabilities()
	if len(first) != len(second) {
		t.Fatalf("Capabilities() lengths differ: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].Canonical() != second[i].Canonical() {
			t.Errorf("[%d] canonical mismatch: %q vs %q", i, first[i].Canonical(), second[i].Canonical())
		}
	}
}

// ---------------------------------------------------------------------------
// Execute — dispatcher routing (verify raw type per capability)
// ---------------------------------------------------------------------------

func TestExecute_StatusReturnsStatusRaw(t *testing.T) {
	a := newTestAdapter(t)
	cap := capByName(t, a, "status")
	p := mustPayload(t)
	raw, err := a.Execute(context.Background(), cap, p)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if _, ok := raw.(*statusRaw); !ok {
		t.Errorf("raw type = %T, want *statusRaw", raw)
	}
}

func TestExecute_CloneReturnsCloneRaw(t *testing.T) {
	a := newTestAdapter(t)
	cap := capByName(t, a, "clone")
	p := mustPayload(t)
	raw, err := a.Execute(context.Background(), cap, p)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if _, ok := raw.(*cloneRaw); !ok {
		t.Errorf("raw type = %T, want *cloneRaw", raw)
	}
}

func TestExecute_DiffReturnsDiffRaw(t *testing.T) {
	a := newTestAdapter(t)
	cap := capByName(t, a, "diff")
	p := mustPayload(t)
	raw, err := a.Execute(context.Background(), cap, p)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if _, ok := raw.(*diffRaw); !ok {
		t.Errorf("raw type = %T, want *diffRaw", raw)
	}
}

func TestExecute_CommitReturnsCommitRaw(t *testing.T) {
	a := newTestAdapter(t)
	cap := capByName(t, a, "commit")
	p := mustPayload(t)
	raw, err := a.Execute(context.Background(), cap, p)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if _, ok := raw.(*commitRaw); !ok {
		t.Errorf("raw type = %T, want *commitRaw", raw)
	}
}

func TestExecute_StubsReturnNotImpl(t *testing.T) {
	a := newTestAdapter(t)
	p := mustPayload(t)
	for _, name := range []string{"status", "clone", "diff", "commit"} {
		cap := capByName(t, a, name)
		raw, err := a.Execute(context.Background(), cap, p)
		if err != nil {
			t.Fatalf("%s Execute: %v", name, err)
		}
		switch r := raw.(type) {
		case *statusRaw:
			if !r.notImpl {
				t.Errorf("status stub: notImpl should be true")
			}
		case *cloneRaw:
			if !r.notImpl {
				t.Errorf("clone stub: notImpl should be true")
			}
		case *diffRaw:
			if !r.notImpl {
				t.Errorf("diff stub: notImpl should be true")
			}
		case *commitRaw:
			if !r.notImpl {
				t.Errorf("commit stub: notImpl should be true")
			}
		default:
			t.Errorf("unexpected raw type %T", raw)
		}
	}
}

// ---------------------------------------------------------------------------
// Execute — cancelled ctx
// ---------------------------------------------------------------------------

func TestExecute_CancelledCtx_StatusCapability(t *testing.T) {
	a := newTestAdapter(t)
	cap := capByName(t, a, "status")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	raw, err := a.Execute(ctx, cap, mustPayload(t))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	r, ok := raw.(*statusRaw)
	if !ok {
		t.Fatalf("raw type = %T, want *statusRaw", raw)
	}
	if r.ctxErr == nil {
		t.Error("ctxErr should be set for cancelled ctx")
	}
}

func TestExecute_CancelledCtx_CloneCapability(t *testing.T) {
	a := newTestAdapter(t)
	cap := capByName(t, a, "clone")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	raw, err := a.Execute(ctx, cap, mustPayload(t))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	r, ok := raw.(*cloneRaw)
	if !ok {
		t.Fatalf("raw type = %T, want *cloneRaw", raw)
	}
	if r.ctxErr == nil {
		t.Error("ctxErr should be set for cancelled ctx")
	}
}

func TestExecute_CancelledCtx_DiffCapability(t *testing.T) {
	a := newTestAdapter(t)
	cap := capByName(t, a, "diff")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	raw, err := a.Execute(ctx, cap, mustPayload(t))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	r, ok := raw.(*diffRaw)
	if !ok {
		t.Fatalf("raw type = %T, want *diffRaw", raw)
	}
	if r.ctxErr == nil {
		t.Error("ctxErr should be set for cancelled ctx")
	}
}

func TestExecute_CancelledCtx_CommitCapability(t *testing.T) {
	a := newTestAdapter(t)
	cap := capByName(t, a, "commit")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	raw, err := a.Execute(ctx, cap, mustPayload(t))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	r, ok := raw.(*commitRaw)
	if !ok {
		t.Fatalf("raw type = %T, want *commitRaw", raw)
	}
	if r.ctxErr == nil {
		t.Error("ctxErr should be set for cancelled ctx")
	}
}

// ---------------------------------------------------------------------------
// Execute — unknown capability
// ---------------------------------------------------------------------------

func TestExecute_UnknownCapabilityReturnsValidationRaw(t *testing.T) {
	a := newTestAdapter(t)
	id, _ := valueobjects.NewAdapterID("git")
	c, _ := valueobjects.NewCapability(id, "nonexistent", "v1", false, 10*time.Second)
	raw, err := a.Execute(context.Background(), c, mustPayload(t))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	r, ok := raw.(*statusRaw)
	if !ok {
		t.Fatalf("raw type = %T, want *statusRaw (for unknown cap)", raw)
	}
	if r.validation == "" {
		t.Error("validation should be non-empty for unknown capability")
	}
}

// ---------------------------------------------------------------------------
// Normalizer — notImpl path
// ---------------------------------------------------------------------------

func TestNormalizeStatus_NotImpl_ReturnsAdapterInternalError(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	cap := capByName(t, a, "status")
	result, err := a.NormalizeStatus(cap, &statusRaw{notImpl: true}, clk)
	if err != nil {
		t.Fatalf("NormalizeStatus: %v", err)
	}
	if result.Status != valueobjects.StatusFailure {
		t.Errorf("status = %q, want failure", result.Status)
	}
	if result.ErrorClass != valueobjects.ErrAdapterInternalError {
		t.Errorf("error_class = %q, want adapter_internal_error", result.ErrorClass)
	}
}

func TestNormalizeClone_NotImpl_ReturnsAdapterInternalError(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	cap := capByName(t, a, "clone")
	result, err := a.NormalizeClone(cap, &cloneRaw{notImpl: true}, clk)
	if err != nil {
		t.Fatalf("NormalizeClone: %v", err)
	}
	if result.ErrorClass != valueobjects.ErrAdapterInternalError {
		t.Errorf("error_class = %q, want adapter_internal_error", result.ErrorClass)
	}
}

func TestNormalizeDiff_NotImpl_ReturnsAdapterInternalError(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	cap := capByName(t, a, "diff")
	result, err := a.NormalizeDiff(cap, &diffRaw{notImpl: true}, clk)
	if err != nil {
		t.Fatalf("NormalizeDiff: %v", err)
	}
	if result.ErrorClass != valueobjects.ErrAdapterInternalError {
		t.Errorf("error_class = %q, want adapter_internal_error", result.ErrorClass)
	}
}

func TestNormalizeCommit_NotImpl_ReturnsAdapterInternalError(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	cap := capByName(t, a, "commit")
	result, err := a.NormalizeCommit(cap, &commitRaw{notImpl: true}, clk)
	if err != nil {
		t.Fatalf("NormalizeCommit: %v", err)
	}
	if result.ErrorClass != valueobjects.ErrAdapterInternalError {
		t.Errorf("error_class = %q, want adapter_internal_error", result.ErrorClass)
	}
}

func TestNormalizeStatus_ForeignRawType_ReturnsError(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	cap := capByName(t, a, "status")
	_, err := a.NormalizeStatus(cap, &foreignRaw{}, clk)
	if err == nil {
		t.Error("expected error for foreign raw type, got nil")
	}
}

func TestNormalizePlaceholder_CtxCancelled(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	cap := capByName(t, a, "status")
	result, err := a.NormalizeStatus(cap, &statusRaw{ctxErr: context.Canceled}, clk)
	if err != nil {
		t.Fatalf("NormalizeStatus: %v", err)
	}
	if result.Status != valueobjects.StatusCancelled {
		t.Errorf("status = %q, want cancelled", result.Status)
	}
	if result.ErrorClass != valueobjects.ErrCancelled {
		t.Errorf("error_class = %q, want cancelled", result.ErrorClass)
	}
}

func TestNormalizePlaceholder_CtxDeadlineExceeded(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	cap := capByName(t, a, "status")
	result, err := a.NormalizeStatus(cap, &statusRaw{ctxErr: context.DeadlineExceeded}, clk)
	if err != nil {
		t.Fatalf("NormalizeStatus: %v", err)
	}
	if result.Status != valueobjects.StatusTimeout {
		t.Errorf("status = %q, want timeout", result.Status)
	}
	if result.ErrorClass != valueobjects.ErrTimeout {
		t.Errorf("error_class = %q, want timeout", result.ErrorClass)
	}
}

func TestNormalizePlaceholder_ValidationFailure(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	cap := capByName(t, a, "status")
	result, err := a.NormalizeStatus(cap, &statusRaw{validation: "some validation error"}, clk)
	if err != nil {
		t.Fatalf("NormalizeStatus: %v", err)
	}
	if result.Status != valueobjects.StatusFailure {
		t.Errorf("status = %q, want failure", result.Status)
	}
	if result.ErrorClass != valueobjects.ErrValidationFailure {
		t.Errorf("error_class = %q, want validation_failure", result.ErrorClass)
	}
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func mustPayload(t *testing.T) valueobjects.Payload {
	t.Helper()
	p, err := valueobjects.NewPayload(valueobjects.ContentTypeJSON, []byte(`{}`), 0)
	if err != nil {
		t.Fatalf("NewPayload: %v", err)
	}
	return p
}

// foreignRaw is a test-only type to exercise the type-assertion guard.
type foreignRaw struct{}

func (*foreignRaw) IsAdapterRawOutcome() {}
