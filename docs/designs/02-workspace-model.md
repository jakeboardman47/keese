<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: workspace
depends:
  - 01-tenancy-capsule.md
  - 03-workflow-argo-delegation.md
  - 04a-openfga-authz-model.md
  - 04b-projected-sa-identity.md
  - 04c-token-revocation.md
  - 05a-envoy-ai-gateway-topology.md
  - 06-guardrailbinding.md
  - 07-agent-runtime-spi.md
  - 08b-goose-acp-stdio-k8s.md
  - 10b-token-accounting.md
  - 18-process-lifecycle.md
  - 20a-api-group-layout.md
  - 23-agent-supervision.md
  - 24-tenant-crd.md
related_skills: [crd-authoring, controller-authoring]
status: current
last_verified: 2026-04-21
rollback: |
  Interactive → non-interactive switch is forbidden by VAP (InteractivityImmutable); recreate
  Workspace with spec.interactive: false + spec.resumeFrom pointing to last checkpoint.
  For topology changes: spec.suspended=true + drain; create new Workspace with resumeFrom;
  terminate old. CR deletion triggers finalizer drain: OpenFGA tuples → NetworkPolicy →
  projected SA → PVC (per spec.storage.retentionPolicy). Revert operator via OLM `replaces`
  chain; no CRD migration until v1beta1 promotion.
---

# 02 — Workspace Model

## Context

A `Workspace` CR is the durable identity of one autonomous agent. `spec.interactive` is the
primary axis: `false` (default) = WorkflowRun-driven execution (Argo); `true` = attach-driven
interactive sessions via `WorkspaceSession` CRs (D27). The two modes have distinct FSMs, pod
topologies, and admission invariants. This design owns: spec schema, bifurcated FSM, pod topology,
idle/eviction, storage, scheduling merge, supervision schema, attach policy, and concurrency policy.

## Spec schema (iter-2 fields marked *)

Full schema in `workspace.operator.keese.ai-v1alpha1.md`.

| Field | Default | VAP constraint |
|---|---|---|
| `spec.tenantRef.name` | — | Immutable |
| `spec.runtimeRef.name` | — | Must resolve to AgentRuntime |
| `spec.topology` | `single` | Immutable; `single\|pod-per-subagent` |
| `spec.interactive` * | `false` | **Immutable** (`InteractivityImmutable`) |
| `spec.suspended` | `false` | Boolean |
| `spec.idleTimeout` | `15m` | [1m, tenant ceiling] |
| `spec.evictionTimeout` | `2h` | [15m, tenant ceiling] |
| `spec.resumeFrom` | `""` | Checkpoint path or prior Workspace name |
| `spec.storage.{size,className,retentionPolicy}` | `10Gi / cluster default / Retain` | [tenant floor,ceiling]; allowedClasses[] |
| `spec.forceRevoke.epoch` | `0` | Monotonic ms > lastEpoch (04c) |
| `spec.revocationMode` | `abort` | `abort\|finish` |
| `spec.evictionPolicy.deleteAfter` | `168h` | Post-Evicted auto-terminate + PVC delete |
| `spec.resources.{cpu,memory}` | `1 / 2Gi` | [tenant floor, ceiling] |
| `spec.supervision.*` | see §supervision | Duration per tenant bounds (23) |
| `spec.concurrencyPolicy` * | `Allow` | `Allow\|Forbid\|Replace`; ignored if interactive |
| `spec.sessionMode` * | `shared` | `shared\|per-user\|per-attach`; interactive only |
| `spec.attachPolicy.allowedSubjects[]` * | `[]` | Optional allowlist; interactive only |
| `spec.attachPolicy.requiredClaims` * | `{}` | OIDC claim key→values; interactive only |
| `spec.attachPolicy.maxConcurrentSessionsPerUser` * | `0` (unbounded) | VAP rejects over cap |
| `spec.attachPolicy.maxConcurrentAttaches` * | `0` (unbounded) | VAP rejects over cap |
| `spec.attachGrace` * | `5m` | VAP: [0s, 24h]; interactive only |
| `spec.subagentLimits.{max,budgetMode}` | `10 / shared` | VAP: ≤ tenant limit (08c) |
| `spec.maintenance.quietHours.{start,end,timezone}` * | _unset_ | Optional; HH:MM window + IANA tz; operator delays `Resume` during window |
| `spec.maintenance.maxDisruptionsPerHour` * | `0` (unbounded) | Rate-limits node-drain-driven pod restarts per operator hints (08a) |

