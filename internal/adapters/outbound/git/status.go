package git

import (
	"context"
	"fmt"

	gogit "github.com/go-git/go-git/v5"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
)

// status implements git.status@v1 — read-only snapshot of the working
// tree. Returns: clean (bool), current branch + HEAD SHA, list of
// modified entries with their staging/worktree status codes.
//
// No artifacts are emitted (read-only operation — D8.8).
func (a *Adapter) status(ctx context.Context, _ valueobjects.Capability, payload valueobjects.Payload) (services.AdapterRawOutcome, error) {
	t0 := a.clk.Now()

	var p StatusPayload
	if err := decodeStrict(payload, &p); err != nil {
		return &statusRaw{validation: fmt.Sprintf("decode payload: %v", err), durationMs: a.elapsedMs(t0)}, nil
	}
	repoPath, err := a.cfg.resolvePath(p.RepoPath)
	if err != nil {
		return &statusRaw{validation: err.Error(), durationMs: a.elapsedMs(t0)}, nil
	}

	repo, err := gogit.PlainOpen(repoPath)
	if err != nil {
		return &statusRaw{
			runErr:     fmt.Errorf("open repo: %w", err),
			durationMs: a.elapsedMs(t0),
		}, nil
	}

	// Current branch + HEAD SHA.
	head, err := repo.Head()
	if err != nil {
		return &statusRaw{
			runErr:     fmt.Errorf("head: %w", err),
			durationMs: a.elapsedMs(t0),
		}, nil
	}
	branch := head.Name().Short()
	sha := head.Hash().String()

	wt, err := repo.Worktree()
	if err != nil {
		return &statusRaw{
			runErr:     fmt.Errorf("worktree: %w", err),
			branch:     branch,
			head:       sha,
			durationMs: a.elapsedMs(t0),
		}, nil
	}

	// Honor ctx between operations.
	if err := ctx.Err(); err != nil {
		return &statusRaw{ctxErr: err, branch: branch, head: sha, durationMs: a.elapsedMs(t0)}, nil
	}

	st, err := wt.Status()
	if err != nil {
		return &statusRaw{
			runErr:     fmt.Errorf("status: %w", err),
			branch:     branch,
			head:       sha,
			durationMs: a.elapsedMs(t0),
		}, nil
	}

	entries := make([]statusEntry, 0, len(st))
	for path, fs := range st {
		entries = append(entries, statusEntry{
			Path:     path,
			Staging:  byte(fs.Staging),
			Worktree: byte(fs.Worktree),
		})
	}
	return &statusRaw{
		clean:      st.IsClean(),
		branch:     branch,
		head:       sha,
		entries:    entries,
		durationMs: a.elapsedMs(t0),
	}, nil
}
