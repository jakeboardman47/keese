<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: spec
category: contract
depends:
  - keese.ai-v1alpha1-workspace.md
  - ../designs/02-workspace-model.md
  - ../designs/02-ii-iter-log.md
  - ../designs/12-network-isolation.md
  - ../designs/04b-projected-sa-identity.md
related_skills: [crd-authoring, controller-authoring]
status: current
last_verified: 2026-04-21
---

# keese.ai v1alpha1-ii — Workspace CRD detail

Companion to [keese.ai-v1alpha1-workspace.md](keese.ai-v1alpha1-workspace.md).
Owns: authoritative CRD YAML sketch, VAP CEL expressions, finalizer chain, FSM detail.

## CRD YAML sketch

```yaml
apiVersion: keese.ai/v1alpha1
kind: Workspace
metadata:
  name: ws-dev
  namespace: tenant-acme
  labels:
    keese.ai/tenant: acme
    keese.ai/workspace: ws-dev
  finalizers:
    - finalizers.workspace.keese.ai/cleanup
spec:
  tenantRef:
    name: acme                            # immutable
  runtimeRef:
    name: goose-v1                        # resolves to AgentRuntime
  topology: single                        # immutable; single|pod-per-subagent
  interactive: false                      # immutable; bifurcates FSM
  suspended: false
  concurrencyPolicy: Allow               # Allow|Forbid|Replace
  sessionMode: shared                    # interactive only
  attachGrace: 5m                        # interactive only; [0s,24h]
  attachPolicy:
    allowedSubjects: []
    requiredClaims: {}
    maxConcurrentSessionsPerUser: 0
    maxConcurrentAttaches: 0
  idleTimeout: 15m
  evictionTimeout: 2h
  resumeFrom: ""
  storage:
    size: 10Gi
    className: ""                         # defaults to cluster default
    retentionPolicy: Retain
  evictionPolicy:
    deleteAfter: 168h
  resources:
    cpu: "1"
    memory: 2Gi
  forceRevoke:
    epoch: 0
  revocationMode: abort
  subagentLimits:
    max: 10
    budgetMode: shared
  guardrailBindingRefs: []               # +keese:rebac-tuple=workspace#guardrail
  memoryRefs: []                         # +keese:rebac-tuple=workspace#memory
  transportRefs: []                      # +keese:rebac-tuple=workspace#transport
  supervision:
    overrides:
      zeroTokenUsage: 10m
      noPhaseTransition: 15m
      acpIdle: 5m
      noArtifactTouch: 30m
      expectsArtifacts: false
    escalationLadder: []
  maintenance:
    quietHours:
      start: ""
      end: ""
      timezone: ""
    maxDisruptionsPerHour: 0
status:
  phase: Pending
  observedGeneration: 0
  conditions: []
  activeRunRef: {}
  lastCheckpoint: ""
  idleAt: null
  evictedAt: null
```

## VAP CEL expressions (selected)

| Rule | CEL sketch | Reason |
|---|---|---|
| `spec.interactive` immutable | `oldSelf.spec.interactive == self.spec.interactive` | `InteractivityImmutable` |
| `spec.tenantRef` immutable | `oldSelf.spec.tenantRef.name == self.spec.tenantRef.name` | `TenantRefImmutable` |
| `spec.topology` immutable | `oldSelf.spec.topology == self.spec.topology` | `TopologyImmutable` |
| idleTimeout bounds | `duration(self.spec.idleTimeout) >= duration("1m")` | `IdleTimeoutTooShort` |
| evictionTimeout bounds | `duration(self.spec.evictionTimeout) >= duration("15m")` | `EvictionTimeoutTooShort` |
| WorkflowRun rejected if interactive | `!self.spec.interactive` on WorkflowRun create | `WorkflowRunNotAllowedOnInteractiveWorkspace` |
| Attach rejected if non-interactive | `self.spec.interactive` on WorkspaceSession create | `AttachNotAllowedOnNonInteractiveWorkspace` |
| Quota ≤ tenant ceiling | `quantity(self.spec.resources.cpu) <= quantity(tenant.spec.defaultWorkspaceQuota.cpu)` | `QuotaExceedsTenantCeiling` |

