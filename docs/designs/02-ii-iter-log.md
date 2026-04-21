<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: workspace
depends: [02-workspace-model.md]
related_skills: []
status: current
last_verified: 2026-04-21
---

# 02-ii — Workspace Model: Iteration Log + Background

Companion to [02-workspace-model.md](02-workspace-model.md). Contains iteration scoring,
background sections carried from iter-1, and the full failure-mode + observability inventory.

## Background (from iter-1, unchanged)

### Idle + eviction

Two independent timers; configurable via `ConfigMap keese-workspace-defaults` in `keese-system`.

| Timer | Default | Tenant override | Floor/ceiling | Transition |
|---|---|---|---|---|
| `spec.idleTimeout` | 15 min | `Tenant.spec.defaultIdleTimeout` | VAP: [1m, 4h] | Running → Idle |
| `spec.evictionTimeout` | 2 hr (from Idle entry) | `Tenant.spec.defaultEvictionTimeout` | VAP: [15m, 7d] | Idle → Evicted |

Evicted: PVC + OpenFGA tuples + SA retained. `spec.evictionPolicy.deleteAfter` (default `168h`)
auto-terminates and deletes PVC after that duration in Evicted phase.

### PVC sizing

| Env | Default StorageClass | RWX StorageClass |
|---|---|---|
| dev (kind) | `standard` | none — RWX rejected at admission |
| EKS | `gp3-csi` | `efs-sc` |
| GKE | `pd-ssd` | `filestore-rwx` |
| AKS | `managed-premium` | `azurefile-rwx` |

`spec.storage.size` defaults to 10Gi; VAP enforces `[Tenant.spec.storage.min, Tenant.spec.storage.max]`.
Daily VolumeSnapshot; retention per `Tenant.spec.storage.snapshotRetention` (default 7 days).
Checkpoint path: `/var/run/keese/sessions/<workspace-uid>/session.sqlite` (18).

### Scheduling merge with Capsule

- Workspace `nodeSelector` must be a superset of `Tenant.spec.nodeSelector` (VAP). Conflicting
  key values → `SchedulingCollision` event + admission reject.
- Tolerations are additive.
- `affinity` merged by intersection; Workspace cannot weaken Capsule affinity groups.
- Mode A (no Capsule): `keese-workspace-defaults` ConfigMap provides defaults.

### `spec.supervision` bounds

Schema owned here; consumed by design 23. VAP enforces bounds per `Tenant.spec.supervision.bounds`.

| Field | Default | Floor | Ceiling |
|---|---|---|---|
| `overrides.zeroTokenUsage` | `10m` | `1m` | `60m` |
| `overrides.noPhaseTransition` | `15m` | `1m` | `120m` |
| `overrides.acpIdle` | `5m` | `1m` | `30m` |
| `overrides.noArtifactTouch` | `30m` | `5m` | `120m` |
| `overrides.expectsArtifacts` | `false` | — | — |
| `escalationLadder` | `[]` | — | max 7 steps |

CEL pattern: `duration(self.spec.supervision.overrides.zeroTokenUsage) >= duration("1m") && duration(...) <= duration("60m")`.

### Trade-offs (iter-1)

Single FSM / one controller: simpler causality; D25 GUPP requires tight loop.
Topology immutable (VAP): RWO vs. RWX Deployment graph differs materially; in-place change unsafe.
SA token + PVC retained on Evicted: D24 durable identity; revocation is an explicit 04c act.
Two timers (idle/evict): idle pod costs compute; evicted costs storage only — separate levers needed.
Supervision schema in 02: 23 mandates it; 02 is the canonical Workspace spec source.

### Upgrade / rollback

Topology change requires new Workspace + `spec.resumeFrom`. FSM changes non-destructive; controller
reconverges ≤ 3 reconciles (D24). v1alpha1 → v1beta1 requires conversion webhook +
`docs/plans/migration-workspace-v1beta1.md` ≥ 90.

## Full failure-mode table

