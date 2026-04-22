<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: spec
category: contract
depends:
  - ../designs/01-tenancy-capsule.md
  - ../designs/02-workspace-model.md
  - ../designs/02-ii-iter-log.md
  - ../designs/04b-projected-sa-identity.md
  - ../designs/08b-goose-acp-stdio-k8s.md
  - ../designs/08b-ii-session-crd-spec.md
  - ../designs/12-network-isolation.md
  - ../designs/24-tenant-crd.md
related_skills: [crd-authoring, controller-authoring]
status: current
last_verified: 2026-04-21
regression_lock: false
tests:
  unit: []
  envtest:
    - TestWorkspaceIdempotency3Passes
    - TestWorkspaceFSMPendingToRunning
    - TestWorkspaceAdmissionRejectsInteractivityChange
    - TestWorkspaceNPApplied
    - TestWorkspaceShareAdmission
    - TestWorkspaceShareReferenceGrant
    - TestWorkspaceShareTupleCleaned
    - TestWorkspaceSessionIdempotency3Passes
    - TestWorkspaceSessionFSMStartingToRunning
    - TestWorkspaceSessionAdmissionRejectsImmutableFields
    - TestWorkspaceSessionPreserveOnPodFailure
    - TestWorkspaceSessionConcurrencyCapEnforced
  kuttl:
    - TestNegativeEgressInternet
    - TestPositiveEgressGateway
    - TestWorkspaceSessionDrainOnDelete
metrics:
  - keese_workspace_phase_total{phase,tenant}
  - keese_workspace_reconcile_duration_seconds{phase}
  - keese_workspace_idle_duration_seconds{tenant}
  - keese_workspace_eviction_total{tenant}
  - keese_workspace_attach_total{tenant,result}
  - keese_workspace_session_active{tenant,mode}
  - keese_workspace_np_apply_total{result}
  - keese_workspace_np_conflict_total{namespace}
events:
  # Workspace FSM
  - WorkspaceReady; WorkspaceRunning; WorkspaceIdle; WorkspaceEvicted; WorkspaceResumed
  - WorkspaceSuspended; WorkspaceDegraded; WorkspaceTerminating; WorkspaceScaledToZero
  - WorkspaceAutoTerminated; ForceRevokeApplied; PVCProvisionFailed; AgentUnresponsive; SchedulingCollision
  # Attach / session
  - AttachRejected; WorkspaceSessionFailed; SessionsPerUserLimitExceeded
  - ConcurrentAttachLimitExceeded; AttachSessionNameForbidden; ConcurrentRunForbidden; ConcurrentRunForced
  # ACP bridge
  - ACPAttached; ACPDetached; ACPSessionLost; ACPGraceExpired; SessionStuckAfterPodFailure
  # Network
  - NetworkPolicyApplied; NetworkPolicyConflict
---

# workspace.operator.keese.ai v1alpha1 — spec

API group `workspace.operator.keese.ai/v1alpha1`. Three kinds in this group:
**Workspace** · **WorkspaceShare** · **WorkspaceSession**.

## Companion files (split per 200-line rule)

| File | Owns |
|---|---|
| [v1alpha1-ii-workspace.md](workspace.operator.keese.ai-v1alpha1-ii-workspace.md) | Workspace CRD YAML, VAP CEL, finalizer chain, bifurcated FSM, NP application |
| [v1alpha1-ii-share.md](workspace.operator.keese.ai-v1alpha1-ii-share.md) | WorkspaceShare CRD YAML, VAP, finalizer chain, failure modes |
| [v1alpha1-ii-session.md](workspace.operator.keese.ai-v1alpha1-ii-session.md) | WorkspaceSession CRD detail, RBAC, event reasons, acceptance tests |
| [v1alpha1-ii-iter-log.md](workspace.operator.keese.ai-v1alpha1-ii-iter-log.md) | Rubric iteration log (3 passes) |

## Workspace — summary

### Spec fields

| Field | Default | VAP constraint |
|---|---|---|
| `spec.tenantRef.name` | — | Immutable (`TenantRefImmutable`) |
| `spec.runtimeRef.name` | — | Must resolve to AgentRuntime |
| `spec.topology` | `single` | Immutable; `single\|pod-per-subagent` |
| `spec.interactive` | `false` | **Immutable** (`InteractivityImmutable`) |
| `spec.concurrencyPolicy` | `Allow` | `Allow\|Forbid\|Replace`; ignored if interactive |
| `spec.sessionMode` | `shared` | `shared\|per-user\|per-attach`; interactive only |
| `spec.attachPolicy.*` | see companion | allowedSubjects, requiredClaims, caps |
| `spec.attachGrace` | `5m` | [0s, 24h]; interactive only |
| `spec.idleTimeout` | `15m` | [1m, tenant ceiling] |
| `spec.evictionTimeout` | `2h` | [15m, tenant ceiling] |
| `spec.storage.*` | `10Gi/Retain` | [tenant floor, ceiling]; allowedClasses |
| `spec.resources.*` | `1cpu/2Gi` | [tenant floor, ceiling] |
| `spec.forceRevoke.epoch` | `0` | Monotonic ms > lastEpoch (04c) |
| `spec.subagentLimits.*` | `10/shared` | ≤ tenant limit (08c) |
| `spec.supervision.*` | see 02-ii | Duration bounds per 23 |
| `spec.maintenance.*` | _unset_ | quietHours + maxDisruptionsPerHour |