VAP manifest path: `config/overlays/base/vap/workspace-immutable-fields.yaml` (to author P8).

## Finalizer chain on DELETE

Phase → `Terminating` (SSA, `fieldOwner: keese-workspace-controller`).

1. Drain in-flight WorkflowRuns or WorkspaceSessions (send SIGTERM; wait `terminationGracePeriodSeconds`).
2. Delete Pod (if interactive and pod running).
3. Remove OpenFGA tuples: `workspace:<name>#member`, `#runtime`, `#guardrail`, `#memory`, `#transport`.
4. Delete NetworkPolicy `keese-workspace-default-deny` and `keese-workspace-egress-allow` (SSA delete).
5. Delete projected SA token volumes; revoke SA if `spec.storage.retentionPolicy: Delete`.
6. PVC: retain if `Retain`; delete if `Delete`.
7. Remove finalizer.

## FSM: non-interactive (`spec.interactive: false`)

| From | To | Condition | Controller action |
|---|---|---|---|
| _(new)_ | `Pending` | CR created | Validate tenantRef; queue |
| `Pending` | `Provisioning` | Tenant resolved | Apply SA, PVC, NP-1/2, OpenFGA tuples |
| `Provisioning` | `Ready` | 7 resources healthy | `WorkspaceReady` event; `conditions[Ready=True]` |
| `Ready` | `Running` | WorkflowRun accepted | `WorkspaceRunning`; `status.activeRunRef` |
| `Running` | `Idle` | No runs for `idleTimeout` | `WorkspaceIdle` |
| `Idle` | `Running` | New WorkflowRun | `WorkspaceResumed` |
| `Idle` | `Evicted` | Idle for `evictionTimeout` | Checkpoint write; `WorkspaceEvicted` |
| `Evicted` | `Provisioning` | New WorkflowRun | Recreate pod |

## FSM: interactive (`spec.interactive: true`)

| From | To | Condition | Controller action |
|---|---|---|---|
| `Pending` | `Provisioning` | Tenant resolved | Apply SA, NP (no pod) |
| `Provisioning` | `Ready` | SA + tuples + NP healthy | `WorkspaceReady`; await attach |
| `Ready` | `Starting` | First attach; WorkspaceSession created | Pod created |
| `Starting` | `Running` | Pod + ACP bridge healthy | `WorkspaceRunning` |
| `Running` | `Idle` | All clients disconnect; `attachGrace` starts | `WorkspaceIdle` |
| `Idle` | `Running` | Reconnect within grace | `WorkspaceResumed` |
| `Idle` | `Ready` | Grace expires | Pod deleted; `WorkspaceScaledToZero` |

## Projected SA token volumes

Per 04b iter-3, workspace controller applies two projections:

```
/var/run/keese/tokens/
  egress      # keese-egress-<tenant>    → Envoy AI Gateway
  supervisor  # keese-supervisor-<ws-uid> → ACP bridge (interactive only)
```

Workflow controller adds `workflowRun` projection when Argo Workflow is projected (03 iter-3).

## NetworkPolicy application

Controller applies NP-1 (default-deny) and NP-2 (egress-allow: gateway + NATS + kube-dns)
via SSA (`fieldOwner: keese-workspace-controller`) at `Provisioning` entry and re-asserts
on every reconcile. Full templates: design [12](../designs/12-network-isolation.md).

## Refs

[02](../designs/02-workspace-model.md) · [02-ii](../designs/02-ii-iter-log.md) · [04b](../designs/04b-projected-sa-identity.md) · [12](../designs/12-network-isolation.md) · [primary spec](keese.ai-v1alpha1-workspace.md)
