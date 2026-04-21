package git_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/sophia-ecosystem/runtime-adapters/internal/adapters/outbound/git"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
	"github.com/sophia-ecosystem/runtime-adapters/test/contract"
)

func TestGitAdapter_ContractSuite(t *testing.T) {
	// Build a real on-the-fly repo so the "valid" payloads have a target
	// path that resolves and yields a real outcome (the suite's cancelled-
	// ctx and deadline-exceeded sub-tests don't require the repo to exist
	// — they trip ctx checks before adapter logic — but the repo helps
	// sanity-test the dispatcher).
	repoDir := t.TempDir()
	mustInitContractRepo(t, repoDir)

	tmpRoot := os.TempDir()
	a, err := git.NewAdapter(git.Config{
		AllowedWorkingDirs: []string{tmpRoot},
		SSHAgent:           false, // keep auth surface simple for the suite
		InlineStreamLimit:  16 * 1024,
	}, &shared.FakeClock{T: time.Now()})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}

	// destPath is INSIDE tmpRoot (so allowlist accepts) but doesn't yet
	// exist (so clone has somewhere to write its valid sample).
	destPath := filepath.Join(t.TempDir(), "clone-target")

	contract.RunContractSuite(t, a, map[string]contract.SamplePayload{
		"git.status@v1": {
			Valid: jsonRawC(t, map[string]any{"repo_path": repoDir}),
			Invalid: []json.RawMessage{
				jsonRawC(t, map[string]any{}),                                // missing repo_path
				jsonRawC(t, map[string]any{"repo_path": "/etc/passwd"}),      // outside allowlist
				jsonRawC(t, map[string]any{"repo_path": repoDir, "junk": 1}), // unknown field
				json.RawMessage(`not json`),
			},
		},
		"git.clone@v1": {
			Valid: jsonRawC(t, map[string]any{
				"repo_url":         "file://" + repoDir,
				"destination_path": destPath,
				"auth_mode":        "none",
			}),
			Invalid: []json.RawMessage{
				jsonRawC(t, map[string]any{}),                                            // missing repo_url
				jsonRawC(t, map[string]any{"repo_url": "x", "destination_path": "/etc"}), // outside allowlist
				jsonRawC(t, map[string]any{
					"repo_url":         "file://" + repoDir,
					"destination_path": destPath,
					"auth_mode":        "ssh-agent", // disabled in this Config
				}),
				jsonRawC(t, map[string]any{
					"repo_url":         "file://" + repoDir,
					"destination_path": destPath,
					"unknown_field":    1,
				}),
				json.RawMessage(`not json`),
			},
		},
		"git.diff@v1": {
			Valid: jsonRawC(t, map[string]any{
				"repo_path": repoDir,
				"from":      "HEAD",
				"to":        "HEAD",
			}),
			Invalid: []json.RawMessage{
				jsonRawC(t, map[string]any{}),                                                  // missing repo_path
				jsonRawC(t, map[string]any{"repo_path": repoDir}),                              // missing from/to
				jsonRawC(t, map[string]any{"repo_path": repoDir, "from": "HEAD"}),              // missing to
				jsonRawC(t, map[string]any{"repo_path": "/etc", "from": "HEAD", "to": "HEAD"}), // outside allowlist
				json.RawMessage(`not json`),
			},
		},
		"git.commit@v1": {
			Valid: jsonRawC(t, map[string]any{
				"repo_path":    repoDir,
				"message":      "test",
				"author_name":  "Suite",
				"author_email": "suite@test.local",
				"allow_empty":  true,
			}),
			Invalid: []json.RawMessage{
				jsonRawC(t, map[string]any{}),                                     // missing all
				jsonRawC(t, map[string]any{"repo_path": repoDir}),                 // missing message + author
				jsonRawC(t, map[string]any{"repo_path": repoDir, "message": "x"}), // missing author
				jsonRawC(t, map[string]any{
					"repo_path":    "/etc",
					"message":      "x",
					"author_name":  "X",
					"author_email": "x@x.local",
				}), // outside allowlist
				json.RawMessage(`not json`),
			},
		},
	})
}

// mustInitContractRepo creates a small repo with one commit so the
// contract suite's "valid sample" payloads have a real target.
func mustInitContractRepo(t *testing.T, dir string) {
	t.Helper()
	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	fp := filepath.Join(dir, "README.md")
	if err := os.WriteFile(fp, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("README.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("initial", &gogit.CommitOptions{
		Author: &object.Signature{
			Name:  "Suite",
			Email: "suite@test.local",
			When:  time.Now(),
		},
	}); err != nil {
		t.Fatal(err)
	}
}

// jsonRawC marshals a value to JSON or fails the test.
// Named with suffix C (for Contract) to avoid conflicts if this file
// is ever merged into a package that already defines jsonRaw.
func jsonRawC(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return b
}
