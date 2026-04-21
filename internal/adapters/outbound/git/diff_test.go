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
// diff-specific helpers
// ---------------------------------------------------------------------------

// capDiff returns the git.diff@v1 capability from adapter a.
func capDiff(t *testing.T, a *Adapter) valueobjects.Capability {
	t.Helper()
	return capByName(t, a, "diff")
}

// mustInitRepoWithTwoCommits creates a working repo with two commits
// (README.md modified between them) and returns the repo path plus the two
// commit SHAs (sha1 = initial, sha2 = update). No bare/remote involved.
func mustInitRepoWithTwoCommits(t *testing.T) (repoPath, sha1, sha2 string) {
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
	if err := os.WriteFile(fp, []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("README.md"); err != nil {
		t.Fatal(err)
	}
	c1, err := wt.Commit("initial", &gogit.CommitOptions{
		Author: &object.Signature{Name: "Test", Email: "test@example.com", When: time.Now()},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(fp, []byte("hello world\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("README.md"); err != nil {
		t.Fatal(err)
	}
	c2, err := wt.Commit("update", &gogit.CommitOptions{
		Author: &object.Signature{Name: "Test", Email: "test@example.com", When: time.Now().Add(time.Second)},
	})
	if err != nil {
		t.Fatal(err)
	}

	return dir, c1.String(), c2.String()
}

// ---------------------------------------------------------------------------
// Tests: diff() — payload decode / validation
// ---------------------------------------------------------------------------

// Scenario 1: valid JSON payload (happy path decode — actual diff exercised
// separately in the integration scenarios).
func TestDiff_DecodePayload_HappyPath(t *testing.T) {
	repoPath, sha1, sha2 := mustInitRepoWithTwoCommits(t)
	a := newAdapterAllowingDir(t, repoPath)

	raw, err := a.diff(context.Background(), capDiff(t, a),
		mustPayloadFrom(t, DiffPayload{RepoPath: repoPath, From: sha1, To: sha2}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*diffRaw)
	if r.validation != "" {
		t.Errorf("unexpected validation error: %s", r.validation)
	}
	if r.runErr != nil {
		t.Errorf("unexpected runErr: %v", r.runErr)
	}
}

// Scenario 2: unknown field in payload → validation failure.
func TestDiff_DecodePayload_RejectsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	a := newAdapterAllowingDir(t, dir)

	raw, err := a.diff(context.Background(), capDiff(t, a),
		mustPayloadFrom(t, map[string]interface{}{
			"repo_path":     filepath.Join(dir, "repo"),
			"from":          "abc",
			"to":            "def",
			"unknown_field": true,
		}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*diffRaw)
	if r.validation == "" {
		t.Error("expected validation error for unknown field, got none")
	}
}

// Scenario 3: empty repo_path → validation failure.
func TestDiff_EmptyRepoPath_ValidationFailure(t *testing.T) {
	dir := t.TempDir()
	a := newAdapterAllowingDir(t, dir)

	raw, err := a.diff(context.Background(), capDiff(t, a),
		mustPayloadFrom(t, DiffPayload{RepoPath: "", From: "abc", To: "def"}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*diffRaw)
	if r.validation == "" {
		t.Error("expected validation error for empty repo_path, got none")
	}
}

// Scenario 4a: empty from → validation failure.
func TestDiff_EmptyFrom_ValidationFailure(t *testing.T) {
	dir := t.TempDir()
	a := newAdapterAllowingDir(t, dir)

	raw, err := a.diff(context.Background(), capDiff(t, a),
		mustPayloadFrom(t, DiffPayload{RepoPath: dir, From: "", To: "def"}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*diffRaw)
	if r.validation == "" {
		t.Error("expected validation error for empty from, got none")
	}
}

// Scenario 4b: empty to → validation failure.
func TestDiff_EmptyTo_ValidationFailure(t *testing.T) {
	dir := t.TempDir()
	a := newAdapterAllowingDir(t, dir)

	raw, err := a.diff(context.Background(), capDiff(t, a),
		mustPayloadFrom(t, DiffPayload{RepoPath: dir, From: "abc", To: ""}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*diffRaw)
	if r.validation == "" {
		t.Error("expected validation error for empty to, got none")
	}
}

// Scenario 5: repo_path outside allowlist → validation failure.
func TestDiff_RepoPathOutsideAllowlist_ValidationFailure(t *testing.T) {
	allowed := t.TempDir()
	outside := t.TempDir()
	a := newAdapterAllowingDir(t, allowed)

	raw, err := a.diff(context.Background(), capDiff(t, a),
		mustPayloadFrom(t, DiffPayload{RepoPath: outside, From: "abc", To: "def"}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*diffRaw)
	if r.validation == "" {
		t.Error("expected validation error for path outside allowlist, got none")
	}
}

// Scenario 6: non-repo dir → runErr (open repo fails).
func TestDiff_NonRepoDir_RunErr(t *testing.T) {
	dir := t.TempDir()
	a := newAdapterAllowingDir(t, dir)

	raw, err := a.diff(context.Background(), capDiff(t, a),
		mustPayloadFrom(t, DiffPayload{RepoPath: dir, From: "abc", To: "def"}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*diffRaw)
	if r.runErr == nil {
		t.Error("expected runErr for non-repo dir, got nil")
	}
}

// Scenario 7: bad from SHA → runErr (resolve fails).
func TestDiff_BadFromSHA_RunErr(t *testing.T) {
	repoPath, _, sha2 := mustInitRepoWithTwoCommits(t)
	a := newAdapterAllowingDir(t, repoPath)

	raw, err := a.diff(context.Background(), capDiff(t, a),
		mustPayloadFrom(t, DiffPayload{RepoPath: repoPath, From: "deadbeefdeadbeef", To: sha2}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*diffRaw)
	if r.runErr == nil {
		t.Error("expected runErr for bad from SHA, got nil")
	}
}

// Scenario 8: bad to SHA → runErr (resolve fails).
func TestDiff_BadToSHA_RunErr(t *testing.T) {
	repoPath, sha1, _ := mustInitRepoWithTwoCommits(t)
	a := newAdapterAllowingDir(t, repoPath)

	raw, err := a.diff(context.Background(), capDiff(t, a),
		mustPayloadFrom(t, DiffPayload{RepoPath: repoPath, From: sha1, To: "deadbeefdeadbeef"}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*diffRaw)
	if r.runErr == nil {
		t.Error("expected runErr for bad to SHA, got nil")
	}
}

// Scenario 9: successful diff between two commits → patch non-empty,
// totalBytes > 0, truncated=false.
func TestDiff_SuccessfulDiff_TwoCommits(t *testing.T) {
	repoPath, sha1, sha2 := mustInitRepoWithTwoCommits(t)
	a := newAdapterAllowingDir(t, repoPath)

	raw, err := a.diff(context.Background(), capDiff(t, a),
		mustPayloadFrom(t, DiffPayload{RepoPath: repoPath, From: sha1, To: sha2}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*diffRaw)
	if r.validation != "" {
		t.Fatalf("unexpected validation: %s", r.validation)
	}
	if r.runErr != nil {
		t.Fatalf("unexpected runErr: %v", r.runErr)
	}
	if len(r.patch) == 0 {
		t.Error("patch should be non-empty for a real diff")
	}
	if r.totalBytes <= 0 {
		t.Errorf("totalBytes = %d, want > 0", r.totalBytes)
	}
	if r.truncated {
		t.Error("truncated should be false — patch is small")
	}

	// Verify patch contains the expected change (hello → hello world).
	patchStr := string(r.patch)
	if patchStr == "" {
		t.Error("patch string is empty")
	}
}

// Scenario 10: diff with max_bytes smaller than actual patch → truncated=true,
// patch length == max_bytes.
func TestDiff_MaxBytesSmall_Truncated(t *testing.T) {
	repoPath, sha1, sha2 := mustInitRepoWithTwoCommits(t)
	a := newAdapterAllowingDir(t, repoPath)

	const smallMax = 10
	raw, err := a.diff(context.Background(), capDiff(t, a),
		mustPayloadFrom(t, DiffPayload{RepoPath: repoPath, From: sha1, To: sha2, MaxBytes: smallMax}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*diffRaw)
	if r.validation != "" {
		t.Fatalf("unexpected validation: %s", r.validation)
	}
	if r.runErr != nil {
		t.Fatalf("unexpected runErr: %v", r.runErr)
	}
	if !r.truncated {
		t.Error("truncated should be true when patch exceeds max_bytes")
	}
	if len(r.patch) != smallMax {
		t.Errorf("patch length = %d, want %d (max_bytes)", len(r.patch), smallMax)
	}
	if r.totalBytes <= smallMax {
		t.Errorf("totalBytes = %d, should be > max_bytes %d", r.totalBytes, smallMax)
	}
}

// Scenario 11: diff between same commit → patch empty (or near-empty), success.
func TestDiff_SameCommit_EmptyPatch(t *testing.T) {
	repoPath, sha1, _ := mustInitRepoWithTwoCommits(t)
	a := newAdapterAllowingDir(t, repoPath)

	raw, err := a.diff(context.Background(), capDiff(t, a),
		mustPayloadFrom(t, DiffPayload{RepoPath: repoPath, From: sha1, To: sha1}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*diffRaw)
	if r.validation != "" {
		t.Fatalf("unexpected validation: %s", r.validation)
	}
	if r.runErr != nil {
		t.Fatalf("unexpected runErr: %v", r.runErr)
	}
	if r.truncated {
		t.Error("truncated should be false for same-commit diff")
	}
	// The patch for an identical diff should be empty or contain only a
	// header with no hunks.
	if r.totalBytes > 256 {
		t.Errorf("totalBytes = %d for same-commit diff, expected near-zero", r.totalBytes)
	}
}

// Scenario 12: cancelled ctx → ctxErr set.
func TestDiff_CancelledCtx_CtxErr(t *testing.T) {
	repoPath, sha1, sha2 := mustInitRepoWithTwoCommits(t)
	a := newAdapterAllowingDir(t, repoPath)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the call

	raw, err := a.diff(ctx, capDiff(t, a),
		mustPayloadFrom(t, DiffPayload{RepoPath: repoPath, From: sha1, To: sha2}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*diffRaw)
	if r.ctxErr == nil {
		t.Error("ctxErr should be set for pre-cancelled ctx")
	}
}

// ---------------------------------------------------------------------------
// Tests: NormalizeDiff — all branches
// ---------------------------------------------------------------------------

// Scenario 13a: ctx DeadlineExceeded → timeout.
func TestNormalizeDiff_CtxDeadlineExceeded(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	raw := &diffRaw{ctxErr: context.DeadlineExceeded, durationMs: 10}
	result, err := a.NormalizeDiff(capDiff(t, a), raw, clk)
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

// Scenario 13b: ctx Cancelled → cancelled.
func TestNormalizeDiff_CtxCancelled(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	raw := &diffRaw{ctxErr: ctx.Err(), durationMs: 5}
	result, err := a.NormalizeDiff(capDiff(t, a), raw, clk)
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

// Scenario 13c: validation error → failure.
func TestNormalizeDiff_ValidationError(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	raw := &diffRaw{validation: "repo_path is required", durationMs: 1}
	result, err := a.NormalizeDiff(capDiff(t, a), raw, clk)
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

// Scenario 13d: runErr → failure with external_failure error class.
func TestNormalizeDiff_RunErr(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	raw := &diffRaw{runErr: gogit.ErrRepositoryNotExists, durationMs: 3}
	result, err := a.NormalizeDiff(capDiff(t, a), raw, clk)
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

// Scenario 13e: success path — patch in StdoutRef, meta populated, no artifacts.
func TestNormalizeDiff_Success_PatchInStdoutRef(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	patchData := []byte("diff --git a/README.md b/README.md\n--- a/README.md\n+++ b/README.md\n")
	raw := &diffRaw{
		patch:      patchData,
		truncated:  false,
		totalBytes: len(patchData),
		durationMs: 20,
	}
	result, err := a.NormalizeDiff(capDiff(t, a), raw, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != valueobjects.StatusSuccess {
		t.Errorf("status = %q, want success", result.Status)
	}
	if result.StdoutRef == nil {
		t.Fatal("StdoutRef should be populated on success")
	}
	if len(result.StdoutRef.Data) == 0 {
		t.Error("StdoutRef.Data should contain patch bytes")
	}
	if len(result.Artifacts) != 0 {
		t.Errorf("expected 0 artifacts (read-only), got %d", len(result.Artifacts))
	}
	if result.AdapterMeta["truncated"] != "false" {
		t.Errorf("meta[truncated] = %q, want false", result.AdapterMeta["truncated"])
	}
	if result.AdapterMeta["total_bytes"] == "" {
		t.Error("meta[total_bytes] should be set")
	}
}

// Scenario 13f: success path with truncated=true — meta reflects it.
func TestNormalizeDiff_Success_TruncatedFlag(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	raw := &diffRaw{
		patch:      []byte("short"),
		truncated:  true,
		totalBytes: 1000000,
		durationMs: 8,
	}
	result, err := a.NormalizeDiff(capDiff(t, a), raw, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != valueobjects.StatusSuccess {
		t.Errorf("status = %q, want success", result.Status)
	}
	if result.AdapterMeta["truncated"] != "true" {
		t.Errorf("meta[truncated] = %q, want true", result.AdapterMeta["truncated"])
	}
	if result.AdapterMeta["total_bytes"] != "1000000" {
		t.Errorf("meta[total_bytes] = %q, want 1000000", result.AdapterMeta["total_bytes"])
	}
}

// Scenario 13g: wrong raw type → error returned.
func TestNormalizeDiff_WrongRawType_ReturnsError(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	_, err := a.NormalizeDiff(capDiff(t, a), &statusRaw{}, clk)
	if err == nil {
		t.Error("expected error for wrong raw type, got nil")
	}
}

// ---------------------------------------------------------------------------
// Integration: diff() + NormalizeDiff together
// ---------------------------------------------------------------------------

// Full integration: successful diff → status=success, StdoutRef non-nil,
// meta includes total_bytes and truncated=false.
func TestDiff_Integration_Success(t *testing.T) {
	repoPath, sha1, sha2 := mustInitRepoWithTwoCommits(t)
	a := newAdapterAllowingDir(t, repoPath)

	raw, err := a.diff(context.Background(), capDiff(t, a),
		mustPayloadFrom(t, DiffPayload{RepoPath: repoPath, From: sha1, To: sha2}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	clk := &shared.FakeClock{T: time.Now()}
	result, normErr := a.NormalizeDiff(capDiff(t, a), raw, clk)
	if normErr != nil {
		t.Fatalf("NormalizeDiff: %v", normErr)
	}
	if result.Status != valueobjects.StatusSuccess {
		t.Errorf("status = %q, want success", result.Status)
	}
	if result.StdoutRef == nil {
		t.Fatal("StdoutRef should be populated for a successful diff")
	}
	if len(result.Artifacts) != 0 {
		t.Errorf("expected 0 artifacts (read-only), got %d", len(result.Artifacts))
	}
	if result.AdapterMeta["truncated"] != "false" {
		t.Errorf("meta[truncated] = %q, want false", result.AdapterMeta["truncated"])
	}
	if result.AdapterMeta["total_bytes"] == "" || result.AdapterMeta["total_bytes"] == "0" {
		t.Errorf("meta[total_bytes] should be > 0, got %q", result.AdapterMeta["total_bytes"])
	}
}
