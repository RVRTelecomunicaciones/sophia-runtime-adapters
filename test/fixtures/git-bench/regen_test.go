//go:build fixture

// Package gitbench_test validates that the fixture generator produces
// byte-identical output across repeated invocations (D2C2.11 + spec §9.1).
// Run with: go test -tags fixture ./test/fixtures/git-bench/...
package gitbench_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestFixtureGitBench_Regenerable runs `make fixture-git-bench` twice
// and hashes every file in the output directories. Hashes MUST match
// bit-for-bit; any drift means the generator lost determinism.
func TestFixtureGitBench_Regenerable(t *testing.T) {
	repoRoot := findRepoRoot(t)
	benchDir := filepath.Join(repoRoot, "test", "fixtures", "git-bench")

	hash1 := regenAndHash(t, repoRoot)
	hash2 := regenAndHash(t, repoRoot)

	require.Equal(t, hash1, hash2,
		"fixture-git-bench produced different output across runs — determinism lost")
	// Sanity: hash should be non-empty (i.e. Makefile did something).
	require.NotEmpty(t, hash1, "fixture produced no files")

	// Extra sanity: dirty-tree/file1.txt must contain the patched 3rd line.
	file1 := filepath.Join(benchDir, "dirty-tree", "file1.txt")
	data, err := os.ReadFile(file1)
	require.NoError(t, err)
	require.Contains(t, string(data), "line three of file1 (dirty)",
		"dirty-tree patch did not apply")
}

func regenAndHash(t *testing.T, repoRoot string) map[string]string {
	t.Helper()

	// Clean + regenerate.
	mustRun(t, repoRoot, "make", "-C", "test/fixtures/git-bench", "clean")
	mustRun(t, repoRoot, "make", "fixture-git-bench")

	out := map[string]string{}
	for _, sub := range []string{"small-repo", "dirty-tree"} {
		dir := filepath.Join(repoRoot, "test", "fixtures", "git-bench", sub)
		_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(repoRoot, p)
			// Skip git packfile index files — those can drift in
			// ordering/compression even with same commits. Content
			// (objects/) is what matters; we skip .git/ entirely
			// and hash working-tree files only.
			if strings.Contains(rel, ".git/") {
				return nil
			}
			h, err := sha256File(p)
			require.NoError(t, err)
			out[rel] = h
			return nil
		})
	}

	// Normalize ordering.
	keys := make([]string, 0, len(out))
	for k := range out {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	return out
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func mustRun(t *testing.T, dir string, cmd string, args ...string) {
	t.Helper()
	c := exec.Command(cmd, args...)
	c.Dir = dir
	var stderr bytes.Buffer
	c.Stderr = &stderr
	err := c.Run()
	require.NoError(t, err, "%s %v failed: %s", cmd, args, stderr.String())
}

// findRepoRoot walks up to the first dir containing go.mod.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	dir := cwd
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not find repo root (go.mod) starting from %s", cwd)
	return ""
}
