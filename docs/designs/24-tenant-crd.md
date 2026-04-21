<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: tenancy
depends:
  - 01-tenancy-capsule.md
  - 04a-openfga-authz-model.md
  - 04b-projected-sa-identity.md
  - 05a-envoy-ai-gateway-topology.md
  - 05c-mcp-policy-enforcement.md
  - 10a-otel-topology.md
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
| `spec.jwksCacheFailOpenSeconds` | No (default 300) | Int in [30, 600]; default 60 when `dedicatedGateway: true` (mutating webhook) | User (tenant-admin) |
| `spec.auditArgumentsRedacted` | No (default false) | Boolean (OpenAPI-enforced) | User (tenant-admin) |
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

`spec.jwksCacheFailOpenSeconds` semantics: controls the per-tenant fail-open window for
Envoy JWT Authn JWKS caching during kube-apiserver unavailability. Values < 30s rejected
(apiserver JWKS load dominates); values > 600s rejected (excessive fail-open). Default 300s
for standard tenants; drops to 60s at admission when `dedicatedGateway: true` (mutating
webhook). During the window, JWT Authn accepts tokens validated against the last cached JWKS;
past the window, fails-closed (all egress denied with 401). Metric:
`keese_envoy_jwks_cache_fail_open_seconds_remaining{tenant}`; event `JWKSCacheExhausted`
at < 10% remaining. See 05a-envoy-ai-gateway-topology.md iter-2.

`spec.auditArgumentsRedacted` semantics: default `false` (PII-safe) — `mcp.arguments` never
reaches ES/Loki. Set `true` → OTEL processor `keese-argument-redactor` routes sanitized
arguments into audit span attribute `mcp.arguments.redacted` via the Presidio engine configured
per tenant (05c + 06). No redaction engine → arguments still dropped; event
`AuditRedactionUnavailable` emitted. Cross-ref: 10a (stub) for OTEL collector topology.
Compliance note: `true` is an explicit opt-in to process potentially-sensitive data through a
redaction pipeline; tenant-admin must verify Presidio config matches data residency requirements.
Rule 05.10 applies — tokens/bodies are never logged; only redacted argument attributes appear.

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
- `spec.jwksCacheFailOpenSeconds` in range: CEL
  `self.spec.jwksCacheFailOpenSeconds == 0 || (self.spec.jwksCacheFailOpenSeconds >= 30 && self.spec.jwksCacheFailOpenSeconds <= 600)`.
  Zero is treated as unset (mutating webhook applies default).
- `spec.auditArgumentsRedacted` is boolean (OpenAPI schema enforces; VAP noted for completeness).

**Mutating webhook (defaulting):**
- If `spec.jwksCacheFailOpenSeconds` unset (0) and `spec.dedicatedGateway == true` → default to 60.
- If `spec.jwksCacheFailOpenSeconds` unset (0) and `spec.dedicatedGateway == false` → default to 300.
- Envtest assertions: `tenant_fields_test.go` in `test/envtest/admission/` — three cases:
  admission-accept (valid range), admission-reject (out-of-range), webhook-default
  (unset field gets correct default for each dedicatedGateway value).

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
- **05a-envoy-ai-gateway-topology.md (iter-2, current):** `spec.jwksCacheFailOpenSeconds`
  field defined here; 05a iter-2 reads it in JWT Authn filter configuration. Semantics
  alignment settled.
- **05c-mcp-policy-enforcement.md (iter-1, current):** `spec.auditArgumentsRedacted`
  field defined here; 05c iter-1 references it in the audit log opt-in section. Semantics
  alignment settled.
- **10a-otel-topology.md (stub dep):** OTEL processor `keese-argument-redactor` topology
  is specified here but realized in 10a; treat 10a as a stub dependency until current.

## Refs

- [01-tenancy-capsule.md](01-tenancy-capsule.md) · [04a-openfga-authz-model.md](04a-openfga-authz-model.md)
- [04b-projected-sa-identity.md](04b-projected-sa-identity.md)
- [20a-api-group-layout.md](20a-api-group-layout.md) · [../plans/scaffolding-plan.md](../plans/scaffolding-plan.md)
- [../references/crd-design-checklist.md](../references/crd-design-checklist.md) · [../plans/rubric.md](../plans/rubric.md)
- [24b-tenant-crd.md](24b-tenant-crd.md)
