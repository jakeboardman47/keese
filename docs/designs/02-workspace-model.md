<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: workspace
depends: [01-tenancy-capsule.md]
related_skills: []
status: draft
last_verified: 2026-04-19
rollback: TODO — document migration path when status flips to current
---

# 02 — Workspace Model

> **Status: draft.** This doc is a gate-placeholder. Architect agent
> fills content across 3 rubric iterations (target ≥ 90/100) before
> any spec or controller code references this design.

## Context

_One paragraph. What problem does this design address? A single `Workspace` CR
projects ~7 resources (Deployment + PVC + SA + NP + HTTPRoute + OpenFGA tuples +
Capsule labels); its status FSM governs idle eviction and runtime binding._

## Open questions (must be answered before `status: current`)

1. What are the exact states in the workspace lifecycle FSM, and what events
   trigger transitions (e.g. `Pending → Running → Idle → Evicted`)?
2. Single-pod vs. pod-per-agent: what heuristic determines which topology is
   chosen, and can it be changed after workspace creation?
3. What is the idle eviction policy — time-based, resource-based, or both —
   and how is the threshold configured at tenant vs. cluster scope?
4. How does PVC sizing and access-mode selection interact with the underlying
   storage class in dev (kind) vs. production (EKS/GKE/AKS)?
5. How does `spec.scheduling` (nodeSelector/tolerations) compose with Capsule's
   tenant-level scheduling constraints?

## Refs

- [../plans/rubric.md](../plans/rubric.md)
- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)
- [01-tenancy-capsule.md](01-tenancy-capsule.md)
- [../specs/workspace.operator.keese.ai-v1alpha1.md](../specs/workspace.operator.keese.ai-v1alpha1.md)

TODO(design-gate)
