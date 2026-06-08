<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: index
depends: [docs/designs/29-conductor-orchestration.md, docs/references/agent-dispatch.md, docs/references/git-worktree-merging.md]
related_skills: [conduct, agent-dispatch, worktree-merge, plan-management]
status: current
last_verified: 2026-06-05
---

# conductor/ — autonomous parallel phase orchestrator

Conductor runs the build in **waves**: each wave re-scans plan state, selects a
conflict-free batch of ready phases, dispatches each into an isolated git
worktree as a Claude implementer, monitors them with on-disk checkpoints, and
merges finished work back through the green gate. It turns the hand-driven
night-build into a recoverable, budget-aware mechanism.

Design rationale and the full requirement→mechanism table live in
[ADR 29](../docs/designs/29-conductor-orchestration.md). The dispatch contract (env vars,
ledger schema, CLI flags) lives in
[agent-dispatch.md](../docs/references/agent-dispatch.md).

> **Protected path.** Everything under `conductor/` controls what dispatched
> agents may do, so `worktree-merge.sh` refuses to merge any branch that touches
> it. Author here on `main` only — never from a worktree. See
> [`.claude/rules/07-autonomy.md`](../.claude/rules/07-autonomy.md).

## The wave loop

```text
recover() → [ scan → conflict-batch → budget pre-flight (ASK if over ceiling)
            → dispatch (detached, staggered) → poll (heartbeat · budget · refresh)
            → completion gate (ASK to merge) → advance ] repeat until drained
```

**Autonomy is hybrid.** Scan, batch, dispatch, implement, refresh, and recover
are autonomous. Two gates always ASK the user: **merging to `main`** and any
wave whose projected cost would **breach the budget ceiling**. Run unattended
(no tty), both gates take the safe default — hold the merge, don't overspend —
so an overnight run never blocks and never runs past the ceiling.

## Two ways to run it

| Mode | Driver | Use |
|---|---|---|
| **Chat-driven** (default) | `/conduct` skill — *this* chat session is the conductor, dispatching each phase as a worktree-isolated subagent via the Agent tool | normal interactive use; recovery = git commits + chat continuity |
| **Program** (opt-in) | `conductor.sh` — a standalone detached daemon dispatching headless `claude -p` processes | unattended / overnight runs; recovery = ledger + OS pids |

Both share the same scheduler, budget guard, and merge gate. Start with
`/conduct`; reach for the program only when you explicitly want an unattended
daemon. See [`../.claude/commands/conduct.md`](../.claude/commands/conduct.md).

## Directory map

### Entry points (executable)

| Script | Role |
|---|---|
| [`conductor.sh`](conductor.sh) | Program-mode daemon: runs the whole wave loop. Flags: `--max N`, `--once`, `--dry-run`, `--resume`, `--conflict-check`, `--review`, `--budget-setup`. |
| [`scheduler.sh`](scheduler.sh) | Read-only. Scans every `docs/plans/**/*.md` (with a `phase:` frontmatter), picks READY phases (deps met), greedy-colors them by footprint into a conflict-free batch, emits a wave manifest as JSON on stdout. |
| [`dispatch.sh`](dispatch.sh) | Launches ONE phase as a detached, headless `claude -p --agent <name>` (default `implementer`) in its worktree; model/effort from the persona; prints a JSON slot descriptor for the ledger. |
| [`agent-dispatch.sh`](agent-dispatch.sh) | Lower-level: create or attach a worktree + branch for `<phase> <agent>`. Used by `dispatch.sh` and the `agent-dispatch` skill directly. |
| [`worktree-merge.sh`](worktree-merge.sh) | Rebase a finished branch onto current `main`, run the green gate (`make lint && make test`) **after** rebase, fast-forward merge, clean up. Rejects protected-path diffs. |
| [`worktree-refresh.sh`](worktree-refresh.sh) | Rebase a still-running worktree onto an advanced `main` when drift overlaps its footprint. Never touches a dirty tree; aborts + escalates on conflict. |
| [`status.sh`](status.sh) | Live, read-only ANSI dashboard for a run (ledger + per-phase heartbeat + budget snapshot). |
| [`workflows.sh`](workflows.sh) | `/workflows` surface: `board` (registry runs + agent worktrees + `~/.claude` workflow journals), `tail`/`kill`/`pause`/`resume` for shell-owned (conductor + issue) runs. |
| [`intake.sh`](intake.sh) | GitHub-issue intake: poll `agent:build` issues → author-gate → claim → design (`architect`) + implement (`implementer`) → **draft** PR (`Closes #N`), no auto-merge. `--dry-run` previews. |

