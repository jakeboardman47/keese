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
status: current
last_verified: 2026-04-21
regression_lock: false
tests:
  unit:
    - internal/controller/tenancy/tenant/suite_test.go
    - internal/controller/tenancy/tenant/reconcile_test.go
    - internal/controller/tenancy/tenant/admission_test.go
    - test/envtest/admission/tenant_fields_test.go
    - internal/controller/tenancy/crosstenanagreement/suite_test.go
    - internal/controller/tenancy/crosstenanagreement/approval_test.go
    - internal/controller/tenancy/crosstenanagreement/tuplesync_test.go
    - internal/controller/tenancy/crosstenanagreement/expiry_test.go
    - internal/controller/tenancy/crosstenanagreement/conflict_test.go
  envtest:
    - internal/controller/tenancy/tenant/suite_test.go
    - internal/controller/tenancy/crosstenanagreement/suite_test.go
    - test/envtest/admission/tenant_fields_test.go
  kuttl:
    - test/kuttl/tenancy/tenant-mode-switch/
    - test/kuttl/tenancy/cra-bilateral-approval/
    - test/kuttl/tenancy/cra-expiry/
    - test/kuttl/tenancy/tenant-finalizer-cascade/
metrics:
  - keese_tenant_reconcile_duration_seconds{mode,phase}
  - keese_tenant_namespace_count{tenant,mode}
  - keese_tenant_capsule_sync_errors_total{tenant}
  - keese_tenant_deletion_blocked_total{tenant}
  - keese_envoy_jwks_cache_fail_open_seconds_remaining{tenant}
  - keese_cta_phase_total{phase}
  - keese_cta_approval_latency_seconds
  - keese_cta_tuple_sync_duration_seconds
events:
  - TenantProvisioned
  - NamespaceAdded
  - NamespaceRemoved
  - TenantLabelLocked
  - CapsuleTenantNotFound
  - RefNotResolved
  - TenantDeletionBlocked
  - SelectorOverlapDenied
  - NamespaceSelectorIgnoredInModeB
  - JWKSCacheExhausted
  - AuditRedactionUnavailable
  - CRAApproved
  - CRAExpired
  - CRARejected
  - OutOfBandTupleObserved
  - TupleSyncFailed
  - WorkspaceSnapshotDrift
  - CRAApprovalInvalid
  - SignatureVerificationFailed
  - CRAConflict
---

# tenancy.operator.keese.ai v1alpha1 — spec

Group: `tenancy.operator.keese.ai` · Version: `v1alpha1` · Both kinds cluster-scoped.

Companion docs:
- [`tenancy.operator.keese.ai-v1alpha1-ii-tenant.md`](tenancy.operator.keese.ai-v1alpha1-ii-tenant.md) — Tenant CRD detail + acceptance tests
- [`tenancy.operator.keese.ai-v1alpha1-ii-cra.md`](tenancy.operator.keese.ai-v1alpha1-ii-cra.md) — CrossTenantAgreement CRD detail + acceptance tests
- [`tenancy.operator.keese.ai-v1alpha1-iter-log.md`](tenancy.operator.keese.ai-v1alpha1-iter-log.md) — Rubric iteration log

## Owning designs (all `status: current`)

| Design | Decision |
|---|---|
| [`24-tenant-crd.md`](../designs/24-tenant-crd.md) | D26 Tenant CRD: spec, reconcile, admission, migration |
| [`24b-tenant-crd.md`](../designs/24b-tenant-crd.md) | D26 trade-offs, failure modes, upgrade, observability |
| [`25-cross-tenant-agreement.md`](../designs/25-cross-tenant-agreement.md) | D29 CRA decision + approval model |
| [`25-ii-spec-schema.md`](../designs/25-ii-spec-schema.md) | D29 full YAML schema + VAP CEL |
| [`25-iii-approval-flow.md`](../designs/25-iii-approval-flow.md) | D29 approval flow + 10-row failure modes |
| [`04a-openfga-authz-model.md`](../designs/04a-openfga-authz-model.md) | `tenant.allows_messaging`, `workspace.messageable_from`, `tenant.can_approve_cra` (iter-5) |
| [`01-tenancy-capsule.md`](../designs/01-tenancy-capsule.md) | Mode A vs Mode B switching |

## Kinds summary

### Tenant (D26)

Cluster-scoped. Identity key for OpenFGA: `Tenant.metadata.name`.
SSA fieldOwner: `keese-tenant-controller`.
Finalizers: `finalizers.tenant.operator.keese.ai/workspaces`,
`finalizers.tenant.operator.keese.ai/namespaces` (Mode A, namespace-level),
`finalizers.tenant.operator.keese.ai/agreements` (blocks deletion while Approved CRA exists).

Printer columns: `Age`, `Ready`, `Phase`, `Mode` (`ModeA`|`ModeB`), `Namespaces` (count).

Status FSM: `Pending → Provisioning → Active → Suspended → Terminating`.

Full CRD schema, VAP CEL invariants, Mode A/B reconcile, admission webhooks,
samples, acceptance tests: [tenancy.operator.keese.ai-v1alpha1-ii-tenant.md](tenancy.operator.keese.ai-v1alpha1-ii-tenant.md).

### CrossTenantAgreement (D29)

Cluster-scoped. Identity key: `CrossTenantAgreement.metadata.name`.
SSA fieldOwner: `keese-crosstenanagreement-controller`.
Finalizer: `finalizers.crosstenanagreement.operator.keese.ai/nats`.

Printer columns: `Age`, `Ready`, `Phase`, `From`, `To`, `ExpiresAt`.

Status phases: `Pending → Approved|Rejected`; `Approved → Expired`; terminals immutable.

Full CRD schema, approval handshake, tuple sync, expiry, conflict detection,
samples, acceptance tests: [tenancy.operator.keese.ai-v1alpha1-ii-cra.md](tenancy.operator.keese.ai-v1alpha1-ii-cra.md).

## Security invariants

- Tenant `metadata.name` is the OpenFGA identity key (not `.uid`); stable across
  delete+recreate; avoids full tuple backfill on typo-fix cycles (04a iter-5).
- ReBAC tuples for cross-tenant messaging written ONLY after bilateral approval
  with valid signatures; OpenFGA Check gates the annotation write (rule 05 + 04.14).
- No kubeconfig or upstream API key in agent pods; projected SA token is the only
  credential (rule 05.1–05.3).
- VAP-first for static invariants (rule 04.12); webhook only for cross-resource checks.
- All controller writes via SSA; no direct `client-go` (rule 04.7).

## Threat model cross-cut

Covered by [`05-security-zero-trust.md`](../.claude/../../.claude/rules/05-security-zero-trust.md):

- Tenant namespace isolation: fail-closed NetworkPolicy per workspace (rule 05.4–05.5).
- Cross-tenant tuple blast radius bounded by CRA workspace snapshot (TOFU).
- Signature forgery on approval: webhook rejects invalid cosign/SA-token signature;
  `SignatureVerificationFailed` event; no tuple written.
- Tuple injection via `kubectl`: `OutOfBandTupleObserved` on pre-existing tuples;
  controller no-ops; alert surfaced.