| Failure | Detection | Mitigation |
|---|---|---|
| Pod crash before session reconnect | 30 s timeout; `WorkspaceSessionFailed` | Auto-delete WorkspaceSession CR; scale-to-zero |
| Attach rejected (policy violation) | Webhook + VAP; 403 | `AttachRejected` with reason code |
| `per-attach` caller provides session name | VAP reject | `AttachSessionNameForbidden` |
| Grace expires mid-reconnect | Timer race | Pod deleted; reconnect triggers cold boot |
| Projected resource reconcile failure | `Degraded`; `WorkspaceDegraded` | Exponential backoff; `WorkspaceStuck` alert after 5 m |
| SPI `Start`/`Resume` ErrTransient | 2 m without progress | D23 escalation; `AgentUnresponsive` |
| PVC provision failure | `Provisioning` stuck | `PVCProvisionFailed`; StorageClass validated at admission |
| RWX StorageClass unavailable in dev | Admission reject | `pod-per-subagent` blocked in kind |
| SIGKILL during drain | `status.lastCheckpoint` | Resume from checkpoint; ≤ 1 step lost (18) |
| ForceRevoke `can_revoke` check fails | OpenFGA down | Fail-closed deny; `ForbiddenToRevoke` |
| Eviction deleteAfter elapsed with live data | Auto-terminate | VolumeSnapshot retained; `WorkspaceAutoTerminated` alert |
| SchedulingCollision (VAP) | Admission reject | `SchedulingCollision` event; correct nodeSelector |
| concurrencyPolicy=Replace drain timeout | `replaceDrainSeconds` elapsed | Force-terminate; `ConcurrentRunForced` (03) |

## Full observability inventory

**OTEL spans:** `workspace.reconcile`, `workspace.provision`, `workspace.attach`,
`workspace.spi.start`, `workspace.spi.resume`, `workspace.spi.drain`,
`workspace.session.create`, `workspace.scale_to_zero`.

**Events** (const table in `internal/controller/workspace/events.go`): `WorkspaceReady`,
`WorkspaceIdle`, `WorkspaceEvicted`, `WorkspaceResumed`, `WorkspaceSuspended`,
`WorkspaceDegraded`, `WorkspaceTerminating`, `WorkspaceRunning`, `WorkspaceScaledToZero`,
`ForceRevokeApplied`, `PVCProvisionFailed`, `AgentUnresponsive`, `SchedulingCollision`,
`WorkspaceAutoTerminated`, `AttachRejected`, `WorkspaceSessionFailed`,
`SessionsPerUserLimitExceeded`, `ConcurrentAttachLimitExceeded`, `AttachSessionNameForbidden`,
`ConcurrentRunForbidden`, `ConcurrentRunForced`.

**Metrics:** `keese_workspace_phase_total{phase,tenant}`,
`keese_workspace_reconcile_duration_seconds{phase}`,
`keese_workspace_idle_duration_seconds{tenant}`,
`keese_workspace_eviction_total{tenant}`,
`keese_workspace_attach_total{tenant,result}`,
`keese_workspace_session_active{tenant,mode}`.

**Printer columns** (rule 04.5): `Age`, `Ready`, `Phase`, `Topology`, `Runtime`, `Interactive`.

**Alerts:** `WorkspaceStuck` (Degraded > 5 m); `WorkspaceAutoTerminated` (informational page).

## Iteration log

### Iteration 1 — 2026-04-21

| # | Category | Wt | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | FSM, topology, idle/eviction, PVC, scheduling, supervision bounded. |
| 2 | Architecture fit | 10 | 1.0 | 10 | D24/D25 honored; SSA; VAP-first; compose-over-replicate. |
| 3 | Security posture | 15 | 1.0 | 15 | SA retained on eviction but not revoked; fail-closed can_revoke; RWX guard; finalizer drain order. |
| 4 | Automatability | 10 | 0.5 | 5 | VAP CEL stated; ConfigMap named; StorageClass annotation convention stated. Pre-gate. |
| 5 | Verifiability | 15 | 0.5 | 7.5 | FSM transitions have event reasons; envtest/kuttl test names not authored (pre-gate P8). |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | 8 failure modes; SIGKILL + scheduling collision covered. |
| 7 | Context efficiency | 10 | 1.0 | 10 | ≤ 200 lines; single responsibility; all deps linked. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; frontmatter; rollback concrete. |
| 9 | Observability | 5 | 1.0 | 5 | OTEL spans, events, metrics, printer columns, alerts. |
| 10 | Operational readiness | 10 | 1.0 | 10 | Topology migration; v1beta1 gated; eviction deleteAfter. |
| | **Total** | 100 | | **92.5** | |

