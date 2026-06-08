<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: orchestration
depends:
  - ../references/agent-dispatch.md
  - ../references/git-worktree-merging.md
  - ../references/model-selection.md
related_skills: [agent-dispatch, worktree-merge, plan-management]
status: draft
last_verified: 2026-06-08
---

# Autonomous parallel phase orchestration (Conductor)

## Question

keese can dispatch **one** agent into **one** worktree
([agent-dispatch.md](../references/agent-dispatch.md)) and merge it back
([git-worktree-merging.md](../references/git-worktree-merging.md)). Driving
*many* phases in parallel — choosing what can run together, surviving connection
drops, staying inside Claude usage limits, and merging into a `main` that a human
session keeps changing — was done by hand. How do we make that orchestration loop
a first-class, recoverable, budget-aware tool?

## Decision

Adopt **Conductor**: a self-contained bash orchestrator on `main` under
`conductor/` that runs the build in **waves**. Each wave re-scans plan state,
selects a conflict-free batch of ready phases, scores/plans them in parallel,
dispatches each into an isolated git worktree as a Claude agent, monitors them
with on-disk checkpoints, and merges completed work back through the existing
green gate. Locked decisions:

- **Autonomy = hybrid.** Autonomous: scan, score, plan, implement-in-worktree,
  mark stubs, refresh worktrees, recover. **Ask the user** before any merge to
  `main` and before any wave whose projected cost would breach the budget ceiling
  (`.claude/rules/07-autonomy.md`).
- **Usage guard = static estimate + live monitor.** Pre-flight a per-phase
  token/cost estimate; at runtime read local usage and scale or pause concurrency
  so plan limits are never exceeded.
- **The dispatch tooling is protected-path.** Conductor authors only on `main`;
  the merge gate never lets a worktree agent edit the sandbox controls (see the
  protected-path list in [`conductor/worktree-merge.sh`](../../conductor/worktree-merge.sh)).
- **Phases route to specialized personas.** keese ships 13 purpose-built agents;
  a phase's `agent:` frontmatter (or stage/tier fallback) selects the right one
  (a new CRD → `crd-author`, a reconciler → `controller-author`, the OLM bundle →
  `olm-author`, the OpenFGA model → `rebac-modeler`, …).

## Why not the status quo

The hand-driven build proved the pattern but was prose-bounded and
unrecoverable: a dropped connection lost all in-flight state, "don't exceed
usage" was a human judgement, and overlap was hand-picked from the plans README.
Conductor turns each of those into a mechanism.

## Architecture — the wave loop

```text
recover() → [ scan → conflict-batch → budget pre-flight (ASK if over)
            → score+plan (parallel) → dispatch (detached, staggered)
            → poll (heartbeat · budget · refresh) → completion gate (ASK to merge)
            → advance ] repeat until no ready phases or budget exhausted
```

Six subsystems, one shared on-disk truth (the **run ledger**):

| Subsystem | Role |
|---|---|
| **Scheduler** | Reads every `docs/plans/**/*.md` with a `phase:` frontmatter each wave; picks READY phases (deps satisfied) and greedy-colors them by predicted **file/domain footprint** into a conflict-free batch. |
| **Budget guard** | Static estimator sums expected cost per wave; live monitor reads local usage + per-agent `total_cost_usd`; a clamped formula scales concurrency and hard-pauses near the ceiling. |
| **Checkpoint/recovery** | Atomic JSON **ledger** + git-commits-as-checkpoints. On restart, classify each slot and re-dispatch unfinished work to the same worktree. |
| **Status/logs** | Per-thread `status.json` heartbeat + tailable `session.log`; an ANSI dashboard. Only a bounded `SUMMARY.md` ever re-enters orchestrator context. |
| **Merge/drift** | Merge rebases onto current `main` and runs `make lint && make test` **after** rebase; long worktrees are refreshed mid-flight by a dependency/overlap heuristic. |
| **Pre-flight/revisit** | Parallel rubric scoring gates dispatch; shipped stubs record a `revisit_when_*` trigger so a later wave auto-requeues them. |

## Footprint model (keese)

A phase's footprint is a set of coarse DOMAIN tokens plus HOT shared paths; two
phases conflict if these intersect ([`conductor/lib/footprint.sh`](../../conductor/lib/footprint.sh)).
keese tokens: `api:<group>/<kind>`, `ctrl:<group>/<kind>`, `cmd:<bin>`,
`go:internal/<pkg>`. HOT (force serialize): `go.mod`/`go.sum`/`PROJECT` (`HOT:deps`),
the generated deepcopy (`HOT:gen:<group>`), `config/crd/kustomization.yaml` +
`config/rbac/role.yaml` (`HOT:manifests`), the OLM CSV + `bundle/**` (`HOT:olm`),
and the OpenFGA model + `internal/authz/**` (`HOT:rebac`). This replaces the older
"solo agent" notion for `olm-author`/`rebac-modeler` with footprint-driven
serialization: their CSV / `model.fga` edits never run in parallel with a
conflicting phase. Phases declare `outputs:` accurately; the merge green gate +
rebase-conflict detection are the backstop if a phase under-declares.

