<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: reference
depends: [docs/references/agent-dispatch.md, docs/references/conventional-commits.md]
related_skills: [worktree-merge]
status: current
last_verified: 2026-04-19
---

# Git Worktree Merging

Agents ship work in a worktree branch; the orchestrator merges back to `main`.
This file specifies how.

## Merge Command

```sh
scripts/worktree-merge.sh <branch> [--squash] [--keep-worktree]
```

Flags:

- `--squash` — collapse all commits into one with a rewritten, conventional
  message. Use for WIP-heavy branches.
- `--keep-worktree` — leave the worktree and branch in place after merge.
  Default deletes both.

Example:

```sh
scripts/worktree-merge.sh feat/phase-02 --squash
```

## Default Flow (non-squash)

The script performs, in order:

1. `cd` to the worktree.
2. `git fetch origin main`
3. `git rebase origin/main` — abort on conflict (see policy below).
4. Run the pre-merge gate (below). Abort on any failure.
5. `cd` to main checkout; `git merge --ff-only <branch>`.
6. `git push origin main`.
7. `git worktree remove <path>` and `git branch -d <branch>` unless `--keep-worktree`.

## Squash Flow

Adds two steps before the ff-only merge:

1. `git reset --soft $(git merge-base HEAD origin/main)`
2. `git commit -m "<conventional message>"` — message is taken from
   `.plan-logs/<phase>/SUMMARY.md` frontmatter or prompted if missing.

Squash is the only case where history is rewritten. Never squash review
branches (security-reviewer output); those carry fine-grained audit trail.

## Conflict Policy

**Abort and escalate.** The script:

1. `git rebase --abort` on conflict.
2. Writes `.plan-logs/<phase>/CONFLICT.md` with `git status` + conflict files.
3. Exits non-zero; prints next-step command for the orchestrator.

Never auto-resolve. A Claude agent resolving a conflict unsupervised has
erased correct work more than once; this rule is load-bearing.

## Protected Paths

The script refuses to merge any branch whose diff touches these four paths —
they drive prompt-cache warmth and global agent behavior and must be edited on
`main` only:

- `CLAUDE.md`
- `MEMORY.md`
- `.claude/rules/**`
- `.claude/settings.json`

Everything else is editable on a branch. In particular, the following are
**explicitly allowed** even though agents sometimes hesitate to touch them:

- `docs/**` — designs, specs, plans, features, references. Implementation
  branches routinely update `last_verified`, add `source_refs:`, or revise a
  spec that turned out wrong. Subagents can author designs/features in a
  worktree.
- `book/**` — user documentation should track the code it describes.
- `.claude/skills/**`, `.claude/agents/**`, `.claude/commands/**`,
  `.claude/hooks/**` — operational guidance evolves with the code.
- Source code, manifests, scripts, CI — obvious.

To add a stricter guardrail on a specific path, edit the `protected_hits`
regex in [`../../scripts/worktree-merge.sh`](../../scripts/worktree-merge.sh) on
`main`. There is intentionally no override flag — changes that would touch
protected paths must happen on `main`.

## Pre-Merge Gate

Must all pass before the ff-only merge:

```sh
make lint
make test
make test-integration
git log origin/main..HEAD --format=%s | commitlint    # every commit conforms
```

Add project-specific gate steps (code generators, manifests, etc.) to the
script as needed. A dirty working tree after regen means someone forgot to
commit generated files — block merge.

## After Merge

- Post the `SUMMARY.md` content to the PR thread if one exists.
- Append one line to `MEMORY.md` (main checkout only) capturing the phase outcome.
- Remove `.plan-logs/<phase>/AGENT.md` (the active marker); keep `SUMMARY.md` for history.

## Related

- Script: `scripts/worktree-merge.sh`.
- Commit rules: [conventional-commits.md](conventional-commits.md).
- Dispatch: [agent-dispatch.md](agent-dispatch.md).
