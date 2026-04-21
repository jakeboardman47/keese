<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: workspace
depends:
  - 01-tenancy-capsule.md
  - 04a-openfga-authz-model.md
  - 04b-projected-sa-identity.md
  - 04c-token-revocation.md
  - 05a-envoy-ai-gateway-topology.md
  - 06-guardrailbinding.md
  - 07-agent-runtime-spi.md
  - 10b-token-accounting.md
  - 18-process-lifecycle.md
  - 20a-api-group-layout.md
  - 23-agent-supervision.md
  - 24-tenant-crd.md
related_skills: [crd-authoring, controller-authoring]
status: current
last_verified: 2026-04-21
rollback: |
  spec.suspended=true + wait for drain (operator emits WorkspaceSuspended with
  drain_duration_ms). Pod-topology change requires new Workspace + spec.resumeFrom
  pointing to the evicted Workspace's last checkpoint; suspend then terminate the
  original. CR deletion triggers finalizer drain: OpenFGA tuples → NetworkPolicy →
  projected SA → PVC (retained per spec.storage.retentionPolicy). Revert operator
  image via OLM `replaces` chain; no CRD migration needed until v1beta1 promotion.
---

# 02 — Workspace Model

## Context

A `Workspace` CR is the durable identity of one autonomous agent. It projects
~7 resources (Deployment + PVC + ServiceAccount + NetworkPolicy + HTTPRoute +
OpenFGA tuples + Capsule labels) and governs the full agent lifecycle from first
`WorkflowRun` through eviction and resume. D24 establishes that Workspace identity
survives pod churn; D25 mandates the controller call `Resume` whenever work is
pending with no active session. This design owns the lifecycle FSM, pod topology,
idle/eviction policy, storage model, scheduling merge, and the `spec.supervision`
schema that design 23 depends on.

## Spec schema sketch

Top-level spec fields (full schema in `workspace.operator.keese.ai-v1alpha1.md` spec):

| Field | Required | Default | Constraint |
|---|---|---|---|
| `spec.tenantRef.name` | Mode A | — | Immutable (VAP) |
| `spec.runtimeRef.name` | Yes | — | Must resolve to AgentRuntime CR |
| `spec.topology` | No | `single` | `single\|pod-per-subagent`; immutable (VAP) |
| `spec.suspended` | No | `false` | Boolean |
| `spec.idleTimeout` | No | `15m` | VAP: [1m, tenant ceiling] |
| `spec.evictionTimeout` | No | `2h` | VAP: [15m, tenant ceiling]; from Idle entry |
| `spec.resumeFrom` | No | `""` | Checkpoint path or prior Workspace name |
| `spec.guardrails.inherit[]` | Injected | `[keese.ai/default]` | Mutating webhook; VAP rejects removal |
| `spec.storage.size` | No | `10Gi` | VAP: [tenant floor, ceiling] |
| `spec.storage.className` | No | cluster default | VAP: allowedClasses[] |
| `spec.storage.retentionPolicy` | No | `Retain` | `Retain\|Delete` on termination |
| `spec.forceRevoke.epoch` | No | `0` | Monotonic ms; VAP epoch > lastEpoch |
| `spec.revocationMode` | No | `abort` | `abort\|finish` (04c) |
| `spec.supervision.overrides.*` | No | see §supervision | Duration; VAP bounds per tenant |
| `spec.evictionPolicy.deleteAfter` | No | `168h` | Post-Evicted auto-terminate + PVC delete |

## Lifecycle FSM

States and transitions (controller uses SSA; fieldOwner `keese-workspace-controller`):

| From | To | Event / Condition | Controller Action |
|---|---|---|---|
| _(new)_ | `Pending` | CR created | Validate tenant ref; queue provisioning |
| `Pending` | `Provisioning` | Tenant resolved; resources reconciling | Apply SA, PVC, NetworkPolicy, Deployment, OpenFGA tuples; write `workspace:W#owner@tenant:T` |
| `Provisioning` | `Ready` | All 7 projected resources healthy; SA token projected; tuples written | Set `conditions[Ready=True]`; emit `WorkspaceReady` |
| `Ready` | `Running` | First `WorkflowRun` accepted; SPI `Start` called | Write `status.activeSessionRef`; update `conditions[Running=True]` |
| `Running` | `Idle` | No active `WorkflowRun` for `spec.idleTimeout` OR step-boundary checkpoint with no pending work | Emit `WorkspaceIdle`; pod stays running |
| `Idle` | `Running` | New `WorkflowRun` pending; D25 GUPP | Call `AgentRuntime.Resume`; emit `WorkspaceResumed` |
| `Idle` | `Evicted` | Idle for `spec.evictionTimeout` | Scale Deployment to 0; write `status.lastCheckpoint`; retain PVC + tuples + SA; emit `WorkspaceEvicted` |
| `Evicted` | `Provisioning` | New `WorkflowRun`; controller must Resume per D25 | Create new pod; call `Start`/`Resume`; re-enter Provisioning→Ready→Running |
| `Running\|Idle\|Evicted` | `Suspended` | `spec.suspended: true` (tenant-admin) | Graceful SPI `Drain`; pods scaled to 0; tuples retained; emit `WorkspaceSuspended` |
| `Suspended` | `Running` | `spec.suspended: false` | SPI `Resume`; re-enter Running |
| `*` | `Revoked` | `spec.forceRevoke.epoch > 0`; 04c `revocationMode` | Abort or finish per mode; delete OpenFGA tuples; emit `ForceRevokeApplied`; terminal unless `spec.resumeFrom` set |
| `*` | `Degraded` | Any projected resource unhealthy | Retry with backoff; emit `WorkspaceDegraded`; does not evict |
| `*` | `Terminating` | CR deletion; finalizer drain order: OpenFGA tuples → NetworkPolicy → projected SA → PVC (per retentionPolicy) | Emit `WorkspaceTerminating`; release resources in order |

