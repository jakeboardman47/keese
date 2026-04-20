<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: packaging
depends: [20-api-group-layout.md]
related_skills: []
status: draft
last_verified: 2026-04-19
rollback: TODO — document migration path when status flips to current
---

# 14a — OLM Channels and Upgrades

> **Status: draft.** This doc is a gate-placeholder. Architect agent
> fills content across 3 rubric iterations (target ≥ 90/100) before
> any spec or controller code references this design.

## Context

_One paragraph. What problem does this design address? OLM channel strategy
(`alpha`/`beta`/`stable`) and the `replaces` chain govern how operators are
upgraded in production without breaking existing CRD instances._

## Open questions (must be answered before `status: current`)

1. What are the promotion criteria for a bundle moving from `alpha` → `beta`
   → `stable` (test coverage, scorecard score, prod soak time)?
2. How is the `replaces` chain maintained by release-please — does the CSV
   `replaces` field need manual update or is it automated?
3. What is the skipped-version policy for emergency patches — can a `stable`
   bundle skip a `beta` revision in the `replaces` chain?
4. How does OLM handle in-place CRD schema changes (additive vs. breaking) at
   `v1alpha1` before a conversion webhook is introduced?
5. What is the rollback procedure when a `stable` bundle introduces a regression
   — revert to previous bundle version, or ship a patch bundle?

## Refs

- [../plans/rubric.md](../plans/rubric.md)
- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)
- [14b-olm-dependencies.md](14b-olm-dependencies.md)
- [../references/olm-bundle-authoring.md](../references/olm-bundle-authoring.md)

TODO(design-gate)
