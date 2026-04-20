---
description: Dispatch a Claude subagent into a git worktree for a phase
argument-hint: "<phase-id> <agent-name> [--branch=<name>]"
allowed-tools:
  - Bash(scripts/agent-dispatch.sh *)
  - Bash(git worktree list)
  - Read
model: sonnet
---

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) {{YEAR}} {{ORG_NAME}} -->

Dispatch an agent into an isolated git worktree to work on a phase.

Usage: `/dispatch phase-04 implementer`

Steps:
1. Verify the phase exists: `docs/plans/$ARGUMENTS_1.md` must be present with
   status `planned` or `in-progress`.
2. Run `scripts/agent-dispatch.sh $ARGUMENTS` to create the worktree and kick off
   the agent.
3. Return the worktree path and branch name.
4. Remind the user: merge back with `/merge-worktree <branch>` when the agent
   reports complete.

See [docs/references/agent-dispatch.md](../../docs/references/agent-dispatch.md).