ReBAC markers on authz-affecting fields:
```go
// +keese:rebac-tuple=workspace#member    // spec.tenantRef.name
// +keese:rebac-tuple=workspace#runtime   // spec.runtimeRef.name
// +keese:rebac-tuple=workspace#guardrail // spec.guardrailBindingRefs[]
// +keese:rebac-tuple=workspace#memory    // spec.memoryRefs[]
// +keese:rebac-tuple=workspace#transport // spec.transportRefs[]
```

### Controller identifiers

- Finalizer: `finalizers.workspace.operator.keese.ai/cleanup`
- SSA fieldOwner: `keese-workspace-controller`
- Status: `observedGeneration` + conditions `Ready`, `Running`, `Revoked`, `Degraded`
- FSM phases: `Pending|Provisioning|Ready|Starting|Running|Idle|Evicted|Suspended|Revoked|Degraded|Terminating`
- Printer columns: `Age`, `Ready`, `Phase`, `Topology`, `Runtime`, `Interactive`

---

## WorkspaceShare — summary

Opt-in cross-namespace sharing. Controller creates a `ReferenceGrant` (Gateway API) in
`spec.targetNamespace` and writes OpenFGA tuples `workspace:<name>#<role>@<subject>`.

| Field | Required | VAP |
|---|---|---|
| `spec.workspaceRef.name` | Yes | Immutable |
| `spec.targetNamespace` | Yes | Immutable; != Workspace namespace |
| `spec.subjects[]` | Yes | Non-empty; `kind ∈ {User,Group,ServiceAccount}` |
| `spec.role` | Yes | `viewer\|editor`; immutable |

```go
// +keese:rebac-tuple=workspace#viewer  // spec.role == viewer
// +keese:rebac-tuple=workspace#editor  // spec.role == editor
```

- Finalizer: `finalizers.workspaceshare.operator.keese.ai/cleanup`
- SSA fieldOwner: `keese-workspaceshare-controller`
- Printer columns: `Age`, `Ready`, `Phase`, `Workspace`, `TargetNS`, `Role`

---

## WorkspaceSession (D27) — summary

Per-attach interactive session. Full detail in companion [v1alpha1-ii-session.md](workspace.operator.keese.ai-v1alpha1-ii-session.md) and design [08b-ii](../designs/08b-ii-session-crd-spec.md).

| Field | Mutable | VAP |
|---|---|---|
| `spec.workspaceRef` | No | `WorkspaceRefImmutable` |
| `spec.attachSubject` | No | `AttachSubjectImmutable`; OpenFGA subject (04b iter-2) |
| `spec.sessionName` | No | `SessionNameImmutable` |
| `spec.mode` | No | `SessionModeImmutable` |
| `spec.attachGraceSeconds` | Yes | [0, 86400] |
| `spec.preserveOnPodFailure` | Yes | Boolean |

```go
// +keese:rebac-tuple=workspace#editor  // spec.attachSubject
```

- Finalizer: `finalizers.workspacesession.operator.keese.ai/cleanup`
- SSA fieldOwner: `keese-workspacesession-controller`
- Phases: `Starting|Running|Idle|Draining|Terminating`
- Printer columns: `Age`, `Ready`, `Phase`, `Subject`, `Session`

---

## Cross-kind failure modes

Full table in [02-ii-iter-log.md](../designs/02-ii-iter-log.md). Key items:

| Failure | Detection | Mitigation |
|---|---|---|
| PVC provision fails | `Provisioning` stuck; `PVCProvisionFailed` | StorageClass validated at admission |
| SIGKILL during drain | `status.lastCheckpoint` stale | Resume from checkpoint; ≤ 1 step lost (18) |
| Attach rejected | Webhook + VAP; 403 | `AttachRejected` with specific reason code |
| NP drift | SSA conflict counter; `NetworkPolicyConflict` | Re-asserted on next reconcile |
| WorkspaceSession drain timeout | 90s elapsed; `DrainTimeout` | Pod deleted; PVC retained |

## Iteration log summary

See [v1alpha1-ii-iter-log.md](workspace.operator.keese.ai-v1alpha1-ii-iter-log.md).

| Iteration | Emphasis | Score | Verdict |
|---|---|---|---|
| 1 | Correctness + security | 92.5 | SHIP |
| 2 | Performance + quality | 95 | SHIP |
| 3 | Operational readiness | 95 | SHIP |

Final status: `current`. Residual Cat 4 (−5) is pre-gate structural.
