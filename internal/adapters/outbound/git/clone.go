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

// clone implements git.clone@v1 — clones a remote repo into a destination
// path under AllowedWorkingDirs. The capability has AllowsPartial=true
// (D4.6): when fetch completes and a checkout follow-up fails, the
// outcome is "partial" with the cloned directory + git_commit + git_ref
// artifacts retained.
//
// Auth: AuthMode=none for public repos; AuthMode=ssh-agent defers to
// the local SSH agent (D8.3 / A8.3 — embedded credentials NEVER accepted).
func (a *Adapter) clone(ctx context.Context, _ valueobjects.Capability, payload valueobjects.Payload) (services.AdapterRawOutcome, error) {
	t0 := a.clk.Now()

	var p ClonePayload
	if err := decodeStrict(payload, &p); err != nil {
		return &cloneRaw{validation: fmt.Sprintf("decode payload: %v", err), durationMs: a.elapsedMs(t0)}, nil
	}
	if p.RepoURL == "" {
		return &cloneRaw{validation: "repo_url is required", durationMs: a.elapsedMs(t0)}, nil
	}
	dest, err := a.cfg.resolvePath(p.DestinationPath)
	if err != nil {
		return &cloneRaw{validation: err.Error(), durationMs: a.elapsedMs(t0)}, nil
	}

	// Refuse to overwrite an existing non-empty destination.
	if entries, err := os.ReadDir(dest); err == nil && len(entries) > 0 {
		return &cloneRaw{
			validation: fmt.Sprintf("destination %q already exists and is non-empty", dest),
			durationMs: a.elapsedMs(t0),
		}, nil
	}

	auth, err := a.cfg.buildAuth(p.AuthMode)
	if err != nil {
		return &cloneRaw{validation: err.Error(), durationMs: a.elapsedMs(t0)}, nil
	}

	if err := ctx.Err(); err != nil {
		return &cloneRaw{ctxErr: err, durationMs: a.elapsedMs(t0)}, nil
	}

	cloneOpts := &gogit.CloneOptions{
		URL:   p.RepoURL,
		Auth:  auth,
		Depth: p.Depth,
	}
	if p.Ref != "" {
		cloneOpts.ReferenceName = plumbing.ReferenceName(p.Ref)
	}

	repo, cloneErr := gogit.PlainCloneContext(ctx, dest, false, cloneOpts)
	raw := &cloneRaw{repoPath: dest, durationMs: a.elapsedMs(t0)}
	if cloneErr != nil {
		// Fetch failure path: no repo, no artifacts.
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			raw.ctxErr = ctx.Err()
		} else {
			raw.runErr = fmt.Errorf("clone: %w", cloneErr)
		}
		return raw, nil
	}

	// Fetch + initial checkout succeeded.
	raw.fetchOK = true
	raw.checkoutOK = true

	// Read HEAD to populate headSHA and refResolved.
	if head, err := repo.Head(); err == nil {
		raw.headSHA = head.Hash().String()
		raw.refResolved = head.Name().Short()
	} else {
		// Fetch ok but HEAD unreadable → partial-eligible (artifacts present).
		raw.checkoutOK = false
		raw.runErr = fmt.Errorf("read HEAD post-clone: %w", err)
	}

	// If caller specified a ref different from the clone's default branch,
	// attempt a follow-up checkout via worktree. Failure here marks
	// checkoutOK=false WITHOUT removing the cloned dir → partial-eligible.
	if p.Ref != "" && raw.refResolved != "" && raw.refResolved != plumbing.ReferenceName(p.Ref).Short() {
		wt, err := repo.Worktree()
		if err != nil {
			raw.checkoutOK = false
			raw.runErr = fmt.Errorf("worktree post-clone: %w", err)
		} else {
			if err := wt.Checkout(&gogit.CheckoutOptions{
				Branch: plumbing.ReferenceName(p.Ref),
			}); err != nil {
				raw.checkoutOK = false
				raw.runErr = fmt.Errorf("checkout %q: %w", p.Ref, err)
			} else {
				if head, err := repo.Head(); err == nil {
					raw.headSHA = head.Hash().String()
					raw.refResolved = head.Name().Short()
				}
			}
		}
	}

	return raw, nil
}