### `lib/` (sourced, not executed)

| File | Role |
|---|---|
| [`common.sh`](lib/common.sh) | Repo paths (`REPO_ROOT`, `PLAN_LOGS`, worktree base) + colorized logging. No dependency on `scripts/lib` — the tree is self-contained. |
| [`conductor-utils.sh`](lib/conductor-utils.sh) | Portable date/stat, UUIDs, atomic writes, a `flock`-free singleton lock, exponential backoff. |
| [`ledger.sh`](lib/ledger.sh) | The **run ledger** — the sole cross-restart truth (`.plan-logs/conduct/<run>/ledger.json`). Every mutation is atomic; only `conductor.sh` writes it. |
| [`footprint.sh`](lib/footprint.sh) | Predict a phase's file/domain footprint (apps, Go packages, SDK + HOT shared paths like the OpenAPI contract, mount file, migrations, lockfiles) and decide whether two phases conflict. |
| [`budget-guard.sh`](lib/budget-guard.sh) | Static per-wave cost estimate + live usage monitor (ccusage or local fallback) that scales concurrency and hard-pauses near the ceiling; the budget setup wizard. |
| [`review.sh`](lib/review.sh) | Optional LLM passes: a wave conflict second-opinion (`--conflict-check`) and a post-implementation diff review that bounces blocking findings back to the implementer (`--review`). Best-effort; failure = safe default. |
| [`agents.sh`](lib/agents.sh) | Single adapter over `.claude/agents/*.md` so both dispatch modes resolve the same persona/model/effort/tools (`agents::resolve`/`system_prompt`/`for_stage`/`for_tier`). Replaces the old hardcoded `model-effort.sh` map; program mode now dispatches `claude -p --agent <name>`. |
| [`conduct-log.sh`](lib/conduct-log.sh) | Agent-side reporting. A dispatched implementer sources this and calls `conduct::state` / `conduct::pct` to write its `session.log` + `status.json` heartbeat. No-op outside a conductor run; sanitizes credential-shaped strings. |
| [`frontmatter.py`](lib/frontmatter.py) | Parse a phase doc's YAML frontmatter → JSON, no third-party deps. |
| [`usage-window.py`](lib/usage-window.py) | Local, network-free usage/cost estimate from `~/.claude/projects/*.jsonl` — the budget guard's fallback when `ccusage` is absent (approximate). |

### `config/` and `hooks/`

| File | Role |
|---|---|
| [`config/budget-guard.json`](config/budget-guard.json) | Per-window USD ceiling, warn/pause fractions, per-agent hard cap, cost estimator, and learned calibration. `windowCeilingUSD` starts `null`; the wizard writes it on first run. |
| [`config/stages.json`](config/stages.json) | Pipeline stage / model-tier → `.claude/agents/*` persona map; the single source `agents.sh` reads for `for_stage` / `for_tier`. |
| [`hooks/worktree-guard.sh`](hooks/worktree-guard.sh) | PreToolUse defense-in-depth for `bypassPermissions` agents: keeps Edit/Write inside the project tree, blocks `cd` out of it and writes to system paths, and screens Bash for prompt-injection patterns. |

## Quick start

```bash
nix develop                              # bash >= 4, jq, python3, claude required

conductor/scheduler.sh --max 3 | jq .    # preview the next conflict-free wave
conductor/conductor.sh --budget-setup    # one-time: set the per-window USD ceiling
conductor/conductor.sh --dry-run         # full wave preview + budget pre-flight, no dispatch

conductor/conductor.sh --once --review   # run exactly one wave, with the review-fix loop
conductor/status.sh                      # watch a live run (another terminal)
tail -f .plan-logs/conduct/latest/<phase>/session.log   # tail one thread
conductor/conductor.sh --resume          # reattach / re-dispatch after an interruption
```

Prefer driving the loop from chat with `/conduct` for normal work; the commands
above are the program-mode equivalents.

