package valueobjects

import (
	"reflect"
	"testing"
	"time"
)

// TestNewPhase1Capabilities_Count verifies exactly 8 capabilities are returned.
func TestNewPhase1Capabilities_Count(t *testing.T) {
	caps, err := NewPhase1Capabilities()
	if err != nil {
		t.Fatalf("NewPhase1Capabilities() error: %v", err)
	}
	if len(caps) != 8 {
		t.Errorf("len = %d, want 8", len(caps))
	}
}

// TestNewPhase1Capabilities_Order verifies stable canonical ordering of the
// returned slice (adapter-grouped, then capability name within adapter).
func TestNewPhase1Capabilities_Order(t *testing.T) {
	caps, err := NewPhase1Capabilities()
	if err != nil {
		t.Fatalf("NewPhase1Capabilities() error: %v", err)
	}
	want := []string{
		"shell.exec@v1",
		"git.status@v1",
		"git.clone@v1",
		"git.diff@v1",
		"git.commit@v1",
		"filesystem.read_file@v1",
		"filesystem.write_file@v1",
		"http.request@v1",
	}
	for i, c := range caps {
		if c.Canonical() != want[i] {
			t.Errorf("caps[%d].Canonical() = %q, want %q", i, c.Canonical(), want[i])
		}
	}
}

// TestNewPhase1Capabilities_Canonicals asserts that each canonical string
// matches the spec §5.4 table exactly.
func TestNewPhase1Capabilities_Canonicals(t *testing.T) {
	caps, err := NewPhase1Capabilities()
	if err != nil {
		t.Fatalf("NewPhase1Capabilities() error: %v", err)
	}
	wantSet := map[string]bool{
		"shell.exec@v1":            true,
		"git.status@v1":            true,
		"git.clone@v1":             true,
		"git.diff@v1":              true,
		"git.commit@v1":            true,
		"filesystem.read_file@v1":  true,
		"filesystem.write_file@v1": true,
		"http.request@v1":          true,
	}
	for _, c := range caps {
		canon := c.Canonical()
		if !wantSet[canon] {
			t.Errorf("unexpected canonical %q in Phase 1 catalog", canon)
		}
		delete(wantSet, canon)
	}
	for missing := range wantSet {
		t.Errorf("missing canonical %q from Phase 1 catalog", missing)
	}
}

// TestNewPhase1Capabilities_AllowsPartial verifies only git.clone@v1 has
// AllowsPartial=true; all others must be false.
func TestNewPhase1Capabilities_AllowsPartial(t *testing.T) {
	caps, err := NewPhase1Capabilities()
	if err != nil {
		t.Fatalf("NewPhase1Capabilities() error: %v", err)
	}
	for _, c := range caps {
		canon := c.Canonical()
		want := canon == "git.clone@v1"
		if c.AllowsPartial() != want {
			t.Errorf("%s: AllowsPartial() = %v, want %v", canon, c.AllowsPartial(), want)
		}
	}
}

// TestNewPhase1Capabilities_DefaultTimeouts verifies each capability has the
// exact DefaultTimeout from spec §5.4.
func TestNewPhase1Capabilities_DefaultTimeouts(t *testing.T) {
	caps, err := NewPhase1Capabilities()
	if err != nil {
		t.Fatalf("NewPhase1Capabilities() error: %v", err)
	}
	wantTimeout := map[string]time.Duration{
		"shell.exec@v1":            30 * time.Second,
		"git.status@v1":            10 * time.Second,
		"git.clone@v1":             120 * time.Second,
		"git.diff@v1":              15 * time.Second,
		"git.commit@v1":            15 * time.Second,
		"filesystem.read_file@v1":  5 * time.Second,
		"filesystem.write_file@v1": 10 * time.Second,
		"http.request@v1":          15 * time.Second,
	}
	for _, c := range caps {
		canon := c.Canonical()
		want, ok := wantTimeout[canon]
		if !ok {
			t.Errorf("no expected timeout for canonical %q", canon)
			continue
		}
		if c.DefaultTimeout() != want {
			t.Errorf("%s: DefaultTimeout() = %v, want %v", canon, c.DefaultTimeout(), want)
		}
	}
}

