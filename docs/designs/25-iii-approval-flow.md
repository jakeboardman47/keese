<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: tenancy
depends:
  - 25-cross-tenant-agreement.md
  - 25-ii-spec-schema.md
  - 04a-openfga-authz-model.md
  - 03c-workflow-messaging-plane.md
related_skills: [controller-authoring]
status: current
last_verified: 2026-04-21
rollback: |
  Approval annotations are idempotent. If a bad approval is written, transition CRA to
  Rejected (controller action via annotate keese.ai/cra-reject=true on either tenant admin).
  Controller deletes tuples and emits CRARejected. New CRA required to re-establish.
---

# 25-iii — CrossTenantAgreement: Approval Flow

Companion to [25-cross-tenant-agreement.md](25-cross-tenant-agreement.md).

## Phase state machine

```
[Created] → Pending
  Pending → Approved   (both approvals valid + expiresAt > now)
  Pending → Rejected   (either tenant admin annotates cra-reject=true, or expiresAt reached)
  Pending → Expired    (expiresAt reached with fewer than 2 approvals)
  Approved → Expired   (expiresAt crossed; controller transitions + deletes tuples)
  Rejected / Expired   (terminal; immutable)
```

## Approval handshake

Each tenant admin (or delegated `can_approve_cra` subject) signals approval via annotation:

```
kubectl annotate crosstenanagreement <name> keese.ai/cra-approve=true
```

The **validating webhook** fires on this annotation write:
1. Resolve annotator identity from the `UserInfo` in the `AdmissionRequest`.
2. `Check(tenant:<T>#can_approve_cra@<annotator-subject>)` via OpenFGA
   (`HIGHER_CONSISTENCY`; p99 ≤ 25 ms per 04a tier table).
3. If denied → reject with `CRAApprovalForbidden`.
4. Compute signature over `(cra-uid || tenant-uid || expiresAt)`:
   - **Human (OIDC-authed user):** cosign keyless OIDC signature; annotator's OIDC
     token from `UserInfo.extra["authentication.kubernetes.io/credential-id"]` used
     as identity binding. Signature stored as `status.approvals[].signature`
     (`signatureType: oidc-keyless`).
   - **CI/automation (ServiceAccount):** SHA-256 HMAC over the payload keyed by the
     SA's projected token (audience `keese-egress-<tenant>`); stored as `status.approvals[]`
     (`signatureType: sa-token`). Not a cosign signature — the SA token IS the identity
     attestation (rule 05.3).
5. Webhook SSA-patches `status.approvals[]` with the new entry. Controller reconciles
   and checks if both tenants have approved.

Rejection: `kubectl annotate crosstenanagreement <name> keese.ai/cra-reject=true` from
either tenant admin; webhook validates identity same as above; controller transitions to
`Rejected` and deletes tuples.

## Tuple sync on Approved transition

When controller detects both `status.approvals[]` entries are present and valid:

1. **expiresAt guard:** abort if `expiresAt ≤ now`; transition to `Expired`.
2. **Out-of-band check (Q4):** for each tuple in the cartesian product, call
   `fga read` to detect pre-existing tuples. If found: emit `OutOfBandTupleObserved`
   (structured: `{cra, tuple, owner: unknown}`); do NOT overwrite; proceed to next.
3. **Conflict check:** query controller-maintained index (informer on all CRAs,
   keyed by `(from-tenant, to-tenant, workspace-pair)`) for overlapping Approved CRAs.
   On conflict: emit `CRAConflict` event on both CRAs; the later-Approved CRA is
   superseded and held in `Pending` until the earlier expires.
4. **Write tuples via OpenFGA API** (server-side-apply pattern — upsert, idempotent):
   - `tenant:T_to#allows_messaging@tenant:T_from`
   - `workspace:W_to#messageable_from@workspace:W_from` (per snapshot pair)
5. Populate `status.workspaceSnapshot[]` with the pairs written.
6. Transition `status.phase` to `Approved`; emit `CRAApproved`.

On OpenFGA unavailability: log `TupleSyncFailed`; requeue with exponential backoff
(1s → 2s → 4s → max 5 min). Phase stays `Pending` until write succeeds.

## Workspace selector snapshot (Q3 — TOFU)

At Approved transition the controller enumerates:
- `from`-side: all Workspaces in `spec.from.tenantRef`'s namespaces matching
  `spec.from.workspaceSelector`
- `to`-side: same for `spec.to`

The cartesian product is written as tuples AND stored in `status.workspaceSnapshot[]`.

On subsequent reconciles the controller compares current selector results with the
snapshot. Divergence (new matching Workspace) → emit `WorkspaceSnapshotDrift` event;
do NOT auto-extend tuples. Humans must create a new CRA covering the new workspace pair.
Old workspace removed from tenant → controller deletes its tuples; emits `CRAExpired`
for that pair (not the whole CRA).

## Expiry

Controller runs a time-based reconcile triggered by `expiresAt`. On crossing the
deadline:

1. Delete all tuples in `status.workspaceSnapshot[]` from OpenFGA (idempotent calls).
2. On delete failure: emit `TupleSyncFailed`; retry backoff; phase stays `Approved`
   until delete succeeds (fail-closed: tuples outlive CRA briefly, never dropped
   silently).
3. Transition `status.phase` to `Expired`; emit `CRAExpired`.
4. Finalizer removed only after tuples deleted.

