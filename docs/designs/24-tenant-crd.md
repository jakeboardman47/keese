<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: tenancy
depends: [01-tenancy-capsule.md, 04a-openfga-authz-model.md, 20a-api-group-layout.md]
related_skills: [crd-authoring]
status: draft
last_verified: 2026-04-20
rollback: TODO — document migration path when status flips to current
---

# 24 — Keese `Tenant` CRD

> **Status: draft.** Gate-placeholder. Architect iterates across 3 rubric passes
> (target ≥ 90/100) before any spec or controller code references this design.

## Context

_One paragraph. D26 (2026-04-20) added a keese `Tenant` CRD to hold keese-specific
tenancy config that previously had no canonical home: guardrail defaults, token
budget references, credential pool references, default workspace quota. Keese's
`Tenant` does **not** reimplement namespace aggregation — Capsule owns that in
Mode B, or it's derived from namespace label selectors in Mode A. The CRD is
cluster-scoped; its primary purpose is (1) to give ReBAC `tenant:X` a K8s-object
backing with finalizers, events, and status, and (2) to aggregate keese-specific
tenant settings in one place. Amendment to D23 — the only keese CRD we add after
the compose-over-replicate cut._

## Open questions (must be answered before `status: current`)

1. **Spec schema — minimum viable fields.** Which of the following land in v1alpha1:
   `guardrailBindings[]` refs, `tokenBudgetRef`, `credentialPoolRef` (forward-
   compat for credential pooling per tier-2 gaps), `defaultWorkspaceQuota`
   (ResourceList), `capsuleTenantRef` (Mode B only), `namespaceSelector`
   (Mode A), `adminSubjects[]` (list of users/groups who get
   `keese-tenant-admin` bound), `status.namespaces[]` (observed list)?
2. **Mode A reconcile: namespace aggregation via label.** Controller watches
   namespaces with label `keese.ai/tenant=<name>`; populates
   `Tenant.status.namespaces[]`. What happens when the label is removed from a
   namespace that has live Workspaces? Fail-closed (block delete of last tenant
   ref), or cascade (orphan warning)?
3. **Mode B reconcile: delegate to Capsule.** When `capsuleTenantRef` is set,
   controller reads the Capsule Tenant's namespace list. Who owns conflict
   resolution if the keese `Tenant.spec.namespaceSelector` disagrees with Capsule's
   `namespaceOptions.additionalMetadataLabels`?
4. **OwnerRef chain.** Which keese objects carry an OwnerRef pointing at
   `Tenant` — Workspace? TokenBudget? GuardrailBinding? Or is the relationship
   strictly label-based (`keese.ai/tenant=<name>`) to avoid cascading deletes
   wiping tenant data on tenant removal?
5. **Admission invariants (VAP + webhook).** Unique name; `capsuleTenantRef`
   must resolve to an existing Capsule Tenant when set; `namespaceSelector`
   must not overlap another keese Tenant's namespaces; `adminSubjects[]`
   non-empty. Which of these are VAP (CEL) vs. webhook (cross-resource
   lookup)?
6. **Migration from "no keese Tenant" (iter-1 01) to "keese Tenant required"
   (post-D26).** Existing Workspaces in Mode A have no Tenant object. Migration
   Job creates one `Tenant` per unique `keese.ai/tenant=<name>` label; existing
   OpenFGA `tenant:X` tuples are preserved. Can this run non-destructively?

## Refs

- [../plans/rubric.md](../plans/rubric.md)
- [../plans/scaffolding-plan.md](../plans/scaffolding-plan.md) — D3, D23, D26
- [01-tenancy-capsule.md](01-tenancy-capsule.md) — Mode A/B definitions; Capsule integration
- [04a-openfga-authz-model.md](04a-openfga-authz-model.md) — `tenant` ReBAC type consumes this CRD
- [20a-api-group-layout.md](20a-api-group-layout.md) — 9-group layout incl. `tenancy.operator.keese.ai`
- [../references/crd-design-checklist.md](../references/crd-design-checklist.md)

TODO(design-gate)