// TestNewPhase1Capabilities_RegistryNoDuplicates verifies all 8 capabilities
// can be fed into NewCapabilityRegistry without duplicate errors.
func TestNewPhase1Capabilities_RegistryNoDuplicates(t *testing.T) {
	caps, err := NewPhase1Capabilities()
	if err != nil {
		t.Fatalf("NewPhase1Capabilities() error: %v", err)
	}
	_, err = NewCapabilityRegistry(caps...)
	if err != nil {
		t.Errorf("NewCapabilityRegistry(Phase1Capabilities): unexpected error: %v", err)
	}
}

// TestNewPhase1Capabilities_FilterByAdapter verifies the per-adapter counts
// when a registry is built from the Phase 1 catalog.
func TestNewPhase1Capabilities_FilterByAdapter(t *testing.T) {
	caps, err := NewPhase1Capabilities()
	if err != nil {
		t.Fatalf("NewPhase1Capabilities() error: %v", err)
	}
	reg, err := NewCapabilityRegistry(caps...)
	if err != nil {
		t.Fatalf("NewCapabilityRegistry: %v", err)
	}
	cases := []struct {
		adapter string
		want    int
	}{
		{"shell", 1},
		{"git", 4},
		{"filesystem", 2},
		{"http", 1},
	}
	for _, tc := range cases {
		aid, err := NewAdapterID(tc.adapter)
		if err != nil {
			t.Fatalf("NewAdapterID(%q): %v", tc.adapter, err)
		}
		got := reg.FilterByAdapter(aid)
		if len(got) != tc.want {
			t.Errorf("FilterByAdapter(%q) = %d entries, want %d", tc.adapter, len(got), tc.want)
		}
	}
}

// TestMustNewPhase1Capabilities_NoPanic verifies MustNewPhase1Capabilities
// does not panic and returns the same set as NewPhase1Capabilities.
func TestMustNewPhase1Capabilities_NoPanic(t *testing.T) {
	var caps []Capability
	// Ensure no panic.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("MustNewPhase1Capabilities panicked: %v", r)
		}
	}()
	caps = MustNewPhase1Capabilities()
	if len(caps) != 8 {
		t.Errorf("MustNewPhase1Capabilities() len = %d, want 8", len(caps))
	}
}

// TestNewPhase1Capabilities_MustEqualsNew verifies MustNewPhase1Capabilities
// returns the same slice as NewPhase1Capabilities (same length, same canonicals).
func TestNewPhase1Capabilities_MustEqualsNew(t *testing.T) {
	must := MustNewPhase1Capabilities()
	regular, err := NewPhase1Capabilities()
	if err != nil {
		t.Fatalf("NewPhase1Capabilities() error: %v", err)
	}
	if !reflect.DeepEqual(must, regular) {
		t.Error("MustNewPhase1Capabilities() != NewPhase1Capabilities()")
	}
}

// TestNewPhase1Capabilities_HTTPAdapterIDGap1 is the spec-adjacent regression
// for Gap-1 resolution: the HTTP capability's AdapterID wire string must be
// "http", NOT "httpreq" (the internal Go package name).
func TestNewPhase1Capabilities_HTTPAdapterIDGap1(t *testing.T) {
	caps, err := NewPhase1Capabilities()
	if err != nil {
		t.Fatalf("NewPhase1Capabilities() error: %v", err)
	}
	for _, c := range caps {
		if c.Canonical() == "http.request@v1" {
			got := c.AdapterID().String()
			if got != "http" {
				t.Errorf("http.request@v1: AdapterID().String() = %q, want \"http\" (Gap-1: internal pkg is httpreq/, external wire is http)", got)
			}
			return
		}
	}
	t.Fatal("http.request@v1 not found in Phase 1 capabilities")
}

// TestNewPhase1Capabilities_StableSort verifies calling NewPhase1Capabilities
// twice returns equal slices (deterministic ordering).
func TestNewPhase1Capabilities_StableSort(t *testing.T) {
	first, err := NewPhase1Capabilities()
	if err != nil {
		t.Fatalf("first call error: %v", err)
	}
	second, err := NewPhase1Capabilities()
	if err != nil {
		t.Fatalf("second call error: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Error("two calls to NewPhase1Capabilities() returned different slices")
	}
}
