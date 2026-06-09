<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: api
depends: [docs/plans/scaffolding-plan.md, docs/designs/01-tenancy-capsule.md]
related_skills: [crd-authoring, doc-authoring]
status: current
last_verified: 2026-06-08
rollback: Revert to prior commit; no migration plan required at v1alpha1 because no
  conversion webhooks exist yet. At v1beta1 promotion a migration plan in
  docs/plans/migration-<kind>.md is required before rollback of any group.
---

# 20a — API Group Layout: Groups, Kinds, Shared Types, Versioning

**Decision:** keese's kinds are split across **3 API groups**, all at
`v1alpha1`: `keese.ai` (core workload primitives), `authz.keese.ai` (access
control), and `policy.keese.ai` (quantitative constraints). Each group is a
self-contained Go package (`api/{keese,authz,policy}/v1alpha1`) with no
cross-group imports; the only shared types are intra-`keese.ai` helpers in
`api/keese/v1alpha1/common_types.go`. Promotion to `v1beta1` requires a rubric
score ≥ 90, a 90-day customer-production soak, and architect sign-off via a
migration plan doc. (D26, 2026-04-20: added `keese.ai/Tenant` — the one
cluster-scoped core kind. See `docs/designs/24-tenant-crd.md`.)

## Context

Keese is a secure multi-tenant K8s operator orchestrating autonomous AI agent
workflows on pluggable runtimes. Three API groups (`keese.ai`,
`authz.keese.ai`, `policy.keese.ai`) host all kinds, all at `v1alpha1`. Three
concerns drive this design:
(1) group boundaries that map cleanly to controller ownership and RBAC,
(2) a per-kind status convention (no shared base struct) that keeps
`observedGeneration` + `conditions` + a `phase` enum uniform across kinds, and
(3) a versioning policy that is conservative enough to avoid premature v1beta1
promotion and the conversion webhook overhead that comes with it.

## The 3 Groups and Their Kinds

The **Logical domain** column is a readability grouping only; at runtime every
row in the same API group shares one Go package (`api/<group>/v1alpha1`).

| Logical domain | API Group | Kinds | Go package path | Scope |
|---|---|---|---|---|
| tenancy | `keese.ai` | `Tenant` (D26) | `api/keese/v1alpha1` | cluster |
| workspace | `keese.ai` | `Workspace`, `WorkspaceShare`, `WorkspaceSession` (D27) | `api/keese/v1alpha1` | namespace |
| workflow | `keese.ai` | `Workflow`, `WorkflowRun` | `api/keese/v1alpha1` | namespace |
| runtime | `keese.ai` | `AgentRuntime`, `RuntimeExtension` | `api/keese/v1alpha1` | namespace |
| memory | `keese.ai` | `Memory`, `SharedMemory` | `api/keese/v1alpha1` | namespace |
| recipe | `keese.ai` | `Recipe`, `RecipeSource` | `api/keese/v1alpha1` | namespace |
| transport | `keese.ai` | `Transport` | `api/keese/v1alpha1` | namespace |
| guardrail | `authz.keese.ai` | `GuardrailBinding` | `api/authz/v1alpha1` | namespace |
| authz | `authz.keese.ai` | `OIDCProvider` (D28) | `api/authz/v1alpha1` | cluster |
| observability | `policy.keese.ai` | `TokenBudget` | `api/policy/v1alpha1` | namespace |

`Tenant` and `OIDCProvider` are cluster-scoped (tenants span namespaces; OIDC
providers are cluster-wide). All other kinds enumerated here are namespace-scoped.
Additional cluster-scoped kinds require an ADR in `docs/designs/`. (RAG kinds —
D28's `KnowledgeBase` family — also live in `keese.ai`; see
`docs/designs/28-rag-ingestion.md`.)

## Status Convention & Shared Types

There is **no shared "core" types package**. Each group package
(`api/{keese,authz,policy}/v1alpha1`) is self-contained and imports no other
group package — cross-group coordination happens in the controller layer, not in
the API types layer. Shared types are deliberately minimal and scoped to a single
group:

| Type | Package | Purpose |
|---|---|---|
| `LocalObjectReference` (`{ Name string }`) | `api/keese/v1alpha1` (`common_types.go`) | Same-namespace, name-only refs reused across `keese.ai` kinds. |
| `ConcurrencyPolicy` (`Allow` / `Forbid` / `Replace`) | `api/keese/v1alpha1` (`common_types.go`) | Workflow concurrency vocabulary. |

**Status pattern.** Every kind's `*Status` declares its fields inline — no
shared base struct:

- `ObservedGeneration int64` (rule 04.4),
- `Conditions []metav1.Condition` (`patchStrategy:"merge"`, list-map keyed on `type`),
- `Phase <Kind>Phase` — a per-kind string enum (below).

ReBAC tuples are not a Go type: authz-affecting fields carry a
`// +keese:rebac-tuple=<relation>` marker (rule 04.14), enforced by
`scripts/check-rebac-markers.sh`.

### Phase Enums (per-kind)

There is no canonical cross-kind `Phase` type. Each kind defines its own phase
enum sized to its lifecycle, with a `+kubebuilder:validation:Enum` marker for
admission-time enforcement.

Example — `Workspace` (`api/keese/v1alpha1/workspace_types.go`):

