<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: spec
category: contract
depends: [../designs/16-recipe-distribution.md]
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

# recipe.operator.keese.ai v1alpha1 — spec

> **Status: draft.** Authored AFTER all owning design docs reach
> `status: current`. Until then, this is a placeholder.

## Owning design(s)

- [`designs/16-recipe-distribution.md`](../designs/16-recipe-distribution.md) (status must be current)

## Acceptance test categories (to fill in)

- Schema validation: `RecipeSource.spec.oci.image` must include digest.
- Controller idempotency: pulled recipe content stable across 3 reconciles.
- Admission: cosign signature verification blocks unsigned recipe OCI images.
- Entitlement gate: recipe referencing disallowed tool rejected against workspace allow-list.
- Observability: recipe pull event emitted with source digest and cosign verify result.

## Ref

- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)

TODO(design-gate)