Conditions: `Ready`, `Running`, `Revoked`, `Degraded`. `observedGeneration` on every status update (rule 04.4).

## Pod topology

`spec.topology` is **immutable** after creation (VAP `oldSelf.spec.topology == self.spec.topology`).

- `single` (default): one Pod; goose runtime container + optional sidecar(s). `accessMode: ReadWriteOnce`. Works in all environments.
- `pod-per-subagent`: one Pod per sub-agent (max 10 per 08c). Requires `CapabilitySupportsSubAgents: true` on the registered `AgentRuntime`. `accessMode: ReadWriteMany`; requires an RWX-capable StorageClass (VAP rejects in kind/dev where only `standard` is available). Admission validates StorageClass RWX support via annotation `keese.ai/rwx-capable: "true"` on the StorageClass object.

Migration path: create a new Workspace with `spec.resumeFrom: <old-ws-name-or-checkpoint>` and `spec.topology: pod-per-subagent`; set old Workspace `spec.suspended: true`; after migration confirm, terminate old Workspace.

## Idle + eviction

Two independent timers; both configurable at cluster scope via `ConfigMap keese-workspace-defaults` in `keese-system`.

| Timer | Default | Tenant override | Cluster floor/ceiling | Phase transition |
|---|---|---|---|---|
| `spec.idleTimeout` | 15 min | `Tenant.spec.defaultIdleTimeout` | VAP: [1m, 4h] | `Running → Idle` |
| `spec.evictionTimeout` | 2 hr (from Idle entry) | `Tenant.spec.defaultEvictionTimeout` | VAP: [15m, 7d] | `Idle → Evicted` |

Evicted workspaces retain PVC + OpenFGA tuples + SA token (not revoked). `spec.evictionPolicy.deleteAfter` (default `168h`) auto-terminates + deletes PVC after that duration in `Evicted` phase. Pod is scaled to 0 replicas; Deployment manifest retained for fast re-provision.

## PVC sizing and access mode

| Env | Default StorageClass | RWX StorageClass |
|---|---|---|
| dev (kind) | `standard` | none — RWX rejected at admission |
| EKS | `gp3-csi` | `efs-sc` |
| GKE | `pd-ssd` | `filestore-rwx` |
| AKS | `managed-premium` | `azurefile-rwx` |

`spec.storage.size` defaults to 10Gi; VAP enforces `[Tenant.spec.storage.min, Tenant.spec.storage.max]`. `spec.storage.className` allows override; VAP validates against `Tenant.spec.storage.allowedClasses[]`. Daily VolumeSnapshot via VolumeSnapshot CR; retention per `Tenant.spec.storage.snapshotRetention` (default 7 days). Checkpoint path within PVC: `/var/run/keese/sessions/<workspace-uid>/session.sqlite` (18).

## Scheduling merge with Capsule

`Workspace.spec.scheduling` fields compose with Capsule (Mode B) or cluster defaults (Mode A):

- Workspace `nodeSelector` must be a **superset** of `Tenant.spec.nodeSelector` (VAP). Conflicting key values → `SchedulingCollision` event + admission reject.
- Tolerations are additive; Workspace may add tolerations the tenant requires.
- `affinity` merged by intersection (both must match); workspace cannot weaken Capsule affinity groups.
- Mode A (no Capsule): `keese-workspace-defaults` ConfigMap provides defaults; Workspace spec is authoritative.

## `spec.supervision` (bounded)

Schema defined here; consumed by design 23. VAP enforces bounds per `Tenant.spec.supervision.bounds` (cluster defaults in `keese-workspace-defaults`):

| Field | Default | Cluster floor | Cluster ceiling | Notes |
|---|---|---|---|---|
| `overrides.zeroTokenUsage` | `10m` | `1m` | `60m` | Duration string |
| `overrides.noPhaseTransition` | `15m` | `1m` | `120m` | |
| `overrides.acpIdle` | `5m` | `1m` | `30m` | D25 GUPP; workspace must be Running |
| `overrides.noArtifactTouch` | `30m` | `5m` | `120m` | Enabled only if `expectsArtifacts: true` |
| `overrides.expectsArtifacts` | `false` | — | — | Boolean gate for artifact signal |
| `escalationLadder` | `[]` (use cluster default) | — | max 7 steps | Step `action` values enumerated by 23 |