```go
// +kubebuilder:validation:Enum=Pending;Provisioning;Running;Idle;Evicted;Terminating
type WorkspacePhase string

type WorkspaceStatus struct {
    ObservedGeneration int64              `json:"observedGeneration,omitempty"`
    Phase              WorkspacePhase     `json:"phase,omitempty"`
    Conditions         []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
    // … kind-specific fields
}
```

Other kinds follow the same shape with their own enum — e.g. `BindingPhase`
(`GuardrailBinding`), `TokenBudgetPhase` (`TokenBudget`). Phase vocabularies are
reviewed per-kind in the owning spec.

### Status testing

Per rule 04.16, every reconciler ships a `suite_test.go` that loads CRDs from
`config/crd/bases/` and asserts reconcile idempotency over ≥ 3 passes with no
spec change. Invalid-phase rejection is covered by each kind's own envtest.
There is no shared-types test suite — there is no shared-types package.

## Versioning and Promotion Policy (v1alpha1 → v1beta1)

**Decision:** A group promotes from `v1alpha1` to `v1beta1` only when all four
gates are cleared:

| Gate | Criterion |
|---|---|
| **Rubric score** | Owning spec doc scores ≥ 90/100 on its iteration-3 pass. |
| **Soak time** | ≥ 90 calendar days starting at the earliest timestamp a customer (external, not keese-authored CI/e2e) runs the group at `v1alpha1` against a production cluster. The architect sign-off migration plan must cite the customer deployment event by its Elastic APM trace ID or a release-notes entry. |
| **Architect sign-off** | An architect-signed commit adds `docs/plans/migration-<group>.md` scoring ≥ 90. |
| **Conversion webhook** | A Hub-spoke conversion webhook is implemented and covered by envtest round-trip tests before the `v1beta1` CRD ships. |

No group promotes before its kinds are deployed at `v1alpha1` and the P8 design
gate is open.

At `v1alpha1` there are **no conversion webhooks** (rule 04.13). The only admission
webhooks at `v1alpha1` are mutating (defaulting) and validating (cross-resource
checks where CEL is insufficient — see D16).

## Printer Columns Required Per Kind

Every kind ships at minimum: `Age` (`.metadata.creationTimestamp`), `Ready`
(derived from `.status.conditions[?(@.type=="Ready")].status`), `Phase`
(`.status.phase`), plus at minimum one domain column:

| Group | Domain column(s) |
|---|---|
| workspace | `Runtime` (`.spec.runtimeRef.name`) |
| workflow | `RunCount` (`.status.runCount`) |
| runtime | `Provider` (`.spec.provider`) |
| memory | `Backend` (`.spec.provider.type`) |
| recipe | `Source` (`.spec.sourceRef.name`) |
| guardrail | `Scope` (`.spec.scope`) |
| observability | `Budget` (`.spec.limitTokens`) |
| transport | `Type` (`.spec.type`) |

Printer column JSONPath validation policy and enforcement hooks are specified in
[20b-api-group-layout.md](20b-api-group-layout.md) § Printer Column Validation.

## Per-Group RBAC Summary

The `docs/designs/01-tenancy-capsule.md` scaffold is the authoritative source;
this table is a pointer. Nine ClusterRoles + 2 aggregate roles introduced in
design 01 map to groups as follows:

| Group | Primary ClusterRole(s) |
|---|---|
| workspace | `keese-workspace-viewer`, `keese-workspace-editor` |
| workflow | `keese-runtime-invoker` (for WorkflowRun create) |
| runtime | `keese-runtime-admin` |
| memory | `keese-memory-admin` |
| recipe | `keese-recipe-publisher` |
| guardrail | `keese-guardrail-author` |
| observability | `keese-observability-viewer` |
| transport | `keese-transport-admin` |

RBAC markers on every reconciler enumerate exact verbs and resources per rule
04.9. Capsule `additionalRoleBindings` injects the workspace and runtime-invoker
roles into every tenant namespace automatically (D-01.2).

## PROJECT Encoding and New-Kind Policy

The PROJECT file uses `multigroup: true`, `domain: keese.ai`. Core kinds use an
empty `--group=""` (group `keese.ai`); access-control kinds use `--group=authz`
(`authz.keese.ai`) and quantitative-constraint kinds use `--group=policy`
(`policy.keese.ai`). Each of the 3 group packages carries its own SchemeBuilder;
the shared helpers in `common_types.go` live inside `api/keese/v1alpha1` (there is
no separate shared package).

After a group's `v1alpha1` is published, new kinds are added at the same version
(`operator-sdk create api --group=<g> --version=v1alpha1 --kind=<K>`). API
versions attach to kinds, not groups. New kinds must pass: CRD design checklist,
`make manifests generate` with no drift, ≥ 2 samples passing
`kubectl apply --dry-run=server`, and a new row in the owning design doc.

## Refs

- [../plans/scaffolding-plan.md](../plans/scaffolding-plan.md) — D2, D16, D23
- [../designs/01-tenancy-capsule.md](../designs/01-tenancy-capsule.md) — RBAC scaffold
- [20b-api-group-layout.md](20b-api-group-layout.md) — trade-offs, failure modes,
  upgrade/rollback, observability, iteration log
- [../references/crd-design-checklist.md](../references/crd-design-checklist.md)
- [../../PROJECT](../../PROJECT) — live multigroup layout
- [../plans/rubric.md](../plans/rubric.md)
