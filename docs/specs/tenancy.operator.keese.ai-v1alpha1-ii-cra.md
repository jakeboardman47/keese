<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: spec
category: contract
depends:
  - ../designs/25-cross-tenant-agreement.md
  - ../designs/25-ii-spec-schema.md
  - ../designs/25-iii-approval-flow.md
  - ../designs/04a-openfga-authz-model.md
  - ../designs/24-tenant-crd.md
related_skills: [crd-authoring, controller-authoring]
status: current
last_verified: 2026-04-21
regression_lock: false
---

# tenancy.operator.keese.ai v1alpha1 — CrossTenantAgreement CRD

Companion to [`tenancy.operator.keese.ai-v1alpha1.md`](tenancy.operator.keese.ai-v1alpha1.md).

## Spec schema

```yaml
apiVersion: tenancy.operator.keese.ai/v1alpha1
kind: CrossTenantAgreement
metadata:
  name: <name>   # cluster-scoped
spec:
  from:
    # +keese:rebac-tuple=tenant.allows_messaging
    tenantRef:
      name: <tenant-name>     # immutable after create (VAP); must resolve (webhook)
    workspaceSelector:
      matchLabels:
        keese.ai/cra-eligible: "true"
  to:
    # +keese:rebac-tuple=tenant.allows_messaging
    tenantRef:
      name: <tenant-name>     # must differ from from.tenantRef.name (VAP)
    workspaceSelector:
      matchLabels:
        keese.ai/cra-eligible: "true"
  scope:
    # +keese:rebac-tuple=workspace.messageable_from
    natsSubjects:             # max 50; each must start with "keese.cta." (VAP)
      - "keese.cta.*.events"
    a2aRoles:                 # each ∈ {reader, writer, bidirectional} (VAP)
      - bidirectional
  expiresAt: "2026-12-31T23:59:59Z"   # RFC3339; > now on create; immutable (VAP)
status:
  observedGeneration: 0
  phase: Pending              # Pending|Approved|Rejected|Expired
  conditions:
    - type: Approved
      status: "False"
      reason: AwaitingApprovals
      lastTransitionTime: "..."
  approvals:                  # append-only; max 2 entries (one per tenant)
    - tenant: <tenant-name>
      approvedBy: <oidc-email-or-sa>
      approvedAt: "..."
      # +keese:rebac-tuple=tenant.can_approve_cra
      signature: <base64>
      signatureType: oidc-keyless   # or sa-token
  workspaceSnapshot:          # frozen at Approved transition (TOFU)
    - fromWorkspace: <ws-name>
      toWorkspace: <ws-name>
      snapshotAt: "..."
```

## VAP CEL invariants

Named: `crosstenanagreement-policy.tenancy.operator.keese.ai/v1alpha1`

```cel
# No self-agreement
self.spec.from.tenantRef.name != self.spec.to.tenantRef.name

# expiresAt immutable after create
!has(oldSelf.spec.expiresAt) || self.spec.expiresAt == oldSelf.spec.expiresAt

# from/to tenantRef immutable after create
!has(oldSelf.spec.from) ||
  self.spec.from.tenantRef.name == oldSelf.spec.from.tenantRef.name

# natsSubjects prefix + count
self.spec.scope.natsSubjects.all(s, s.startsWith("keese.cta.")) &&
  size(self.spec.scope.natsSubjects) <= 50

# a2aRoles enum
self.spec.scope.a2aRoles.all(r,
  r == "reader" || r == "writer" || r == "bidirectional")

# Terminal phase immutability
has(oldSelf.status.phase) &&
  (oldSelf.status.phase == "Rejected" || oldSelf.status.phase == "Expired") ?
  self.status.phase == oldSelf.status.phase : true
```

## Validating webhook (cross-resource)

- `from.tenantRef` and `to.tenantRef` resolve to existing `Tenant` CRs
- No overlapping `Approved` CRA covers the same `(from-tenant, to-tenant)` workspace pair
  (controller-maintained indexer keyed by `from/to` tenant names; O(n) workspace-pair scan)
- Workspace selector results must belong to the named tenantRef's namespaces
  (`WorkspaceSelectorTenantMismatch` on violation)

## Approval handshake

```
kubectl annotate crosstenanagreement <name> keese.ai/cra-approve=true
```

Webhook on annotation write:
1. Resolve annotator identity from `AdmissionRequest.UserInfo`.
2. `Check(tenant:<T>#can_approve_cra@<annotator-subject>)` — `HIGHER_CONSISTENCY`, p99 ≤ 25 ms.
3. Denied → reject with `CRAApprovalForbidden`.
4. Compute signature over `(cra-uid || tenant-uid || expiresAt)`:
   - Human: cosign keyless OIDC (`signatureType: oidc-keyless`)
   - CI/SA: SHA-256 HMAC keyed by projected SA token (`signatureType: sa-token`)
5. SSA-patch `status.approvals[]`; controller reconciles.

