<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: runtime
depends:
  - 08b-goose-acp-stdio-k8s.md
  - 02-workspace-model.md
  - 04a-openfga-authz-model.md
  - 04b-projected-sa-identity.md
  - 18-process-lifecycle.md
related_skills: [crd-authoring, controller-authoring]
status: current
last_verified: 2026-04-21
rollback: |
  WorkspaceSession is a v1alpha1 CRD. No conversion webhook exists until v1beta1
  promotion. To roll back: delete all WorkspaceSession CRs (finalizer chain drains
  pods cleanly), then remove the CRD. Workspaces with spec.interactive: true become
  non-functional until a replacement CRD is deployed. No OpenFGA tuple migration
  needed (session-scoped tuples cleaned by finalizer before CRD removal).
---

# 08b-ii — WorkspaceSession CRD Spec + Attach Flow

Companion to [08b](08b-goose-acp-stdio-k8s.md). Owns: authoritative CRD YAML,
VAP rules on CREATE/UPDATE, finalizer chain, and the server-side attach sequence.

## WorkspaceSession CRD

```yaml
apiVersion: keese.ai/v1alpha1
kind: WorkspaceSession
metadata:
  name: ws-dev-alice-default          # <workspace>-<subject-hash-16>-<session-name>
  namespace: tenant-acme
  labels:
    keese.ai/workspace: ws-dev
    keese.ai/subject-hash: <sha256[:16]>
    keese.ai/session-name: default
  ownerReferences:
    - apiVersion: keese.ai/v1alpha1
      kind: Workspace
      name: ws-dev
      uid: <workspace-uid>
      blockOwnerDeletion: true
      controller: false              # workspace controller is NOT the managing controller
  finalizers:
    - finalizers.workspacesession.keese.ai/cleanup
spec:
  workspaceRef: ws-dev               # required; immutable
  attachSubject: "user:alice@example.com"  # required; immutable; OpenFGA subject (04b iter-2)
  sessionName: default               # required; immutable; user-visible name
  mode: per-user                     # shared|per-user|per-attach; inherited; immutable
  attachGraceSeconds: 300            # mutable; inherits Workspace.spec.attachGrace; [0,86400]
  preserveOnPodFailure: false        # mutable; if true: leave CR in Failed; require manual delete
status:
  phase: Running                     # Starting|Running|Idle|Draining|Terminating
  podRef:
    name: ws-dev-alice-default-pod
    uid: <pod-uid>
  attachedAt: 2026-04-21T10:00:00Z
  lastActivityAt: 2026-04-21T10:15:00Z
  attachedClientCount: 1             # >1 valid in shared mode; also in per-user (two terminals, same session)
  tokenBudgetRef:                    # when sessionMode: split (08c)
    name: ws-dev-alice-default-budget
  observedGeneration: 1
  conditions:
    - type: Ready
      status: "True"
      reason: SessionReady
      lastTransitionTime: 2026-04-21T10:00:00Z
```

**Name uniqueness** enforced by label selector, not name alone: controller queries
`keese.ai/workspace=<ws> AND keese.ai/subject-hash=<hash> AND keese.ai/session-name=<name>`
before CREATE. VAP CEL expression provides fast-path at admission.

## VAP on CREATE

All checks apply only when `Workspace.spec.interactive: true` (reject reason:
`AttachNotAllowedOnNonInteractiveWorkspace` if not interactive).

| Rule | CEL expression (sketch) | Reject reason |
|---|---|---|
| Workspace exists + interactive | `workspace.spec.interactive == true` | `AttachNotAllowedOnNonInteractiveWorkspace` |
| Uniqueness `(workspaceRef, attachSubject, sessionName)` | label-selector count == 0 | `DuplicateSession` |
| `per-attach` sessionName caller-provided | `self.spec.mode != "per-attach" || self.metadata.labels["keese.ai/session-name"] matches "^attach-"` | `AttachSessionNameForbidden` |
| `attachGraceSeconds` bounds | `self.spec.attachGraceSeconds >= 0 && self.spec.attachGraceSeconds <= 86400` | `AttachGraceOutOfBounds` |
| `maxConcurrentSessionsPerUser` | active CR count for (subject, workspace) ≤ cap | `SessionsPerUserLimitExceeded` |
| `maxConcurrentAttaches` | total active CR count for workspace ≤ cap | `ConcurrentAttachLimitExceeded` |

