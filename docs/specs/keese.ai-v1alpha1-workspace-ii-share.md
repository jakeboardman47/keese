<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: spec
category: contract
depends:
  - keese.ai-v1alpha1-workspace.md
  - ../designs/02-workspace-model.md
  - ../designs/04a-openfga-authz-model.md
related_skills: [crd-authoring, controller-authoring]
status: current
last_verified: 2026-04-21
---

# keese.ai v1alpha1-ii — WorkspaceShare CRD detail

Companion to [keese.ai-v1alpha1-workspace.md](keese.ai-v1alpha1-workspace.md).
Owns: WorkspaceShare CRD YAML, VAP, finalizer chain.

## Purpose

`WorkspaceShare` enables a Workspace owner to grant read or write access to subjects in
a different namespace, without violating the fail-closed NetworkPolicy model. The controller:
1. Creates a `ReferenceGrant` (Gateway API `gateway.networking.k8s.io/v1beta1`) in `spec.targetNamespace`.
2. Writes OpenFGA tuples `workspace:<workspaceRef.name>#<role>@<subject>` for each entry in `spec.subjects[]`.

The `ReferenceGrant` allows objects in `spec.targetNamespace` to reference the Workspace by name
across namespace boundaries (e.g., via `WorkspaceSession` or client tooling). It does NOT widen
the NetworkPolicy — all egress still routes through Envoy AI Gateway.

## CRD YAML sketch

```yaml
apiVersion: keese.ai/v1alpha1
kind: WorkspaceShare
metadata:
  name: ws-dev-share-acme-infra
  namespace: tenant-acme                # must equal Workspace namespace
  labels:
    keese.ai/workspace: ws-dev
  finalizers:
    - finalizers.workspaceshare.keese.ai/cleanup
spec:
  workspaceRef:
    name: ws-dev                        # required; immutable
  targetNamespace: tenant-infra         # required; immutable; != workspace namespace
  subjects:                             # required; non-empty
    - kind: User
      name: alice@example.com
    - kind: Group
      name: eng-platform
  role: viewer                          # required; viewer|editor; immutable
status:
  phase: Active                         # Pending|Active|Degraded|Terminating
  observedGeneration: 0
  conditions:
    - type: Ready
      status: "True"
      reason: ShareReady
      lastTransitionTime: 2026-04-21T00:00:00Z
  referenceGrantRef:
    name: keese-ws-dev-share
    namespace: tenant-infra
  openFGATuplesWritten: 2
```

## VAP on CREATE

| Rule | CEL sketch | Reject reason |
|---|---|---|
| `targetNamespace` != Workspace namespace | `self.spec.targetNamespace != workspaceNamespace` | `ShareSameNamespaceForbidden` |
| `subjects[]` non-empty | `size(self.spec.subjects) > 0` | `ShareSubjectsEmpty` |
| `spec.role` valid | `self.spec.role in ["viewer","editor"]` | `ShareRoleInvalid` |
| `workspaceRef` resolves | cross-resource webhook | `WorkspaceNotFound` |

## VAP on UPDATE

All spec fields are **immutable** after creation. Any mutation is rejected with `ShareImmutable`.
To change subjects or role: delete and re-create. Finalizer ensures tuple cleanup before deletion.

## Finalizer chain on DELETE

Phase → `Terminating` (SSA, `fieldOwner: keese-workspaceshare-controller`).

1. Delete OpenFGA tuples: `workspace:<workspaceRef.name>#<role>@<subject>` for all `spec.subjects[]`.
2. Delete `ReferenceGrant` in `spec.targetNamespace`.
3. Remove finalizer.

If OpenFGA is unreachable: retry 3× with backoff; emit `OpenFGAUnavailable`; do NOT proceed to
step 2 until tuples confirmed deleted (fail-closed; rule 05).

## RBAC markers

```
// +kubebuilder:rbac:groups=keese.ai,resources=workspaceshares,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=keese.ai,resources=workspaceshares/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=keese.ai,resources=workspaceshares/finalizers,verbs=update
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=referencegrants,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
```

ReBAC markers:

```go
// +keese:rebac-tuple=workspace#viewer  // spec.subjects[] when spec.role == viewer
// +keese:rebac-tuple=workspace#editor  // spec.subjects[] when spec.role == editor
```

## SSA fieldOwner · Status conditions · Printer columns

- SSA fieldOwner: `keese-workspaceshare-controller`
- `observedGeneration` on every status write (rule 04.4)
- Phases: `Pending | Active | Degraded | Terminating`
- Conditions: `Ready`

```
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Workspace",type="string",JSONPath=".spec.workspaceRef.name"
// +kubebuilder:printcolumn:name="TargetNS",type="string",JSONPath=".spec.targetNamespace"
// +kubebuilder:printcolumn:name="Role",type="string",JSONPath=".spec.role"
```

## Event reasons

Finite const table in `internal/controller/workspace/share/events.go`:
`ShareReady`, `ShareDegraded`, `ShareTerminating`, `OpenFGAUnavailable`,
`WorkspaceNotFound`, `ReferenceGrantApplied`, `TupleWriteFailed`, `TupleDeleteFailed`.

## Acceptance tests

| Test | Kind | Assertion |
|---|---|---|
| `TestWorkspaceShareAdmission` | envtest | VAP rejects `targetNamespace == workspace.namespace` |
| `TestWorkspaceShareReferenceGrant` | envtest | ReferenceGrant created in targetNamespace; SSA fieldOwner correct; idempotent over 3 reconciles |
| `TestWorkspaceShareTupleCleaned` | envtest | Delete WorkspaceShare → finalizer runs → tuples removed → ReferenceGrant deleted |
| `TestWorkspaceShareImmutableUpdate` | envtest | UPDATE of `spec.role` rejected with `ShareImmutable` |

## Failure modes

| Failure | Detection | Mitigation |
|---|---|---|
| OpenFGA unavailable on create | Tuple write fails; `TupleWriteFailed` event; phase = `Degraded` | Retry backoff; `ShareDegraded` alert after 5m |
| OpenFGA unavailable on delete | Tuple delete fails | Fail-closed; finalizer blocks; `TupleDeleteFailed`; manual recovery |
| ReferenceGrant namespace deleted | Controller re-creates on next reconcile | SSA re-apply |
| Workspace deleted before Share | OwnerRef cascade deletes Share; finalizer runs | Finalizer cleans tuples first |

## Refs

[02](../designs/02-workspace-model.md) · [04a](../designs/04a-openfga-authz-model.md) · [primary spec](keese.ai-v1alpha1-workspace.md)
