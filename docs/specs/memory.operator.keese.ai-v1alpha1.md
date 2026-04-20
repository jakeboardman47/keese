<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: spec
category: contract
depends: [../designs/15-memory-management.md]
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

# memory.operator.keese.ai v1alpha1 — spec

> **Status: draft.** Authored AFTER all owning design docs reach
> `status: current`. Until then, this is a placeholder.

## Owning design(s)

- [`designs/15-memory-management.md`](../designs/15-memory-management.md) (status must be current)

## Acceptance test categories (to fill in)

- Schema validation: `Memory.spec.provider` discriminated one-of validated at admission.
- Controller idempotency: backend connection config stable across 3 reconcile passes.
- Admission: webhook rejects invalid provider type combination.
- SharedMemory ReBAC: cross-workspace read access denied without OpenFGA tuple.
- Deletion: PVC retention policy enforced per configured `reclaimPolicy`.

## Ref

- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)

TODO(design-gate)
