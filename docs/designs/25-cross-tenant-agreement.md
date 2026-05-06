<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: tenancy
depends:
  - 04a-openfga-authz-model.md
  - 04b-projected-sa-identity.md
  - 09-transport-crd.md
  - 03c-workflow-messaging-plane.md
  - 24-tenant-crd.md
related_skills: [crd-authoring, controller-authoring]
status: current
last_verified: 2026-04-21
rollback: |
  Phase Approved CrossTenantAgreements to Rejected; controller deletes
  the synced ReBAC tuples (tenant.allows_messaging, workspace.messageable_from).
  In-flight cross-tenant NATS streams drain via 09's stream-deletion path
  (keese.cta.<cta-uid>.* streams are owner-ref'd to CRA; GC on CRA deletion).
---

# 25 — CrossTenantAgreement CRD (D29)

## Decision

`CrossTenantAgreement` (CRA) is a **cluster-scoped** CRD at
`keese.ai/v1alpha1`. It gates all cross-tenant a2a messaging
with a **bilateral approval handshake** before any ReBAC tuple is written.
Intra-tenant a2a requires no CRA. Schema detail: [25-ii-spec-schema.md](25-ii-spec-schema.md).
Approval flow + NATS prefix + failure modes: [25-iii-approval-flow.md](25-iii-approval-flow.md).

## Context

Workspace is the security boundary for keese agents. Cross-tenant communication
must be explicitly, bilaterally, workspace-granularly authorized before the
Envoy AI Gateway (04a) or NATS bridge (09) allows the first message.

The design resolves all five open questions from the stub:

- **Q1 — approval subject:** `tenant.can_approve_cra` relation introduced
  (defaults to `admin` via OpenFGA computed relation). See §Approval model.
- **Q2 — signature:** cosign keyless OIDC for human approvals; SA-token signature
  for CI automation. Both stored in `status.approvals[].signature`. See 25-iii.
- **Q3 — selector scope creep:** new workspaces matching the selector do NOT
  inherit — explicit re-approval required (TOFU only on initial Approved).
- **Q4 — out-of-band tuples:** controller no-ops when tuple exists pre-approval;
  emits `OutOfBandTupleObserved` event. See 25-iii §Tuple sync.
- **Q5 — expiry:** `expiresAt` triggers tuple deletion + `Expired` phase; re-approval
  requires a new CRA object (append-only, audit-friendly).

## Approval model

To avoid hard-coupling to `tenant#admin`, a new OpenFGA relation is added:

```
tenant.can_approve_cra: [user, service_account]
              computed: admin from self
```

Default: `admin` is `can_approve_cra` via computed relation. Tenant admins may
delegate to a narrower group by writing explicit `tenant:T#can_approve_cra@user:U`
tuples. This isolates the CRA approval privilege from day-to-day admin operations.

ReBAC marker: `// +keese:rebac-tuple=tenant.can_approve_cra` on the approval
annotation handler (controller validates subject at approval time via
`Check(tenant:T#can_approve_cra@<annotator-subject>)`).

## CRD summary

Group: `keese.ai` · Version: `v1alpha1` · Kind: `CrossTenantAgreement`
Scope: **Cluster** · Identity key: `metadata.name`.

Printer columns (rule 04.5): `Age`, `Ready`, `Phase`, `From`, `To`, `ExpiresAt`.

```yaml
# +kubebuilder:subresource:status
# +kubebuilder:printcolumn:name=Phase,type=string,JSONPath=.status.phase
# +kubebuilder:printcolumn:name=From,type=string,JSONPath=.spec.from.tenantRef.name
# +kubebuilder:printcolumn:name=To,type=string,JSONPath=.spec.to.tenantRef.name
# +kubebuilder:printcolumn:name=ExpiresAt,type=string,JSONPath=.spec.expiresAt
```

Full YAML schema sketch: [25-ii-spec-schema.md](25-ii-spec-schema.md).

## ReBAC tuples written

Written by the CRA controller exclusively (SSA, `fieldOwner=keese-crosstenanagreement-controller`):

| Tuple | Condition | Written when | Deleted when |
|---|---|---|---|
| `tenant:T_to#allows_messaging@tenant:T_from` | phase = Approved | Both approvals recorded + expiresAt > now | Rejected, Expired, or CRA deleted |
| `workspace:W_to#messageable_from@workspace:W_from` (per cartesian product) | phase = Approved | Tuples above written + snapshot matches selectors | Same as above |

ReBAC markers:
```go
// +keese:rebac-tuple=tenant.allows_messaging
// +keese:rebac-tuple=workspace.messageable_from
```

Workspace-selector snapshot: at Approved transition the controller enumerates matching
Workspaces in each tenant and writes the cartesian product of tuples. This snapshot is
stored in `status.workspaceSnapshot[]`. New workspaces added after the initial Approved
transition do **not** inherit — controller compares current selector results with the
snapshot and emits `WorkspaceSnapshotDrift` (Q3 resolved: TOFU semantics).

## NATS cross-tenant stream prefix

Cross-tenant NATS messaging uses a separate stream per CRA:

- **Stream name:** `keese-cta-<cra-uid>` (short; ≤ 64 char per 09 field VAP)
- **Subjects:** `["keese.cta.<cra-uid>.>"]`
- **Provisioned by:** Workflow controller (03c) at first cross-tenant `transportRef` use
- **Owner-ref:** Argo Workflow → GC on Workflow deletion; CRA deletion triggers
  stream deletion (finalizer `finalizers.crosstenanagreement.keese.ai/nats`)
- **Cleanup owner:** Workflow controller (09/03c cross-dep confirmed)

## Observability

OTEL spans: `keese.cta.approve`, `keese.cta.sync`, `keese.cta.expire`.

Metrics:
- `keese_cta_phase_total{phase}` — gauge per phase
- `keese_cta_approval_latency_seconds` — histogram from creation to second approval
- `keese_cta_tuple_sync_duration_seconds` — histogram per Approved transition

Event reasons (finite const table in
`internal/controller/tenancy/crosstenanagreement/events.go`):
`CRAApproved`, `CRAExpired`, `CRARejected`, `OutOfBandTupleObserved`,
`TupleSyncFailed`, `WorkspaceSnapshotDrift`, `CRAApprovalInvalid`,
`SignatureVerificationFailed`.

## Admission invariants (VAP + webhook)

VAP CEL (static):
- `spec.expiresAt` must be future on create; `expiresAt` is immutable after create.
- `spec.from.tenantRef.name != spec.to.tenantRef.name` (no self-agreement).
- `spec.scope.natsSubjects[]` max 50 entries; each entry matches `keese.cta.*` pattern.
- `spec.scope.a2aRoles[]` ∈ allowed enum.
- Phase transitions: `Pending → Approved|Rejected`; `Approved → Expired`; terminal
  phases are immutable (CEL: `!has(oldSelf.status.phase) || ...`).

Validating webhook (cross-resource):
- `spec.from.tenantRef` and `spec.to.tenantRef` must resolve to existing Tenant CRs.
- No overlapping Approved CRA covers the same `(from-tenant, to-tenant)` workspace pair
  (checked via controller-maintained index — see 25-iii §Conflict detection).

No conversion webhooks at v1alpha1 (rule 04.13).

## Failure modes summary

Full 10-row table: [25-iii-approval-flow.md](25-iii-approval-flow.md) §Failure modes.

Quick reference of the highest-risk paths:
- One tenant approves, the other doesn't: no tuples written; controller requeues
  until `expiresAt`; then transitions to `Expired`.
- Signature invalid: webhook rejects annotation; `CRAApprovalInvalid` event; phase stays
  `Pending`.
- OpenFGA unavailable on Approved transition: `TupleSyncFailed` event; retries with
  backoff; phase stays `Pending` until write succeeds.
- Tenant deletion while CRA Approved: finalizer on Tenant blocks deletion until all
  owned CRAs are Expired/Rejected.

## Refs

- [25-ii-spec-schema.md](25-ii-spec-schema.md) — full YAML schema + VAP CEL
- [25-iii-approval-flow.md](25-iii-approval-flow.md) — approval flow, failure modes, samples
- [04a-openfga-authz-model.md](04a-openfga-authz-model.md) — tuple shapes (iter-5)
- [09-transport-crd.md](09-transport-crd.md) — a2a cross-tenant scope + NATS prefix
- [03c-workflow-messaging-plane.md](03c-workflow-messaging-plane.md) — CTA admission check
- [24-tenant-crd.md](24-tenant-crd.md) — Tenant CRD (D26)
- [../plans/rubric.md](../plans/rubric.md)

## Iteration log

Full rubric tables: [25-iv-iter-log.md](25-iv-iter-log.md).

- **Iter-1 2026-04-21** — 87.5 REVISE. Gaps: Cat 10 HA/resource ceilings not stated;
  conflict-detection index not specified; Cat 4/5 pre-gate structural.
- **Iter-2 2026-04-21** — 97.5 SHIP. Closed Cat 10 (leader-election, ceilings, upgrade
  path in 25-iii); conflict-detection index in 25-iii. Pre-gate Cat 4/5 residuals accepted.
- **Iter-3 2026-04-21** — 97.5 SHIP. Operational readiness pass; all cross-deps confirmed;
  Expired-retry runbook ref added. Status: `current`.
