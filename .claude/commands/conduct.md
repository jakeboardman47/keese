---
description: Conductor — drive parallel phase implementation from THIS chat (scan → batch → dispatch subagents → review → merge)
argument-hint: "[next | --once | --max N | --max-effort | --dry-run] | program [--resume]"
allowed-tools:
  - Agent
  - Bash(conductor/*)
  - Bash(git worktree list*)
  - Bash(git -C * rev-list*)
  - Bash(git -C * log*)
  - Read
  - Edit
model: opus
---

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

You (this chat session) are the **Conductor**. Drive the wave loop yourself using
the Agent tool + the `conductor/` scripts. Do **not** launch a separate program
unless the user explicitly asks for `program` mode.

See [ADR 29](../../docs/designs/29-conductor-orchestration.md) and
[agent-dispatch.md](../../docs/references/agent-dispatch.md). Autonomy + protected
paths: [`.claude/rules/07-autonomy.md`](../rules/07-autonomy.md).

## Chat-driven wave loop (default)

Run ONE wave per invocation unless told to continue. Each wave:

1. **Scan & batch** — `conductor/scheduler.sh --max <N>` (default N=3). It returns
   `wave` (ready, conflict-free), `deferred`, `blocked`. Each wave entry already
   carries the resolved `agent` (the specialized persona) and `phase_file`. It
   excludes done/shipped/in-progress/running and `dispatch: manual` phases and
   resolves a moving `main`.
   - **Conflict-check (optional):** the footprint coloring is static. For a risky
     wave, skim the batched phases' plans and defer any pair that would obviously
     edit the same files beyond their declared `outputs:`. (Program mode automates
     this with `--conflict-check`.)
2. **Budget pre-flight** —
   `source conductor/lib/common.sh && source conductor/lib/budget-guard.sh`,
   then `budget::config_ready || budget::setup_wizard` (or ask the user for a
   per-window USD ceiling), then `budget::preflight <wave.json>`. Show the user:
   wave phases, per-phase agent+model+effort, and `≈$cost vs $ceiling`.
3. **Confirm (hybrid gate)** — present the plan and **ask the user before
   dispatching**. If projected cost > ceiling, ask again explicitly.
4. **Dispatch in parallel** — for EACH wave phase, one `Agent` call in a SINGLE
   message (so they run concurrently), with:
   - `subagent_type`: the wave entry's `agent` field — keese's SPECIALIZED
     personas: `crd-author` (new CRD types), `controller-author` (reconcilers),
     `olm-author` (bundle/CSV), `rebac-modeler` (OpenFGA model), `guardrail-author`,
     `infra-bootstrap`, `test-engineer`, `architect` (design/ADR), `implementer`
     (general code), `explorer`/`debugger` (search). Trust the resolved `agent`.
   - `isolation: "worktree"`.
   - `model`: leave to the agent's frontmatter (now honored — settings use
     `CLAUDE_CODE_SUBAGENT_MODEL=inherit`). Override only to upgrade a task.
   - prompt: "Implement `<phase_file>` following `.claude/agents/<agent>.md` and
     `.claude/rules/*`. SSA-only writes; obey the keese conventions. Commit per
     logical unit (commits are checkpoints). Stay inside your worktree and your
     phase's declared `outputs:` footprint. If you touch `*_types.go`, run
     `make manifests generate bundle` and commit the regenerated artifacts. Run
     `make lint && make test`. Return a short SUMMARY: what shipped, **stubs**
     (declare each + add a `revisit_when_*` frontmatter trigger and set
     `status: shipped-with-stubs`), follow-ups, test evidence, and any
     `MEMORY.md entries to add on merge`."
5. **Review-fix** — read each returned SUMMARY (keep context lean). Then review
   the diff: run `/code-review` or a `security-reviewer`/`architect` subagent over
   the phase's worktree. If there are **blocking** findings (correctness/security
   bug or an undeclared stub), re-dispatch the implementer with the findings to
   fix them (cap ~2 rounds) **before** offering the merge. (Program mode automates
   this with `--review`.)
6. **Merge (hybrid gate)** — for each finished phase, find its branch
   (`git worktree list` → `agent/<id>-<agent>`), then **ask the user**, then
   `conductor/worktree-merge.sh <branch>` (rebases onto current main, runs
   `make lint && make test` after rebase, abort+escalate on conflict, rejects
   protected-path diffs). Never merge unattended.
7. **Record & loop** — set the phase doc `status:` (`complete`/`shipped` or
   `shipped-with-stubs` + revisit) **and** its `docs/plans/**/README.md` row to
   match. Continue to the next wave only if the user said `next`/continue.

## Model + effort per task

Auto-selected by agent frontmatter (`effort:`): `architect`/`security-reviewer`/
`rebac-modeler` → opus·xhigh; `implementer`/`crd-author`/`controller-author`/
`olm-author`/`guardrail-author`/`infra-bootstrap`/`test-engineer`/`plan-scorer` →
sonnet·high; `explorer`/`debugger` → haiku (no effort). **Max-effort override**
(esp. design): pass `--max-effort` — set `CLAUDE_CODE_EFFORT_LEVEL=max` for the
session before dispatching and prepend `ultrathink` to design prompts. Reset with
`/effort auto` when done.

## Budget / limits

`conductor/config/budget-guard.json` holds the per-window USD ceiling (set via the
wizard) + warn/pause fractions. Before each wave, show projected cost; if a wave
would breach the ceiling, ask. The user does not want to exceed plan limits — when
in doubt, dispatch fewer phases.

## Program mode (only if the user asks for `program`)

`conductor/conductor.sh [--dry-run|--once|--resume] [--conflict-check] [--review]`
runs the loop as a standalone detached-`claude -p` daemon (with `--effort` +
`--max-budget-usd` per agent, exponential backoff on retries, and the
`worktree-guard` hook active for dispatched agents). `--conflict-check` adds an LLM
wave conflict pass; `--review` adds the review-fix loop. Watch with
`conductor/status.sh`; tail a thread with
`tail -f .plan-logs/conduct/latest/<phase>/session.log`. Needs the `claude` CLI in
the dev shell (`nix develop`); not yet pinned in `flake.nix`.
