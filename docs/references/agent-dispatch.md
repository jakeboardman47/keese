<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: reference
depends: [docs/references/git-worktree-merging.md, docs/references/model-selection.md]
related_skills: [agent-dispatch]
status: current
last_verified: 2026-04-19
---

# Multi-Agent Dispatch

## Motivation

One human + many Claude agents editing the same checkout corrupts state fast:
overlapping diffs, racing code-generators, conflicting lockfile writes. Each
agent gets its own **git worktree** so they are isolated at the filesystem
level while sharing one `.git` directory.

## Dispatch Command

```sh
scripts/agent-dispatch.sh <phase-id> <agent-name> [--branch=<name>] [--base=main]
```

Examples:

```sh
scripts/agent-dispatch.sh phase-02 implementer
scripts/agent-dispatch.sh phase-03a security-reviewer --branch=review/phase-03a
```

The script:

1. Creates the branch off `--base` (default `main`) if it does not exist.
2. Adds a worktree under the sibling layout (below).
3. Copies `.claude/` into the worktree (or symlinks on Linux).
4. Writes `.plan-logs/<phase-id>/AGENT.md` with the assigned agent + model.
5. Prints the next-step command: `cd <worktree> && claude code`.

## Layout

Worktrees live in a sibling directory so they never appear in IDE file trees
that watch the main checkout:

```
<path-to-repo>/
  keese/                 # main checkout, branch: main
  keese-worktrees/
    phase-02-implementer/           # branch: feat/phase-02
    phase-03a-security-reviewer/    # branch: review/phase-03a
    ...
```

## Agent Selection Table

| Agent              | Model   | Typical Task                                    |
|--------------------|---------|-------------------------------------------------|
| architect          | Opus    | Draft designs (ADR), critique plans             |
| implementer        | Sonnet  | Write code, tests, docs                         |
| explorer           | Haiku   | Search, enumerate files, list symbols           |
| security-reviewer  | Opus    | Audit RBAC, auth paths, signing wiring          |
| debugger           | Haiku   | Triage failing tests, parse stack traces        |

See [model-selection.md](model-selection.md) for the cost reasoning.

## Shared Config

- `.claude/agents/*.md` — per-agent system prompts with `model:` frontmatter.
- `.claude/skills/*.md` — skills available to all agents.
- `.claude/rules/*.md` — hard rules (read-only in worktrees).
- `MEMORY.md` — **read-only** on non-main branches; only the orchestrator on
  `main` appends to it. The dispatch script sets `chmod a-w MEMORY.md` in the
  worktree to enforce this.

## Completion Protocol

When an agent finishes, it writes:

```
.plan-logs/<phase-id>/SUMMARY.md
```

Contents (minimal):

```markdown
---
phase: phase-02
agent: implementer
model: claude-sonnet-4-6
ended: YYYY-MM-DDTHH:MM:SSZ
branch: feat/phase-02
worktree: ../keese-worktrees/phase-02-implementer
status: ready-for-merge     # ready-for-merge | blocked | abandoned
---

## What shipped
- <bullet>

## Follow-ups
- <bullet>

## Test evidence
- make lint: PASS
- make test: PASS
```

The agent then exits. Merging is the orchestrator's job —
see [git-worktree-merging.md](git-worktree-merging.md).

## Related

- Script: `scripts/agent-dispatch.sh`.
- Merge: [git-worktree-merging.md](git-worktree-merging.md).
- Models: [model-selection.md](model-selection.md).
