<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: runtime
depends:
  - 02-workspace-model.md
  - 04a-openfga-authz-model.md
  - 07-agent-runtime-spi.md
  - 08a-goose-headless-modes.md
  - 10b-token-accounting.md
  - 18-process-lifecycle.md
  - 23-agent-supervision.md
related_skills: [doc-authoring, controller-authoring]
status: current
last_verified: 2026-04-21
rollback: |
  Set cluster-defaults ConfigMap `maxPerWorkspace: 0` to block all new spawns; in-flight
  sub-agents complete naturally. Delete OpenFGA tuples via `scripts/cleanup-subagent-tuples.sh
  <workspace>` if lease-based spawn serialization created orphan tuples. For concurrent-cap
  reduction mid-flight: tuples drain as sub-agents terminate (no forced kill on value decrease).
---

# 08c — Goose Sub-Agents and Limits

## Context

Goose supports spawning sub-agents within a running session. keese caps this at 10
concurrent sub-agents per Workspace, enforced at spawn time via OpenFGA tuple counting
plus a per-Workspace Kubernetes Lease. Budget is shared by default. Orphan sub-agents
on parent SIGTERM are drained then killed within the parent drain window; on SIGKILL
they are tombstoned and deleted by the workspace controller.

## OpenFGA tuple shape and spawn check

**04a iter-5 flag:** add `workspace.active_subagent: [subagent]` relation and
`type subagent` with relations `parent_workspace: [workspace]`,
`budget_shared_with: [workspace]`. Targeted spot-fix on 04a; does not alter existing
relations.

Tuple shapes (written by workspace controller):

| Tuple | Written when | Deleted when |
|---|---|---|
| `workspace:W#active_subagent@subagent:<id>` | Sub-agent spawned | Sub-agent terminates |
| `subagent:<id>#parent_workspace@workspace:W` | Sub-agent spawned | Sub-agent terminates |
| `subagent:<id>#budget_shared_with@workspace:W` | `budgetMode: shared` (default) | Sub-agent terminates |

**Atomic spawn check (TOCTOU-safe):** workspace controller acquires Lease
`coordination.k8s.io/v1/keese-subagent-spawn-<workspace-name>` (TTL 5 s), calls
`ListObjects(relation=active_subagent, object=workspace:W)`, counts result. If count
≥ effective limit: deny `SubAgentLimitExceeded`. If < limit: write all three tuples,
release Lease, call `InvokeSubAgent`. On terminate: controller deletes all three tuples.

TOCTOU window = Lease TTL (5 s). OpenFGA conditional tuples (1.1 schema) flagged as
future optimization to replace Lease; not required for v1alpha1.

**07 iter-2 flag:** add `CleanupSubAgents(ctx, Session) error` optional SPI method
and `CapabilitySupportsSubAgentCleanup bool` in `CapabilityMatrix`.

## Ceiling-hit error surface

When count ≥ effective limit at spawn time:

- SPI `InvokeSubAgent` returns `ErrSubAgentLimitExceeded` (new sentinel in
  `internal/runtime/spi/v1alpha1/errors.go`); callers MUST NOT retry in a loop.
- HTTP: runtime returns `429 Too Many Requests` to the orchestrator; body includes
  `{"code":"SubAgentLimitExceeded","current":10,"limit":10}`.
- K8s event on parent Workspace: reason `SubAgentLimitExceeded`, message
  `"current=<N>, limit=<L>"`. Deduped per workspace per 5 min via event aggregation.
- Metric: `keese_subagent_limit_rejections_total{workspace, tenant}` counter.

## Hierarchical quota

Effective limit = `min(workspace.max, tenant.max, cluster.max)`.

| Layer | Setting | Default | Bounded by |
|---|---|---|---|
| Cluster | `ConfigMap keese-subagent-defaults`.`maxPerWorkspace` | `10` | Operator-managed; no floor |
| Tenant | `Tenant.spec.subagentLimits.maxPerWorkspace` | inherits cluster | VAP: ≤ cluster ceiling |
| Workspace | `Workspace.spec.subagentLimits.max` | inherits tenant | VAP: ≤ effective tenant limit |

