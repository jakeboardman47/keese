<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: tenancy
depends:
  - 25-cross-tenant-agreement.md
  - 04a-openfga-authz-model.md
  - 24-tenant-crd.md
related_skills: [crd-authoring]
status: current
last_verified: 2026-04-21
rollback: |
  See 25-cross-tenant-agreement.md rollback. Schema changes at v1alpha1
  require a migration plan at v1beta1 promotion (rule 04.2).
---

# 25-ii — CrossTenantAgreement: Spec Schema

Companion to [25-cross-tenant-agreement.md](25-cross-tenant-agreement.md).

## Full YAML schema sketch

```yaml
apiVersion: tenancy.operator.keese.ai/v1alpha1
kind: CrossTenantAgreement
metadata:
  name: <name>                  # cluster-scoped; no namespace
spec:
  from:
    # +keese:rebac-tuple=tenant.allows_messaging
    tenantRef:
      name: <tenant-name>       # must resolve to Tenant CR (webhook)
    workspaceSelector:
      matchLabels:
        keese.ai/role: agent    # label selector; snapshotted at Approved
  to:
    # +keese:rebac-tuple=tenant.allows_messaging
    tenantRef:
      name: <tenant-name>
    workspaceSelector:
      matchLabels:
        keese.ai/role: agent
  scope:
    # +keese:rebac-tuple=workspace.messageable_from
    natsSubjects:               # max 50; each must match keese.cta.* pattern (VAP)
      - "keese.cta.*.events"
    a2aRoles:                   # allowed values: reader | writer | bidirectional
      - bidirectional
  expiresAt: "2026-10-21T00:00:00Z"  # RFC3339; required; immutable after create (VAP)
status:
  observedGeneration: 0
  phase: Pending                # Pending | Approved | Rejected | Expired
  conditions:
    - type: Approved
      status: "False"
      reason: AwaitingApprovals
      lastTransitionTime: "..."
  approvals:
    - tenant: <tenant-name>
      approvedBy: <oidc-email-or-sa>
      approvedAt: "..."
      # +keese:rebac-tuple=tenant.can_approve_cra
      signature: <base64-cosign-sig-or-sa-token-sig>
      signatureType: oidc-keyless | sa-token
  workspaceSnapshot:
    - fromWorkspace: <ws-name>
      toWorkspace: <ws-name>
      snapshotAt: "..."
```

## Field-by-field constraints

| Field | Required | Immutable | VAP constraint |
|---|---|---|---|
| `spec.from.tenantRef.name` | Yes | Yes | Must resolve (webhook); `!= spec.to.tenantRef.name` (VAP) |
| `spec.from.workspaceSelector` | Yes | No | Valid K8s LabelSelector (OpenAPI) |
| `spec.to.tenantRef.name` | Yes | Yes | Must resolve (webhook) |
| `spec.to.workspaceSelector` | Yes | No | Valid K8s LabelSelector (OpenAPI) |
| `spec.scope.natsSubjects[]` | No | No | `len ≤ 50`; each matches `^keese\.cta\.` (CEL) |
| `spec.scope.a2aRoles[]` | No | No | Each ∈ `{reader, writer, bidirectional}` (CEL enum) |
| `spec.expiresAt` | Yes | Yes | RFC3339; `> now` on create (CEL: `self > timestamp(now)`) |
| `status.phase` | — | Controlled | Terminal phases immutable: `Approved → Expired` only (CEL) |
| `status.approvals[]` | — | Append-only | Controller-written; max 2 entries (one per tenant) |
| `status.workspaceSnapshot[]` | — | Immutable once set | Snapshotted at Approved; drift detected but not auto-updated |

## VAP CEL rules

Named: `crosstenanagreement-policy.tenancy.operator.keese.ai/v1alpha1`

```cel
# No self-agreement
self.spec.from.tenantRef.name != self.spec.to.tenantRef.name

# expiresAt immutable after create
!has(oldSelf.spec.expiresAt) || self.spec.expiresAt == oldSelf.spec.expiresAt

# tenantRef immutable after create
!has(oldSelf.spec.from.tenantRef) ||
  self.spec.from.tenantRef.name == oldSelf.spec.from.tenantRef.name

# natsSubjects prefix enforcement
self.spec.scope.natsSubjects.all(s, s.startsWith("keese.cta."))

# natsSubjects max 50
size(self.spec.scope.natsSubjects) <= 50

# a2aRoles enum
self.spec.scope.a2aRoles.all(r,
  r == "reader" || r == "writer" || r == "bidirectional")

# Phase transitions: Pending → Approved|Rejected; Approved → Expired; terminals immutable
has(oldSelf.status.phase) &&
(oldSelf.status.phase == "Rejected" || oldSelf.status.phase == "Expired") ?
  self.status.phase == oldSelf.status.phase : true
```

## OpenFGA model addition

New relation for `tenant` type (addition to `dev/bootstrap/openfga/model.fga`):

```fga
type tenant
  relations
    define admin: [user, service_account]
    define member: [service_account]
    define allows_messaging: [tenant]
    # D29: delegates CRA approval; defaults to admin via computed relation
    define can_approve_cra: [user, service_account] or admin
```

The `or admin` union means existing tenant admins can approve without any new tuple.
To restrict to a narrower set, write explicit `tenant:T#can_approve_cra@user:U` tuples.

## Samples

Two samples ship under `config/samples/tenancy/`:

**Minimal:**
```yaml
apiVersion: tenancy.operator.keese.ai/v1alpha1
kind: CrossTenantAgreement
metadata:
  name: cra-alpha-to-beta-minimal
spec:
  from:
    tenantRef: {name: alpha}
    workspaceSelector:
      matchLabels: {keese.ai/cra-eligible: "true"}
  to:
    tenantRef: {name: beta}
    workspaceSelector:
      matchLabels: {keese.ai/cra-eligible: "true"}
  expiresAt: "2026-12-31T23:59:59Z"
```

**Fully populated:**
```yaml
apiVersion: tenancy.operator.keese.ai/v1alpha1
kind: CrossTenantAgreement
metadata:
  name: cra-alpha-to-beta-full
spec:
  from:
    tenantRef: {name: alpha}
    workspaceSelector:
      matchLabels: {keese.ai/role: agent, keese.ai/tier: production}
  to:
    tenantRef: {name: beta}
    workspaceSelector:
      matchLabels: {keese.ai/role: agent, keese.ai/tier: production}
  scope:
    natsSubjects:
      - "keese.cta.*.events"
      - "keese.cta.*.results"
    a2aRoles:
      - bidirectional
  expiresAt: "2026-12-31T23:59:59Z"
```

Both samples pass `kubectl apply --dry-run=server` against envtest (rule 04.15).

## Cross-cuts

- **04a:** `tenant.can_approve_cra` relation added to `model.fga`. Written by operator
  bootstrap Job as `tenant:T#can_approve_cra@user:U` when an admin delegates; the
  computed union `or admin` covers the default case without any tuple.
- **24:** Tenant CR finalizer `finalizers.tenant.operator.keese.ai/agreements` blocks
  Tenant deletion while any owned CRA is `Approved`. CRA controller watches Tenant
  deletion events to transition affected CRAs to `Rejected` + delete tuples.
- **09:** `spec.scope.natsSubjects[]` narrows which `keese.cta.<uid>.*` sub-topics
  the agreement covers; 09 bridge enforces at subscribe time.
