<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: tenancy
depends:
  - 01-tenancy-capsule.md
  - 04a-openfga-authz-model.md
  - 04b-projected-sa-identity.md
  - 20a-api-group-layout.md
  - 20b-api-group-layout.md
related_skills: [crd-authoring]
status: current
last_verified: 2026-04-20
rollback: |
  If Tenant CRD causes regression: set operator flag --tenant-crd-mode=off;
  operator falls back to deriving tenant:X from namespace labels only; delete
  Tenant CRs via finalizer cleanup after Workspaces are reassigned or deleted.
  Document in docs/plans/migration-d26-rollback.md before executing.
---

# 24 — Keese `Tenant` CRD

Trade-offs, failure modes, upgrade/rollback, observability, and iteration log:
[24b-tenant-crd.md](24b-tenant-crd.md).

## Context

D26 (2026-04-20) amends D23: keese owns exactly one additional CRD,
`tenancy.operator.keese.ai/v1alpha1/Tenant` (cluster-scoped). It does NOT
reimplement namespace aggregation — that delegates to Capsule (Mode B) or
derives from `keese.ai/tenant=<name>` label selectors (Mode A). Purpose: give
ReBAC `tenant:X` a K8s-object backing with finalizers, events, and status, and
aggregate keese-specific tenant settings that previously had no canonical home.
Go path: `api/tenancy/v1alpha1`. Identity key for OpenFGA: `Tenant.metadata.name`.

## Spec Schema (Q1)

| Field | Required | VAP constraint | Settable by |
|---|---|---|---|
| `spec.capsuleTenantRef.name` | No (Mode B only) | Must resolve (webhook) | User |
| `spec.namespaceSelector.matchLabels` | No (Mode A) | Ignored if `capsuleTenantRef` set (VAP warn) | User |
| `spec.adminSubjects[]` | Yes | Non-empty; `kind` ∈ {User,Group} (VAP) | User |
| `spec.defaultGuardrailBindings[]` | No | Names must be non-empty strings | User |
| `spec.tokenBudgetRef.{name,namespace}` | No | Must resolve (webhook) | User |
| `spec.credentialPoolRef.{name,namespace}` | No | Must resolve (webhook) | User |
| `spec.defaultWorkspaceQuota` | No | ResourceList; dims ≥ 0 (VAP) | User |
| `spec.dedicatedGateway` | No | May not toggle while `status.namespaces[]` non-empty (VAP) | User |
| `status.observedGeneration` | — | operator-populated | Operator |
| `status.phase` | — | Enum: Pending/Provisioning/Ready/Degraded/Terminating | Operator |
| `status.conditions[]` | — | Standard `metav1.Condition` | Operator |
| `status.namespaces[]` | — | Observed namespace list | Operator |
| `status.capsuleTenantResolved` | — | Mode B only | Operator |

Printer columns (rule 04.5): `Age`, `Ready`, `Phase`, `Namespaces` (count),
`Mode` (`ModeB` if `capsuleTenantRef` set, else `ModeA`).

ReBAC marker on `spec.adminSubjects[]`:
`// +keese:rebac-tuple=tenant:T#admin@user:U` (rule 04.14; written by operator
bootstrap Job per 04a tuple table).

Deferred: `spec.tokenTTLOverride` (04b handles tier overrides at Workspace);
`spec.vclusterRef` (hard isolation deferred per D-01.2).

## Mode A Reconcile — Namespace Aggregation via Label (Q2)

Controller watches Namespaces matching `spec.namespaceSelector`; populates
`status.namespaces[]`. Informer on Namespace list; emits `NamespaceAdded` /
`NamespaceRemoved` events.

**Decision: fail-closed (option a).** When the `keese.ai/tenant=<name>` label is
removed from a namespace that has live Workspaces: controller installs finalizer
`finalizers.tenant.operator.keese.ai/namespaces` on the namespace; the VAP from
D-01.7 (`config/overlays/base/vap/namespace-tenant-label.yaml`) blocks label
removal while that finalizer is present. Controller removes the finalizer only
after all Workspaces in the namespace are Terminating or deleted. Event reason:
`TenantLabelLocked`. Rationale: accidental de-tenanting silently orphans OpenFGA
tuples and suspends quota enforcement — a security event, not a warning.

## Mode B Reconcile — Capsule Delegation (Q3)

