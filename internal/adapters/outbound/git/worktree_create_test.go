package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
)

// capWorktreeCreate returns the git.worktree.create@v1 capability from adapter a.
func capWorktreeCreate(t *testing.T, a *Adapter) valueobjects.Capability {
	t.Helper()
	return capByName(t, a, "worktree.create")
}

// ---------------------------------------------------------------------------
// Tests: worktreeCreate() — payload decode / validation
// ---------------------------------------------------------------------------

func TestWorktreeCreate_DecodePayload_RejectsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	a := newAdapterAllowingDir(t, dir)

	raw, err := a.worktreeCreate(context.Background(), capWorktreeCreate(t, a),
		mustPayloadFrom(t, map[string]interface{}{
			"path":          filepath.Join(dir, "dest"),
			"upstream_path": filepath.Join(dir, "upstream"),
			"base_ref":      "HEAD",
			"unknown_field": true,
		}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*worktreeCreateRaw)
	if r.validation == "" {
		t.Error("expected validation error for unknown field, got none")
	}
}

func TestWorktreeCreate_EmptyPath_ValidationFailure(t *testing.T) {
	dir := t.TempDir()
	a := newAdapterAllowingDir(t, dir)
	raw, err := a.worktreeCreate(context.Background(), capWorktreeCreate(t, a),
		mustPayloadFrom(t, WorktreeCreatePayload{UpstreamPath: dir, BaseRef: "HEAD"}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	if got := raw.(*worktreeCreateRaw).validation; got != "path is required" {
		t.Errorf("validation = %q, want %q", got, "path is required")
	}
}

func TestWorktreeCreate_EmptyUpstream_ValidationFailure(t *testing.T) {
	dir := t.TempDir()
	a := newAdapterAllowingDir(t, dir)
	raw, err := a.worktreeCreate(context.Background(), capWorktreeCreate(t, a),
		mustPayloadFrom(t, WorktreeCreatePayload{Path: filepath.Join(dir, "dest"), BaseRef: "HEAD"}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	if got := raw.(*worktreeCreateRaw).validation; got != "upstream_path is required" {
		t.Errorf("validation = %q, want %q", got, "upstream_path is required")
	}
}

func TestWorktreeCreate_EmptyBaseRef_ValidationFailure(t *testing.T) {
	dir := t.TempDir()
	a := newAdapterAllowingDir(t, dir)
	raw, err := a.worktreeCreate(context.Background(), capWorktreeCreate(t, a),
		mustPayloadFrom(t, WorktreeCreatePayload{Path: filepath.Join(dir, "dest"), UpstreamPath: dir}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	if got := raw.(*worktreeCreateRaw).validation; got != "base_ref is required" {
		t.Errorf("validation = %q, want %q", got, "base_ref is required")
	}
}

func TestWorktreeCreate_PathOutsideAllowlist_ValidationFailure(t *testing.T) {
	allowed := t.TempDir()
	upstream := mustNewRepoWithCommit(t) // upstream lives elsewhere
	// build an adapter whose allowlist covers ONLY `allowed` so the
	// upstream resolveProtect path also fails — but we still want to
	// assert the FIRST path (Path) failed validation.
	a := newAdapterAllowingDir(t, allowed)
	_ = upstream // unused (path validation fails before upstream is checked)

	raw, err := a.worktreeCreate(context.Background(), capWorktreeCreate(t, a),
		mustPayloadFrom(t, WorktreeCreatePayload{
			Path:         "/etc/wt", // outside allowlist
			UpstreamPath: upstream,
			BaseRef:      "HEAD",
		}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*worktreeCreateRaw)
	if r.validation == "" {
		t.Error("expected validation error for outside-allowlist path")
	}
}

func TestWorktreeCreate_UpstreamOutsideAllowlist_ValidationFailure(t *testing.T) {
	allowed := t.TempDir()
	a := newAdapterAllowingDir(t, allowed)
	raw, err := a.worktreeCreate(context.Background(), capWorktreeCreate(t, a),
		mustPayloadFrom(t, WorktreeCreatePayload{
			Path:         filepath.Join(allowed, "dest"),
			UpstreamPath: "/etc",
			BaseRef:      "HEAD",
		}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	if r := raw.(*worktreeCreateRaw); r.validation == "" {
		t.Error("expected validation error for outside-allowlist upstream")
	}
}

func TestWorktreeCreate_DestExistsAndNonEmpty_ValidationFailure(t *testing.T) {
	root := t.TempDir()
	upstream := mustNewRepoWithCommitUnder(t, root)
	dest := filepath.Join(root, "dest")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "stale.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := newAdapterAllowingDir(t, root)
	raw, err := a.worktreeCreate(context.Background(), capWorktreeCreate(t, a),
		mustPayloadFrom(t, WorktreeCreatePayload{Path: dest, UpstreamPath: upstream, BaseRef: "HEAD"}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	if r := raw.(*worktreeCreateRaw); r.validation == "" {
		t.Error("expected validation error for non-empty dest")
	}
}

func TestWorktreeCreate_UpstreamNotARepo_RunError(t *testing.T) {
	root := t.TempDir()
	fakeUpstream := filepath.Join(root, "not-a-repo")
	if err := os.MkdirAll(fakeUpstream, 0o755); err != nil {
		t.Fatal(err)
	}
	a := newAdapterAllowingDir(t, root)
	raw, err := a.worktreeCreate(context.Background(), capWorktreeCreate(t, a),
		mustPayloadFrom(t, WorktreeCreatePayload{
			Path:         filepath.Join(root, "dest"),
			UpstreamPath: fakeUpstream,
			BaseRef:      "HEAD",
		}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*worktreeCreateRaw)
	if r.runErr == nil {
		t.Error("expected runErr for non-repo upstream")
	}
}

func TestWorktreeCreate_BaseRefNotFound_ValidationFailure(t *testing.T) {
	root := t.TempDir()
	upstream := mustNewRepoWithCommitUnder(t, root)
	a := newAdapterAllowingDir(t, root)
	raw, err := a.worktreeCreate(context.Background(), capWorktreeCreate(t, a),
		mustPayloadFrom(t, WorktreeCreatePayload{
			Path:         filepath.Join(root, "dest"),
			UpstreamPath: upstream,
			BaseRef:      "no-such-branch-or-sha",
		}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	if r := raw.(*worktreeCreateRaw); r.validation == "" {
		t.Error("expected validation error for unresolvable base_ref")
	}
}

// ---------------------------------------------------------------------------
// Tests: worktreeCreate() — successful provisioning
// ---------------------------------------------------------------------------

func TestWorktreeCreate_Success_DetachedHEAD(t *testing.T) {
	root := t.TempDir()
	upstream := mustNewRepoWithCommitUnder(t, root)
	dest := filepath.Join(root, "wt-detached")
	a := newAdapterAllowingDir(t, root)

	raw, err := a.worktreeCreate(context.Background(), capWorktreeCreate(t, a),
		mustPayloadFrom(t, WorktreeCreatePayload{
			Path: dest, UpstreamPath: upstream, BaseRef: "HEAD",
		}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*worktreeCreateRaw)
	if r.validation != "" {
		t.Fatalf("unexpected validation error: %s", r.validation)
	}
	if r.runErr != nil {
		t.Fatalf("unexpected runErr: %v", r.runErr)
	}
	if r.worktreePath != dest {
		t.Errorf("worktreePath = %q, want %q", r.worktreePath, dest)
	}
	if r.branch != "" {
		t.Errorf("branch = %q, want empty (detached)", r.branch)
	}
	if r.headSHA == "" {
		t.Error("headSHA must be set on success")
	}
	// README.md from the upstream must be present in the worktree.
	if _, err := os.Stat(filepath.Join(dest, "README.md")); err != nil {
		t.Errorf("README.md missing from worktree: %v", err)
	}
}

func TestWorktreeCreate_Success_WithBranch(t *testing.T) {
	root := t.TempDir()
	upstream := mustNewRepoWithCommitUnder(t, root)
	upstreamHead := headSHA(t, upstream)
	dest := filepath.Join(root, "wt-branched")
	a := newAdapterAllowingDir(t, root)

	raw, err := a.worktreeCreate(context.Background(), capWorktreeCreate(t, a),
		mustPayloadFrom(t, WorktreeCreatePayload{
			Path: dest, UpstreamPath: upstream, BaseRef: "HEAD", Branch: "feature/x",
		}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*worktreeCreateRaw)
	if r.validation != "" || r.runErr != nil {
		t.Fatalf("unexpected error: validation=%q runErr=%v", r.validation, r.runErr)
	}
	if r.branch != "feature/x" {
		t.Errorf("branch = %q, want %q", r.branch, "feature/x")
	}
	if r.headSHA != upstreamHead {
		t.Errorf("headSHA = %q, want %q (BaseRef=HEAD)", r.headSHA, upstreamHead)
	}
	// Verify branch ref landed on disk.
	if _, err := os.Stat(filepath.Join(dest, ".git/refs/heads/feature/x")); err != nil {
		t.Errorf("branch ref not on disk: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Tests: NormalizeWorktreeCreate
// ---------------------------------------------------------------------------

func TestNormalizeWorktreeCreate_WrongRawType_ReturnsError(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	if _, err := a.NormalizeWorktreeCreate(capWorktreeCreate(t, a), &statusRaw{}, clk); err == nil {
		t.Error("expected error for wrong raw type")
	}
}

func TestNormalizeWorktreeCreate_CtxDeadlineExceeded(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	raw := &worktreeCreateRaw{ctxErr: context.DeadlineExceeded, durationMs: 100}
	result, err := a.NormalizeWorktreeCreate(capWorktreeCreate(t, a), raw, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != valueobjects.StatusTimeout {
		t.Errorf("status = %v, want timeout", result.Status)
	}
}

func TestNormalizeWorktreeCreate_CtxCancelled(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	raw := &worktreeCreateRaw{ctxErr: context.Canceled, durationMs: 50}
	result, err := a.NormalizeWorktreeCreate(capWorktreeCreate(t, a), raw, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != valueobjects.StatusCancelled {
		t.Errorf("status = %v, want cancelled", result.Status)
	}
}

func TestNormalizeWorktreeCreate_ValidationError(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	raw := &worktreeCreateRaw{validation: "path is required", durationMs: 10}
	result, err := a.NormalizeWorktreeCreate(capWorktreeCreate(t, a), raw, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != valueobjects.StatusFailure {
		t.Errorf("status = %v, want failure", result.Status)
	}
	if result.ErrorClass != valueobjects.ErrValidationFailure {
		t.Errorf("error_class = %v, want validation_failure", result.ErrorClass)
	}
}

func TestNormalizeWorktreeCreate_RunErr(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	raw := &worktreeCreateRaw{runErr: errors.New("boom"), durationMs: 200}
	result, err := a.NormalizeWorktreeCreate(capWorktreeCreate(t, a), raw, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != valueobjects.StatusFailure {
		t.Errorf("status = %v, want failure", result.Status)
	}
}

func TestNormalizeWorktreeCreate_Success_DetachedHEAD_TwoArtifacts(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	raw := &worktreeCreateRaw{
		worktreePath: "/tmp/wt",
		headSHA:      "abc123",
		durationMs:   500,
	}
	result, err := a.NormalizeWorktreeCreate(capWorktreeCreate(t, a), raw, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != valueobjects.StatusSuccess {
		t.Errorf("status = %v, want success", result.Status)
	}
	if got := len(result.Artifacts); got != 2 {
		t.Errorf("len(artifacts) = %d, want 2 (directory + commit)", got)
	}
}

func TestNormalizeWorktreeCreate_Success_WithBranch_ThreeArtifacts(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	raw := &worktreeCreateRaw{
		worktreePath: "/tmp/wt",
		branch:       "feature/x",
		headSHA:      "abc123",
		durationMs:   500,
	}
	result, err := a.NormalizeWorktreeCreate(capWorktreeCreate(t, a), raw, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := len(result.Artifacts); got != 3 {
		t.Errorf("len(artifacts) = %d, want 3 (directory + commit + ref)", got)
	}
	// Verify artifact types.
	types := map[entities.ArtifactType]bool{}
	for _, art := range result.Artifacts {
		types[art.Type] = true
	}
	for _, want := range []entities.ArtifactType{
		entities.ArtifactDirectory,
		entities.ArtifactGitCommit,
		entities.ArtifactGitRef,
	} {
		if !types[want] {
			t.Errorf("missing artifact of type %q", want)
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers used by worktree.create tests
// ---------------------------------------------------------------------------

// mustNewRepoWithCommitUnder mirrors mustNewRepoWithCommit but creates the
// repo under the given root (so callers can keep both the upstream and the
// destination under a single allowlist root).
func mustNewRepoWithCommitUnder(t *testing.T, root string) string {
	t.Helper()
	repoPath := filepath.Join(root, "upstream")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}
	repo, err := gogit.PlainInit(repoPath, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	fp := filepath.Join(repoPath, "README.md")
	if err := os.WriteFile(fp, []byte("initial content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("README.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("initial", &gogit.CommitOptions{
		Author: &object.Signature{Name: "Test", Email: "test@example.com", When: time.Now()},
	}); err != nil {
		t.Fatal(err)
	}
	return repoPath
}
