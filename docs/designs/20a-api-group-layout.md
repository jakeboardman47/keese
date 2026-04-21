<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: api
depends: [docs/plans/scaffolding-plan.md, docs/designs/01-tenancy-capsule.md]
related_skills: [crd-authoring, doc-authoring]
status: current
last_verified: 2026-04-20
rollback: Revert to prior commit; no migration plan required at v1alpha1 because no
  conversion webhooks exist yet. At v1beta1 promotion a migration plan in
  docs/plans/migration-<kind>.md is required before rollback of any group.
---

# 20a — API Group Layout: Groups, Kinds, Shared Types, Versioning

**Decision:** 16 kinds across 10 sub-groups all under `operator.keese.ai`,
all at `v1alpha1`. A shared-types package at
`github.com/keese-ai/keese/api/core/v1alpha1` holds cross-group primitives.
Promotion to `v1beta1` requires a rubric score ≥ 90, 90-day customer-production
soak, and architect sign-off via a migration plan doc. (D26, 2026-04-20: added
`tenancy.operator.keese.ai/Tenant` — the one cluster-scoped kind. See
`docs/designs/24-tenant-crd.md`.)

## Context

Keese is a secure multi-tenant K8s operator orchestrating autonomous AI agent
workflows on pluggable runtimes. Nine API groups under `*.operator.keese.ai`
host 16 kinds, all at `v1alpha1`. Three concerns drive this design:
(1) group boundaries that map cleanly to controller ownership and RBAC,
(2) a shared-types package that prevents duplication of conditions and status
patterns while keeping cross-group imports unidirectional, and
(3) a versioning policy that is conservative enough to avoid premature v1beta1
promotion and the conversion webhook overhead that comes with it.

## The 10 Groups and 16 Kinds

| Group | Full API Group | Kinds | Go package path | Scope |
|---|---|---|---|---|
| tenancy | `tenancy.operator.keese.ai` | `Tenant` (D26) | `api/tenancy/v1alpha1` | cluster |
| workspace | `workspace.operator.keese.ai` | `Workspace`, `WorkspaceShare`, `WorkspaceSession` (D27) | `api/workspace/v1alpha1` | namespace |
| workflow | `workflow.operator.keese.ai` | `Workflow`, `WorkflowRun` | `api/workflow/v1alpha1` | namespace |
| runtime | `runtime.operator.keese.ai` | `AgentRuntime`, `RuntimeExtension` | `api/runtime/v1alpha1` | namespace |
| memory | `memory.operator.keese.ai` | `Memory`, `SharedMemory` | `api/memory/v1alpha1` | namespace |
| recipe | `recipe.operator.keese.ai` | `Recipe`, `RecipeSource` | `api/recipe/v1alpha1` | namespace |
| guardrail | `guardrail.operator.keese.ai` | `GuardrailBinding` | `api/guardrail/v1alpha1` | namespace |
| observability | `observability.operator.keese.ai` | `TokenBudget` | `api/observability/v1alpha1` | namespace |
| transport | `transport.operator.keese.ai` | `Transport` | `api/transport/v1alpha1` | namespace |
| authz | `authz.operator.keese.ai` | `OIDCProvider` (D28) | `api/authz/v1alpha1` | cluster |

`Tenant` and `OIDCProvider` are cluster-scoped (tenants span namespaces; OIDC
providers are cluster-wide). All other 14 kinds are namespace-scoped. Additional
cluster-scoped kinds require an ADR in `docs/designs/`.

## Shared-Types Package Layout

**Decision:** A shared package at `github.com/keese-ai/keese/api/core/v1alpha1`
holds cross-group primitives. All ten group packages import it; it imports nothing
from the group packages (unidirectional).

The package name `core` is intentional. It does not collide with `k8s.io/api/core/v1`
because the Go import path is fully qualified; callers use a distinct alias, e.g.
`keesecore "github.com/keese-ai/keese/api/core/v1alpha1"`, to eliminate ambiguity.

