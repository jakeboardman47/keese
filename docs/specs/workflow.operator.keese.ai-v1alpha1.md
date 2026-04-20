<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: spec
category: contract
depends: [../designs/03-workflow-argo-delegation.md, ../designs/22-workflow-composition-examples.md]
related_skills: []
status: draft
last_verified: 2026-04-19
regression_lock: false
tests:
  unit: []
  envtest: []
  kuttl: []
metrics: []
events: []
---

# workflow.operator.keese.ai v1alpha1 — spec

> **Status: draft.** Authored AFTER all owning design docs reach
> `status: current`. Until then, this is a placeholder.

## Owning design(s)

- [`designs/03-workflow-argo-delegation.md`](../designs/03-workflow-argo-delegation.md) (status must be current)
- [`designs/22-workflow-composition-examples.md`](../designs/22-workflow-composition-examples.md) (status must be current)

## Acceptance test categories (to fill in)

- Schema validation: `spec.triggers[]` and `spec.outputs[]` enum values validated.
- Controller idempotency: `WorkflowTemplate` projection stable across 3 reconciles.
- Admission: webhook rejects `WorkflowRun` referencing non-existent `Workflow`.
- Argo delegation: `Workflow` creates Argo `WorkflowTemplate` in target namespace.
- Observability: OTEL trace root span created per `WorkflowRun` activation.

## Ref

- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)

TODO(design-gate)
