<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: spec
category: contract
depends: [../designs/10a-otel-topology.md, ../designs/10b-token-accounting.md]
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

# observability.operator.keese.ai v1alpha1 — spec

> **Status: draft.** Authored AFTER all owning design docs reach
> `status: current`. Until then, this is a placeholder.

## Owning design(s)

- [`designs/10a-otel-topology.md`](../designs/10a-otel-topology.md) (status must be current)
- [`designs/10b-token-accounting.md`](../designs/10b-token-accounting.md) (status must be current)

## Acceptance test categories (to fill in)

- Schema validation: `TokenBudget.spec.limit` positive integer required.
- Controller idempotency: budget counters converge after 3 reconcile passes.
- Admission: VAP rejects budget decrease below current usage.
- Budget enforcement: request blocked at Envoy when budget exhausted.
- Billing export: Prometheus metric `keese_token_budget_used_total` emitted per tenant.

## Ref

- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)

TODO(design-gate)
