package git

import (
	"context"
	"fmt"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
)

// diff implements git.diff@v1 — read-only unified diff between two commit
// refs. Phase 1 requires BOTH `from` and `to` payload fields explicitly
// (commit SHAs, branch names, or tags). Worktree diff is deferred to Phase 2.
//
// Output: unified-diff bytes in patch field, truncated at MaxBytes (default
// 1 MiB) with truncated=true flag in adapter_meta. No artifacts emitted.
func (a *Adapter) diff(ctx context.Context, _ valueobjects.Capability, payload valueobjects.Payload) (services.AdapterRawOutcome, error) {
	t0 := a.clk.Now()
	const defaultMaxBytes = 1 << 20 // 1 MiB

	var p DiffPayload
	if err := decodeStrict(payload, &p); err != nil {
		return &diffRaw{validation: fmt.Sprintf("decode payload: %v", err), durationMs: a.elapsedMs(t0)}, nil
	}
	if p.RepoPath == "" {
		return &diffRaw{validation: "repo_path is required", durationMs: a.elapsedMs(t0)}, nil
	}
	if p.From == "" || p.To == "" {
		return &diffRaw{validation: "both 'from' and 'to' are required (Phase 1 supports commit-to-commit diff only)", durationMs: a.elapsedMs(t0)}, nil
	}
	repoPath, err := a.cfg.resolvePath(p.RepoPath)
	if err != nil {
		return &diffRaw{validation: err.Error(), durationMs: a.elapsedMs(t0)}, nil
	}

	maxBytes := p.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxBytes
	}

	if err := ctx.Err(); err != nil {
		return &diffRaw{ctxErr: err, durationMs: a.elapsedMs(t0)}, nil
	}

	repo, err := gogit.PlainOpen(repoPath)
	if err != nil {
		return &diffRaw{runErr: fmt.Errorf("open repo: %w", err), durationMs: a.elapsedMs(t0)}, nil
	}

	fromCommit, err := resolveCommit(repo, p.From)
	if err != nil {
		return &diffRaw{runErr: fmt.Errorf("resolve from %q: %w", p.From, err), durationMs: a.elapsedMs(t0)}, nil
	}
	toCommit, err := resolveCommit(repo, p.To)
	if err != nil {
		return &diffRaw{runErr: fmt.Errorf("resolve to %q: %w", p.To, err), durationMs: a.elapsedMs(t0)}, nil
	}

	patch, err := fromCommit.Patch(toCommit)
	if err != nil {
		return &diffRaw{runErr: fmt.Errorf("patch: %w", err), durationMs: a.elapsedMs(t0)}, nil
	}

	patchStr := patch.String()
	totalBytes := len(patchStr)
	truncated := false
	if totalBytes > maxBytes {
		patchStr = patchStr[:maxBytes]
		truncated = true
	}

	return &diffRaw{
		patch:      []byte(patchStr),
		truncated:  truncated,
		totalBytes: totalBytes,
		durationMs: a.elapsedMs(t0),
	}, nil
}

// resolveCommit resolves a name (SHA prefix, branch, or tag) to a
// *object.Commit using go-git's ResolveRevision.
func resolveCommit(repo *gogit.Repository, name string) (*object.Commit, error) {
	h, err := repo.ResolveRevision(plumbing.Revision(name))
	if err != nil {
		return nil, fmt.Errorf("revision %q not found: %w", name, err)
	}
	return repo.CommitObject(*h)
}
