package filesystem

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
)

// ---------------------------------------------------------------------------
// Shared test helpers (used by adapter_test, read_test, write_test)
// ---------------------------------------------------------------------------

func newTestFSAdapter(t *testing.T) (string, *Adapter) {
	t.Helper()
	root := t.TempDir()
	a, err := NewAdapter(Config{
		AllowedFilesystemRoots: []string{root},
		InlineStreamLimit:      16 * 1024,
	}, &shared.FakeClock{T: time.Now()})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	return root, a
}

func capFSByName(t *testing.T, a *Adapter, name string) valueobjects.Capability {
	t.Helper()
	for _, c := range a.Capabilities() {
		if c.Name() == name {
			return c
		}
	}
	t.Fatalf("capability %q not found", name)
	panic("unreachable")
}

func mustFSPayload(t *testing.T, v map[string]any) valueobjects.Payload {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return mustFSPayloadRaw(t, b)
}

func mustFSPayloadRaw(t *testing.T, raw []byte) valueobjects.Payload {
	t.Helper()
	p, err := valueobjects.NewPayload(valueobjects.ContentTypeJSON, raw, 0)
	if err != nil {
		t.Fatalf("NewPayload: %v", err)
	}
	return p
}

// ---------------------------------------------------------------------------
// Constructor validation
// ---------------------------------------------------------------------------

func TestNewAdapter_RejectsZeroInlineStreamLimit(t *testing.T) {
	_, err := NewAdapter(Config{
		AllowedFilesystemRoots: []string{os.TempDir()},
		InlineStreamLimit:      0,
	}, &shared.FakeClock{T: time.Now()})
	if err == nil {
		t.Fatal("expected error for InlineStreamLimit=0, got nil")
	}
}

func TestNewAdapter_RejectsNegativeInlineStreamLimit(t *testing.T) {
	_, err := NewAdapter(Config{
		AllowedFilesystemRoots: []string{os.TempDir()},
		InlineStreamLimit:      -1,
	}, &shared.FakeClock{T: time.Now()})
	if err == nil {
		t.Fatal("expected error for InlineStreamLimit=-1, got nil")
	}
}

func TestNewAdapter_RejectsEmptyAllowedFilesystemRoots(t *testing.T) {
	_, err := NewAdapter(Config{
		AllowedFilesystemRoots: []string{},
		InlineStreamLimit:      4096,
	}, &shared.FakeClock{T: time.Now()})
	if err == nil {
		t.Fatal("expected error for empty AllowedFilesystemRoots, got nil")
	}
}

func TestNewAdapter_RejectsNilClock(t *testing.T) {
	_, err := NewAdapter(Config{
		AllowedFilesystemRoots: []string{os.TempDir()},
		InlineStreamLimit:      4096,
	}, nil)
	if err == nil {
		t.Fatal("expected error for nil Clock, got nil")
	}
}

// ---------------------------------------------------------------------------
// ID and Capabilities
// ---------------------------------------------------------------------------

func TestAdapter_IDReturnsFilesystem(t *testing.T) {
	_, a := newTestFSAdapter(t)
	if got := a.ID().String(); got != "filesystem" {
		t.Errorf("ID() = %q, want %q", got, "filesystem")
	}
}

func TestAdapter_CapabilitiesReturns2(t *testing.T) {
	_, a := newTestFSAdapter(t)
	caps := a.Capabilities()
	if len(caps) != 2 {
		t.Fatalf("len(Capabilities()) = %d, want 2", len(caps))
	}
}

