<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: spec
category: contract
depends: [../designs/09-transport-crd.md]
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

# transport.operator.keese.ai v1alpha1 — spec

> **Status: draft.** Authored AFTER all owning design docs reach
> `status: current`. Until then, this is a placeholder.

## Owning design(s)

- [`designs/09-transport-crd.md`](../designs/09-transport-crd.md) (status must be current)

## Acceptance test categories (to fill in)

- Schema validation: `spec.type` enum enforced; mutually exclusive sub-fields validated.
- Controller idempotency: TLS `Certificate` CR stable across 3 reconcile passes.
- Admission: VAP rejects `spec.type` mutation after creation.
- NATS transport: NACK `Stream` and `Consumer` projected correctly from `Transport`.
- TLS: cert-manager `Certificate` issued and referenced `Secret` present before Ready.

## Ref

- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)

TODO(design-gate)
