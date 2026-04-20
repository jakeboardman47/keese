<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: guardrails
depends: [04a-openfga-authz-model.md, 05c-mcp-policy-enforcement.md]
related_skills: []
status: draft
last_verified: 2026-04-19
rollback: TODO — document migration path when status flips to current
---

# 06 — GuardrailBinding

> **Status: draft.** This doc is a gate-placeholder. Architect agent
> fills content across 3 rubric iterations (target ≥ 90/100) before
> any spec or controller code references this design.

## Context

_One paragraph. What problem does this design address? `GuardrailBinding`
consolidates all guardrail composition (Kyverno + OpenFGA + Envoy SecurityPolicy
+ recipe hooks + TokenBudget) into a single CRD with a role model and
strictest-wins merge lattice._

## Open questions (must be answered before `status: current`)

1. What is the complete role model (cluster-admin / tenant-admin / workspace-admin)
   and which fields may each role tighten vs. loosen?
2. How does the strictest-wins merge lattice work across multiple overlapping
   `GuardrailBinding` objects — union, intersection, or ordered overlay?
3. What VAP (CEL) rule prevents a workspace-admin from weakening a binding
   inherited from tenant-admin level?
4. How does the cluster-scoped `default` binding get auto-injected via mutating
   webhook, and what happens if a workspace has no explicit binding?
5. What happens when a `GuardrailBinding` references a Kyverno `ClusterPolicy`
   that does not yet exist — admission failure or degraded mode?

## Refs

- [../plans/rubric.md](../plans/rubric.md)
- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)
- [05c-mcp-policy-enforcement.md](05c-mcp-policy-enforcement.md)
- [../specs/guardrail.operator.keese.ai-v1alpha1.md](../specs/guardrail.operator.keese.ai-v1alpha1.md)

TODO(design-gate)
