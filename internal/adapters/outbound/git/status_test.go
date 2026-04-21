package git

import (
	"context"
	"encoding/json"
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
// status-specific helpers
// ---------------------------------------------------------------------------

// newAdapterWithDirs constructs an adapter with an explicit allowlist.
// Use this when you need TempDir paths admitted (vs. newTestAdapter which
// uses os.TempDir() as the single root).
func newAdapterWithDirs(t *testing.T, dirs ...string) *Adapter {
	t.Helper()
	a, err := NewAdapter(Config{
		AllowedWorkingDirs: dirs,
		InlineStreamLimit:  16 * 1024,
	}, &shared.FakeClock{T: time.Now()})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	return a
}

// mustPayloadFrom marshals v into a Payload.
func mustPayloadFrom(t *testing.T, v interface{}) valueobjects.Payload {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	p, err := valueobjects.NewPayload(valueobjects.ContentTypeJSON, data, 64*1024)
	if err != nil {
		t.Fatalf("NewPayload: %v", err)
	}
	return p
}

// capStatus returns the git.status@v1 capability from adapter a.
func capStatus(t *testing.T, a *Adapter) valueobjects.Capability {
	t.Helper()
	return capByName(t, a, "status")
}

// mustInitRepoWithCommit initialises a git repo at dir with one commit.
func mustInitRepoWithCommit(t *testing.T, dir string) {
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

	_, err = wt.Commit("initial", &gogit.CommitOptions{
		Author: &object.Signature{
			Name:  "Test",
			Email: "test@example.com",
			When:  time.Now(),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------------------
// Tests: status() — decode / allowlist / real repo
// ---------------------------------------------------------------------------

func TestStatus_DecodePayload_HappyPath(t *testing.T) {
	dir := t.TempDir()
	mustInitRepoWithCommit(t, dir)
	a := newAdapterWithDirs(t, dir)

	raw, err := a.status(context.Background(), capStatus(t, a), mustPayloadFrom(t, map[string]string{"repo_path": dir}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := raw.(*statusRaw)
	if r.validation != "" {
		t.Errorf("unexpected validation error: %s", r.validation)
	}
	if r.runErr != nil {
		t.Errorf("unexpected runErr: %v", r.runErr)
	}
}

func TestStatus_DecodePayload_RejectsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	a := newAdapterWithDirs(t, dir)

	raw, err := a.status(context.Background(), capStatus(t, a),
		mustPayloadFrom(t, map[string]interface{}{"repo_path": dir, "junk": 1}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := raw.(*statusRaw)
	if r.validation == "" {
		t.Error("expected validation error for unknown field, got none")
	}
}

func TestStatus_ResolvePathRejectsOutsideAllowlist(t *testing.T) {
	allowed := t.TempDir()
	outside := t.TempDir()
	a := newAdapterWithDirs(t, allowed)

	raw, err := a.status(context.Background(), capStatus(t, a), mustPayloadFrom(t, StatusPayload{RepoPath: outside}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := raw.(*statusRaw)
	if r.validation == "" {
		t.Error("expected validation error for path outside allowlist")
	}
}

func TestStatus_CleanRepo(t *testing.T) {
	dir := t.TempDir()
	mustInitRepoWithCommit(t, dir)
	a := newAdapterWithDirs(t, dir)

	raw, err := a.status(context.Background(), capStatus(t, a), mustPayloadFrom(t, StatusPayload{RepoPath: dir}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := raw.(*statusRaw)
	if r.runErr != nil {
		t.Fatalf("runErr: %v", r.runErr)
	}
	if !r.clean {
		t.Errorf("expected clean=true, got false; entries=%v", r.entries)
	}
	if len(r.entries) != 0 {
		t.Errorf("expected 0 entries for clean repo, got %d", len(r.entries))
	}
	if r.head == "" {
		t.Error("expected non-empty HEAD SHA")
	}
	if r.branch == "" {
		t.Error("expected non-empty branch name")
	}
}

func TestStatus_DirtyRepo(t *testing.T) {
	dir := t.TempDir()
	mustInitRepoWithCommit(t, dir)

	// Modify the tracked file after commit.
	fp := filepath.Join(dir, "README.md")
	if err := os.WriteFile(fp, []byte("modified"), 0o600); err != nil {
		t.Fatal(err)
	}

	a := newAdapterWithDirs(t, dir)
	raw, err := a.status(context.Background(), capStatus(t, a), mustPayloadFrom(t, StatusPayload{RepoPath: dir}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := raw.(*statusRaw)
	if r.runErr != nil {
		t.Fatalf("runErr: %v", r.runErr)
	}
	if r.clean {
		t.Error("expected clean=false for dirty repo")
	}
	if len(r.entries) == 0 {
		t.Error("expected at least one entry for dirty repo")
	}
}

func TestStatus_NonRepoDir(t *testing.T) {
	dir := t.TempDir() // plain dir, no .git
	a := newAdapterWithDirs(t, dir)

	raw, err := a.status(context.Background(), capStatus(t, a), mustPayloadFrom(t, StatusPayload{RepoPath: dir}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := raw.(*statusRaw)
	if r.runErr == nil {
		t.Error("expected runErr for non-repo dir, got nil")
	}
}

func TestStatus_CancelledCtxBeforeStatus(t *testing.T) {
	dir := t.TempDir()
	mustInitRepoWithCommit(t, dir)
	a := newAdapterWithDirs(t, dir)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before calling status

	raw, err := a.status(ctx, capStatus(t, a), mustPayloadFrom(t, StatusPayload{RepoPath: dir}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	// Must return a *statusRaw without panicking.
	if _, ok := raw.(*statusRaw); !ok {
		t.Errorf("expected *statusRaw, got %T", raw)
	}
}

// ---------------------------------------------------------------------------
// Tests: NormalizeStatus — all branches
// ---------------------------------------------------------------------------

func TestNormalizeStatus_CtxDeadlineExceeded(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	raw := &statusRaw{ctxErr: context.DeadlineExceeded, durationMs: 10}
	result, err := a.NormalizeStatus(capStatus(t, a), raw, clk)
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

func TestNormalizeStatus_CtxCancelled(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	raw := &statusRaw{ctxErr: ctx.Err(), durationMs: 5}
	result, err := a.NormalizeStatus(capStatus(t, a), raw, clk)
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

func TestNormalizeStatus_ValidationError(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	raw := &statusRaw{validation: "path is empty", durationMs: 1}
	result, err := a.NormalizeStatus(capStatus(t, a), raw, clk)
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

func TestNormalizeStatus_RunError(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	raw := &statusRaw{runErr: gogit.ErrRepositoryNotExists, durationMs: 2}
	result, err := a.NormalizeStatus(capStatus(t, a), raw, clk)
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

func TestNormalizeStatus_Success_PacksMeta(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	raw := &statusRaw{
		clean:      true,
		branch:     "main",
		head:       "abc123def456",
		entries:    []statusEntry{},
		durationMs: 8,
	}
	result, err := a.NormalizeStatus(capStatus(t, a), raw, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != valueobjects.StatusSuccess {
		t.Errorf("status = %q, want success", result.Status)
	}
	if result.ErrorClass != "" {
		t.Errorf("expected empty error_class on success, got %q", result.ErrorClass)
	}
	if result.AdapterMeta["branch"] != "main" {
		t.Errorf("meta[branch] = %q, want main", result.AdapterMeta["branch"])
	}
	if result.AdapterMeta["head"] != "abc123def456" {
		t.Errorf("meta[head] = %q, want abc123def456", result.AdapterMeta["head"])
	}
	if result.AdapterMeta["clean"] != "true" {
		t.Errorf("meta[clean] = %q, want true", result.AdapterMeta["clean"])
	}
	if result.AdapterMeta["entries_count"] != "0" {
		t.Errorf("meta[entries_count] = %q, want 0", result.AdapterMeta["entries_count"])
	}
	if len(result.Artifacts) != 0 {
		t.Errorf("expected no artifacts for read-only status, got %d", len(result.Artifacts))
	}
}

func TestNormalizeStatus_WrongRawType_ReturnsError(t *testing.T) {
	a := newTestAdapter(t)
	clk := &shared.FakeClock{T: time.Now()}
	_, err := a.NormalizeStatus(capStatus(t, a), &cloneRaw{}, clk)
	if err == nil {
		t.Error("expected error for wrong raw type, got nil")
	}
}