`spec.interactive` VAP CEL: `oldSelf.spec.interactive == self.spec.interactive`.
Reason: distinct FSMs, pod topology (bridge sidecar conditional on 07), and lazy vs eager
pod lifecycle. Mode switch = recreate.

## Bifurcated FSM

### Non-interactive (`spec.interactive: false`)

No persistent workspace pod. Argo manages step pods in the Workspace namespace (03 iter-2).

| From | To | Condition | Controller action |
|---|---|---|---|
| _(new)_ | `Pending` | CR created | Validate tenantRef; queue provisioning |
| `Pending` | `Provisioning` | Tenant resolved | Apply SA, PVC, NetworkPolicy, OpenFGA tuples |
| `Provisioning` | `Ready` | 7 resources healthy; tuples written | `conditions[Ready=True]`; `WorkspaceReady` |
| `Ready` | `Running` | WorkflowRun accepted | `status.activeRunRef`; `conditions[Running=True]` |
| `Running` | `Idle` | No in-flight runs for `spec.idleTimeout` | `WorkspaceIdle` |
| `Idle` | `Running` | New WorkflowRun; D25 GUPP | SPI `Resume`; `WorkspaceResumed` |
| `Idle` | `Evicted` | Idle for `spec.evictionTimeout` | Write checkpoint; retain PVC + tuples + SA; `WorkspaceEvicted` |
| `Evicted` | `Provisioning` | New WorkflowRun | Recreate pod; re-enter Provisioning→Ready→Running |

`spec.concurrencyPolicy` (`Allow|Forbid|Replace`) enforced at WorkflowRun admission (03 iter-2).
02 owns the field; 03 owns semantics.

### Interactive (`spec.interactive: true`)

Lazy pod. Bridge sidecar always present (07 iter-2). No Deployment at `Ready`; pod created on first attach.

| From | To | Condition | Controller action |
|---|---|---|---|
| `Pending` | `Provisioning` | Tenant resolved | Apply SA, NetworkPolicy, OpenFGA tuples (no Deployment) |
| `Provisioning` | `Ready` | SA + tuples + NP healthy; no Pod | `WorkspaceReady`; await attach |
| `Ready` | `Starting` | First/subsequent attach passes admission | Attach webhook → controller creates WorkspaceSession CR + Pod |
| `Starting` | `Running` | Pod running; ACP bridge healthy | `conditions[Running=True]`; `WorkspaceRunning` |
| `Running` | `Idle` | All clients disconnect; `spec.attachGrace` starts | `WorkspaceIdle`; pod stays up |
| `Idle` | `Running` | Client reconnects within grace | Reuse pod; `WorkspaceResumed` |
| `Idle` | `Ready` | Grace expires; scale-to-zero | Delete pod; `WorkspaceScaledToZero`; next attach = cold boot (~15–30 s) |

`spec.concurrencyPolicy` is **ignored** for interactive Workspaces. `spec.sessionMode` governs multi-attach.

### Shared states (both FSMs)

`Suspended`, `Revoked`, `Degraded`, `Terminating` — semantics unchanged from iter-1.
Conditions: `Ready`, `Running`, `Revoked`, `Degraded`. `observedGeneration` on every status update (04.4).

## WorkspaceSession integration (D27)

**Attach flow:** `kubectl-keese attach` → attach webhook → admission chain → controller creates
`WorkspaceSession` CR + Pod. Session name defaults to `"default"`; caller may supply `--session=<name>`.

**Uniqueness by `spec.sessionMode`:**
- `shared`: one CR per Workspace; attachSubject ignored for uniqueness; all attaches join one pod.
- `per-user`: `(subject, sessionName)` unique; each user's `default` is their own CR + pod.
- `per-attach`: sessionName operator-generated; caller-provided name rejected (`AttachSessionNameForbidden`).

