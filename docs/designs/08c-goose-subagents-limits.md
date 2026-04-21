<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: runtime
depends:
  - 02-workspace-model.md
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
  sub-agents complete naturally. For concurrent-cap reduction mid-flight: informer count
  drains as sub-agents terminate (no forced kill on value decrease). No OpenFGA tuple
  cleanup needed — no sub-agent types exist in the authz model.
---

# 08c — Goose Sub-Agents and Limits

## Context

Goose supports spawning sub-agents within a running session. keese caps this at 10
concurrent sub-agents per Workspace, enforced at spawn time via a K8s informer count
(`pod-per-subagent` topology) or runtime-internal counter (`single` topology). Budget is
shared by default via Prometheus labels. Orphan sub-agents on parent SIGTERM are drained
via `CleanupSubAgents` (07 iter-2) then killed by batch pod delete; on SIGKILL the
workspace controller batch-deletes by label.

## Why not OpenFGA for sub-agent counting

The sub-agent → parent-workspace relationship is a **construction invariant**, not an
authz question. The parent controller spawns the child and bakes the relationship into K8s
owner references and pod labels (`keese.ai/parent-workspace: <uid>`). No authz decision
depends on "is this sub-agent allowed to belong to this workspace."

OpenFGA `ListObjects` is O(N) and designed for reachability, not cardinality. There is no
atomic INCR/DECR; iter-1's Kubernetes Lease workaround added complexity with no benefit.
Correct stores: K8s informer (counts) and Prometheus (token metrics) — both already in
the stack. Removed: `subagent` OpenFGA type; `active_subagent`, `parent_workspace`,
`budget_shared_with` relations; Kubernetes Lease spawn serialization; `ListObjects` flow;
04a iter-5 cross-dep flag.

## Spawn-count mechanism

**`pod-per-subagent` topology (08a).** Each sub-agent pod carries labels
`keese.ai/parent-workspace: <uid>` and `keese.ai/role: subagent`. Workspace controller
holds an informer indexed by parent UID. Pre-spawn check:

1. Read count from informer cache (in-process, microsecond).
2. Compare to `Workspace.status.effectiveSubAgentLimit`.
3. Count ≥ limit: return `ErrSubAgentLimitExceeded` (fail-closed).
4. Count < limit: create pod + write tool-authz tuple
   `workspace:W#can_run@service_account:ksa-subagent-<id>` (04a model) → `InvokeSubAgent`.

No Kubernetes Lease needed — workspace controller reconcile is single-writer per Workspace.

**`single` topology (08a).** Sub-agents are goroutines inside goose. Count from
`AgentRuntime.Health(ctx, session)` → `HealthReport.ActiveSubAgentCount int` or ACP
event stream (`SubAgentSpawned` / `SubAgentCompleted`). Pre-spawn check is
runtime-internal; goose refuses to spawn at cap. Cap injected at pod startup via env var
`KEESE_SUBAGENT_LIMIT` from `Workspace.status.effectiveSubAgentLimit`.

## Ceiling-hit error surface

- SPI `InvokeSubAgent` returns `ErrSubAgentLimitExceeded`.
- HTTP 429: body `{"code":"SubAgentLimitExceeded","current":N,"limit":L}`.
- K8s event `SubAgentLimitExceeded` on parent Workspace; deduped per 5 min.
- Metric: `keese_subagent_limit_rejections_total{workspace,tenant}`.

## Hierarchical quota

Effective limit = `min(workspace.max, tenant.max, cluster.max)`.

| Layer | Setting | Default | Bounded by |
|---|---|---|---|
| Cluster | `ConfigMap keese-subagent-defaults`.`maxPerWorkspace` | `10` | Operator-managed |
| Tenant | `Tenant.spec.subagentLimits.maxPerWorkspace` | inherits cluster | VAP: ≤ cluster ceiling |
| Workspace | `Workspace.spec.subagentLimits.max` | inherits tenant | VAP: ≤ effective tenant limit |