VAP rejects duration values outside `[clusterFloor, clusterCeiling]`. CEL pattern: `duration(self.spec.supervision.overrides.zeroTokenUsage) >= duration("1m") && duration(self.spec.supervision.overrides.zeroTokenUsage) <= duration("60m")`.

## Trade-offs

Single FSM / one controller: simpler causality; D25 GUPP requires tight loop.
Topology immutable (VAP): RWO vs. RWX Deployment graph differs materially; in-place change unsafe.
SA token + PVC retained on Evicted: D24 durable identity; revocation is an explicit 04c act.
Two timers (idle/evict): idle pod costs compute; evicted costs storage only — separate levers needed.
Supervision schema in 02: 23 mandates it; 02 is the canonical Workspace spec source.

## Failure modes

| Failure | Detection | Mitigation |
|---|---|---|
| Projected resource reconcile failure | `Degraded` phase; `WorkspaceDegraded` event | Exponential backoff (max 1000s per rule 04); alert `WorkspaceStuck` after 5m |
| SPI `Start`/`Resume` returns `ErrTransient` | Controller retries; `AgentUnresponsive` after 2m | D23 escalation ladder; supervisor assesses |
| PVC provision failure | `Provisioning` stuck; span `pvc.provision=false` | Event `PVCProvisionFailed`; webhook validates StorageClass at admission |
| RWX StorageClass unavailable in dev | Admission reject | `topology: pod-per-subagent` blocked in kind; create new Workspace with `single` |
| SIGKILL during Drain | Pod `Failed`; controller reads `status.lastCheckpoint` | `Resume` from checkpoint; ≤ 1 step of lost progress (18) |
| ForceRevoke `can_revoke` check fails | OpenFGA down; fail-closed deny | Admission returns `ForbiddenToRevoke`; supervisor falls back to pod restart (step 4 of 23) |
| SchedulingCollision (VAP) | Admission reject | Event `SchedulingCollision`; correct `nodeSelector` mismatch |
| Eviction deleteAfter elapsed with live data | Auto-terminate triggered | VolumeSnapshot exists (daily); operator emits `WorkspaceAutoTerminated` alert |

## Upgrade / rollback

Rollback in frontmatter. Topology change requires new Workspace + `spec.resumeFrom`. FSM changes are non-destructive; controller reconverges ≤ 3 reconciles (D24). v1alpha1 → v1beta1 requires conversion webhook + `docs/plans/migration-workspace-v1beta1.md` ≥ 90.

## Observability

- OTEL spans: `workspace.reconcile`, `workspace.provision`, `workspace.spi.start`, `workspace.spi.resume`, `workspace.spi.drain`.
- Event reasons (in `internal/controller/workspace/events.go`): `WorkspaceReady`, `WorkspaceIdle`, `WorkspaceEvicted`, `WorkspaceResumed`, `WorkspaceSuspended`, `WorkspaceDegraded`, `WorkspaceTerminating`, `ForceRevokeApplied`, `PVCProvisionFailed`, `AgentUnresponsive`, `SchedulingCollision`, `WorkspaceAutoTerminated`.
- Metrics: `keese_workspace_phase_total{phase,tenant}`, `keese_workspace_reconcile_duration_seconds{phase}`, `keese_workspace_idle_duration_seconds{tenant}`, `keese_workspace_eviction_total{tenant}`.
- Printer columns (rule 04.5): `Age`, `Ready`, `Phase`, `Topology`, `Runtime`.
- Alert: `WorkspaceStuck` (Degraded > 5m); `WorkspaceAutoTerminated` (informational page).

## Refs

[01](01-tenancy-capsule.md) · [04a](04a-openfga-authz-model.md) · [04b](04b-projected-sa-identity.md) · [04c](04c-token-revocation.md) · [05a](05a-envoy-ai-gateway-topology.md) · [06](06-guardrailbinding.md) · [07](07-agent-runtime-spi.md) · [10b](10b-token-accounting.md) · [18](18-process-lifecycle.md) · [20a](20a-api-group-layout.md) · [23](23-agent-supervision.md) · [24](24-tenant-crd.md) · [spec](../specs/workspace.operator.keese.ai-v1alpha1.md) · [rubric](../plans/rubric.md)

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
Verdict: **SHIP** (92.5 ≥ 90). Status: `current`.
Gaps: Cat 4 — VAP manifests pre-gate. Cat 5 — FSM envtest names (stuck-state, eviction timer, suspend/resume, 3-reconcile idempotency) flag for spec phase.
Cross-deps settled: 23 supervision schema satisfied; 07 topology gates on CapabilitySupportsSubAgents; 24 association is label-based (not OwnerRef).
