# test/fixtures/git-bench

Regenerable git working-tree fixtures used by the 2C.2 baseline
scenarios (`ops/load/scenarios/git_status.js` + `git_rough.js`).

## Structure

- `small-repo.sources/` — blueprint files for a 2-commit repo.
- `dirty-tree.sources/` — blueprint patch + untracked file applied on
  top of `small-repo` to produce a predictable dirty working tree.
- `Makefile` — `make fixture-git-bench` reconstructs the output
  directories from blueprints.

**Constructed directories are NOT committed.** The `.gitignore`
excludes `small-repo/` and `dirty-tree/`. Commits, branches, and
blob hashes are deterministic when the Makefile is run because it
pins git author/committer name + email + dates.

## Regenerating

From the repo root:

```bash
make fixture-git-bench
```

This runs `test/fixtures/git-bench/Makefile`'s default target, which:
1. Removes any existing `small-repo/` + `dirty-tree/`.
2. Builds `small-repo/` as a real git repo (2 commits, fixed authorship,
   fixed timestamps) from `small-repo.sources/`.
3. Clones `small-repo/` → `dirty-tree/`, then applies `file1.patch` +
   drops `new-file.txt` as untracked.

## Determinism

`TestFixtureGitBench_Regenerable` (Go, `-tags fixture`) runs the
Makefile twice and asserts byte-identical output. If you change a
blueprint, that test captures the drift.
