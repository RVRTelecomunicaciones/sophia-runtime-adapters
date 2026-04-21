package git

import (
	"context"
	"errors"
	"fmt"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
)

// commit implements git.commit@v1 — stages the requested paths (or all
// tracked changes if Paths is empty) and creates a single commit.
// Atomic: either the commit lands and an SHA is returned, or nothing
// changes and a failure outcome is reported. Never partial.
//
// Author identity is REQUIRED in the payload (no inheritance from git
// config). Empty commits are rejected unless allow_empty=true.
//
// IMPORTANT (A8.1 / ADR-0002): client-side hooks (pre-commit, commit-msg,
// pre-push) are NEVER executed. go-git does not invoke external git
// binaries — the operation runs entirely in-process. Operators relying
// on hooks for side effects must run them upstream.
func (a *Adapter) commit(ctx context.Context, _ valueobjects.Capability, payload valueobjects.Payload) (services.AdapterRawOutcome, error) {
	t0 := a.clk.Now()

	var p CommitPayload
	if err := decodeStrict(payload, &p); err != nil {
		return &commitRaw{validation: fmt.Sprintf("decode payload: %v", err), durationMs: a.elapsedMs(t0)}, nil
	}
	if p.RepoPath == "" {
		return &commitRaw{validation: "repo_path is required", durationMs: a.elapsedMs(t0)}, nil
	}
	if p.Message == "" {
		return &commitRaw{validation: "message is required", durationMs: a.elapsedMs(t0)}, nil
	}
	if p.AuthorName == "" || p.AuthorEmail == "" {
		return &commitRaw{validation: "author_name and author_email are required", durationMs: a.elapsedMs(t0)}, nil
	}
	repoPath, err := a.cfg.resolvePath(p.RepoPath)
	if err != nil {
		return &commitRaw{validation: err.Error(), durationMs: a.elapsedMs(t0)}, nil
	}

	if err := ctx.Err(); err != nil {
		return &commitRaw{ctxErr: err, durationMs: a.elapsedMs(t0)}, nil
	}

	repo, err := gogit.PlainOpen(repoPath)
	if err != nil {
		return &commitRaw{runErr: fmt.Errorf("open repo: %w", err), durationMs: a.elapsedMs(t0)}, nil
	}
	wt, err := repo.Worktree()
	if err != nil {
		return &commitRaw{runErr: fmt.Errorf("worktree: %w", err), durationMs: a.elapsedMs(t0)}, nil
	}

	// Stage requested paths or all tracked changes.
	if len(p.Paths) > 0 {
		for _, path := range p.Paths {
			if _, err := wt.Add(path); err != nil {
				return &commitRaw{
					runErr:     fmt.Errorf("stage %q: %w", path, err),
					durationMs: a.elapsedMs(t0),
				}, nil
			}
		}
	} else {
		// Stage all tracked changes (modified + deleted, NOT new untracked).
		// go-git's AddWithOptions{All: true} performs `git add -u` semantics.
		if err := wt.AddWithOptions(&gogit.AddOptions{All: true}); err != nil {
			return &commitRaw{runErr: fmt.Errorf("stage all: %w", err), durationMs: a.elapsedMs(t0)}, nil
		}
	}

	commitOpts := &gogit.CommitOptions{
		Author: &object.Signature{
			Name:  p.AuthorName,
			Email: p.AuthorEmail,
			When:  a.clk.Now().UTC(),
		},
		AllowEmptyCommits: p.AllowEmpty,
	}

	hash, err := wt.Commit(p.Message, commitOpts)
	if err != nil {
		// Map empty-commit rejection from go-git to a more actionable message.
		if errors.Is(err, gogit.ErrEmptyCommit) {
			return &commitRaw{
				runErr:     fmt.Errorf("empty commit not allowed (set allow_empty=true to permit)"),
				durationMs: a.elapsedMs(t0),
			}, nil
		}
		return &commitRaw{runErr: fmt.Errorf("commit: %w", err), durationMs: a.elapsedMs(t0)}, nil
	}

	// Read updated HEAD branch name.
	branch := ""
	if head, err := repo.Head(); err == nil {
		branch = head.Name().Short()
	}
	return &commitRaw{
		commitSHA:  hash.String(),
		branch:     branch,
		durationMs: a.elapsedMs(t0),
	}, nil
}
