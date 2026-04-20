---
description: Merge a completed agent worktree back to main
argument-hint: "<branch-name> [--squash] [--keep-worktree]"
allowed-tools:
  - Bash(scripts/worktree-merge.sh *)
  - Bash(git log *)
  - Bash(git status)
  - Read
model: sonnet
---

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 Aviz Networks, Inc. -->

Merge a completed agent worktree back to `main`.

Usage: `/merge-worktree agent/phase-04-crd-bootstrap`

Steps:
1. Verify the branch has commits that build (the agent should have run tests before
   reporting complete).
2. Run `scripts/worktree-merge.sh $ARGUMENTS`. By default the script rebases on main
   and fast-forwards; `--squash` collapses into a single commit.
3. Report the merge outcome and the HEAD SHA.
4. If a conflict occurs, stop and surface the conflicting files — do not attempt
   automated resolution.

See [docs/references/git-worktree-merging.md](../../docs/references/git-worktree-merging.md).
