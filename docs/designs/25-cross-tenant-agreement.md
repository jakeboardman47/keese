<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: tenancy
depends:
  - 04a-openfga-authz-model.md
  - 04b-projected-sa-identity.md
  - 09-transport-crd.md
  - 24-tenant-crd.md
related_skills: [crd-authoring]
status: draft
last_verified: 2026-04-21
rollback: |
  Rollback: phase Approved CrossTenantAgreements to Rejected; controller deletes
  the synced ReBAC tuples (`tenant.allows_messaging`, `workspace.messageable_from`).
  In-flight cross-tenant NATS streams drain via 09's stream-deletion path.
---

# 25 — CrossTenantAgreement CRD (D29)

## Context

D29 introduces `CrossTenantAgreement` — a cluster-scoped CRD that gates
**cross-tenant a2a messaging** with a cert-manager-style **bilateral
handshake** before any ReBAC tuple is written.

Workspace-as-security-boundary reframe (2026-04-21): cross-tenant
communication requires explicit, bilateral, workspace-granular authz.
Intra-tenant a2a is implicit via Workflow definition (NATS topic existence
within `keese.tenant.<tenant-uid>.wf.<workflow-run-uid>.*` IS authz);
this CRD is exclusively for crossing tenant boundaries.

## TODO(design-gate)

This doc is a stub. Authoring proceeds via architect agent dispatch
once D29 is ratified in `docs/plans/scaffolding-plan.md` (done 2026-04-21).

## Open questions

1. **Approval subject.** Who can `approve` on a Tenant — only
   `tenant:T#admin@user:*`, or any `tenant:T#member` with a sub-relation?
2. **Signature requirement.** Approval `signature` field — cosign-style
   keyless OIDC commit signature, plain ServiceAccount token sig, or
   either-of? Recommend keyless OIDC for human approvals; SA-token sig
   for automation.
3. **Workspace-selector scope creep.** A `workspaceSelector` matching
   future workspaces (label selector) means new workspaces inherit the
   agreement implicitly. Acceptable? Or require explicit re-approval on
   workspace-set changes? Recommend explicit re-approval (Trust on
   First Use only on initial Approved).
4. **Out-of-band tuple writes.** When a tuple exists in OpenFGA before
   the CRA reaches Approved, controller no-ops (preserves the tuple).
   Should it audit-log the divergence? Recommend yes —
   `OutOfBandTupleObserved` event.
5. **Expiry behavior.** On `expiresAt`, tuples are removed and phase is
   `Expired`. Re-approval requires a new CRA (not `kubectl edit`)? This
   keeps approvals append-only and audit-friendly.

## Tuple shapes written

After both-side approval (status.phase = Approved), the controller
writes (Server-Side Apply via `dev/bootstrap/openfga/seed.yaml`-pattern):

| Tuple | Lifecycle |
|---|---|
| `tenant:T_to#allows_messaging@tenant:T_from` | Written on Approved; deleted on Rejected/Expired |
| `workspace:W_to#messageable_from@workspace:W_from` (per pair from selectors) | Written on Approved; deleted on Rejected/Expired |

Cross-cuts: see 04a iter-5 for the relation definitions.

## Refs

- [04a-openfga-authz-model.md](04a-openfga-authz-model.md) — relations
- [09-transport-crd.md](09-transport-crd.md) — a2a peer-auth modes + scope field
- [03-workflow-argo-delegation.md](03-workflow-argo-delegation.md) — Workflow controller cross-tenant admission check
- [24-tenant-crd.md](24-tenant-crd.md) — Tenant CRD (D26)
- [../plans/scaffolding-plan.md](../plans/scaffolding-plan.md) — D29

## Iteration log

### Iter-0 2026-04-21 — stub created. Awaiting architect dispatch.