Rejection: annotate `keese.ai/cra-reject=true` from either tenant admin.

## Tuple sync on Approved transition

Order: expiresAt guard → out-of-band check → conflict check → write tuples →
populate `status.workspaceSnapshot[]` → transition phase.

ReBAC tuples written:
- `tenant:T_to#allows_messaging@tenant:T_from`
- `workspace:W_to#messageable_from@workspace:W_from` (cartesian product of snapshot)

TOFU: `workspaceSnapshot` frozen at Approved; new workspaces matching selector
do NOT inherit — emit `WorkspaceSnapshotDrift`; new CRA required.

Out-of-band: pre-existing tuple → no-op + `OutOfBandTupleObserved`.

On OpenFGA unavailable: `TupleSyncFailed`; exponential backoff 1s → 5 min max;
phase stays `Pending`.

## Expiry

Controller time-based reconcile at `expiresAt`:
1. Delete all tuples in `workspaceSnapshot[]` (idempotent).
2. Delete failure: `TupleSyncFailed`; retry backoff; phase stays `Approved`
   (fail-closed: tuples never silently dropped).
3. Transition phase to `Expired`; emit `CRAExpired`.
4. Finalizer `finalizers.crosstenanagreement.operator.keese.ai/nats` triggers
   stream `keese-cta-<cra-uid>` deletion.

## NATS stream provisioning

Stream `keese-cta-<cra-uid>` provisioned by Workflow controller (03c) at first
cross-tenant `transportRef` use. Subjects: `keese.cta.<cra-uid>.>`.
Deleted by finalizer on CRA deletion or `Expired` transition.

## Samples

Minimal: `config/samples/tenancy/cra-alpha-to-beta-minimal.yaml`
Fully populated: `config/samples/tenancy/cra-alpha-to-beta-full.yaml`
Both pass `kubectl apply --dry-run=server` against envtest (rule 04.15).

## Acceptance tests

| ID | Name | File | Description |
|---|---|---|---|
| C-01 | BilateralApprovalHappyPath | `test/kuttl/tenancy/cra-bilateral-approval/` | Both tenants annotate; tuples written; phase → Approved; snapshot frozen |
| C-02 | SignatureInvalid | `internal/controller/tenancy/crosstenanagreement/approval_test.go` | Tampered signature rejected by webhook; `CRAApprovalInvalid` event; phase stays Pending |
| C-03 | TOFUDrift | `internal/controller/tenancy/crosstenanagreement/tuplesync_test.go` | New workspace matching selector after Approved; `WorkspaceSnapshotDrift` emitted; no new tuples |
| C-04 | OutOfBandTupleNoOp | `internal/controller/tenancy/crosstenanagreement/tuplesync_test.go` | Pre-existing tuple; controller no-ops; `OutOfBandTupleObserved` event |
| C-05 | ExpiryTupleCleanup | `test/kuttl/tenancy/cra-expiry/` | `expiresAt` crossed; tuples deleted; phase → Expired; NATS stream deleted |
| C-06 | OverlappingCRAConflict | `internal/controller/tenancy/crosstenanagreement/conflict_test.go` | Two CRAs overlap same workspace pair; `CRAConflict` on both; later-Approved held Pending |
| C-07 | EnvtestIdempotency | `internal/controller/tenancy/crosstenanagreement/suite_test.go` | ≥ 3 reconciles with no spec change; `observedGeneration` stable |
| C-08 | CosignBilateralApproval | `internal/controller/tenancy/crosstenanagreement/cosign_approval_test.go` | Inject mock cosign keyless OIDC token for both tenant admins; verify `signatureType: oidc-keyless` in `status.approvals[]`; phase → Approved; tuples written in declared order (expiresAt guard → out-of-band check → conflict check → write) |
| C-09 | TOFUSnapshotWorkspaceAddRemove | `internal/controller/tenancy/crosstenanagreement/tofu_drift_test.go` | After Approved: (a) add workspace matching selector → `WorkspaceSnapshotDrift` emitted; no tuple written; snapshot unchanged; (b) remove workspace from snapshot tenant → tuple deleted; `WorkspaceSnapshotDrift` not re-emitted |
| C-10 | ExpiryTupleCleanupRace | `internal/controller/tenancy/crosstenanagreement/expiry_race_test.go` | Inject OpenFGA unavailable during expiry delete; verify phase stays Approved (fail-closed); retry backoff; `TupleSyncFailed` event each attempt; when OpenFGA recovers → tuples deleted → phase → Expired |
| C-11 | OverlappingCRAConflictBothDirections | `internal/controller/tenancy/crosstenanagreement/conflict_test.go` | CRA-A covers (alpha→beta, ws-1); CRA-B covers (alpha→beta, ws-1) with different scope; CRA-B held Pending; `CRAConflict` on CRA-B; CRA-A Approved; verify reverse-direction CRA (beta→alpha, ws-1) is NOT blocked (distinct tuple relation) |