When `spec.capsuleTenantRef` is set, controller reads the referenced Capsule
Tenant's `status.namespaces[]` via informer on `capsule.clastix.io/v1beta2/Tenant`.
Keese `status.namespaces[]` mirrors Capsule's list. `status.capsuleTenantResolved`
flips to `true` once the Capsule Tenant is found.

**Conflict resolution:** In Mode B, `spec.namespaceSelector` is **ignored**.
Capsule is authoritative for namespace membership. VAP emits non-blocking admission
warning `NamespaceSelectorIgnoredInModeB` if both fields are set (warn, not reject,
to permit declarative users to carry both fields). The label-immutability VAP
(D-01.7) still operates on `keese.ai/tenant` — no conflict with Capsule's
`capsule.clastix.io/tenant` key.

## OwnerRef Chain (Q4)

| Object | OwnerRef to Tenant? | Rationale |
|---|---|---|
| `TokenBudget` (via `spec.tokenBudgetRef`) | Yes (controller-set) | Budget is tenant-lifecycle-coupled; GC with Tenant is correct. |
| `GuardrailBinding` (via `spec.defaultGuardrailBindings`) | No | Bindings may be shared; cascade would be destructive for shared resources. |
| `Workspace` | No | Association is label-based; OwnerRef would cascade workspace deletion on tenant removal. |

Finalizer on Tenant CR: `finalizers.tenant.operator.keese.ai/workspaces`. Blocks
deletion until `status.namespaces[]` is empty.

## Admission Invariants (Q5)

Rule 04.12: VAP-first; webhook only for cross-resource lookups.

**VAP (CEL — static):**
- `spec.adminSubjects[]` non-empty on create and update.
- `spec.dedicatedGateway` must not toggle while `status.namespaces[]` length > 0.
  CEL: `oldSelf.spec.dedicatedGateway == self.spec.dedicatedGateway || size(self.status.namespaces) == 0`.
- Warn if both `capsuleTenantRef` and `namespaceSelector` set (`NamespaceSelectorIgnoredInModeB`).
- `spec.defaultWorkspaceQuota` dims must be valid Kubernetes quantity strings.

**Validating webhook (cross-resource):**
- `spec.capsuleTenantRef` must resolve to an existing `capsule.clastix.io/v1beta2/Tenant`.
- `spec.namespaceSelector` must not overlap another keese `Tenant`'s selector
  (checked via controller-maintained indexer on `status.namespaces[]`).
- `spec.tokenBudgetRef` and `spec.credentialPoolRef` must resolve when set.

No conversion webhooks at v1alpha1 (rule 04.13).

## Migration from Pre-D26 (Q6)

String-derived identity (`keese.ai/tenant=<name>` labels) → CR-backed identity.
OpenFGA tuples already use `<name>` as identity key; no tuple rewrite required.

Migration Job `migrations/tenant-backfill.yaml`:
1. List all Namespaces with `keese.ai/tenant` label; group by value.
2. For each unique `<name>`: if Tenant CR absent, create with
   `spec.namespaceSelector.matchLabels[keese.ai/tenant]=<name>` and
   `spec.adminSubjects[]` from existing `keese-tenant-admin` RoleBindings.
3. Idempotent: skips existing Tenant CRs.

ADR entry: `docs/plans/migration-d23-tenant-crd.md` (to author; cross-ref from D26).
Run: `kubectl apply -f migrations/tenant-backfill.yaml`.

## Cross-Reference Impacts

- **05a-envoy-ai-gateway-topology.md:** `spec.dedicatedGateway=true` triggers
  per-tenant gateway provisioning; 05a owns provisioning logic.
- **04a-openfga-authz-model.md:** Identity key for `tenant` type is
  `Tenant.metadata.name` (not `.uid` as implied in iter-4). 04a iter-5 should
  align. Tuple shapes unchanged; no migration required.

## Refs

- [01-tenancy-capsule.md](01-tenancy-capsule.md) · [04a-openfga-authz-model.md](04a-openfga-authz-model.md)
- [04b-projected-sa-identity.md](04b-projected-sa-identity.md)
- [20a-api-group-layout.md](20a-api-group-layout.md) · [../plans/scaffolding-plan.md](../plans/scaffolding-plan.md)
- [../references/crd-design-checklist.md](../references/crd-design-checklist.md) · [../plans/rubric.md](../plans/rubric.md)
- [24b-tenant-crd.md](24b-tenant-crd.md)
