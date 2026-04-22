<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: spec
category: contract
depends:
  - ../designs/24-tenant-crd.md
  - ../designs/24b-tenant-crd.md
  - ../designs/25-cross-tenant-agreement.md
  - ../designs/25-ii-spec-schema.md
  - ../designs/25-iii-approval-flow.md
  - ../designs/04a-openfga-authz-model.md
  - ../designs/01-tenancy-capsule.md
related_skills: [crd-authoring, controller-authoring]
status: draft
last_verified: 2026-04-21
regression_lock: false
tests:
  unit: []
  envtest: []
  kuttl: []
metrics: []
events: []
---

# tenancy.operator.keese.ai v1alpha1 — spec

> **Status: draft.** This spec is a NEW addition tracking the kinds added by
> D26 (Tenant) and D29 (CrossTenantAgreement). It is intentionally a stub
> pending architect dispatch — owning designs are all `current`, so the
> design-gate predicate is satisfied for promotion.

## Owning design(s)

All `status: current`:

- [`designs/24-tenant-crd.md`](../designs/24-tenant-crd.md) — D26 Tenant CRD: spec, reconcile, admission, migration
- [`designs/24b-tenant-crd.md`](../designs/24b-tenant-crd.md) — D26 Tenant CRD: trade-offs, failure modes, upgrade, observability
- [`designs/25-cross-tenant-agreement.md`](../designs/25-cross-tenant-agreement.md) — D29 CrossTenantAgreement CRD
- [`designs/25-ii-spec-schema.md`](../designs/25-ii-spec-schema.md) — D29 spec schema + VAP CEL
- [`designs/25-iii-approval-flow.md`](../designs/25-iii-approval-flow.md) — D29 approval flow + failure modes
- [`designs/04a-openfga-authz-model.md`](../designs/04a-openfga-authz-model.md) — `tenant.allows_messaging` + `workspace.messageable_from` relations (iter-5)

## Kinds covered

Group: `tenancy.operator.keese.ai/v1alpha1`. Cluster-scoped both.

- **Tenant** (D26) — keese-specific tenancy config; delegates namespace aggregation to Capsule (Mode B) or labels (Mode A). Spec: `guardrailBindings[]`, `tokenBudgetRef`, `credentialPoolRef`, `defaultQuota`, optional `capsuleTenantRef`, `oidc.allowedProviders[]` (D28 cross-cut), `security.allowUnsafeTransports` (09 cross-cut).
- **CrossTenantAgreement** (D29) — bilateral handshake gating cross-tenant a2a messaging. Spec: `from.tenantRef + workspaceSelector`, `to.tenantRef + workspaceSelector`, `scope.{natsSubjects,a2aRoles}`, `expiresAt`. Status: `phase`, `approvals[]`, `workspaceSnapshot[]`, `observedGeneration`.

## Acceptance test categories (to fill in)

- Tenant: schema validation, FSM transitions, Capsule Mode B / Mode A switching, OIDC allowedProviders enforcement, quota merge precedence.
- CrossTenantAgreement: bilateral approval webhook, signature verification (cosign + SA-token), TOFU snapshot drift detection, expiry tuple cleanup, out-of-band tuple no-op, NATS stream provisioning + finalizer GC.

TODO(architect-dispatch): Author iter-1/iter-2/iter-3 to score ≥ 90 honestly.