func TestAdapter_CapabilitiesHaveCorrectCanonicals(t *testing.T) {
	_, a := newTestFSAdapter(t)
	want := []string{
		"filesystem.read_file@v1",
		"filesystem.write_file@v1",
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

func TestAdapter_CapabilitiesIsStable(t *testing.T) {
	_, a := newTestFSAdapter(t)
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

func TestAdapter_NeitherCapabilityAllowsPartial(t *testing.T) {
	_, a := newTestFSAdapter(t)
	for _, c := range a.Capabilities() {
		if c.AllowsPartial() {
			t.Errorf("capability %q should not AllowsPartial", c.Canonical())
		}
	}
}

func TestAdapter_CapabilityDefaultTimeouts(t *testing.T) {
	_, a := newTestFSAdapter(t)
	wantTimeouts := map[string]time.Duration{
		"read_file":  defaultReadTimeout,
		"write_file": defaultWriteTimeout,
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

// ---------------------------------------------------------------------------
// Execute — dispatcher routing
// ---------------------------------------------------------------------------

func TestExecute_ReadFileReturnsReadRaw(t *testing.T) {
	_, a := newTestFSAdapter(t)
	cap := capFSByName(t, a, "read_file")
	p := mustFSPayloadRaw(t, []byte(`{}`))
	raw, err := a.Execute(context.Background(), cap, p)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if _, ok := raw.(*readRaw); !ok {
		t.Errorf("raw type = %T, want *readRaw", raw)
	}
}

func TestExecute_WriteFileReturnsWriteRaw(t *testing.T) {
	_, a := newTestFSAdapter(t)
	cap := capFSByName(t, a, "write_file")
	p := mustFSPayloadRaw(t, []byte(`{}`))
	raw, err := a.Execute(context.Background(), cap, p)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if _, ok := raw.(*writeRaw); !ok {
		t.Errorf("raw type = %T, want *writeRaw", raw)
	}
}

func TestExecute_UnknownCapabilityReturnsValidationRaw(t *testing.T) {
	_, a := newTestFSAdapter(t)
	id, _ := valueobjects.NewAdapterID("filesystem")
	c, _ := valueobjects.NewCapability(id, "nonexistent", "v1", false, 10*time.Second)
	p := mustFSPayloadRaw(t, []byte(`{}`))
	raw, err := a.Execute(context.Background(), c, p)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	r, ok := raw.(*readRaw)
	if !ok {
		t.Fatalf("raw type = %T, want *readRaw (for unknown cap)", raw)
	}
	if r.validation == "" {
		t.Error("validation should be non-empty for unknown capability")
	}
}

// ---------------------------------------------------------------------------
// Execute — cancelled ctx
// ---------------------------------------------------------------------------

func TestExecute_CancelledCtx_ReadFileCapability(t *testing.T) {
	_, a := newTestFSAdapter(t)
	cap := capFSByName(t, a, "read_file")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	raw, err := a.Execute(ctx, cap, mustFSPayloadRaw(t, []byte(`{}`)))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	r, ok := raw.(*readRaw)
	if !ok {
		t.Fatalf("raw type = %T, want *readRaw", raw)
	}
	if r.ctxErr == nil {
		t.Error("ctxErr should be set for cancelled ctx")
	}
}

func TestExecute_CancelledCtx_WriteFileCapability(t *testing.T) {
	_, a := newTestFSAdapter(t)
	cap := capFSByName(t, a, "write_file")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	raw, err := a.Execute(ctx, cap, mustFSPayloadRaw(t, []byte(`{}`)))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	r, ok := raw.(*writeRaw)
	if !ok {
		t.Fatalf("raw type = %T, want *writeRaw", raw)
	}
	if r.ctxErr == nil {
		t.Error("ctxErr should be set for cancelled ctx")
	}
}

func TestExecute_CancelledCtx_UnknownCapability(t *testing.T) {
	_, a := newTestFSAdapter(t)
	id, _ := valueobjects.NewAdapterID("filesystem")
	c, _ := valueobjects.NewCapability(id, "nonexistent", "v1", false, 10*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	raw, err := a.Execute(ctx, c, mustFSPayloadRaw(t, []byte(`{}`)))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Unknown cap with cancelled ctx returns readRaw with ctxErr.
	r, ok := raw.(*readRaw)
	if !ok {
		t.Fatalf("raw type = %T, want *readRaw", raw)
	}
	if r.ctxErr == nil {
		t.Error("ctxErr should be set for cancelled ctx on unknown cap")
	}
}
