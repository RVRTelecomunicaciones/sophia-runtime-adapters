package chaos

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testdataPath returns the absolute path of a file inside the chaos testdata
// directory, rooted at the repo working directory regardless of where the test
// binary runs.
func testdataPath(name string) string {
	// __file__ of this test is internal/infrastructure/chaos/loader_test.go.
	// From there: ../../.. brings us to the module root.
	_, thisFile, _, _ := runtime.Caller(0)
	chaosDir := filepath.Dir(thisFile)
	return filepath.Join(chaosDir, "testdata", name)
}

// TestLoadProfile_AllowedHostPath_Parses loads the minimal-enabled fixture via
// the relative allowlist path and asserts the profile is returned correctly.
func TestLoadProfile_AllowedHostPath_Parses(t *testing.T) {
	// Relative path inside internal/infrastructure/chaos/testdata — this is an
	// allowlist root so it must succeed when resolved against the repo root.
	cat := testCatalog()
	p, err := LoadProfile("internal/infrastructure/chaos/testdata/minimal-enabled.yaml", cat)
	require.NoError(t, err)
	assert.Equal(t, "minimal-enabled", p.Name)
	assert.Equal(t, 1, p.Version)
}

// TestLoadProfile_AbsolutePathInAllowlist converts the testdata path to an
// absolute path and asserts LoadProfile accepts it.
func TestLoadProfile_AbsolutePathInAllowlist(t *testing.T) {
	cat := testCatalog()
	absPath := testdataPath("minimal-enabled.yaml")
	p, err := LoadProfile(absPath, cat)
	require.NoError(t, err)
	assert.Equal(t, "minimal-enabled", p.Name)
}

// TestLoadProfile_ParentTraversal_Rejected passes a path that contains ".."
// segments. After filepath.Clean the path may not contain ".." (Clean reduces
// them) but if it escapes the working dir it will either be caught by the
// parent-traversal guard or the allowlist check. Either rejection is acceptable.
func TestLoadProfile_ParentTraversal_Rejected(t *testing.T) {
	cat := testCatalog()
	_, err := LoadProfile("internal/infrastructure/chaos/testdata/../../../etc/passwd", cat)
	require.Error(t, err)
	msg := err.Error()
	// After Clean, "internal/infrastructure/chaos/testdata/../../../etc/passwd"
	// reduces to "internal/etc/passwd" — no leading ".." so traversal guard
	// won't fire, but the resolved absolute path is not in the allowlist.
	assert.True(t,
		strings.Contains(msg, "parent traversal") || strings.Contains(msg, "not in allowlist"),
		"error %q should mention parent traversal or not in allowlist", msg)
}

// TestLoadProfile_PathOutsideAllowlist_Rejected writes a minimal valid YAML to
// a temp directory (outside any allowlist root) and asserts LoadProfile rejects
// it with "not in allowlist".
func TestLoadProfile_PathOutsideAllowlist_Rejected(t *testing.T) {
	dir := t.TempDir()
	tmpFile := filepath.Join(dir, "profile.yaml")
	content := `version: 1
name: outside
adapters: {}
expected_outcome:
  status: success
  error_class: ""
  retry_hint: unknown
`
	require.NoError(t, os.WriteFile(tmpFile, []byte(content), 0o600))

	cat := testCatalog()
	_, err := LoadProfile(tmpFile, cat)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not in allowlist")
}

// TestLoadProfile_SymlinkEscape_Rejected creates a symlink inside the testdata
// allowlist root that points to a file in t.TempDir() (outside the allowlist).
// LoadProfile must reject it with "escapes allowlist via symlink".
func TestLoadProfile_SymlinkEscape_Rejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on Windows")
	}

	// Target: a real YAML file outside the allowlist.
	dir := t.TempDir()
	target := filepath.Join(dir, "outside.yaml")
	content := `version: 1
name: escape-target
adapters: {}
expected_outcome:
  status: success
  error_class: ""
  retry_hint: unknown
`
	require.NoError(t, os.WriteFile(target, []byte(content), 0o600))

	// Link: lives inside the testdata allowlist root so the Abs check passes.
	link := testdataPath("escape-symlink.yaml")
	// Clean up the symlink after the test regardless of outcome.
	t.Cleanup(func() { _ = os.Remove(link) })

	require.NoError(t, os.Symlink(target, link))

	cat := testCatalog()
	_, err := LoadProfile(link, cat)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "escapes allowlist via symlink")
}