## VAP on UPDATE

| Field | Mutable? | Reject reason if changed |
|---|---|---|
| `spec.workspaceRef` | No | `WorkspaceRefImmutable` |
| `spec.attachSubject` | No | `AttachSubjectImmutable` |
| `spec.sessionName` | No | `SessionNameImmutable` |
| `spec.mode` | No | `SessionModeImmutable` |
| `spec.attachGraceSeconds` | Yes | — |
| `spec.preserveOnPodFailure` | Yes | — |

## Finalizer chain on DELETE

Phase transitions to `Draining` at entry; removed at exit.

1. `status.phase = Draining` (SSA, `fieldOwner: keese-workspacesession-controller`).
2. Call `AgentRuntime.Drain(ctx, session, 90s)` on the pod — flushes SQLite to PVC (18).
3. Delete Pod (cascades: PVC retained if `Workspace.spec.storage.retentionPolicy: Retain`,
   deleted if `Delete`; in `shared` mode PVC is owned by Workspace, not session).
4. Remove OpenFGA tuples scoped to this session (typically none beyond workspace-level
   `editor` tuple; any session-created tuples written by witness cleanup here).
5. Remove finalizer → CR deleted.

Drain timeout: if `AgentRuntime.Drain` does not return within 90 s, log
`DrainTimeout`, proceed to pod delete anyway. Pod has `terminationGracePeriodSeconds: 120`
(rule 06-signal-handling §3 budget for agent runtime).

## Server-side attach sequence

```
kubectl-keese    attach webhook      OpenFGA     K8s API     workspace
(with JWT)       (in operator)                               controller
     |                |                 |           |             |
     |--attach ws --->|                 |           |             |
     |  {jwt, ws,     |                 |           |             |
     |   session}     |                 |           |             |
     |                |--validate JWT---|           |             |
     |                |  OIDCProvider  |           |             |
     |                |  → subject     |           |             |
     |                |--Check(ws#editor@subj)---->|             |
     |                |<-allow/deny----------------|             |
     |                |--attachPolicy checks        |             |
     |                |  (allowedSubjects,          |             |
     |                |   requiredClaims, caps)     |             |
     |                |--GET WorkspaceSession------>|             |
     |                |  (label selector)           |             |
     |                |<-404 or existing CR---------|             |
     |                |  [404: CREATE               |             |
     |                |   WorkspaceSession CR]----->|             |
     |                |                             |  controller |
     |                |                             |  observes   |
     |                |                             |  creates Pod|
     |                |                             |  + bridge   |
     |                |--watch pod Ready----------->|             |
     |                |<-pod Ready------------------|             |
     |<-attach-url----|                             |             |
     |  {pod, c=bridge}                             |             |
     |--kubectl exec -it <pod> -c bridge ---------->|             |
     |  /usr/local/bin/bridge                       |             |
     |<-ACP frames over WebSocket exec channel----->|             |
```

Webhook waits up to 30 s for pod `Ready` (cold boot). If pod does not become `Ready`
within 30 s, webhook returns `503 SessionStartTimeout`; CR left; controller continues
reconciling (client may retry attach and find the existing CR).

## Printer columns

```
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Subject",type="string",JSONPath=".spec.attachSubject"
// +kubebuilder:printcolumn:name="Session",type="string",JSONPath=".spec.sessionName"
```

## ReBAC markers

```go
// +keese:rebac-tuple=workspace#editor
```

`attachSubject` carries the caller; the workspace controller writes
`workspace:<name>#editor@<attachSubject>` if not already present. This is the
workspace-level tuple (04a), not a session-level tuple. Session deletion does not
remove this tuple (subject may have other sessions or future re-attaches).

## Refs

[08b](08b-goose-acp-stdio-k8s.md) · [02](02-workspace-model.md) · [04a](04a-openfga-authz-model.md) · [04b](04b-projected-sa-identity.md) · [18](18-process-lifecycle.md) · [plan D27](../plans/scaffolding-plan.md)
