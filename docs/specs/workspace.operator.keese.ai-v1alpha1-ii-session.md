<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: spec
category: contract
depends:
  - workspace.operator.keese.ai-v1alpha1.md
  - ../designs/08b-goose-acp-stdio-k8s.md
  - ../designs/08b-ii-session-crd-spec.md
  - ../designs/02-workspace-model.md
  - ../designs/04b-projected-sa-identity.md
  - ../designs/18-process-lifecycle.md
related_skills: [crd-authoring, controller-authoring]
status: current
last_verified: 2026-04-21
---

# workspace.operator.keese.ai v1alpha1-ii — WorkspaceSession CRD detail

Companion to [workspace.operator.keese.ai-v1alpha1.md](workspace.operator.keese.ai-v1alpha1.md).
Authoritative CRD schema delegates to design [08b-ii-session-crd-spec.md](../designs/08b-ii-session-crd-spec.md)
which owns the canonical YAML. This file adds: controller RBAC, event reasons, test
acceptance criteria, and failure modes not in 08b-ii.

## CRD summary (from 08b-ii — do not duplicate full YAML here)

Name pattern: `<workspace>-<subject-hash-16>-<session-name>`.

| Field | Mutable | Notes |
|---|---|---|
| `spec.workspaceRef` | No | `WorkspaceRefImmutable`; owner Workspace must have `spec.interactive: true` |
| `spec.attachSubject` | No | `AttachSubjectImmutable`; OpenFGA subject form `user:<email>` (04b iter-2) |
| `spec.sessionName` | No | `SessionNameImmutable`; user-visible; `default` if unset |
| `spec.mode` | No | `SessionModeImmutable`; inherited from `Workspace.spec.sessionMode` |
| `spec.attachGraceSeconds` | Yes | [0, 86400]; inherits `Workspace.spec.attachGrace` |
| `spec.preserveOnPodFailure` | Yes | Boolean; default `false` |

Status fields: `phase` (`Starting|Running|Idle|Draining|Terminating`), `podRef`,
`attachedAt`, `lastActivityAt`, `attachedClientCount`, `tokenBudgetRef` (split mode),
`observedGeneration`, `conditions[Ready]`.

## VAP on CREATE (from 08b-ii)

All VAP rules from [08b-ii §VAP on CREATE](../designs/08b-ii-session-crd-spec.md) apply.
Key reasons: `AttachNotAllowedOnNonInteractiveWorkspace`, `DuplicateSession`,
`AttachSessionNameForbidden`, `AttachGraceOutOfBounds`, `SessionsPerUserLimitExceeded`,
`ConcurrentAttachLimitExceeded`.

VAP manifest path: `config/overlays/base/vap/workspacesession-create.yaml` (to author P8).

## VAP on UPDATE (from 08b-ii)

`workspaceRef`, `attachSubject`, `sessionName`, `mode` — all immutable.
`attachGraceSeconds`, `preserveOnPodFailure` — mutable.

## RBAC markers

```
// +kubebuilder:rbac:groups=workspace.operator.keese.ai,resources=workspacesessions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=workspace.operator.keese.ai,resources=workspacesessions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=workspace.operator.keese.ai,resources=workspacesessions/finalizers,verbs=update
// +kubebuilder:rbac:groups=workspace.operator.keese.ai,resources=workspaces,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
```

ReBAC marker:

```go
// +keese:rebac-tuple=workspace#editor  // spec.attachSubject
```

Tuple written by workspace controller: `workspace:<workspaceRef.name>#editor@<attachSubject>`.
Tuple is workspace-scoped; NOT removed on session delete (subject may reconnect).
Session-created derived tuples (e.g., `witness` tuples) are cleaned by finalizer step 4.

## SSA fieldOwner · Finalizer · Printer columns

- SSA fieldOwner: `keese-workspacesession-controller`
- Finalizer: `finalizers.workspacesession.operator.keese.ai/cleanup`
- `observedGeneration` on every status write (rule 04.4)

```
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Subject",type="string",JSONPath=".spec.attachSubject"
// +kubebuilder:printcolumn:name="Session",type="string",JSONPath=".spec.sessionName"
```

## Finalizer chain on DELETE (from 08b-ii, annotated)

Phase → `Draining` (SSA).

1. Call `AgentRuntime.Drain(ctx, session, 90s)` — flush SQLite to PVC (18).
   Drain timeout: if 90s elapsed, log `DrainTimeout`, proceed.
2. Delete Pod (if `Running` or `Idle`).
   PVC: retained if `Workspace.spec.storage.retentionPolicy: Retain` (shared mode: PVC owned by Workspace).
3. Remove session-scoped OpenFGA tuples (derived; workspace-level `#editor` tuple is NOT removed).
4. Remove finalizer.

Pod `terminationGracePeriodSeconds: 120` (rule 06 budget for agent runtime).

## Event reasons

Finite const table in `internal/controller/workspace/session/events.go`:

`SessionReady`, `SessionStartTimeout`, `SessionDraining`, `SessionTerminating`,
`DrainTimeout`, `ACPAttached`, `ACPDetached`, `ACPSessionLost`, `ACPGraceExpired`,
`BridgeOOMKilled`, `OpenFGAUnavailable`, `SessionStuckAfterPodFailure`,
`PodCreateFailed`, `DuplicateSessionRejected`.

## Acceptance tests

| Test | Kind | Assertion |
|---|---|---|
| `TestWorkspaceSessionIdempotency3Passes` | envtest | 3 reconciles on non-interactive Workspace rejected; 3 on interactive Workspace produce stable CR + Pod |
| `TestWorkspaceSessionFSMStartingToRunning` | envtest | Phase Starting → Running; `ACPAttached` emitted; pod `Ready` within 30s (cold-boot SLO) |
| `TestWorkspaceSessionAdmissionRejectsImmutableFields` | envtest | UPDATE of `spec.workspaceRef` rejected; UPDATE of `spec.attachGraceSeconds` allowed |
| `TestWorkspaceSessionDrainOnDelete` | kuttl | Delete WorkspaceSession → `Draining` → Drain called → pod deleted → finalizer removed |
| `TestWorkspaceSessionPreserveOnPodFailure` | envtest | Pod failure + `preserveOnPodFailure: true` → CR stays `Failed`; `SessionStuckAfterPodFailure` event |
| `TestWorkspaceSessionConcurrencyCapEnforced` | envtest | VAP rejects 3rd session when `maxConcurrentAttaches: 2` |

## Failure modes (supplementary to 08b)

| Failure | Detection | Mitigation |
|---|---|---|
| Cold-boot > 30s | `SessionStartTimeout`; webhook returns 503 | CR left; client retries; controller continues reconciling |
| Drain timeout (90s) | `DrainTimeout` log + event | Pod deleted anyway; PVC checkpoint available for resume |
| `preserveOnPodFailure=true` + pod failed | `SessionStuckAfterPodFailure`; manual delete required | Operator alert; `kubectl delete workspacesession` triggers finalizer |
| OpenFGA unavailable during CREATE | Admission fails; 503 | Fail-closed; client retries |
| Duplicate session (race) | VAP label-selector check; `DuplicateSessionRejected` | Controller drops duplicate; returns existing CR ref |

## Refs

[08b](../designs/08b-goose-acp-stdio-k8s.md) · [08b-ii](../designs/08b-ii-session-crd-spec.md) · [02](../designs/02-workspace-model.md) · [04b](../designs/04b-projected-sa-identity.md) · [18](../designs/18-process-lifecycle.md) · [primary spec](workspace.operator.keese.ai-v1alpha1.md)