`Workspace.status.effectiveSubAgentLimit` surfaces the resolved value. Optional
`Tenant.spec.subagentLimits.maxPerTenant` (default unbounded): tenant controller sums
`status.activeSubAgents` across all Workspaces; sets `conditions[SubAgentLimitBlocked]`
on over-limit Workspaces.

## TokenBudget sharing via Prometheus (10b iter-2)

`Workspace.spec.subagentLimits.budgetMode` controls attribution.

**`shared` (default):** all sub-agents emit `keese_token_budget_consumed_total` with
parent Workspace labels; parent `TokenBudget` aggregates via Prometheus sum.

**`split` (opt-in):** each sub-agent emits with its own `subagent_id` label. Controller
auto-creates per-sub-agent `TokenBudget` CR (`keese-subagent-<parent-uid>-<id>`) with
owner-ref to parent Workspace. Per-sub-agent exhaustion → 429 on that sub-agent only.

## Orphan cleanup on parent SIGTERM

```
Workspace controller detects parent drain
  ├─ CapabilitySupportsSubAgentCleanup = true
  │     └─ CleanupSubAgents(ctx, session) → sub-agents drain to PVC checkpoint
  │           └─ ErrTransient → batch delete pods -l parent=<uid>,role=subagent
  └─ Post-drain: delete workspace:W#can_run@service_account:ksa-subagent-<id> tuples (SSA)
```

SIGKILL: workspace controller batch-deletes pods labeled `keese.ai/parent-workspace: <uid>`
+ `keese.ai/role: subagent`. Sub-agent SQLite state is lost; workspace-level PVC
checkpoint preserved (D24 identity is workspace-scoped).

## Trade-offs

| Option | Decision | Rationale |
|---|---|---|
| K8s informer count (not OpenFGA) | Yes | Construction invariant, not authz; informer is in-process and microsecond |
| No Kubernetes Lease | Yes | SSA + single-writer reconcile is inherently serialized |
| Sub-agent state on SIGKILL | Lost | Sub-agents are ephemeral workers; D24 identity is workspace-level |
| `shared` budget default | Yes | Billing is per-workspace; sub-agents are orchestrated under parent intent |

## Failure modes

| Failure | Detection | Mitigation |
|---|---|---|
| Informer cache stale at spawn | Over-limit pod created transiently | Next reconcile deletes excess pod; `SubAgentOverLimit` event |
| `CleanupSubAgents` ErrTransient | Controller catches error | Fall back to batch pod delete by label |
| Sub-agent pod not deleted after SIGKILL | Workspace controller re-list on next tick | Re-delete; idempotent |
| Tenant aggregate cap race | Tenant controller detects over-limit on next reconcile | `SubAgentLimitBlocked` condition; new spawns denied |
| `split` sub-budget exhaustion | ext_authz 429 on sub-agent | Sub-agent stops; parent receives `ErrBudget` |
| `KEESE_SUBAGENT_LIMIT` env stale (`single`) | New limit not injected until pod restart | `SubAgentLimitStale` event; pods rotate on next drain+resume |

## Upgrade / rollback

Raising `maxPerWorkspace`: immediate. Lowering: new spawns denied at ≥ new limit;
in-flight complete (no forced kill). `budgetMode` promotion: v1alpha1 → v1beta1 with
conversion webhook per rule 04.2.

## Observability

Events (`internal/controller/workspace/events.go`): `SubAgentLimitExceeded`,
`SubAgentSpawned`, `SubAgentTerminated`, `SubAgentCleanupStarted`,
`SubAgentCleanupTimeout`, `SubAgentOverLimit`, `SubAgentLimitStale`.

OTEL span `workspace.subagent.spawn` (`workspace`, `tenant`, `count_before`, `limit`,
`result`). Metrics: `keese_subagent_active{workspace,tenant}` gauge;
`keese_subagent_spawn_total{workspace,tenant,result}` counter;
`keese_subagent_limit_rejections_total{workspace,tenant}` counter.
`Workspace.status.activeSubAgents` int updated on every spawn/terminate event.

