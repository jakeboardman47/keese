---
name: worktree-merge
description: Merge a completed agent worktree back into main cleanly
type: skill
depends: [agent-dispatch]
options: []
model: haiku
---

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

# Worktree Merge

## When to use

An agent has reported "complete" on a branch created by `agent-dispatch`.

## Steps

1. Verify branch is green: tests pass, lint passes, conventional commits only.
2. `conductor/worktree-merge.sh <branch-name> [--squash] [--keep-worktree]`:
   - Fetches origin, rebases the branch on `main`.
   - If clean: fast-forwards `main`; deletes branch and worktree by default.
   - If conflict: aborts the merge and prints the conflicting files.
3. On conflict: escalate to the parent conversation — do not attempt automated
   resolution in the skill.

## Merge policy

- Default: fast-forward only. No merge commits.
- `--squash` is allowed when the branch has many WIP commits; rewrite the squashed
  commit message to be Conventional Commits compliant.
- Never merge a branch that has `CLAUDE.md` / `MEMORY.md` / `.claude/rules/*` changes
  without explicit review — those edits belong on `main`.

## References

- [../../docs/references/git-worktree-merging.md](../../docs/references/git-worktree-merging.md)
