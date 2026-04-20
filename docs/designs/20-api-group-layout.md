<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: api
depends: []
related_skills: []
status: draft
last_verified: 2026-04-19
rollback: TODO — document migration path when status flips to current
---

# 20 — API Group Layout

> **Status: draft.** This doc is a gate-placeholder. Architect agent
> fills content across 3 rubric iterations (target ≥ 90/100) before
> any spec or controller code references this design.

## Context

_One paragraph. What problem does this design address? Eight API groups under
`*.operator.keese.ai` hosting 13 kinds at `v1alpha1`, plus versioning strategy
(v1alpha1 → v1beta1 requires a conversion webhook), and a shared-types package._

## Open questions (must be answered before `status: current`)

1. What shared types (conditions, phase enums, resource refs, status base) live
   in the shared-types package, and what is its import path?
2. What is the exact promotion criteria for a group to move from `v1alpha1` to
   `v1beta1` — stability score, soak time, or architect sign-off?
3. How are multi-version CRDs handled in the OLM bundle CSV — `additionalPrinterColumns`
   and `versions[]` must be consistent across served versions?
4. What is the strategy for adding a new kind to an existing group after the
   group's `v1alpha1` is already published — new version or new kind in same version?
5. How does the `operator-sdk PROJECT` file encode multi-group layout, and what
   is the `domain` vs. `group` naming convention used in `create api`?

## Refs

- [../plans/rubric.md](../plans/rubric.md)
- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)
- [../references/crd-design-checklist.md](../references/crd-design-checklist.md)

TODO(design-gate)
