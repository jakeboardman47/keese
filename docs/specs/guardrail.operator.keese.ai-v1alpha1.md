<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: spec
category: contract
depends: [../designs/06-guardrailbinding.md, ../designs/05c-mcp-policy-enforcement.md]
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

# guardrail.operator.keese.ai v1alpha1 — spec

> **Status: draft.** Authored AFTER all owning design docs reach
> `status: current`. Until then, this is a placeholder.

## Owning design(s)

- [`designs/06-guardrailbinding.md`](../designs/06-guardrailbinding.md) (status must be current)
- [`designs/05c-mcp-policy-enforcement.md`](../designs/05c-mcp-policy-enforcement.md) (status must be current)

## Acceptance test categories (to fill in)

- Schema validation: `GuardrailBinding` must reference at least one composition target.
- Controller idempotency: composed Kyverno + OpenFGA + Envoy policies stable across 3 reconciles.
- Admission: VAP blocks workspace-admin weakening a tenant-inherited binding.
- Default injection: mutating webhook injects cluster-scoped default binding on new workspaces.
- Strictest-wins merge: overlapping bindings resolve to most restrictive policy set.

## Ref

- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)

TODO(design-gate)
