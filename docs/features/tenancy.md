<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: feature
category: feature
depends:
  - docs/designs/24-tenant-crd.md
  - docs/designs/01-tenancy-capsule.md
  - docs/designs/25-cross-tenant-agreement.md
implements_specs:
  - docs/specs/keese.ai-v1alpha1-tenancy.md
  - docs/specs/keese.ai-v1alpha1-tenancy-ii-cra.md
implements_plans:
  - docs/plans/demo/tech-debt.md
source_refs:
  - api/keese/v1alpha1/tenant_types.go:1-232
  - api/authz/v1alpha1/crosstenanagreement_types.go:1-205
  - internal/controller/keese/tenant_controller.go:1-541
  - internal/controller/keese/tenancy_rebac.go:1-75
  - internal/controller/keese/tenancy_events.go:1-51
  - internal/controller/authz/crosstenanagreement_controller.go:1-531
  - internal/controller/authz/crosstenanagreement_events.go:1-48
related_skills: [controller-authoring]
status: implemented
implemented_in_phase: demo-D1
last_verified: 2026-05-29
---

# Tenancy & CrossTenantAgreement

## Summary

`Tenant` is the cluster-scoped isolation boundary for all keese workloads. Each Tenant
aggregates namespaces either by label selector (Mode A) or by delegating to an existing
Capsule `capsule.clastix.io/v1beta2/Tenant` (Mode B). The controller governs the namespace
list, writes OpenFGA ReBAC tuples for admin subjects and OIDC providers, and gates deletion
behind three finalizers that prevent removal while Workspaces, Capsule-managed namespaces,
or active CrossTenantAgreements remain. `CrossTenantAgreement` (CRA) is a bilateral,
cert-manager-style handshake: once both tenants approve, the controller freezes a workspace
snapshot, provisions shared NATS subjects via JetStream, and writes `messageable_from` ReBAC
tuples enabling controlled A2A communication between tenants.

## Behavior

**Tenant**

- Created cluster-scoped (`shortName: tenant`); its `metadata.name` is the OpenFGA identity key.
- Phases: `Pending` → `Provisioning` → `Active`; `Suspended` and `Terminating` on deletion.
- **Mode A** (`spec.namespaceSelector`): controller lists namespaces by label selector, emits
  `NamespaceAdded` / `NamespaceRemoved` events on membership changes, and removes the
  `keese.ai/tenant` label from tracked namespaces on deletion.
- **Mode B** (`spec.capsuleTenantRef`): controller fetches the named Capsule Tenant and mirrors
  its `status.namespaces[]`; requeues on 5 s backoff if absent; emits `CapsuleTenantNotFound`
  warning. A Capsule Tenant watch triggers re-aggregation of all referencing keese Tenants.
- If both `capsuleTenantRef` and `namespaceSelector` are set, Mode B wins and a
  `NamespaceSelectorIgnoredInModeB` warning event is emitted.
- On each reconcile, admin and OIDC-provider OpenFGA tuples are synced via `TenantRebacWriter`
  (production: `TenantOpenFGARebacWriter`; dev without OpenFGA: `TenantNoopRebacWriter`).
- Deletion is blocked until all three finalizers clear in order:
  `finalizers.tenant.keese.ai/agreements` (no Approved CRAs reference the tenant) →
  `finalizers.tenant.keese.ai/workspaces` (no Workspace claims the tenant) →
  `finalizers.tenant.keese.ai/namespaces` (Mode A label cleanup). ReBAC tuples are deleted last.

**CrossTenantAgreement**

- Created cluster-scoped (`shortName: cra`); `spec.from` and `spec.to` name two distinct tenants
  (CRD `XValidation`-enforced). `spec.expiresAt` (RFC3339) is immutable after creation.
- Phases: `Pending` → `Approved` | `Rejected` | `Expired`.
- Approval is driven by annotation `keese.ai/cra-approve: "true"` plus four companion
  annotations: `cra-approving-tenant` (tenant name), `cra-approver` (OIDC email or SA),
  `cra-signature` (base64 payload), and `cra-signature-type` (signature scheme selector).
  `cra-signature-type: oidc-keyless` routes verification through cosign keyless OIDC
  (`CosignVerifier`); any other value — or omitting the annotation — defaults to `sa-token`,
  which verifies an HMAC signed with the projected SA token for audience
  `keese-egress-<approvingTenant>` (`SATokenVerifier`). The controller removes all five
  annotations via a metadata patch after processing.
- When both tenants have approved, the controller freezes a `WorkspaceSnapshot` (TOFU) and writes
  `tenant.allows_messaging` and `workspace.messageable_from` OpenFGA tuples.
- Conflict detection at `Pending` (no approvals yet): if an existing Approved CRA already covers
  the same tenant pair, the new CRA immediately transitions to `Rejected`.
- Snapshot drift: on each reconcile of an `Approved` CRA, current selector results are compared
  to the frozen snapshot; a `WorkspaceSnapshotDrift` warning event is emitted on divergence but
  coverage is NOT auto-extended (create a new CRA to extend).
- On expiry the controller deletes the synced ReBAC tuples and transitions to `Expired`.
- Finalizer `finalizers.crosstenanagreement.keese.ai/nats` triggers NATS stream deletion (via
  `NatsStreamDeleter`) on CR deletion.

## Configuration surface

Key `Tenant` spec fields — see `api/keese/v1alpha1/tenant_types.go:96-167`:

