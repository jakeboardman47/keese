---
name: agent-dispatch
description: Dispatch a Claude subagent into a git worktree; select appropriate model tier
type: skill
depends: []
options: [worktree_base]
model: haiku
---

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) {{YEAR}} {{ORG_NAME}} -->

# Agent Dispatch

## When to use

Farming out a phase of work to an isolated subagent so other agents can work in parallel
without conflict.

## Inputs

- `worktree_base`: parent directory for worktrees. Default: sibling of repo,
  `../{{PROJECT_NAME}}-worktrees/`.

## Steps

1. Pick the right agent:
   - `architect` (Opus) — design or plan authoring
   - `implementer` (Sonnet) — write code
   - `explorer` (Haiku) — code/doc search
   - `security-reviewer` (Opus) — audit pass
   - `debugger` (Haiku) — investigate a failure
2. `scripts/agent-dispatch.sh <phase-id> <agent-name> [--branch=...]` creates:
   - a fresh branch `agent/<phase-id>-<slug>` off `main`
   - a worktree in `$worktree_base/<phase-id>-<slug>`
   - a prompt file in the worktree's `.plan-logs/prompt.md`
3. The agent runs in that worktree with the shared `.claude/` config.

## Model-tier discipline

- Never use Opus for search / summary / formatting — use Haiku.
- Use Sonnet for code production; Opus only when trade-offs and architecture actually
  need it.

## Anti-patterns

- Do not dispatch multiple agents into the same worktree.
- Do not let agents touch `CLAUDE.md` / `MEMORY.md` / `.claude/rules/*` on branches —
  those edits happen on `main`.

## References

- [../../docs/references/agent-dispatch.md](../../docs/references/agent-dispatch.md)
