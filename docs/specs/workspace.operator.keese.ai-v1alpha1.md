<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: spec
category: contract
depends: [../designs/01-tenancy-capsule.md, ../designs/02-workspace-model.md]
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

# workspace.operator.keese.ai v1alpha1 — spec

> **Status: draft.** Authored AFTER all owning design docs reach
> `status: current`. Until then, this is a placeholder.

## Owning design(s)

- [`designs/01-tenancy-capsule.md`](../designs/01-tenancy-capsule.md) (status must be current)
- [`designs/02-workspace-model.md`](../designs/02-workspace-model.md) (status must be current)

## Acceptance test categories (to fill in)

- Schema validation: CRD openAPIV3Schema enforces required fields and enum values.
- Controller idempotency: 3 reconcile passes produce identical resource set.
- Admission: VAP rejects immutable-field mutations (tenant binding).
- FSM transitions: Pending → Running, Running → Idle, Idle → Evicted all covered.
- Network isolation: workspace `NetworkPolicy` default-deny applied at creation.

## Ref

- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)

TODO(design-gate)
