package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
)

// ---------------------------------------------------------------------------
// commit-specific helpers
// ---------------------------------------------------------------------------

// capCommit returns the git.commit@v1 capability from adapter a.
func capCommit(t *testing.T, a *Adapter) valueobjects.Capability {
	t.Helper()
	return capByName(t, a, "commit")
}

// mustNewRepoWithCommit creates a git repo with one initial commit
// (README.md) and returns the repo path. Subsequent commit tests use
// this as their starting state.
func mustNewRepoWithCommit(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	fp := filepath.Join(dir, "README.md")
	if err := os.WriteFile(fp, []byte("initial content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("README.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("initial", &gogit.CommitOptions{
		Author: &object.Signature{Name: "Setup", Email: "setup@test.com", When: time.Now()},
	}); err != nil {
		t.Fatal(err)
	}
	return dir
}

// headSHA returns the current HEAD commit SHA for the repo at dir.
func headSHA(t *testing.T, dir string) string {
	t.Helper()
	repo, err := gogit.PlainOpen(dir)
	if err != nil {
		t.Fatalf("headSHA: open repo: %v", err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatalf("headSHA: Head: %v", err)
	}
	return head.Hash().String()
}

// ---------------------------------------------------------------------------
// Tests: commit() — payload decode / validation
// ---------------------------------------------------------------------------

// Scenario 1: valid payload on clean repo with allow_empty — happy path decode.
func TestCommit_DecodePayload_HappyPath(t *testing.T) {
	repoPath := mustNewRepoWithCommit(t)
	a := newAdapterAllowingDir(t, repoPath)

	raw, err := a.commit(context.Background(), capCommit(t, a),
		mustPayloadFrom(t, CommitPayload{
			RepoPath:    repoPath,
			Message:     "test: happy path",
			AuthorName:  "Tester",
			AuthorEmail: "tester@example.com",
			AllowEmpty:  true,
		}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*commitRaw)
	if r.validation != "" {
		t.Errorf("unexpected validation error: %s", r.validation)
	}
}

// Scenario 2: unknown field in payload → validation failure.
func TestCommit_DecodePayload_RejectsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	a := newAdapterAllowingDir(t, dir)

	raw, err := a.commit(context.Background(), capCommit(t, a),
		mustPayloadFrom(t, map[string]interface{}{
			"repo_path":     filepath.Join(dir, "repo"),
			"message":       "test",
			"author_name":   "A",
			"author_email":  "a@b.com",
			"unknown_field": true,
		}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*commitRaw)
	if r.validation == "" {
		t.Error("expected validation error for unknown field, got none")
	}
}

// Scenario 3: empty repo_path → validation failure.
func TestCommit_EmptyRepoPath_ValidationFailure(t *testing.T) {
	dir := t.TempDir()
	a := newAdapterAllowingDir(t, dir)

	raw, err := a.commit(context.Background(), capCommit(t, a),
		mustPayloadFrom(t, CommitPayload{
			RepoPath:    "",
			Message:     "test",
			AuthorName:  "A",
			AuthorEmail: "a@b.com",
		}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*commitRaw)
	if r.validation == "" {
		t.Error("expected validation error for empty repo_path, got none")
	}
}

// Scenario 4: empty message → validation failure.
func TestCommit_EmptyMessage_ValidationFailure(t *testing.T) {
	dir := t.TempDir()
	a := newAdapterAllowingDir(t, dir)

	raw, err := a.commit(context.Background(), capCommit(t, a),
		mustPayloadFrom(t, CommitPayload{
			RepoPath:    dir,
			Message:     "",
			AuthorName:  "A",
			AuthorEmail: "a@b.com",
		}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*commitRaw)
	if r.validation == "" {
		t.Error("expected validation error for empty message, got none")
	}
}

// Scenario 5: empty author_name → validation failure.
func TestCommit_EmptyAuthorName_ValidationFailure(t *testing.T) {
	dir := t.TempDir()
	a := newAdapterAllowingDir(t, dir)

	raw, err := a.commit(context.Background(), capCommit(t, a),
		mustPayloadFrom(t, CommitPayload{
			RepoPath:    dir,
			Message:     "test",
			AuthorName:  "",
			AuthorEmail: "a@b.com",
		}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*commitRaw)
	if r.validation == "" {
		t.Error("expected validation error for empty author_name, got none")
	}
}

// Scenario 6: empty author_email → validation failure.
func TestCommit_EmptyAuthorEmail_ValidationFailure(t *testing.T) {
	dir := t.TempDir()
	a := newAdapterAllowingDir(t, dir)

	raw, err := a.commit(context.Background(), capCommit(t, a),
		mustPayloadFrom(t, CommitPayload{
			RepoPath:    dir,
			Message:     "test",
			AuthorName:  "A",
			AuthorEmail: "",
		}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*commitRaw)
	if r.validation == "" {
		t.Error("expected validation error for empty author_email, got none")
	}
}

// Scenario 7: repo_path outside allowlist → validation failure.
func TestCommit_RepoPathOutsideAllowlist_ValidationFailure(t *testing.T) {
	allowed := t.TempDir()
	outside := t.TempDir()
	a := newAdapterAllowingDir(t, allowed)

	raw, err := a.commit(context.Background(), capCommit(t, a),
		mustPayloadFrom(t, CommitPayload{
			RepoPath:    outside,
			Message:     "test",
			AuthorName:  "A",
			AuthorEmail: "a@b.com",
		}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*commitRaw)
	if r.validation == "" {
		t.Error("expected validation error for path outside allowlist, got none")
	}
}

// Scenario 8: non-repo dir → runErr (open repo fails).
func TestCommit_NonRepoDir_RunErr(t *testing.T) {
	dir := t.TempDir()
	a := newAdapterAllowingDir(t, dir)

	raw, err := a.commit(context.Background(), capCommit(t, a),
		mustPayloadFrom(t, CommitPayload{
			RepoPath:    dir,
			Message:     "test",
			AuthorName:  "A",
			AuthorEmail: "a@b.com",
		}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*commitRaw)
	if r.runErr == nil {
		t.Error("expected runErr for non-repo dir, got nil")
	}
}

// Scenario 9: successful commit on dirty repo (no Paths → stage all tracked).
func TestCommit_Success_DirtyRepo_NoPathsArg(t *testing.T) {
	repoPath := mustNewRepoWithCommit(t)
	a := newAdapterAllowingDir(t, repoPath)

	// Modify the tracked file to make the repo dirty.
	fp := filepath.Join(repoPath, "README.md")
	if err := os.WriteFile(fp, []byte("modified content\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	prevSHA := headSHA(t, repoPath)

	raw, err := a.commit(context.Background(), capCommit(t, a),
		mustPayloadFrom(t, CommitPayload{
			RepoPath:    repoPath,
			Message:     "feat: update readme",
			AuthorName:  "Alice",
			AuthorEmail: "alice@example.com",
		}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*commitRaw)
	if r.validation != "" {
		t.Fatalf("unexpected validation: %s", r.validation)
	}
	if r.runErr != nil {
		t.Fatalf("unexpected runErr: %v", r.runErr)
	}
	if r.commitSHA == "" {
		t.Error("commitSHA should be non-empty on success")
	}
	if r.commitSHA == prevSHA {
		t.Errorf("HEAD should have advanced; both SHA = %s", r.commitSHA)
	}
	if r.branch == "" {
		t.Error("branch should be non-empty on success")
	}
	// Confirm HEAD actually advanced.
	newSHA := headSHA(t, repoPath)
	if newSHA != r.commitSHA {
		t.Errorf("HEAD SHA = %s, raw.commitSHA = %s — mismatch", newSHA, r.commitSHA)
	}
}

// Scenario 10: successful commit on specific Paths.
func TestCommit_Success_SpecificPaths(t *testing.T) {
	repoPath := mustNewRepoWithCommit(t)
	a := newAdapterAllowingDir(t, repoPath)

	// Create a new tracked file by adding it to the index manually via go-git,
	// but we do it by writing the file and then passing its path to the adapter.
	newFile := filepath.Join(repoPath, "NEW.md")
	if err := os.WriteFile(newFile, []byte("new file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	raw, err := a.commit(context.Background(), capCommit(t, a),
		mustPayloadFrom(t, CommitPayload{
			RepoPath:    repoPath,
			Message:     "feat: add new file",
			AuthorName:  "Bob",
			AuthorEmail: "bob@example.com",
			Paths:       []string{"NEW.md"},
		}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*commitRaw)
	if r.validation != "" {
		t.Fatalf("unexpected validation: %s", r.validation)
	}
	if r.runErr != nil {
		t.Fatalf("unexpected runErr: %v", r.runErr)
	}
	if r.commitSHA == "" {
		t.Error("commitSHA should be non-empty on success")
	}
	if r.branch == "" {
		t.Error("branch should be non-empty on success")
	}
}

// Scenario 11: empty commit rejected without allow_empty.
func TestCommit_EmptyCommit_RejectedWithoutAllowEmpty(t *testing.T) {
	repoPath := mustNewRepoWithCommit(t)
	a := newAdapterAllowingDir(t, repoPath)

	// Repo is clean (no staged or unstaged changes).
	raw, err := a.commit(context.Background(), capCommit(t, a),
		mustPayloadFrom(t, CommitPayload{
			RepoPath:    repoPath,
			Message:     "should fail: nothing to commit",
			AuthorName:  "Alice",
			AuthorEmail: "alice@example.com",
			AllowEmpty:  false,
		}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*commitRaw)
	if r.runErr == nil {
		t.Error("expected runErr for empty commit without allow_empty, got nil")
	}
	// The error message should mention allow_empty.
	if r.runErr != nil && r.commitSHA != "" {
		t.Error("commitSHA should be empty when commit was rejected")
	}
}

// Scenario 12: empty commit accepted with allow_empty=true.
func TestCommit_EmptyCommit_AcceptedWithAllowEmpty(t *testing.T) {
	repoPath := mustNewRepoWithCommit(t)
	a := newAdapterAllowingDir(t, repoPath)

	prevSHA := headSHA(t, repoPath)

	// Repo is clean but allow_empty=true.
	raw, err := a.commit(context.Background(), capCommit(t, a),
		mustPayloadFrom(t, CommitPayload{
			RepoPath:    repoPath,
			Message:     "chore: empty commit (allowed)",
			AuthorName:  "Alice",
			AuthorEmail: "alice@example.com",
			AllowEmpty:  true,
		}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*commitRaw)
	if r.validation != "" {
		t.Fatalf("unexpected validation: %s", r.validation)
	}
	if r.runErr != nil {
		t.Fatalf("unexpected runErr: %v", r.runErr)
	}
	if r.commitSHA == "" {
		t.Error("commitSHA should be non-empty on success")
	}
	if r.commitSHA == prevSHA {
		t.Errorf("HEAD should have advanced from empty commit; SHA unchanged = %s", r.commitSHA)
	}
}

// Scenario 13: cancelled ctx → ctxErr set (checked post-validation).
func TestCommit_CancelledCtx_CtxErr(t *testing.T) {
	repoPath := mustNewRepoWithCommit(t)
	a := newAdapterAllowingDir(t, repoPath)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the call

	raw, err := a.commit(ctx, capCommit(t, a),
		mustPayloadFrom(t, CommitPayload{
			RepoPath:    repoPath,
			Message:     "test",
			AuthorName:  "Alice",
			AuthorEmail: "alice@example.com",
		}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*commitRaw)
	if r.ctxErr == nil {
		t.Error("ctxErr should be set for pre-cancelled ctx")
	}
}

// ---------------------------------------------------------------------------
// Tests: NormalizeCommit — all branches
// ---------------------------------------------------------------------------

// Scenario 14a: ctx DeadlineExceeded → timeout.
func TestNormalizeCommit_CtxDeadlineExceeded(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	raw := &commitRaw{ctxErr: context.DeadlineExceeded, durationMs: 10}
	result, err := a.NormalizeCommit(capCommit(t, a), raw, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != valueobjects.StatusTimeout {
		t.Errorf("status = %q, want timeout", result.Status)
	}
	if result.ErrorClass != valueobjects.ErrTimeout {
		t.Errorf("error_class = %q, want timeout", result.ErrorClass)
	}
}

// Scenario 14b: ctx Cancelled → cancelled.
func TestNormalizeCommit_CtxCancelled(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	raw := &commitRaw{ctxErr: ctx.Err(), durationMs: 5}
	result, err := a.NormalizeCommit(capCommit(t, a), raw, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != valueobjects.StatusCancelled {
		t.Errorf("status = %q, want cancelled", result.Status)
	}
	if result.ErrorClass != valueobjects.ErrCancelled {
		t.Errorf("error_class = %q, want cancelled", result.ErrorClass)
	}
}

// Scenario 14c: validation error → failure + ErrValidationFailure.
func TestNormalizeCommit_ValidationError(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	raw := &commitRaw{validation: "repo_path is required", durationMs: 1}
	result, err := a.NormalizeCommit(capCommit(t, a), raw, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != valueobjects.StatusFailure {
		t.Errorf("status = %q, want failure", result.Status)
	}
	if result.ErrorClass != valueobjects.ErrValidationFailure {
		t.Errorf("error_class = %q, want validation_failure", result.ErrorClass)
	}
}

// Scenario 14d: runErr → failure + ErrExternalFailure.
func TestNormalizeCommit_RunErr(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	raw := &commitRaw{runErr: gogit.ErrRepositoryNotExists, durationMs: 3}
	result, err := a.NormalizeCommit(capCommit(t, a), raw, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != valueobjects.StatusFailure {
		t.Errorf("status = %q, want failure", result.Status)
	}
	if result.ErrorClass != valueobjects.ErrExternalFailure {
		t.Errorf("error_class = %q, want external_failure", result.ErrorClass)
	}
}

// Scenario 14e: success → 2 artifacts (git_commit + git_ref), meta populated.
func TestNormalizeCommit_Success_TwoArtifacts(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	raw := &commitRaw{
		commitSHA:  "abc123def456abc123def456abc123def456abc1",
		branch:     "main",
		durationMs: 20,
	}
	result, err := a.NormalizeCommit(capCommit(t, a), raw, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != valueobjects.StatusSuccess {
		t.Errorf("status = %q, want success", result.Status)
	}
	if len(result.Artifacts) != 2 {
		t.Errorf("len(Artifacts) = %d, want 2 (git_commit + git_ref)", len(result.Artifacts))
	}
	if result.AdapterMeta["commit_sha"] != raw.commitSHA {
		t.Errorf("meta[commit_sha] = %q, want %q", result.AdapterMeta["commit_sha"], raw.commitSHA)
	}
	if result.AdapterMeta["branch"] != "main" {
		t.Errorf("meta[branch] = %q, want main", result.AdapterMeta["branch"])
	}
}

// Scenario 14f: success with empty branch → only git_commit artifact (no git_ref).
func TestNormalizeCommit_Success_NoBranchArtifact(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	raw := &commitRaw{
		commitSHA:  "abc123def456abc123def456abc123def456abc1",
		branch:     "", // detached HEAD or unreadable
		durationMs: 5,
	}
	result, err := a.NormalizeCommit(capCommit(t, a), raw, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != valueobjects.StatusSuccess {
		t.Errorf("status = %q, want success", result.Status)
	}
	if len(result.Artifacts) != 1 {
		t.Errorf("len(Artifacts) = %d, want 1 (git_commit only, no branch)", len(result.Artifacts))
	}
}

// Scenario 14g: wrong raw type → error returned.
func TestNormalizeCommit_WrongRawType_ReturnsError(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	_, err := a.NormalizeCommit(capCommit(t, a), &statusRaw{}, clk)
	if err == nil {
		t.Error("expected error for wrong raw type, got nil")
	}
}

// ---------------------------------------------------------------------------
// Integration: commit() + NormalizeCommit together
// ---------------------------------------------------------------------------

// Full integration: dirty repo → commit → status=success, 2 artifacts,
// meta commit_sha and branch populated.
func TestCommit_Integration_DirtyRepo_Success(t *testing.T) {
	repoPath := mustNewRepoWithCommit(t)
	a := newAdapterAllowingDir(t, repoPath)

	// Dirty the repo.
	fp := filepath.Join(repoPath, "README.md")
	if err := os.WriteFile(fp, []byte("integration test\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	raw, err := a.commit(context.Background(), capCommit(t, a),
		mustPayloadFrom(t, CommitPayload{
			RepoPath:    repoPath,
			Message:     "feat: integration commit",
			AuthorName:  "Integration",
			AuthorEmail: "integration@example.com",
		}))
	if err != nil {
		t.Fatalf("commit: unexpected hard error: %v", err)
	}
	clk := &shared.FakeClock{T: time.Now()}
	result, normErr := a.NormalizeCommit(capCommit(t, a), raw, clk)
	if normErr != nil {
		t.Fatalf("NormalizeCommit: %v", normErr)
	}
	if result.Status != valueobjects.StatusSuccess {
		t.Errorf("status = %q, want success; errorMsg = %s", result.Status, result.ErrorMessage)
	}
	if len(result.Artifacts) != 2 {
		t.Errorf("len(Artifacts) = %d, want 2 (git_commit + git_ref)", len(result.Artifacts))
	}
	if result.AdapterMeta["commit_sha"] == "" {
		t.Error("meta[commit_sha] should be non-empty")
	}
	if result.AdapterMeta["branch"] == "" {
		t.Error("meta[branch] should be non-empty")
	}
}