## Refs

- [02-workspace-model.md](02-workspace-model.md) — `spec.subagentLimits.{max,budgetMode}`; `status.{activeSubAgents,effectiveSubAgentLimit}`
- [07-agent-runtime-spi.md](07-agent-runtime-spi.md) — `InvokeSubAgent`; `CleanupSubAgents` iter-2; `CapabilitySupportsSubAgentCleanup`
- [08a-goose-headless-modes.md](08a-goose-headless-modes.md) — topology; pod labels
- [10b-token-accounting.md](10b-token-accounting.md) — Prometheus counter; `shared`/`split` budget
- [18-process-lifecycle.md](18-process-lifecycle.md) — 90 s drain budget; checkpoint path
- [23-agent-supervision.md](23-agent-supervision.md) — supervision applies to parent workspace
- [../plans/rubric.md](../plans/rubric.md)

## Iteration log

Iter-1 2026-04-21 — 92.5 SHIP. OpenFGA tuple counting + Kubernetes Lease mechanism.
Gaps: Cat 4 cleanup script unimplemented; Cat 5 five test cases pre-gate.
Cross-deps: 04a iter-5 (active_subagent type), 07 iter-2 (CleanupSubAgents), 02, 08a.

### Iteration 2 — 2026-04-21

Changes: (1) dropped OpenFGA `subagent` type, `active_subagent`/`parent_workspace`/
`budget_shared_with` relations, Kubernetes Lease, `ListObjects` flow — construction
invariant, not authz; (2) K8s informer count for `pod-per-subagent`, runtime-internal
counter for `single`; (3) TokenBudget sharing via Prometheus label attribution (10b
iter-2); (4) `CleanupSubAgents` fallback sequence documented (07 iter-2); (5) 04a iter-5
cross-dep dropped — no new OpenFGA types needed; (6) informer-stale + env-stale failure
modes added.

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | "Why not OpenFGA" anchors scope decision; mechanism corrected; inputs bounded. |
| 2 | Architecture fit | 10 | 1.0 | 10 | K8s informer aligns with controller-runtime; single-writer reconcile replaces Lease; D24 honored. |
| 3 | Security posture | 15 | 1.0 | 15 | Fail-closed (informer error → deny); batch pod delete by label; tool-authz tuples deleted via SSA; SA token TTL ≤ 10m (rule 05.3). |
| 4 | Automatability | 10 | 0.5 | 5 | ConfigMap + label selectors declared; no manual steps; scripts pre-gate P8. |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Failure modes named; 4 test cases pre-gate: concurrent-spawn-at-9, SIGKILL orphan, budget-split exhaustion, tenant aggregate cap. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Six modes: informer staleness, ErrTransient fallback, idempotent delete, tenant cap race, split exhaustion, stale env. |
| 7 | Context efficiency | 10 | 1.0 | 10 | ≤ 200 lines; ASCII sequence diagram compact; no inline code. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; frontmatter; `depends` corrected (04a removed, 08a added); rollback updated. |
| 9 | Observability | 5 | 1.0 | 5 | Events, OTEL span, 3 metrics, `status.activeSubAgents`; `SubAgentOverLimit` + `SubAgentLimitStale` added. |
| 10 | Operational readiness | 10 | 1.0 | 10 | Raise/lower cap semantics; informer drains naturally; SIGKILL + SIGTERM paths; env-stale rotation; v1beta1 gate. |
| | **Total** | 100 | | **95** | |

Verdict: **SHIP** (95 ≥ 90). Status: `current`. Gaps (pre-gate): Cat 4 — scripts; Cat 5 — 4 test cases.

Cross-deps settled: **07 iter-2** `CleanupSubAgents`; **10b iter-2** Prometheus labels;
**08a** topology labels. Flagged: **02 iter-2** (parallel) — confirm `spec.subagentLimits`
+ `status.{activeSubAgents,effectiveSubAgentLimit}` shapes when 02 lands.
04a: no pending cross-dep — `subagent` type was never merged into 04a.