## Dispatch substrate

Two modes, same scheduler/budget/merge gates:

- **Chat-driven (default).** The chat session *is* the conductor (the `/conduct`
  command): it dispatches each phase as a worktree-isolated **subagent via the
  Agent tool**, persona+model+effort resolved from `.claude/agents/*`, reviews the
  returned SUMMARY, and merges with `worktree-merge.sh`. Recovery = git commits +
  chat continuity.
- **Program (opt-in).** `conductor.sh` runs the loop standalone, dispatching
  detached `claude -p --agent <name> --session-id <uuid> --permission-mode dontAsk
  --max-budget-usd <cap> --add-dir <dir> --output-format stream-json` processes.
  `--agent` adopts the same persona the Agent tool uses, so both modes dispatch
  identically. Liveness = OS pid + `status.json` + commits.

Per-task **model + effort** is authoritative from agent frontmatter
(`CLAUDE_CODE_SUBAGENT_MODEL=inherit`): architect/security-reviewer/rebac-modeler
→ opus·xhigh, implementer/crd-author/controller-author/… → sonnet·high,
explorer/debugger → haiku (no effort); max-effort override via `--effort max` /
`CLAUDE_CODE_EFFORT_LEVEL=max` (esp. design). Contract (env vars, ledger schema)
lives in [agent-dispatch.md](../references/agent-dispatch.md).

## Requirement → mechanism

| Requirement | Mechanism |
|---|---|
| Overlap → safe parallel batches | Scheduler footprint + conflict-coloring |
| Isolated worktrees | `conductor/agent-dispatch.sh` (unchanged isolation) |
| Copious checkpoints + recovery | Git commits + atomic ledger + classify-on-restart |
| Lean main context + status + tailable log | `SUMMARY.md`-only return; `status.json` + `session.log` + dashboard |
| Safe merge w/ drift + periodic refresh | Gate-after-rebase + dependency/overlap refresh heuristic |
| Parallel score+plan; stub→revisit | Pre-flight scoring gate + `revisit_when_*` frontmatter trigger |
| Concurrent human session edits `main` | Per-wave rescan + rebase-onto-current-`main` + footprint refresh |
| Rate-limit aware; warn; scale | Static pre-flight estimate (ASK over ceiling) + live monitor scaling |
| Right tool per phase | `agent:`/stage/tier routing through `conductor/lib/agents.sh` |

## Safety / threat model

- **Blast radius**: every implementer is worktree-isolated; the deny list in
  `.claude/settings.json` is the floor even under `bypassPermissions`;
  `conductor/hooks/worktree-guard.sh` adds path + Bash screening for dispatched
  agents; the merge gate refuses protected-path diffs. A bad agent is discarded
  with its worktree.
- **No unilateral main changes**: merges and budget-overrun are blocking ASK gates.
- **Singleton**: a PID lock (noclobber, `flock`-free for macOS) blocks a second
  conductor from racing the ledger.
- **Secrets**: the agent log helper sanitizes credential-shaped strings; the guard
  reads usage locally with no network call. This complements rule
  `05-security-zero-trust.md` (which still wins on any conflict).

## Deferred / stubs

- Conductor integration suite (scheduler golden file, ledger transactions, mock
  dispatch round-trip) beyond the unit tests in `conductor/tests/`
  (`make conductor-test`) — follow-up test-engineer phase.
- `revisit_when_*` backfill for existing `partial`/`shipped-with-stubs` phases is
  best-effort; until backfilled, those phases are not auto-requeued.

## Related

- Contract + flags: [agent-dispatch.md](../references/agent-dispatch.md)
- Merge + refresh: [git-worktree-merging.md](../references/git-worktree-merging.md)
- Autonomy matrix + protected paths: [`.claude/rules/07-autonomy.md`](../../.claude/rules/07-autonomy.md)
- Entry point: `/conduct` ([`.claude/commands/conduct.md`](../../.claude/commands/conduct.md))
- The system itself: [`conductor/README.md`](../../conductor/README.md)

## Iteration log

| Iter | Date | Score | Verdict | Notes |
|------|------|-------|---------|-------|
| 1 | 2026-06-08 | — | DRAFT | Ported from the conductor reference implementation; adapted to keese's operator layout (api/internal/controller/bundle footprints), 13 specialized agents + `agent:` routing, and the `make lint && make test` green gate. To be scored to ≥ 90 (`/score-plan`) before promotion to `current`. |