| Field | Purpose |
|---|---|
| `spec.capsuleTenantRef.name` | Mode B delegation to Capsule (immutable while namespaces live) |
| `spec.namespaceSelector` | Mode A label-based namespace aggregation |
| `spec.adminSubjects[]` | Users/groups/SAs with `admin` relation in OpenFGA (min 1) |
| `spec.oidc.allowedProviders[]` | OIDCProvider names accepted for this tenant |
| `spec.tokenBudgetRef` | Aggregate spend limit (cross-namespace ref) |
| `spec.defaultGuardrailBindings[]` | GuardrailBinding names inherited by workspaces |
| `spec.dedicatedGateway` | Provision a per-tenant Envoy AI Gateway instance |
| `spec.jwksCacheFailOpenSeconds` | Gateway JWKS fail-open window [30,600]; 0 = default |
| `spec.auditArgumentsRedacted` | Redact agent call arguments in audit logs |

Key `CrossTenantAgreement` spec fields — see `api/authz/v1alpha1/crosstenanagreement_types.go:116-138`:

| Field | Purpose |
|---|---|
| `spec.from` / `spec.to` | Participating tenant + workspace selector (from.tenantRef immutable) |
| `spec.scope.natsSubjects[]` | NATS subjects (prefix `keese.cta.`; max 50) |
| `spec.scope.a2aRoles[]` | A2A roles: `reader`, `writer`, `bidirectional` |
| `spec.expiresAt` | RFC3339 expiry; CRA auto-transitions to `Expired` at this time |

## Observability

**Tenant events** (from `internal/controller/keese/tenancy_events.go`):

| Reason | Type | Trigger |
|---|---|---|
| `TenantProvisioned` | Normal | Tenant transitions to Active |
| `NamespaceAdded` / `NamespaceRemoved` | Normal | Namespace membership changes |
| `CapsuleTenantNotFound` | Warning | Mode B ref resolution failure |
| `TenantDeletionBlocked` | Warning | Finalizer gate active (workspaces, agreements) |
| `NamespaceSelectorIgnoredInModeB` | Warning | Both capsuleTenantRef + namespaceSelector set |
| `RebacTupleWritten` | Normal | OpenFGA tuple sync succeeds |
| `RebacTupleDeleteFailed` | Warning | OpenFGA tuple deletion fails during cleanup |
| `JWKSCacheExhausted` | Warning | Fail-open window expires at gateway |

**Tenant status conditions**: `Ready`, `Progressing`, `CapsuleTenantResolved` (Mode B).
**Tenant printer columns**: `Age`, `Ready`, `Phase`, `Namespaces`.

**CRA events** (from `internal/controller/authz/crosstenanagreement_events.go`):

| Reason | Type | Trigger |
|---|---|---|
| `CRAApproved` | Normal | Both tenants approved; snapshot frozen |
| `CRAExpired` | Normal | `expiresAt` passed; tuples deleted |
| `CRARejected` | Normal | CRA rejected (conflict) |
| `WorkspaceSnapshotDrift` | Warning | Snapshot diverges from live selector |
| `CRAApprovalInvalid` | Warning | Annotation validation or permission failure |
| `SignatureVerificationFailed` | Warning | cosign / SA-token HMAC error |
| `TupleSyncFailed` | Warning | OpenFGA sync error at approval or expiry |
| `CRAConflict` | Warning | Duplicate Approved CRA for same tenant pair |
| `NATSStreamDeleted` | Normal | NATS stream cleaned up on CRA deletion |
| `NATSStreamDeleteFailed` | Warning | NATS stream cleanup error |

**CRA status conditions**: `Ready` (True = Approved, False = Rejected/Expired/Pending).
**CRA printer columns**: `Age`, `Ready`, `Phase`, `From`, `To`.

## Known limitations

- `resolveWorkspaces` in the CRA controller returns a synthetic placeholder name
  (`ws-<tenantName>`) rather than listing real Workspace CRs by `tenantRef`; the actual
  Workspace listing is a TODO gated by an import-cycle barrier between the `authz` and `keese`
  controller packages (`crosstenanagreement_controller.go:478-483`). Cross-tenant workspace
  snapshots are therefore not yet computed from real CRs.
- The admission webhook validating `can_approve_cra` permission at annotation-write time is
  stubbed; permission enforcement relies solely on the controller's runtime check against
  `status.Approvals`.
- `NatsStreamDeleter` and signature verifiers (`CosignVerifier`, `SATokenVerifier`) default to
  fake implementations in non-production builds; real wiring requires NATS and cosign
  infrastructure to be configured before startup.
- `dedicatedGateway` field is declared but gateway provisioning logic is not yet implemented.
- CrossTenantAgreement happy-path e2e smoke requires a live OpenFGA instance; the multi-tenant
  kuttl suite defers CRA verification for this reason (`docs/plans/demo/tech-debt.md TD-P3-07`).

## Change history

- `demo-D1` / `TD-P2-06` (closed 2026-05-07): Tenant Mode B Capsule lookup wired from stub to
  real `capsulev1beta2.Tenant` fetch; Capsule Tenant watch added; CRA controller and tenancy
  ReBAC wiring completed.

## References

- Design: `docs/designs/24-tenant-crd.md`, `docs/designs/01-tenancy-capsule.md`,
  `docs/designs/25-cross-tenant-agreement.md`
- Spec: `docs/specs/keese.ai-v1alpha1-tenancy.md`,
  `docs/specs/keese.ai-v1alpha1-tenancy-ii-cra.md`
- Plan: `docs/plans/demo/tech-debt.md`
- Source: `internal/controller/keese/tenant_controller.go`,
  `internal/controller/authz/crosstenanagreement_controller.go`,
  `internal/controller/keese/tenancy_rebac.go`