## Core concepts

- **Run ledger** — one atomic JSON file per run under
  `.plan-logs/conduct/<run-id>/` (symlinked `latest`). Slots track each phase's
  status, pid, branch, worktree, commits, and cost. It survives restarts;
  `--resume` classifies every non-terminal slot and re-dispatches unfinished
  work (git commits are the per-phase checkpoints).
- **Footprint coloring** — phases are batched only if their predicted footprints
  don't intersect, so parallel worktrees rarely collide on merge. `--conflict-check`
  adds an LLM second opinion.
- **Budget guard** — a wave is pre-flighted against the window ceiling (over →
  ASK); the live monitor scales concurrency down and hard-pauses as actual usage
  approaches the ceiling. `perAgentMaxUSD` is a hard per-implementer cap.
- **Drift handling** — `main` moves under long phases; merge always rebases onto
  current `main` and gates *after* rebase, and `worktree-refresh.sh` rebases
  long-running worktrees mid-flight when drift overlaps their footprint.
- **Stubs → revisit** — an implementer that ships a stub sets
  `status: shipped-with-stubs` plus a `revisit_when_*` frontmatter trigger; a
  later wave auto-requeues it once the trigger clears.

## Configuration

`config/budget-guard.json` (edit directly or re-run `--budget-setup`):
`windowHours`, `windowCeilingUSD`, `perAgentMaxUSD`, `warnFraction`,
`pauseFraction`, `maxConcurrentSlots`, `dispatchStaggerSeconds`,
`estimatorSafetyMargin`, `estimator.{perTierUSD,sizeMultiplier}`, `calibration`.

Operator-tunable env vars (with defaults):

| Var | Default | Effect |
|---|---|---|
| `CONDUCT_MAX_CONCURRENT` | `4` | scheduler wave width |
| `CONDUCT_POLL_SEC` | `45` | poll interval |
| `CONDUCT_STUCK_SEC` | `900` | no heartbeat **and** no commit → stuck |
| `CONDUCT_MAX_ATTEMPTS` | `3` | re-dispatch budget per phase |
| `CONDUCT_MAX_REVIEW_ITERS` | `2` | review-fix rounds before merge |
| `CONDUCT_REFRESH_THRESHOLD` | `5` | commits-behind before a refresh is considered |
| `CONDUCT_NO_NPX` | `0` | skip `npx ccusage`, force the local fallback |
| `WORKTREE_BASE` | `<repo>-worktrees` | where worktrees are created |
| `PLAN_LOGS` | `<repo>/.plan-logs` | run + log root |

`dispatch.sh` injects the agent-side contract (`CONDUCT_PHASE_ID`,
`CONDUCT_LOG_PATH`, `CONDUCT_STATUS_PATH`, `CONDUCT_SUMMARY_PATH`,
`CONDUCT_SESSION_ID`, …) that `conduct-log.sh` reads.

## Safety model

Every implementer is worktree-isolated; the `.claude/settings.json` deny list is
the floor even under `bypassPermissions`; `worktree-guard.sh` adds path + Bash
screening for dispatched agents; and `worktree-merge.sh` refuses any diff
touching protected paths (`CLAUDE.md`, `.claude/**`, `conductor/**`,
`scripts/lib/**`, CI/pre-commit config, …). A bad agent is discarded with its
worktree; `main` stays clean. A PID lock prevents two conductors racing the
ledger. The agent log helper redacts credential-shaped strings.

## Portability

`lib/common.sh` is self-contained (no `scripts/lib` dependency), so the whole
`conductor/` tree can be copied into another repo. You'll still need: `bash >= 4`,
`jq`, `python3`, the `claude` CLI, a `docs/plans/` tree of phase docs with the
frontmatter `scheduler.sh` reads, and a `make lint` / `make test` green gate.

## See also

- [ADR 29 — conductor orchestration](../docs/designs/29-conductor-orchestration.md) (why + architecture)
- [agent-dispatch.md](../docs/references/agent-dispatch.md) (dispatch contract)
- [git-worktree-merging.md](../docs/references/git-worktree-merging.md) (merge + refresh)
- [`/conduct` command](../.claude/commands/conduct.md) (chat-driven entry point)