Re-approval requires a new CRA object (`kubectl create`). The expired CRA is retained
for audit; controller does not delete it. Garbage collection is user-driven
(`kubectl delete`).

## Conflict detection index

Controller maintains a controller-runtime indexer on `CrossTenantAgreement`:
```
field: ".spec.from.tenantRef.name + "/" + .spec.to.tenantRef.name"
```
At Approved transition, the controller lists all CRAs with the same `(from, to)` pair
and computes workspace-pair overlap against `status.workspaceSnapshot[]`. This is an
in-memory O(n) scan over workspace pairs per `(from, to)` key; bounded by
`spec.from.workspaceSelector` + `spec.to.workspaceSelector` cardinality.

## Operational readiness

**Leader election:** CRA controller runs under controller-runtime's leader-election
(same lease as the main operator). Only one controller pod reconciles at a time;
other pods are passive standby. Tuple writes are idempotent; duplicate writes are
safe.

**Resource ceilings:** Maximum CRAs per cluster: 10,000 (advisory; enforced via
`ValidatingAdmissionPolicy` count check using CEL `authorizer.serviceAccount(...)` —
deferred to post-gate; tracked in `docs/plans/phase-08.md`). Per-CRA workspace pairs
bounded by selector match count; recommended ≤ 1,000 pairs per CRA.

**Upgrade path (v1alpha1 → v1beta1):** Schema additions are additive; no conversion
needed at v1alpha1. Promotion requires `docs/plans/migration-crosstenanagreement.md`
scored ≥ 90 (rule 04.2).

**Runbook — Expired tuple delete failure:** `docs/plans/runbook-cta-tuple-delete.md`
(to author P8). Manual recovery: `fga delete` the tuples directly; controller
detects empty tuples on next reconcile and transitions to `Expired`.

## Failure modes

| # | Failure | Detection | Mitigation |
|---|---|---|---|
| 1 | One tenant approves; other doesn't before `expiresAt` | No second `status.approvals[]` entry; `expiresAt` crossed | Controller transitions to `Expired`; emits `CRAExpired`; no tuples written |
| 2 | Approval signature invalid | Webhook rejects annotation; `CRAApprovalInvalid` event | Re-annotate with valid credentials; re-authenticate OIDC session if needed |
| 3 | Selector matches no workspaces | `status.workspaceSnapshot[]` empty on Approved | Emit `WorkspaceSnapshotDrift`; CRA Approved but no `workspace.messageable_from` tuples; Transport `scope: cross-tenant` still requires tuple (09 rejects at subscribe) |
| 4 | Selector matches workspaces in wrong tenant | Webhook: cross-resource lookup verifies workspace labels match tenantRef namespaces; reject `WorkspaceSelectorTenantMismatch` | Fix `workspaceSelector` or `tenantRef` |
| 5 | Controller crash mid-tuple-write | Some tuples written; OpenFGA partially consistent | On restart controller re-enumerates snapshot; writes missing tuples (idempotent); no gap unless crash + immediate subscribe |
| 6 | OpenFGA unavailable on Approved transition | `TupleSyncFailed` event; phase stays `Pending` | Retries with backoff; `keese_cta_tuple_sync_duration_seconds` alert at > 5 min |
| 7 | CRA conflict (overlapping workspace pairs) | `CRAConflict` event on both CRAs; later-Approved held in Pending | Create non-overlapping CRAs; or expire/delete the earlier one |
| 8 | Tenant deletion while CRA Approved | Tenant finalizer `finalizers.tenant.keese.ai/agreements` blocks deletion | CRA controller transitions all owned CRAs to `Rejected` + deletes tuples; finalizer released |
| 9 | Expired CRA tuple delete failure | `TupleSyncFailed` on delete; phase stays `Approved` past deadline | Retry backoff; runbook `runbook-cta-tuple-delete.md`; manual `fga delete` + controller reconcile |
| 10 | Third-party deletes a CRA-owned tuple | `fga read` on next reconcile finds tuple missing; emit `OutOfBandTupleObserved` (deletion variant); controller re-writes tuple | Alert on event rate; audit OpenFGA write logs |

## Automatability

| Target | Command |
|---|---|
| Dry-run samples | `make cra-dry-run` (applies config/samples/tenancy/cra-*.yaml to envtest) |
| Approval webhook unit | `go test ./internal/controller/tenancy/crosstenanagreement/...` |
| Tuple-sync integration | `go test ./internal/controller/tenancy/crosstenanagreement/...` (envtest; mock OpenFGA) |
| Conflict index unit | `go test ./internal/controller/tenancy/crosstenanagreement/conflict/...` |
| e2e bilateral handshake | `test/e2e/cra_bilateral_test.go` (envtest + NATS mock) |

## Tests named

- `internal/controller/tenancy/crosstenanagreement/suite_test.go`: envtest; CRDs loaded;
  idempotency over ≥ 3 reconciles with no spec change.
- `internal/controller/tenancy/crosstenanagreement/approval_test.go`: signature
  validation, OIDC vs SA-token path, forbidden subject.
- `internal/controller/tenancy/crosstenanagreement/tuplesynce_test.go`: out-of-band
  tuple detection, partial write recovery, OpenFGA unavailable retry.
- `internal/controller/tenancy/crosstenanagreement/expiry_test.go`: tuple delete on
  expiry, failed delete retry, phase transition.
- `test/e2e/cra_bilateral_test.go`: full bilateral handshake; SIGTERM mid-approval;
  exit 0 assertion.