Verdict: **SHIP**. Status: `current`.
Gaps: Cat 4 — VAP manifests pre-gate. Cat 5 — envtest names flag for spec phase.
Cross-deps settled: 23 supervision schema satisfied; 07 topology gates on CapabilitySupportsSubAgents; 24 association label-based.

### Iteration 2 — 2026-04-21

Scope additions: `spec.interactive` immutability + bifurcated FSM; `spec.concurrencyPolicy`
(field ownership; 03 owns semantics); `spec.sessionMode` + `spec.attachPolicy.*` + `spec.attachGrace`;
WorkspaceSession CRD integration; interactive ↔ WorkflowRun mutual exclusion; OpenFGA subject
updated to `user:ksa-<workspace-uid>` (04b iter-2). Doc split into `02` (175 lines) + `02-ii-iter-log.md`.

| # | Category | Wt | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Bifurcated FSM, 7 new spec fields, attach policy, WorkspaceSession integration — all bounded with explicit enforcement points. |
| 2 | Architecture fit | 10 | 1.0 | 10 | interactive immutability VAP correct; FSM bifurcation aligns with 07 sidecar conditional; 03 cross-ref for concurrencyPolicy semantics; 04b subject form updated. |
| 3 | Security posture | 15 | 1.0 | 15 | Attach admission chain ordered (OpenFGA first); `user:ksa-<uid>` no tenant suffix (04b iter-2); mutual exclusion VAP; WorkspaceSession finalizer drain; fail-closed on grace race. |
| 4 | Automatability | 10 | 0.5 | 5 | VAP CEL named; admission chain ordered; enforcement points declared. Make targets + envtest pre-gate. |
| 5 | Verifiability | 15 | 0.5 | 7.5 | FSM transitions have event reasons + controller actions; 4 new failure modes (iter-2); envtest names not authored (pre-gate P8). |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | 13 failure modes total; grace race, per-attach name rejection, attach policy violations, concurrencyPolicy Replace timeout all covered. |
| 7 | Context efficiency | 10 | 1.0 | 10 | Main doc 175 lines; companion 02-ii carries iter-log + background. Single responsibility maintained. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; frontmatter updated; depends adds 03, 08b; rollback updated for interactive mode. |
| 9 | Observability | 5 | 1.0 | 5 | 8 OTEL spans (+ 3 new); 21 event reasons (+ 9 new); 6 metrics (+ 2 new); `Interactive` printer column. |
| 10 | Operational readiness | 10 | 1.0 | 10 | Rollback updated; mode-switch = recreate documented; cold boot latency stated; scale-to-zero lifecycle complete. |
| | **Total** | 100 | | **95** | |

Verdict: **SHIP** (95 ≥ 90). Status: `current`.

Top gaps:
1. Cat 4 (0.5): VAP manifests and make targets not authored — blocked on design gate opening (P8).
2. Cat 5 (0.5): envtest test names for bifurcated FSMs not authored — pre-gate by design.
3. Cat 5 flag: interactive FSM cold boot latency (~15–30 s) should have an SLO assertion in the spec phase.

Cross-deps settled: 03 iter-2 concurrencyPolicy + interactive mutual exclusion confirmed; 04b iter-2
`user:ksa-<uid>` subject form confirmed; 04c iter-2 forceRevoke/revocationMode absorbed; 07 iter-2
sidecar conditional confirmed; 08b (WorkspaceSession spec) referenced; 08c iter-1 subagentLimits absorbed;
23 iter-2 supervision schema retained.

Cross-deps flagged for next iterations: 08b iter-2 must publish full WorkspaceSession spec (02 references
field shapes only); 24 iter-3 should add `Tenant.spec.defaults.sessionMode` (flagged); spec phase must
author VAP CEL for all new fields before design gate opens.