// TestLoadProfile_SymlinkWithinAllowlist_OK creates a symlink inside testdata
// that points to another file in testdata. EvalSymlinks resolves within the
// allowlist so it must succeed.
func TestLoadProfile_SymlinkWithinAllowlist_OK(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on Windows")
	}

	target := testdataPath("minimal-enabled.yaml")
	link := testdataPath("internal-symlink.yaml")
	t.Cleanup(func() { _ = os.Remove(link) })

	require.NoError(t, os.Symlink(target, link))

	cat := testCatalog()
	p, err := LoadProfile(link, cat)
	require.NoError(t, err)
	assert.Equal(t, "minimal-enabled", p.Name)
}

// TestLoadProfile_NonexistentFile_Rejected passes a path that is inside the
// allowlist but does not exist on disk. EvalSymlinks returns an error for
// non-existent paths.
func TestLoadProfile_NonexistentFile_Rejected(t *testing.T) {
	cat := testCatalog()
	_, err := LoadProfile("internal/infrastructure/chaos/testdata/does-not-exist.yaml", cat)
	require.Error(t, err)
}

// TestLoadProfile_DelegatesToParseProfile_ValidationErrors loads the
// invalid-fault.yaml fixture (inside allowlist). The path guards all pass;
// the error must come from parseProfile mentioning "fault kind" or the bad
// fault name.
func TestLoadProfile_DelegatesToParseProfile_ValidationErrors(t *testing.T) {
	cat := testCatalog()
	_, err := LoadProfile("internal/infrastructure/chaos/testdata/invalid-fault.yaml", cat)
	require.Error(t, err)
	msg := err.Error()
	assert.True(t,
		strings.Contains(msg, "fault kind") || strings.Contains(msg, "not_a_real_fault"),
		"error %q should mention fault kind or not_a_real_fault", msg)
}

// TestHasParentTraversal_DirectCases exercises hasParentTraversal with explicit
// cleaned inputs. Note: the function receives already-cleaned paths so
// "foo/../bar" never appears — Clean reduces it to "bar" before this runs.
func TestHasParentTraversal_DirectCases(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"../foo", true},
		{"../../bar", true},
		{"../", true},
		// already-cleaned paths — no leading ".."
		{"foo/bar", false},
		{"bar", false}, // result of Clean("foo/../bar")
		{".", false},
		{"foo/baz/qux", false},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.want, hasParentTraversal(tc.input),
				"hasParentTraversal(%q)", tc.input)
		})
	}
}

// TestInAllowlist_DirectCases tests inAllowlist directly with absolute paths.
func TestInAllowlist_DirectCases(t *testing.T) {
	// The testdata directory is an allowlist root; compute its absolute path.
	absTestdata := testdataPath("") // e.g. /repo/internal/infrastructure/chaos/testdata
	absTestdata = filepath.Clean(absTestdata)

	nestedFile := filepath.Join(absTestdata, "minimal-enabled.yaml")

	t.Run("testdata_root_itself", func(t *testing.T) {
		assert.True(t, inAllowlist(absTestdata))
	})
	t.Run("nested_file_in_testdata", func(t *testing.T) {
		assert.True(t, inAllowlist(nestedFile))
	})
	t.Run("tmp_path_outside", func(t *testing.T) {
		assert.False(t, inAllowlist("/tmp/random-file.yaml"))
	})
	t.Run("absolute_container_root_itself", func(t *testing.T) {
		// /etc/runtime-adapters/chaos/profiles is a static allowlist root.
		assert.True(t, inAllowlist("/etc/runtime-adapters/chaos/profiles"))
	})
	t.Run("absolute_container_nested", func(t *testing.T) {
		assert.True(t, inAllowlist("/etc/runtime-adapters/chaos/profiles/ci/profile.yaml"))
	})
}
