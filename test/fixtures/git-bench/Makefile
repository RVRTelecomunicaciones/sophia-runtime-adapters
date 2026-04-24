# test/fixtures/git-bench/Makefile
#
# Deterministic regeneration of the git-bench fixtures. Invoked from
# the repo root via `make fixture-git-bench`, which delegates here.
#
# Determinism guarantees:
#   - Fixed author + committer names/emails.
#   - Fixed commit dates via GIT_*_DATE env vars.
#   - Fixed file content from */sources/*.
#   - Output SHAs stable across runs and across machines.

SHELL := /bin/bash

HERE := $(dir $(realpath $(lastword $(MAKEFILE_LIST))))

# Pinned identity + dates for reproducibility.
export GIT_AUTHOR_NAME    := Bench
export GIT_AUTHOR_EMAIL   := bench@localhost
export GIT_COMMITTER_NAME := Bench
export GIT_COMMITTER_EMAIL:= bench@localhost
export GIT_AUTHOR_DATE    := 2026-01-01T00:00:00 +0000
export GIT_COMMITTER_DATE := 2026-01-01T00:00:00 +0000

.PHONY: all clean small-repo dirty-tree

all: small-repo dirty-tree

clean:
	rm -rf "$(HERE)small-repo" "$(HERE)dirty-tree"

small-repo:
	rm -rf "$(HERE)small-repo"
	mkdir -p "$(HERE)small-repo"
	cp "$(HERE)small-repo.sources/"*.txt   "$(HERE)small-repo/"
	cp "$(HERE)small-repo.sources/README.md" "$(HERE)small-repo/"
	cd "$(HERE)small-repo" && \
		git init -q -b main && \
		git add -A && \
		git commit -q -m "initial import" && \
		echo "line one of file3 (edited)" > file3.txt && \
		echo "line two of file3"          >> file3.txt && \
		git add file3.txt && \
		GIT_AUTHOR_DATE="2026-01-02T00:00:00 +0000" \
		GIT_COMMITTER_DATE="2026-01-02T00:00:00 +0000" \
		git commit -q -m "edit file3"

dirty-tree: small-repo
	rm -rf "$(HERE)dirty-tree"
	# Clone small-repo → dirty-tree (file://) then dirty it deterministically.
	git clone --quiet "file://$(HERE)small-repo" "$(HERE)dirty-tree"
	cd "$(HERE)dirty-tree" && \
		git apply "$(HERE)dirty-tree.sources/file1.patch" && \
		cp "$(HERE)dirty-tree.sources/new-file.txt" ./new-file.txt
	# new-file.txt is untracked; file1.patch is applied but NOT committed.
	# `git status` on dirty-tree will show both: M file1.txt, ?? new-file.txt