`Workspace.status.effectiveSubAgentLimit` surfaces the resolved value.

**Aggregate tenant cap:** `Tenant.spec.subagentLimits.maxPerTenant` (optional; default
unbounded). Tenant controller sums `status.activeSubAgents` across all Workspaces every
reconcile; sets `conditions[SubAgentLimitBlocked]` on over-limit Workspaces.

## Orphan cleanup on parent SIGTERM

SIGTERM path (drain budget 90 s per design 18):

1. Parent `Drain(ctx, session, 90s)` invoked by workspace controller.
2. During drain, runtime calls `CleanupSubAgents(ctx, session)` (gated on
   `CapabilitySupportsSubAgentCleanup`). Budget per sub-agent = `remaining / N`.
3. Each sub-agent receives SIGTERM; drains its own state to
   `/var/run/keese/sessions/<workspace-uid>/subagents/<subagent-id>/session.sqlite`.
4. After drain window: workspace controller pod-deletes any remaining sub-agent pods
   (topology `pod-per-subagent`) or they exit with the parent process (`single`).
5. Workspace controller deletes all OpenFGA tuples for terminated sub-agents in the
   post-drain cleanup phase.

SIGKILL path (no drain): parent pod enters `Failed`; workspace controller deletes all
pods labeled `keese.ai/parent-workspace: <name>` + `keese.ai/role: subagent`, then
calls `DeleteTuples` with type filter `subagent`. Sub-agent SQLite state is lost;
workspace-level PVC checkpoint is preserved (D24 identity is workspace-scoped).

## TokenBudget sharing

Default `budgetMode: shared`: all sub-agent usage attributed to the parent Workspace
budget; `TokenUsed` events carry `keese.workspace.uid = parent.uid`; NATS KV counters
key `workspace/<parent-uid>/<model>/...` (10b).

Opt-in `budgetMode: split`: each sub-agent gets a `TokenBudget` CR
`keese-subagent-<parent-uid>-<id>` with budget `parent_total / N` or per
`SubAgentSpec.budget`. Rare; for multi-tenant sub-agent workflows.

## Trade-offs

| Option | Decision | Rationale |
|---|---|---|
| Lease-based TOCTOU guard | Yes | OpenFGA lacks atomic conditional writes at v1alpha1; 5 s Lease is cheap |
| Sub-agent state on SIGKILL | Lost | Sub-agents are ephemeral workers; D24 identity is workspace-level |
| `shared` budget default | Yes | Billing is per-workspace; sub-agents are orchestrated under parent intent |
| Aggregate tenant cap | Optional | Per-workspace ceiling is primary; tenant aggregate is a safeguard |

## Failure modes

| Failure | Detection | Mitigation |
|---|---|---|
| Lease acquisition timeout at spawn | `LeaseAcquisitionTimeout` event | Retry after 1 s; cap 3 attempts; deny after cap |
| OpenFGA `ListObjects` error at spawn | `SubAgentListFailed` event | Deny spawn (fail-closed); log error |
| Sub-agent pod not deleted after SIGKILL | Workspace controller re-list on next tick | Re-delete; idempotent pod delete |
| Orphan OpenFGA tuples on controller restart | Workspace controller reconcile compares live pods to tuples | Deletes orphan tuples; ≤ 3 reconciles |
| Tenant aggregate cap race (two workspaces) | Tenant controller detects over-limit on next reconcile | Sets `SubAgentLimitBlocked` condition; new spawns denied until below cap |
| `budgetMode: split` sub-budget exhaustion | ext_authz 429 on sub-agent calls; `SubAgentBudgetExhausted` event | Sub-agent stops calling upstreams; parent receives `ErrBudget` from `InvokeSubAgent` |

## Upgrade / rollback

Raising `maxPerWorkspace`: immediate. Lowering: new spawns denied when count ≥ new
limit; in-flight sub-agents complete (no forced kill). OpenFGA model change (04a
iter-5) follows 04a drain-and-rollout. `budgetMode` field promotion: v1alpha1 →
v1beta1 with conversion webhook per rule 04.2.

## Observability

