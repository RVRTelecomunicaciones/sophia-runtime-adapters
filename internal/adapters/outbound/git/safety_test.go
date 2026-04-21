package git

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePath_Empty_ReturnsError(t *testing.T) {
	c := Config{AllowedWorkingDirs: []string{os.TempDir()}}
	_, err := c.resolvePath("")
	if err == nil {
		t.Fatal("expected error for empty path, got nil")
	}
}

func TestResolvePath_Relative_ReturnsError(t *testing.T) {
	c := Config{AllowedWorkingDirs: []string{os.TempDir()}}
	_, err := c.resolvePath("relative/path")
	if err == nil {
		t.Fatal("expected error for relative path, got nil")
	}
}

func TestResolvePath_AbsoluteOutsideAllowlist_ReturnsError(t *testing.T) {
	c := Config{AllowedWorkingDirs: []string{"/tmp/only_this_dir"}}
	_, err := c.resolvePath("/etc")
	if err == nil {
		t.Fatal("expected error for path outside allowlist, got nil")
	}
}

func TestResolvePath_ExactAllowlistRoot_ReturnsAbsPath(t *testing.T) {
	tmp := os.TempDir()
	want, err := filepath.Abs(tmp)
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}
	c := Config{AllowedWorkingDirs: []string{tmp}}
	got, err := c.resolvePath(tmp)
	if err != nil {
		t.Fatalf("resolvePath(%q): %v", tmp, err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolvePath_SubdirWithinAllowlist_ReturnsAbsPath(t *testing.T) {
	tmp := os.TempDir()
	sub := filepath.Join(tmp, "git_safety_test_subdir")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	c := Config{AllowedWorkingDirs: []string{tmp}}
	got, err := c.resolvePath(sub)
	if err != nil {
		t.Fatalf("resolvePath(%q): %v", sub, err)
	}
	wantAbs, _ := filepath.Abs(sub)
	if got != wantAbs {
		t.Errorf("got %q, want %q", got, wantAbs)
	}
}

func TestResolvePath_EmptyAllowlist_ReturnsError(t *testing.T) {
	c := Config{AllowedWorkingDirs: []string{}}
	tmp := os.TempDir()
	_, err := c.resolvePath(tmp)
	if err == nil {
		t.Fatal("expected error for empty allowlist, got nil")
	}
}

func TestResolvePath_MultipleRoots_MatchesCorrectOne(t *testing.T) {
	tmp := os.TempDir()
	sub := filepath.Join(tmp, "multi_root_subdir")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	c := Config{AllowedWorkingDirs: []string{"/nonexistent_dir_xyz", tmp}}
	got, err := c.resolvePath(sub)
	if err != nil {
		t.Fatalf("resolvePath(%q): %v", sub, err)
	}
	wantAbs, _ := filepath.Abs(sub)
	if got != wantAbs {
		t.Errorf("got %q, want %q", got, wantAbs)
	}
}
