<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: migration
depends: [scaffolding-plan.md, ../designs/24-tenant-crd.md, ../designs/01-tenancy-capsule.md]
related_skills: [plan-management]
status: draft
last_verified: 2026-04-20
---

# Migration — D23 → D26 (keese `Tenant` CRD)

> **Status: draft.** Skeleton. Flesh out during P8 with real deployment
> procedures once dev/staging clusters are running pre-D26 code.

## Context

D23 (original scaffolding) dropped the keese `Tenant` CRD in favor of
consuming Capsule's `Tenant` directly. D26 (2026-04-20) partially amended
D23 by adding **exactly one CRD back**: `tenancy.operator.keese.ai/v1alpha1/Tenant`
— a thin CRD that holds keese-specific tenancy config and delegates namespace
aggregation to Capsule (Mode B) or label selectors (Mode A).

This migration plan covers the operational procedure for a cluster running
pre-D26 keese (no keese `Tenant` CRD) to upgrade to post-D26 (keese `Tenant`
required for `tenant:X` ReBAC tuples).

## Pre-migration state (pre-D26)

- No keese `Tenant` CRD installed.
- OpenFGA `tenant:<name>` tuples exist, keyed by the string value of the
  `keese.ai/tenant=<name>` namespace label.
- `keese-tenant-admin` ClusterRoleBindings exist per-namespace, granting
  tenant-admin subjects.
- No `spec.tokenBudgetRef`, `spec.credentialPoolRef`, etc. — those settings
  live in ConfigMaps or are absent.

## Post-migration state (post-D26)

- `tenancy.operator.keese.ai/v1alpha1/Tenant` CRD installed.
- One `Tenant` CR per unique pre-existing `keese.ai/tenant=<name>` label
  value; `spec.namespaceSelector` matches the label.
- `spec.adminSubjects[]` populated from pre-existing RoleBindings.
- OpenFGA tuples **unchanged** — identity key is `Tenant.metadata.name`
  which equals the prior string-derived `<name>`.

## Procedure

### Step 1 — Install Tenant CRD

Via OLM CSV upgrade (post-D26 CSV ships the CRD in
`spec.customresourcedefinitions.owned`). CRD installation is idempotent;
no existing CRs yet.

### Step 2 — Run backfill Job

```sh
kubectl apply -f migrations/tenant-backfill.yaml
```

The Job (to author in P8):

1. Lists namespaces with `keese.ai/tenant` label; groups by value.
2. For each unique `<name>`, if no `Tenant` CR exists yet:
   - Creates `Tenant` with `spec.namespaceSelector.matchLabels[keese.ai/tenant]=<name>`.
   - Populates `spec.adminSubjects[]` from `keese-tenant-admin` RoleBindings
     observed across the tenant's namespaces.
   - Leaves `spec.capsuleTenantRef` empty unless a Capsule Tenant with the
     same name exists — in which case the Job sets the reference (auto-enters
     Mode B).
3. Idempotent — re-running is safe; skips existing Tenant CRs.

### Step 3 — Operator reconciles

Keese operator's Tenant reconciler observes the new CRs, populates
`status.namespaces[]` via the selector, and emits `TenantProvisioned` events.

### Step 4 — Verify

- `kubectl get tenant -o wide` shows one row per pre-existing tenant.
- For each tenant: `kubectl get workspace -n <ns>` still lists the same
  workspaces.
- `fga tuple list --type tenant` count is unchanged (no tuple re-writes).
- Agent egress through the Envoy AI Gateway continues to authorize.

## Rollback

See D26 `rollback:` field. Summary:

1. OLM image-version rollback to pre-D26 CSV.
2. `Tenant` CRD instances remain in etcd but are no longer reconciled
   (pre-D26 operator ignores them).
3. Re-enable pre-D26 code path — it still uses namespace labels as the
   tenant identity. Business continues.
4. To fully clean up: delete `Tenant` CRs (labels remain, so no tenant
   state is lost); then delete the CRD via `kubectl delete crd
   tenants.tenancy.operator.keese.ai`.

No OpenFGA tuple changes required in either direction — tuples are
name-derived by design.

## Verification checklist (fill in P8)

- [ ] Backfill Job authored (`migrations/tenant-backfill.yaml`).
- [ ] Backfill Job e2e test (`test/e2e/tenant_backfill_test.go`).
- [ ] OLM CSV diff captured for the pre-D26 → post-D26 upgrade path.
- [ ] Runbook cross-links to `docs/plans/runbook-model-migration.md` for
      any OpenFGA model changes that land in the same release.
- [ ] Rollback e2e test (`test/e2e/tenant_crd_rollback_test.go`).

## Refs

- [../designs/24-tenant-crd.md](../designs/24-tenant-crd.md)
- [../designs/24b-tenant-crd.md](../designs/24b-tenant-crd.md)
- [../designs/01-tenancy-capsule.md](../designs/01-tenancy-capsule.md)
- [../designs/04a-openfga-authz-model.md](../designs/04a-openfga-authz-model.md)
- [scaffolding-plan.md](scaffolding-plan.md) — D3, D23, D26