| Type | Rationale |
|---|---|
| `Condition` (re-export wrapping `metav1.Condition`) | Uniform condition type/reason/message vocabulary across all 16 kinds. |
| `Phase` (string type + 5 consts: `PhasePending`, `PhaseProvisioning`, `PhaseReady`, `PhaseDegraded`, `PhaseTerminating`) | Shared phase vocabulary; kind-specific extensions declared as additional consts in the kind's own `_types.go`. |
| `ResourceRef` (`{ Name, Namespace, Group, Kind string }`) | Cross-group references (e.g., `Workspace.spec.runtimeRef`, `GuardrailBinding.spec.workspaceRef`). |
| `StatusBase` (`ObservedGeneration int64`, `Conditions []metav1.Condition`, `Phase Phase`) | Embedded in every kind's `*Status` struct — enforces rule 04.4 (`observedGeneration` on every status). |
| `ReBAC` marker type alias (string constant) | Anchor for `// +keese:rebac-tuple=<relation>` markers; not a Go type used at runtime, but keeps the constant definition canonical. |

Import rule: group packages may import `api/core/v1alpha1`. No group package
imports another group package directly. Cross-group coordination goes through
the controller layer, not the API types layer.

### Phase Enum Strategy — Option C (Hybrid)

`api/core/v1alpha1` defines the canonical `Phase` type and 5 consts. Each kind's
`_types.go` **also** declares a `+kubebuilder:validation:Enum` marker on the
`StatusBase.Phase` field listing the 5 core values plus any kind-specific
extensions. This gives admission-time enforcement that the bare string type alone
cannot provide.

Example for `Workspace`:

```go
// WorkspaceStatus defines the observed state of Workspace.
type WorkspaceStatus struct {
    keesecore.StatusBase `json:",inline"`

    // Phase is the current lifecycle phase of the Workspace.
    // +kubebuilder:validation:Enum=Pending;Provisioning;Ready;Degraded;Terminating;Idle;Evicting
    Phase keesecore.Phase `json:"phase,omitempty"`
}
```

The `StatusBase` embedding provides `ObservedGeneration` and `Conditions`; the
per-kind `Phase` field re-declares the field to attach the enum marker. For kinds
with no extensions the enum lists the 5 core values only.

Pre-commit hook `scripts/check-phase-enum-drift.sh` (P3) diffs every kind's enum
marker against the 5 core const values and fails if a kind's marker omits any
core value. Hook is not implemented until the design gate opens.

### Shared-Type Envtest Assertions

| Test name | Assertion |
|---|---|
| `TestCoreCondition_RoundTrip` | `core.Condition` survives serialize/deserialize with JSON and YAML without field loss. |
| `TestCorePhase_EnumValidation` | For each of the 16 kinds, creates a CR with each core `Phase` value and asserts admission accepts; creates a CR with an invalid phase (e.g., `"Exploding"`) and asserts admission rejects. |
| `TestCoreResourceRef_ValidateCrossGroup` | Asserts `core.ResourceRef` with `Group == ""` is rejected by the Workspace VAP; group must be specified for cross-group refs. |
| `TestCoreStatusBase_ObservedGenerationMonotonic` | Asserts `StatusBase.ObservedGeneration` is never set to a value less than `metadata.generation` across any reconcile of any of the 13 controllers. |

These tests live in `internal/controller/suite_test.go` (one file per group) and
run against an envtest API server with CRDs loaded from `config/crd/bases/`.

## Versioning and Promotion Policy (v1alpha1 → v1beta1)

**Decision:** A group promotes from `v1alpha1` to `v1beta1` only when all four
gates are cleared:

| Gate | Criterion |
|---|---|
| **Rubric score** | Owning spec doc scores ≥ 90/100 on its iteration-3 pass. |
| **Soak time** | ≥ 90 calendar days starting at the earliest timestamp a customer (external, not keese-authored CI/e2e) runs the group at `v1alpha1` against a production cluster. The architect sign-off migration plan must cite the customer deployment event by its Elastic APM trace ID or a release-notes entry. |
| **Architect sign-off** | An architect-signed commit adds `docs/plans/migration-<group>.md` scoring ≥ 90. |
| **Conversion webhook** | A Hub-spoke conversion webhook is implemented and covered by envtest round-trip tests before the `v1beta1` CRD ships. |

No group promotes before all 16 kinds are deployed at `v1alpha1` and the P8 design
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

The PROJECT file uses `multigroup: true`, `domain: operator.keese.ai`. Each
`create api` call sets `--group=<subgroup>`; the SDK constructs
`<subgroup>.operator.keese.ai`. `api/core/v1alpha1` is a plain Go package —
no PROJECT entry, no SchemeBuilder, no CRD.

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
