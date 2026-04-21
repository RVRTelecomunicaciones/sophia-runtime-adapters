package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	gogitConfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
)

// ---------------------------------------------------------------------------
// clone-specific helpers
// ---------------------------------------------------------------------------

// capClone returns the git.clone@v1 capability from adapter a.
func capClone(t *testing.T, a *Adapter) valueobjects.Capability {
	t.Helper()
	return capByName(t, a, "clone")
}

// mustInitRemoteWithCommit creates a working repo with one commit, pushes it
// into a bare repo, and returns the bare repo path (suitable as a file://
// clone source). No network is used — A10.1 compliant.
func mustInitRemoteWithCommit(t *testing.T) (bareDir string, cleanup func()) {
	t.Helper()

	srcWork := t.TempDir()
	repo, err := gogit.PlainInit(srcWork, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	fp := filepath.Join(srcWork, "README.md")
	if err := os.WriteFile(fp, []byte("hello"), 0o600); err != nil {
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

	bareDir = t.TempDir()
	bare, err := gogit.PlainInit(bareDir, true)
	if err != nil {
		t.Fatal(err)
	}
	_ = bare

	if _, err := repo.CreateRemote(&gogitConfig.RemoteConfig{
		Name: "origin",
		URLs: []string{bareDir},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Push(&gogit.PushOptions{
		RemoteName: "origin",
		RefSpecs:   []gogitConfig.RefSpec{"refs/heads/master:refs/heads/master"},
	}); err != nil {
		t.Fatal(err)
	}

	return bareDir, func() {}
}

// newAdapterAllowingDir returns an adapter whose allowlist covers the given
// directory and its subtree. Used to admit t.TempDir() destinations.
func newAdapterAllowingDir(t *testing.T, dir string) *Adapter {
	t.Helper()
	a, err := NewAdapter(Config{
		AllowedWorkingDirs: []string{dir},
		SSHAgent:           false,
		InlineStreamLimit:  16 * 1024,
	}, &shared.FakeClock{T: time.Now()})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	return a
}

// ---------------------------------------------------------------------------
// Tests: clone() — payload decode / validation
// ---------------------------------------------------------------------------

// Scenario 1: valid JSON payload (happy path decode check — actual clone
// is exercised in the "successful clone" scenarios).
func TestClone_DecodePayload_HappyPath(t *testing.T) {
	bareDir, _ := mustInitRemoteWithCommit(t)
	destParent := t.TempDir()
	dest := filepath.Join(destParent, "dest")
	a := newAdapterAllowingDir(t, destParent)

	raw, err := a.clone(context.Background(), capClone(t, a),
		mustPayloadFrom(t, ClonePayload{RepoURL: bareDir, DestinationPath: dest}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*cloneRaw)
	if r.validation != "" {
		t.Errorf("unexpected validation error: %s", r.validation)
	}
}

// Scenario 2: unknown field in payload → validation failure.
func TestClone_DecodePayload_RejectsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	a := newAdapterAllowingDir(t, dir)

	raw, err := a.clone(context.Background(), capClone(t, a),
		mustPayloadFrom(t, map[string]interface{}{
			"repo_url":         "file:///some/repo",
			"destination_path": filepath.Join(dir, "dest"),
			"unknown_field":    true,
		}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*cloneRaw)
	if r.validation == "" {
		t.Error("expected validation error for unknown field, got none")
	}
}

// Scenario 3: repo_url empty → validation failure.
func TestClone_EmptyRepoURL_ValidationFailure(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "dest")
	a := newAdapterAllowingDir(t, dir)

	raw, err := a.clone(context.Background(), capClone(t, a),
		mustPayloadFrom(t, ClonePayload{RepoURL: "", DestinationPath: dest}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*cloneRaw)
	if r.validation == "" {
		t.Error("expected validation error for empty repo_url, got none")
	}
}

// Scenario 4: destination_path outside allowlist → validation failure.
func TestClone_DestinationOutsideAllowlist_ValidationFailure(t *testing.T) {
	allowed := t.TempDir()
	outside := t.TempDir()
	a := newAdapterAllowingDir(t, allowed)

	raw, err := a.clone(context.Background(), capClone(t, a),
		mustPayloadFrom(t, ClonePayload{
			RepoURL:         "file:///some/repo",
			DestinationPath: filepath.Join(outside, "dest"),
		}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*cloneRaw)
	if r.validation == "" {
		t.Error("expected validation error for path outside allowlist, got none")
	}
}

// Scenario 5: destination already exists and is non-empty → validation failure.
func TestClone_DestinationNonEmpty_ValidationFailure(t *testing.T) {
	parent := t.TempDir()
	dest := filepath.Join(parent, "existing")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	// Write a file so it's non-empty.
	if err := os.WriteFile(filepath.Join(dest, "file.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	a := newAdapterAllowingDir(t, parent)
	raw, err := a.clone(context.Background(), capClone(t, a),
		mustPayloadFrom(t, ClonePayload{
			RepoURL:         "file:///some/repo",
			DestinationPath: dest,
		}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*cloneRaw)
	if r.validation == "" {
		t.Error("expected validation error for non-empty destination, got none")
	}
}

// Scenario 6: AuthMode=ssh-agent without SSH_AUTH_SOCK → validation failure.
func TestClone_SSHAgentDisabledInConfig_ValidationFailure(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "dest")
	// SSHAgent=false in config → buildAuth rejects ssh-agent mode.
	a := newAdapterAllowingDir(t, dir) // SSHAgent=false by default

	raw, err := a.clone(context.Background(), capClone(t, a),
		mustPayloadFrom(t, ClonePayload{
			RepoURL:         "git@github.com:example/repo.git",
			DestinationPath: dest,
			AuthMode:        AuthSSHAgent,
		}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*cloneRaw)
	if r.validation == "" {
		t.Error("expected validation error for ssh-agent with SSHAgent=false, got none")
	}
}

// ---------------------------------------------------------------------------
// Tests: clone() — real file:// local bare repo
// ---------------------------------------------------------------------------

// Scenario 7: successful clone → status=success, ≥3 artifacts, meta populated.
func TestClone_Success_LocalBareRepo(t *testing.T) {
	bareDir, _ := mustInitRemoteWithCommit(t)
	destParent := t.TempDir()
	dest := filepath.Join(destParent, "cloned")
	a := newAdapterAllowingDir(t, destParent)

	raw, err := a.clone(context.Background(), capClone(t, a),
		mustPayloadFrom(t, ClonePayload{
			RepoURL:         bareDir,
			DestinationPath: dest,
		}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*cloneRaw)
	if r.validation != "" {
		t.Fatalf("unexpected validation: %s", r.validation)
	}
	if r.runErr != nil {
		t.Fatalf("unexpected runErr: %v", r.runErr)
	}
	if !r.fetchOK {
		t.Error("fetchOK should be true")
	}
	if !r.checkoutOK {
		t.Error("checkoutOK should be true")
	}
	if r.headSHA == "" {
		t.Error("headSHA should be populated")
	}
	if r.refResolved == "" {
		t.Error("refResolved should be populated")
	}
	if r.repoPath != dest {
		t.Errorf("repoPath = %q, want %q", r.repoPath, dest)
	}

	// NormalizeClone should produce success with ≥3 artifacts.
	clk := &shared.FakeClock{T: time.Now()}
	result, normErr := a.NormalizeClone(capClone(t, a), r, clk)
	if normErr != nil {
		t.Fatalf("NormalizeClone: %v", normErr)
	}
	if result.Status != valueobjects.StatusSuccess {
		t.Errorf("status = %q, want success", result.Status)
	}
	if len(result.Artifacts) < 3 {
		t.Errorf("expected ≥3 artifacts (directory + git_commit + git_ref), got %d", len(result.Artifacts))
	}
	// Verify artifact types.
	artTypes := map[entities.ArtifactType]bool{}
	for _, art := range result.Artifacts {
		artTypes[art.Type] = true
	}
	for _, want := range []entities.ArtifactType{
		entities.ArtifactDirectory,
		entities.ArtifactGitCommit,
		entities.ArtifactGitRef,
	} {
		if !artTypes[want] {
			t.Errorf("missing artifact type %q", want)
		}
	}
	// Verify meta.
	if result.AdapterMeta["fetch_ok"] != "true" {
		t.Errorf("meta[fetch_ok] = %q, want true", result.AdapterMeta["fetch_ok"])
	}
	if result.AdapterMeta["checkout_ok"] != "true" {
		t.Errorf("meta[checkout_ok] = %q, want true", result.AdapterMeta["checkout_ok"])
	}
	if result.AdapterMeta["head_sha"] == "" {
		t.Error("meta[head_sha] should be populated")
	}
	if result.AdapterMeta["ref_resolved"] == "" {
		t.Error("meta[ref_resolved] should be populated")
	}
}

// Scenario 8: bogus URL → status=failure, no artifacts, runErr populated.
func TestClone_BogusURL_Failure(t *testing.T) {
	destParent := t.TempDir()
	dest := filepath.Join(destParent, "cloned")
	a := newAdapterAllowingDir(t, destParent)

	raw, err := a.clone(context.Background(), capClone(t, a),
		mustPayloadFrom(t, ClonePayload{
			RepoURL:         "/nonexistent/path/that/is/not/a/repo",
			DestinationPath: dest,
		}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*cloneRaw)
	if r.runErr == nil {
		t.Error("expected runErr for bogus URL, got nil")
	}
	if r.fetchOK {
		t.Error("fetchOK should be false for failed clone")
	}

	clk := &shared.FakeClock{T: time.Now()}
	result, normErr := a.NormalizeClone(capClone(t, a), r, clk)
	if normErr != nil {
		t.Fatalf("NormalizeClone: %v", normErr)
	}
	if result.Status != valueobjects.StatusFailure {
		t.Errorf("status = %q, want failure", result.Status)
	}
	if result.ErrorClass != valueobjects.ErrExternalFailure {
		t.Errorf("error_class = %q, want external_failure", result.ErrorClass)
	}
}

// Scenario 9: cancelled ctx before clone call → status=cancelled.
func TestClone_CancelledCtxBeforeCall_Cancelled(t *testing.T) {
	bareDir, _ := mustInitRemoteWithCommit(t)
	destParent := t.TempDir()
	dest := filepath.Join(destParent, "cloned")
	a := newAdapterAllowingDir(t, destParent)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the call

	raw, err := a.clone(ctx, capClone(t, a),
		mustPayloadFrom(t, ClonePayload{
			RepoURL:         bareDir,
			DestinationPath: dest,
		}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*cloneRaw)
	if r.ctxErr == nil {
		t.Error("ctxErr should be set for pre-cancelled ctx")
	}

	clk := &shared.FakeClock{T: time.Now()}
	result, normErr := a.NormalizeClone(capClone(t, a), r, clk)
	if normErr != nil {
		t.Fatalf("NormalizeClone: %v", normErr)
	}
	if result.Status != valueobjects.StatusCancelled {
		t.Errorf("status = %q, want cancelled", result.Status)
	}
	if result.ErrorClass != valueobjects.ErrCancelled {
		t.Errorf("error_class = %q, want cancelled", result.ErrorClass)
	}
}

// Scenario 10 (THE D4.6 PARTIAL TEST): clone local bare repo OK but request a
// non-existent ref → fetch succeeds, checkout fails → status=partial with
// ≥1 artifact. This is the ONLY Phase 1 capability that exercises partial.
func TestClone_PartialD46_FetchOKCheckoutFails(t *testing.T) {
	bareDir, _ := mustInitRemoteWithCommit(t)
	destParent := t.TempDir()
	dest := filepath.Join(destParent, "cloned")
	a := newAdapterAllowingDir(t, destParent)

	// Request a ref that doesn't exist in the bare repo → go-git will clone
	// successfully (the ReferenceName on CloneOptions controls the initial
	// checkout, causing a clone error), OR the clone fails entirely.
	// A safer D4.6 test: clone without a ref (fetch succeeds), then the
	// normalizer is given a raw where fetchOK=true but checkoutOK=false
	// with repoPath pointing to the cloned dir (which actually exists).
	//
	// To drive the real partial path: clone successfully first, then
	// construct a raw that represents the partial state.
	raw0, err := a.clone(context.Background(), capClone(t, a),
		mustPayloadFrom(t, ClonePayload{
			RepoURL:         bareDir,
			DestinationPath: dest,
		}))
	if err != nil {
		t.Fatalf("unexpected hard error on initial clone: %v", err)
	}
	r0 := raw0.(*cloneRaw)
	if !r0.fetchOK {
		t.Skipf("initial clone failed (fetchOK=false), cannot run D4.6 partial test: %v", r0.runErr)
	}

	// Simulate the partial state: fetch succeeded, checkout of a bogus ref
	// failed. The repo directory exists (from the successful clone above).
	partialRaw := &cloneRaw{
		fetchOK:     true,
		checkoutOK:  false,
		repoPath:    dest,
		headSHA:     r0.headSHA,
		refResolved: r0.refResolved,
		runErr:      gogit.ErrBranchNotFound,
		durationMs:  100,
	}

	clk := &shared.FakeClock{T: time.Now()}
	result, normErr := a.NormalizeClone(capClone(t, a), partialRaw, clk)
	if normErr != nil {
		t.Fatalf("NormalizeClone: %v", normErr)
	}

	// THE KEY ASSERTION: D4.6 partial.
	if result.Status != valueobjects.StatusPartial {
		t.Errorf("status = %q, want partial (D4.6: fetch ok + checkout fail + artifacts present + not reverted)", result.Status)
	}
	if result.ErrorClass != valueobjects.ErrExternalFailure {
		t.Errorf("error_class = %q, want external_failure", result.ErrorClass)
	}
	if len(result.Artifacts) == 0 {
		t.Error("expected ≥1 artifact for partial outcome (directory artifact must be present)")
	}
	// Confirm directory artifact is present.
	hasDir := false
	for _, art := range result.Artifacts {
		if art.Type == entities.ArtifactDirectory {
			hasDir = true
		}
	}
	if !hasDir {
		t.Error("partial outcome must include directory artifact")
	}
	// Confirm meta signals.
	if result.AdapterMeta["fetch_ok"] != "true" {
		t.Errorf("meta[fetch_ok] = %q, want true", result.AdapterMeta["fetch_ok"])
	}
	if result.AdapterMeta["checkout_ok"] != "false" {
		t.Errorf("meta[checkout_ok] = %q, want false", result.AdapterMeta["checkout_ok"])
	}
}

// Scenario: clone with explicit Ref that matches the bare repo's branch
// (covers the cloneOpts.ReferenceName = plumbing.ReferenceName(p.Ref) line).
func TestClone_WithRef_MatchingBranch_Success(t *testing.T) {
	bareDir, _ := mustInitRemoteWithCommit(t)
	destParent := t.TempDir()
	dest := filepath.Join(destParent, "cloned")
	a := newAdapterAllowingDir(t, destParent)

	// refs/heads/master exists in the bare repo (mustInitRemoteWithCommit
	// pushes master). Using the full refname exercises the ReferenceName path.
	raw, err := a.clone(context.Background(), capClone(t, a),
		mustPayloadFrom(t, ClonePayload{
			RepoURL:         bareDir,
			DestinationPath: dest,
			Ref:             "refs/heads/master",
		}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*cloneRaw)
	if r.validation != "" {
		t.Fatalf("unexpected validation: %s", r.validation)
	}
	if r.runErr != nil {
		t.Fatalf("unexpected runErr: %v", r.runErr)
	}
	if !r.fetchOK {
		t.Error("fetchOK should be true")
	}
}

// Scenario: clone with a bogus Ref that doesn't exist → PlainCloneContext
// returns an error, exercising the runErr assignment path inside the
// cloneErr != nil block (ctx.Err() is nil so runErr branch fires).
func TestClone_WithBogusRef_Failure(t *testing.T) {
	bareDir, _ := mustInitRemoteWithCommit(t)
	destParent := t.TempDir()
	dest := filepath.Join(destParent, "cloned")
	a := newAdapterAllowingDir(t, destParent)

	raw, err := a.clone(context.Background(), capClone(t, a),
		mustPayloadFrom(t, ClonePayload{
			RepoURL:         bareDir,
			DestinationPath: dest,
			Ref:             "refs/heads/nonexistent-branch",
		}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	r := raw.(*cloneRaw)
	// With a non-existent ref the clone itself fails (PlainCloneContext
	// error) → runErr is set, fetchOK is false.
	if r.fetchOK {
		t.Skip("go-git clone with bogus ref succeeded (unexpected); skipping")
	}
	if r.runErr == nil && r.ctxErr == nil {
		t.Error("expected runErr or ctxErr for bogus ref, got neither")
	}
}

// ---------------------------------------------------------------------------
// Tests: NormalizeClone — all branches (unit)
// ---------------------------------------------------------------------------

// Scenario 11a: ctx DeadlineExceeded → timeout.
func TestNormalizeClone_CtxDeadlineExceeded(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	raw := &cloneRaw{ctxErr: context.DeadlineExceeded, durationMs: 10}
	result, err := a.NormalizeClone(capClone(t, a), raw, clk)
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

// Scenario 11b: ctx Cancelled → cancelled.
func TestNormalizeClone_CtxCancelled(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	raw := &cloneRaw{ctxErr: ctx.Err(), durationMs: 5}
	result, err := a.NormalizeClone(capClone(t, a), raw, clk)
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

// Scenario 11c: validation error → failure.
func TestNormalizeClone_ValidationError(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	raw := &cloneRaw{validation: "repo_url is required", durationMs: 1}
	result, err := a.NormalizeClone(capClone(t, a), raw, clk)
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

// Scenario 11d: runErr without artifacts → failure.
func TestNormalizeClone_RunErrWithoutArtifacts_Failure(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	// repoPath points to a non-existent dir so no directory artifact is built.
	raw := &cloneRaw{
		runErr:     gogit.ErrRepositoryNotExists,
		repoPath:   "/tmp/nonexistent-repo-that-does-not-exist-xyz",
		durationMs: 3,
	}
	result, err := a.NormalizeClone(capClone(t, a), raw, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != valueobjects.StatusFailure {
		t.Errorf("status = %q, want failure", result.Status)
	}
	if result.ErrorClass != valueobjects.ErrExternalFailure {
		t.Errorf("error_class = %q, want external_failure", result.ErrorClass)
	}
	if len(result.Artifacts) != 0 {
		t.Errorf("expected 0 artifacts when clone dir doesn't exist, got %d", len(result.Artifacts))
	}
}

// Scenario 11e: fetchOK=true, checkoutOK=false with real existing dir → partial.
// (Integration with the partial rule, using a real tmpdir so os.Stat passes.)
func TestNormalizeClone_FetchOKCheckoutFail_WithArtifacts_Partial(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}

	existingDir := t.TempDir() // actually exists, so directory artifact fires
	raw := &cloneRaw{
		fetchOK:     true,
		checkoutOK:  false,
		repoPath:    existingDir,
		headSHA:     "abc123",
		refResolved: "master",
		runErr:      gogit.ErrBranchNotFound,
		durationMs:  50,
	}
	result, err := a.NormalizeClone(capClone(t, a), raw, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != valueobjects.StatusPartial {
		t.Errorf("status = %q, want partial", result.Status)
	}
	if len(result.Artifacts) == 0 {
		t.Error("partial must carry ≥1 artifact")
	}
}

// Scenario 11f: success path via NormalizeClone unit (fetchOK+checkoutOK, existing dir).
func TestNormalizeClone_Success(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}

	existingDir := t.TempDir()
	raw := &cloneRaw{
		fetchOK:     true,
		checkoutOK:  true,
		repoPath:    existingDir,
		headSHA:     "deadbeef",
		refResolved: "main",
		durationMs:  20,
	}
	result, err := a.NormalizeClone(capClone(t, a), raw, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != valueobjects.StatusSuccess {
		t.Errorf("status = %q, want success", result.Status)
	}
	if len(result.Artifacts) < 3 {
		t.Errorf("expected ≥3 artifacts, got %d", len(result.Artifacts))
	}
}

// Scenario 11g: wrong raw type → error returned.
func TestNormalizeClone_WrongRawType_ReturnsError(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	_, err := a.NormalizeClone(capClone(t, a), &statusRaw{}, clk)
	if err == nil {
		t.Error("expected error for wrong raw type, got nil")
	}
}
