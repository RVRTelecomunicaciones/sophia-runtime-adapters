package git

import (
	"context"
	"errors"
	"fmt"
	"os"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
)

// worktreeCreate implements git.worktree.create@v1 — provisions a fresh
// workspace directory by cloning UpstreamPath into Path and checking out
// BaseRef. If Branch is set, a new branch by that name is created from
// BaseRef and made current; otherwise the worktree is left detached on
// the BaseRef commit.
//
// This is a CLONE-based worktree, not a linked `git worktree add`,
// because go-git v5 does not support Worktrees.Add (upstream issue
// go-git/go-git#152). Each created worktree is an independent repo with
// its own .git/ — disk use roughly doubles vs. linked worktrees, but
// parallel apply-phase agents get strong isolation (no shared index,
// no shared refs/heads). Atomic: on any failure the destination is
// removed and no partial state is left behind.
//
// Required payload fields: Path, UpstreamPath, BaseRef. Optional:
// Branch. Both Path and UpstreamPath must resolve under
// AllowedWorkingDirs (safety.go). Path MUST NOT already exist or be
// non-empty — caller is responsible for choosing a fresh location.
func (a *Adapter) worktreeCreate(ctx context.Context, _ valueobjects.Capability, payload valueobjects.Payload) (services.AdapterRawOutcome, error) {
	t0 := a.clk.Now()

	var p WorktreeCreatePayload
	if err := decodeStrict(payload, &p); err != nil {
		return &worktreeCreateRaw{validation: fmt.Sprintf("decode payload: %v", err), durationMs: a.elapsedMs(t0)}, nil
	}
	if p.Path == "" {
		return &worktreeCreateRaw{validation: "path is required", durationMs: a.elapsedMs(t0)}, nil
	}
	if p.UpstreamPath == "" {
		return &worktreeCreateRaw{validation: "upstream_path is required", durationMs: a.elapsedMs(t0)}, nil
	}
	if p.BaseRef == "" {
		return &worktreeCreateRaw{validation: "base_ref is required", durationMs: a.elapsedMs(t0)}, nil
	}

	dest, err := a.cfg.resolvePath(p.Path)
	if err != nil {
		return &worktreeCreateRaw{validation: fmt.Sprintf("path: %v", err), durationMs: a.elapsedMs(t0)}, nil
	}
	upstream, err := a.cfg.resolvePath(p.UpstreamPath)
	if err != nil {
		return &worktreeCreateRaw{validation: fmt.Sprintf("upstream_path: %v", err), durationMs: a.elapsedMs(t0)}, nil
	}

	if entries, err := os.ReadDir(dest); err == nil && len(entries) > 0 {
		return &worktreeCreateRaw{
			validation: fmt.Sprintf("path %q already exists and is non-empty", dest),
			durationMs: a.elapsedMs(t0),
		}, nil
	}

	if err := ctx.Err(); err != nil {
		return &worktreeCreateRaw{ctxErr: err, durationMs: a.elapsedMs(t0)}, nil
	}

	// Resolve BaseRef against the upstream BEFORE cloning so we can
	// fail fast with a clear validation error and avoid leaving an
	// orphan clone on disk.
	upRepo, err := gogit.PlainOpen(upstream)
	if err != nil {
		return &worktreeCreateRaw{
			runErr:     fmt.Errorf("open upstream %q: %w", upstream, err),
			durationMs: a.elapsedMs(t0),
		}, nil
	}
	baseHash, err := upRepo.ResolveRevision(plumbing.Revision(p.BaseRef))
	if err != nil {
		return &worktreeCreateRaw{
			validation: fmt.Sprintf("resolve base_ref %q: %v", p.BaseRef, err),
			durationMs: a.elapsedMs(t0),
		}, nil
	}

	// Clone (the URL form go-git accepts for a local repo is just the
	// path; Auth is nil because file:// is local, no remote auth).
	cloneOpts := &gogit.CloneOptions{URL: upstream}
	repo, cloneErr := gogit.PlainCloneContext(ctx, dest, false, cloneOpts)
	if cloneErr != nil {
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			_ = os.RemoveAll(dest)
			return &worktreeCreateRaw{ctxErr: ctx.Err(), durationMs: a.elapsedMs(t0)}, nil
		}
		_ = os.RemoveAll(dest)
		return &worktreeCreateRaw{
			runErr:     fmt.Errorf("clone %q into %q: %w", upstream, dest, cloneErr),
			durationMs: a.elapsedMs(t0),
		}, nil
	}

	wt, err := repo.Worktree()
	if err != nil {
		_ = os.RemoveAll(dest)
		return &worktreeCreateRaw{
			runErr:     fmt.Errorf("worktree of clone: %w", err),
			durationMs: a.elapsedMs(t0),
		}, nil
	}

	checkoutOpts := &gogit.CheckoutOptions{Hash: *baseHash}
	if p.Branch != "" {
		checkoutOpts.Branch = plumbing.NewBranchReferenceName(p.Branch)
		checkoutOpts.Create = true
	}
	if err := wt.Checkout(checkoutOpts); err != nil {
		_ = os.RemoveAll(dest)
		return &worktreeCreateRaw{
			runErr:     fmt.Errorf("checkout base_ref %q (branch=%q): %w", p.BaseRef, p.Branch, err),
			durationMs: a.elapsedMs(t0),
		}, nil
	}

	return &worktreeCreateRaw{
		worktreePath: dest,
		branch:       p.Branch,
		headSHA:      baseHash.String(),
		durationMs:   a.elapsedMs(t0),
	}, nil
}