Events (in `internal/controller/workspace/events.go`): `SubAgentLimitExceeded`,
`SubAgentSpawned`, `SubAgentTerminated`, `SubAgentCleanupStarted`,
`SubAgentCleanupTimeout`, `SubAgentListFailed`, `LeaseAcquisitionTimeout`.

OTEL span: `workspace.subagent.spawn` (`workspace`, `tenant`, `count_before`,
`limit`, `result`). Metric: `keese_subagent_active{workspace,tenant}` gauge;
`keese_subagent_spawn_total{workspace,tenant,result}` counter;
`keese_subagent_limit_rejections_total{workspace,tenant}` counter.

`Workspace.status.activeSubAgents` int field updated by workspace controller on every
spawn/terminate event.

## Refs

- [02-workspace-model.md](02-workspace-model.md) — `spec.topology: pod-per-subagent`; `spec.subagentLimits`
- [04a-openfga-authz-model.md](04a-openfga-authz-model.md) — tuple shapes (iter-5 flag: `active_subagent` + `type subagent`)
- [07-agent-runtime-spi.md](07-agent-runtime-spi.md) — `InvokeSubAgent`; `CleanupSubAgents` iter-2 flag
- [08a-goose-headless-modes.md](08a-goose-headless-modes.md) — `pod-per-subagent` topology (parallel/stub)
- [10b-token-accounting.md](10b-token-accounting.md) — shared/split budget; NATS KV counters
- [18-process-lifecycle.md](18-process-lifecycle.md) — 90 s agent drain budget; sub-agent checkpoint path
- [23-agent-supervision.md](23-agent-supervision.md) — supervision applies to parent workspace
- [../plans/rubric.md](../plans/rubric.md)

## Iteration log

### Iteration 1 — 2026-04-21

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | All 5 open questions answered; bounded inputs (10-cap, tuple shape, error surface, quota, SIGTERM, budget). |
| 2 | Architecture fit | 10 | 1.0 | 10 | Lease aligns with SSA; OpenFGA tuple counting; D24 identity boundary honored; VAP quota bounds. |
| 3 | Security posture | 15 | 1.0 | 15 | Fail-closed spawn (ListObjects error → deny); SIGKILL cleanup deletes tuples; sub-agent pods labeled for batch delete; no orphan tuples. |
| 4 | Automatability | 10 | 0.5 | 5 | `scripts/cleanup-subagent-tuples.sh` named in rollback but not yet authored; ConfigMap named; Lease TTL declared. Pre-gate. |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Failure modes mapped; test names not authored (pre-gate P8): concurrent-spawn-at-9 race, SIGKILL orphan cleanup, budget-split exhaustion, Lease timeout path, tenant aggregate cap. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Six failure modes; Lease race; orphan tuple reconcile; tenant cap race; budget exhaustion covered. |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | ≤ 200 lines; single responsibility; cross-refs only; no inline code. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; frontmatter complete; all 7 `depends` listed; rollback concrete. |
| 9 | Observability | 5 | 1.0 | 5 | Events, OTEL span, 3 metrics, `status.activeSubAgents` field declared. |
| 10 | Operational readiness | 10 | 1.0 | 10 | Raise/lower cap semantics; SIGKILL + SIGTERM paths; v1beta1 gate; OpenFGA drain-and-rollout for model change. |
| | **Total** | 100 | | **92.5** | |

Verdict: **SHIP** (92.5 ≥ 90). Status: `current`.

Gaps: Cat 4 — `scripts/cleanup-subagent-tuples.sh` not yet authored (pre-gate P8).
Cat 5 — 5 test cases unimplemented (pre-gate): concurrent-spawn-at-9 race, SIGKILL orphan
cleanup, budget-split exhaustion, Lease timeout, tenant aggregate cap.

Cross-deps: **04a iter-5** — add `workspace.active_subagent: [subagent]` + `type subagent`.
**07 iter-2** — add `CleanupSubAgents(ctx, Session) error` + `CapabilitySupportsSubAgentCleanup`.
**02** — add `spec.subagentLimits.{max,budgetMode}` + `status.{activeSubAgents,effectiveSubAgentLimit}`.
**08a** — coordinate `keese.ai/role: subagent` pod label for batch delete.