**Cleanup:** finalizer `finalizers.workspacesession.operator.keese.ai/cleanup` handles pod drain,
PVC release (if not shared), and OpenFGA tuple removal. Pod failure: 30 s reconnect window; then
auto-delete WorkspaceSession CR. Override: `WorkspaceSession.spec.preserveOnPodFailure: true`.

OpenFGA subject for the workspace agent SA: `user:ksa-<workspace-uid>` (04b iter-2, bare name).

## Attach policy (interactive only)

Admission check order — ALL must pass:
1. OpenFGA `Check(workspace:W#editor@user:ksa-<uid>)` — base ReBAC.
2. `allowedSubjects[]` — if non-empty, caller subject MUST be in list.
3. `requiredClaims` — JWT MUST carry all listed claim key/value pairs (e.g. `groups: ["eng"]`).
4. `maxConcurrentSessionsPerUser` — VAP counts active `WorkspaceSession` CRs for `(subject, Workspace)`.
5. `maxConcurrentAttaches` — VAP counts all active CRs across subjects.

## Interactive ↔ WorkflowRun mutual exclusion

`interactive: true` → attach only. VAP rejects `WorkflowRun` create: `WorkflowRunNotAllowedOnInteractiveWorkspace`.
`interactive: false` → WorkflowRun only. Attach webhook rejects: `AttachNotAllowedOnNonInteractiveWorkspace`.

## Pod topology, idle/eviction, PVC, scheduling, supervision

Pod topology immutability, RWO vs RWX StorageClass rules, idle/eviction timers (two-timer model),
PVC sizing table, scheduling merge with Capsule, and `spec.supervision.*` VAP bounds — all unchanged
from iter-1; see `02-ii-iter-log.md` §Background.

## Failure modes (iter-2 additions; full table in `02-ii-iter-log.md`)

| Failure | Detection | Mitigation |
|---|---|---|
| Pod crash before session reconnect | 30 s timeout; `WorkspaceSessionFailed` | Auto-delete WorkspaceSession CR; scale-to-zero |
| Attach rejected (policy violation) | Webhook + VAP; 403 | `AttachRejected` with specific reason code |
| `per-attach` caller provides session name | VAP reject | `AttachSessionNameForbidden` |
| Grace expires mid-reconnect | Timer race | Pod deleted; reconnect triggers cold boot |

## Observability (iter-2 additions)

New events: `WorkspaceRunning`, `WorkspaceScaledToZero`, `AttachRejected`, `WorkspaceSessionFailed`,
`SessionsPerUserLimitExceeded`, `ConcurrentAttachLimitExceeded`, `AttachSessionNameForbidden`,
`ConcurrentRunForbidden`, `ConcurrentRunForced`.
New spans: `workspace.attach`, `workspace.session.create`, `workspace.scale_to_zero`.
New metrics: `keese_workspace_attach_total{tenant,result}`, `keese_workspace_session_active{tenant,mode}`.
New printer column: `Interactive` (rule 04.5).
Full observability inventory in `02-ii-iter-log.md`.

## Refs

[01](01-tenancy-capsule.md) · [03](03-workflow-argo-delegation.md) · [04a](04a-openfga-authz-model.md) · [04b](04b-projected-sa-identity.md) · [04c](04c-token-revocation.md) · [05a](05a-envoy-ai-gateway-topology.md) · [06](06-guardrailbinding.md) · [07](07-agent-runtime-spi.md) · [08b](08b-goose-acp-stdio-k8s.md) · [08c](08c-goose-subagents-limits.md) · [10b](10b-token-accounting.md) · [18](18-process-lifecycle.md) · [20a](20a-api-group-layout.md) · [23](23-agent-supervision.md) · [24](24-tenant-crd.md) · [iter-log](02-ii-iter-log.md) · [spec](../specs/workspace.operator.keese.ai-v1alpha1.md) · [rubric](../plans/rubric.md)
