<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: api
depends: [docs/plans/scaffolding-plan.md]
related_skills: [crd-authoring, doc-authoring]
status: current
last_verified: 2026-04-20
rollback: Revert to prior commit; no migration plan required at v1alpha1 because no
  conversion webhooks exist yet. At v1beta1 promotion a migration plan in
  docs/plans/migration-<kind>.md is required before rollback of any group.
---

# 20a — API Group Layout: Groups, Kinds, Shared Types, Versioning

**Decision:** 13 kinds across 8 sub-groups all under `operator.keese.ai`,
all at `v1alpha1`. A shared-types package at
`github.com/keese-ai/keese/api/core/v1alpha1` holds cross-group primitives.
Promotion to `v1beta1` requires a rubric score ≥ 90, 90-day soak, and
architect sign-off via a migration plan doc.

## Context

Keese is a secure multi-tenant K8s operator orchestrating autonomous AI agent
workflows on pluggable runtimes. Eight API groups under `*.operator.keese.ai`
host 13 kinds, all at `v1alpha1`. Three concerns drive this design:
(1) group boundaries that map cleanly to controller ownership and RBAC,
(2) a shared-types package that prevents duplication of conditions and status
patterns while keeping cross-group imports unidirectional, and
(3) a versioning policy that is conservative enough to avoid premature v1beta1
promotion and the conversion webhook overhead that comes with it.

## The 8 Groups and 13 Kinds

| Group | Full API Group | Kinds | Go package path |
|---|---|---|---|
| workspace | `workspace.operator.keese.ai` | `Workspace`, `WorkspaceShare` | `api/workspace/v1alpha1` |
| workflow | `workflow.operator.keese.ai` | `Workflow`, `WorkflowRun` | `api/workflow/v1alpha1` |
| runtime | `runtime.operator.keese.ai` | `AgentRuntime`, `RuntimeExtension` | `api/runtime/v1alpha1` |
| memory | `memory.operator.keese.ai` | `Memory`, `SharedMemory` | `api/memory/v1alpha1` |
| recipe | `recipe.operator.keese.ai` | `Recipe`, `RecipeSource` | `api/recipe/v1alpha1` |
| guardrail | `guardrail.operator.keese.ai` | `GuardrailBinding` | `api/guardrail/v1alpha1` |
| observability | `observability.operator.keese.ai` | `TokenBudget` | `api/observability/v1alpha1` |
| transport | `transport.operator.keese.ai` | `Transport` | `api/transport/v1alpha1` |

All 13 kinds are namespace-scoped. No cluster-scoped kinds at v1alpha1; if a
cluster-scoped kind is needed later it requires an ADR in `docs/designs/`.

## Shared-Types Package Layout

**Decision:** A shared package at `github.com/keese-ai/keese/api/core/v1alpha1`
holds cross-group primitives. All eight group packages import it; it imports nothing
from the group packages (unidirectional).

| Type | Rationale |
|---|---|
| `Condition` (re-export wrapping `metav1.Condition`) | Uniform condition type/reason/message vocabulary across all 13 kinds. |
| `Phase` (string type + const set: `Pending`, `Provisioning`, `Ready`, `Degraded`, `Terminating`) | Shared phase vocabulary; group-specific phases are additional consts in the group package, not in core. |
| `ResourceRef` (`{ Name, Namespace, Group, Kind string }`) | Cross-group references (e.g., `Workspace.spec.runtimeRef`, `GuardrailBinding.spec.workspaceRef`). |
| `StatusBase` (`ObservedGeneration int64`, `Conditions []metav1.Condition`, `Phase Phase`) | Embedded in every kind's `*Status` struct — enforces rule 04.4 (`observedGeneration` on every status). |
| `ReBAC` marker type alias (string constant) | Anchor for `// +keese:rebac-tuple=<relation>` markers; not a Go type used at runtime, but keeps the constant definition canonical. |

Import rule: group packages may import `api/core/v1alpha1`. No group package
imports another group package directly. Cross-group coordination goes through
the controller layer, not the API types layer.

## Versioning and Promotion Policy (v1alpha1 → v1beta1)

**Decision:** A group promotes from `v1alpha1` to `v1beta1` only when all four
gates are cleared:

| Gate | Criterion |
|---|---|
| **Rubric score** | Owning spec doc scores ≥ 90/100 on its iteration-3 pass. |
| **Soak time** | The v1alpha1 CRD has been deployed in a production-like environment for ≥ 90 calendar days. |
| **Architect sign-off** | An architect-signed commit adds `docs/plans/migration-<group>.md` scoring ≥ 90. |
| **Conversion webhook** | A Hub-spoke conversion webhook is implemented and covered by envtest round-trip tests before the v1beta1 CRD ships. |

No group promotes before all 13 kinds are deployed at v1alpha1 and the P8 design
gate is open. The intent is that the first promotion happens no earlier than
90 days after GA launch.

At v1alpha1 there are **no conversion webhooks** (rule 04.13). The only admission
webhooks at v1alpha1 are mutating (defaulting) and validating (cross-resource
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

## operator-sdk PROJECT Multigroup Encoding

**Decision:** The PROJECT file at repo root uses `multigroup: true` with
`domain: operator.keese.ai`. Each `create api` call sets `--group=<subgroup>`
(e.g., `--group=workspace`). The SDK then constructs the full API group as
`<subgroup>.operator.keese.ai`.

```
operator-sdk init \
  --domain=operator.keese.ai \
  --repo=github.com/keese-ai/keese \
  --plugins=go/v4 \
  --project-name=keese

operator-sdk create api \
  --group=workspace \
  --version=v1alpha1 \
  --kind=Workspace \
  --resource --controller
```

The `domain` field in PROJECT is `operator.keese.ai`; the `group` field per
resource entry is the sub-group (e.g., `workspace`). The Go import path per
resource is `github.com/keese-ai/keese/api/<group>/v1alpha1`. This matches the
actual PROJECT file at repo root (verified 2026-04-20).

The `api/core/v1alpha1` package is not a PROJECT resource entry — it is a
plain Go package, not an SDK-managed API group. It has no CRD and no
SchemeBuilder registration beyond type declarations.

## New-Kind-in-Existing-Group Policy

**Decision:** After a group's `v1alpha1` is published, a **new kind** is added
to the same version (`v1alpha1`) by running `operator-sdk create api` with the
same `--group` and `--version=v1alpha1`. No new API version is needed solely
because a new kind is added. API versions attach to kinds, not groups.

A new version (e.g., `v1alpha2` within the same group) is introduced only when
an existing kind needs a breaking schema change. That requires a conversion
webhook and a migration plan — the same gates as `v1beta1` promotion.

New kinds at `v1alpha1` in an existing group must still pass:
- The CRD design checklist (`docs/references/crd-design-checklist.md`).
- `make manifests generate` with no drift.
- ≥ 2 samples passing `kubectl apply --dry-run=server`.
- A new row in the design doc that owns the group.

## Refs

- [../plans/scaffolding-plan.md](../plans/scaffolding-plan.md) — D2, D16, D23
- [20b-api-group-layout.md](20b-api-group-layout.md) — trade-offs, failure modes,
  upgrade/rollback, observability, iteration log
- [../references/crd-design-checklist.md](../references/crd-design-checklist.md)
- [../../PROJECT](../../PROJECT) — live multigroup layout
- [../plans/rubric.md](../plans/rubric.md)
