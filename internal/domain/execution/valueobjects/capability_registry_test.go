package valueobjects

import (
	"errors"
	"testing"
	"time"
)

// buildPhase1Registry returns a CapabilityRegistry with all 8 Phase 1 caps.
func buildPhase1Registry(t *testing.T) *CapabilityRegistry {
	t.Helper()
	caps := []Capability{
		phase1Cap(t, "shell", "exec", "v1", false, 30*time.Second),
		phase1Cap(t, "git", "status", "v1", false, 10*time.Second),
		phase1Cap(t, "git", "clone", "v1", false, 120*time.Second),
		phase1Cap(t, "git", "diff", "v1", true, 30*time.Second),
		phase1Cap(t, "git", "commit", "v1", false, 30*time.Second),
		phase1Cap(t, "filesystem", "read_file", "v1", false, 5*time.Second),
		phase1Cap(t, "filesystem", "write_file", "v1", false, 5*time.Second),
		phase1Cap(t, "http", "request", "v1", true, 60*time.Second),
	}
	reg, err := NewCapabilityRegistry(caps...)
	if err != nil {
		t.Fatalf("NewCapabilityRegistry: %v", err)
	}
	return reg
}

func TestCapabilityRegistry_Construction(t *testing.T) {
	reg := buildPhase1Registry(t)
	list := reg.List()
	if len(list) != 8 {
		t.Fatalf("List() returned %d entries, want 8", len(list))
	}
}

func TestCapabilityRegistry_LookupAllPhase1(t *testing.T) {
	reg := buildPhase1Registry(t)
	canonicals := []string{
		"filesystem.read_file@v1",
		"filesystem.write_file@v1",
		"git.clone@v1",
		"git.commit@v1",
		"git.diff@v1",
		"git.status@v1",
		"http.request@v1",
		"shell.exec@v1",
	}
	for _, canon := range canonicals {
		t.Run(canon, func(t *testing.T) {
			c, err := reg.Lookup(canon)
			if err != nil {
				t.Fatalf("Lookup(%q): %v", canon, err)
			}
			if c.Canonical() != canon {
				t.Errorf("Canonical() = %q, want %q", c.Canonical(), canon)
			}
		})
	}
}

func TestCapabilityRegistry_ListStableOrder(t *testing.T) {
	reg := buildPhase1Registry(t)
	list := reg.List()
	want := []string{
		"filesystem.read_file@v1",
		"filesystem.write_file@v1",
		"git.clone@v1",
		"git.commit@v1",
		"git.diff@v1",
		"git.status@v1",
		"http.request@v1",
		"shell.exec@v1",
	}
	if len(list) != len(want) {
		t.Fatalf("len(List()) = %d, want %d", len(list), len(want))
	}
	for i, c := range list {
		if c.Canonical() != want[i] {
			t.Errorf("List()[%d] = %q, want %q", i, c.Canonical(), want[i])
		}
	}
}

func TestCapabilityRegistry_DuplicateCapabilityErrors(t *testing.T) {
	cap1 := phase1Cap(t, "shell", "exec", "v1", false, 30*time.Second)
	cap2 := phase1Cap(t, "shell", "exec", "v1", true, 60*time.Second) // same canonical
	_, err := NewCapabilityRegistry(cap1, cap2)
	if err == nil {
		t.Fatal("expected error for duplicate canonical, got nil")
	}
}

func TestCapabilityRegistry_LookupUnknown(t *testing.T) {
	reg := buildPhase1Registry(t)
	_, err := reg.Lookup("unknown.capability@v99")
	if err == nil {
		t.Fatal("expected error for unknown canonical, got nil")
	}
	if !errors.Is(err, ErrUnknownCapability) {
		t.Errorf("errors.Is(err, ErrUnknownCapability) = false, err = %v", err)
	}
}

func TestCapabilityRegistry_FilterByAdapter(t *testing.T) {
	reg := buildPhase1Registry(t)
	gitID, _ := NewAdapterID("git")
	got := reg.FilterByAdapter(gitID)
	wantCanons := []string{
		"git.clone@v1",
		"git.commit@v1",
		"git.diff@v1",
		"git.status@v1",
	}
	if len(got) != len(wantCanons) {
		t.Fatalf("FilterByAdapter(git) returned %d entries, want %d", len(got), len(wantCanons))
	}
	for i, c := range got {
		if c.Canonical() != wantCanons[i] {
			t.Errorf("FilterByAdapter(git)[%d] = %q, want %q", i, c.Canonical(), wantCanons[i])
		}
	}
}

func TestCapabilityRegistry_EmptyRegistryListNotNil(t *testing.T) {
	reg, err := NewCapabilityRegistry()
	if err != nil {
		t.Fatalf("NewCapabilityRegistry(): %v", err)
	}
	list := reg.List()
	if list == nil {
		t.Error("List() on empty registry returned nil, want empty slice")
	}
	if len(list) != 0 {
		t.Errorf("List() on empty registry returned %d entries, want 0", len(list))
	}
}
