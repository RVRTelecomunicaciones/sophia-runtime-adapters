package filesystem

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// resolveExistingPath
// ---------------------------------------------------------------------------

func TestResolveExistingPath_Empty_ReturnsError(t *testing.T) {
	c := Config{AllowedFilesystemRoots: []string{os.TempDir()}}
	_, err := c.resolveExistingPath("")
	if err == nil {
		t.Fatal("expected error for empty path, got nil")
	}
}

func TestResolveExistingPath_Relative_ReturnsError(t *testing.T) {
	c := Config{AllowedFilesystemRoots: []string{os.TempDir()}}
	_, err := c.resolveExistingPath("relative/path")
	if err == nil {
		t.Fatal("expected error for relative path, got nil")
	}
}

func TestResolveExistingPath_OutsideAllowlist_ReturnsError(t *testing.T) {
	tmp := t.TempDir()
	c := Config{AllowedFilesystemRoots: []string{tmp}}
	_, err := c.resolveExistingPath("/etc")
	if err == nil {
		t.Fatal("expected error for path outside allowlist, got nil")
	}
}

func TestResolveExistingPath_ExactRoot_ReturnsAbsPath(t *testing.T) {
	tmp := t.TempDir()
	c := Config{AllowedFilesystemRoots: []string{tmp}}
	got, err := c.resolveExistingPath(tmp)
	if err != nil {
		t.Fatalf("resolveExistingPath(%q): %v", tmp, err)
	}
	want, _ := filepath.EvalSymlinks(tmp)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveExistingPath_SubdirWithinRoot_OK(t *testing.T) {
	tmp := t.TempDir()
	sub := filepath.Join(tmp, "subdir")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	c := Config{AllowedFilesystemRoots: []string{tmp}}
	got, err := c.resolveExistingPath(sub)
	if err != nil {
		t.Fatalf("resolveExistingPath(%q): %v", sub, err)
	}
	want, _ := filepath.EvalSymlinks(sub)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveExistingPath_EmptyAllowlist_ReturnsError(t *testing.T) {
	tmp := t.TempDir()
	c := Config{AllowedFilesystemRoots: []string{}}
	_, err := c.resolveExistingPath(tmp)
	if err == nil {
		t.Fatal("expected error for empty allowlist, got nil")
	}
}

func TestResolveExistingPath_MultipleRoots_MatchesCorrect(t *testing.T) {
	tmp := t.TempDir()
	sub := filepath.Join(tmp, "multi_root_subdir")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	c := Config{AllowedFilesystemRoots: []string{"/nonexistent_xyz_abc", tmp}}
	got, err := c.resolveExistingPath(sub)
	if err != nil {
		t.Fatalf("resolveExistingPath(%q): %v", sub, err)
	}
	want, _ := filepath.EvalSymlinks(sub)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveExistingPath_NonExistentFile_ReturnsError(t *testing.T) {
	tmp := t.TempDir()
	c := Config{AllowedFilesystemRoots: []string{tmp}}
	_, err := c.resolveExistingPath(filepath.Join(tmp, "does_not_exist.txt"))
	if err == nil {
		t.Fatal("expected error for non-existent path, got nil")
	}
}

// ---------------------------------------------------------------------------
// resolveTargetPath
// ---------------------------------------------------------------------------

func TestResolveTargetPath_Empty_ReturnsError(t *testing.T) {
	c := Config{AllowedFilesystemRoots: []string{os.TempDir()}}
	_, err := c.resolveTargetPath("")
	if err == nil {
		t.Fatal("expected error for empty path, got nil")
	}
}

func TestResolveTargetPath_Relative_ReturnsError(t *testing.T) {
	c := Config{AllowedFilesystemRoots: []string{os.TempDir()}}
	_, err := c.resolveTargetPath("relative/path")
	if err == nil {
		t.Fatal("expected error for relative path, got nil")
	}
}

func TestResolveTargetPath_OutsideAllowlist_ReturnsError(t *testing.T) {
	tmp := t.TempDir()
	c := Config{AllowedFilesystemRoots: []string{tmp}}
	_, err := c.resolveTargetPath("/etc/passwd")
	if err == nil {
		t.Fatal("expected error for path outside allowlist, got nil")
	}
}

func TestResolveTargetPath_ValidNewFile_ReturnsJoinedPath(t *testing.T) {
	tmp := t.TempDir()
	c := Config{AllowedFilesystemRoots: []string{tmp}}
	target := filepath.Join(tmp, "newfile.txt")
	got, err := c.resolveTargetPath(target)
	if err != nil {
		t.Fatalf("resolveTargetPath(%q): %v", target, err)
	}
	// The resolved path should be within tmp.
	if got == "" {
		t.Error("resolveTargetPath returned empty string")
	}
}

// ---------------------------------------------------------------------------
// checkInRoots
// ---------------------------------------------------------------------------

func TestCheckInRoots_EmptyList_ReturnsError(t *testing.T) {
	c := Config{AllowedFilesystemRoots: []string{}}
	_, err := c.checkInRoots("/some/path")
	if err == nil {
		t.Fatal("expected error for empty roots, got nil")
	}
}

func TestCheckInRoots_ExactMatch_ReturnsPath(t *testing.T) {
	tmp := t.TempDir()
	real, _ := filepath.EvalSymlinks(tmp)
	c := Config{AllowedFilesystemRoots: []string{tmp}}
	got, err := c.checkInRoots(real)
	if err != nil {
		t.Fatalf("checkInRoots: %v", err)
	}
	if got != real {
		t.Errorf("got %q, want %q", got, real)
	}
}
