<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: recipes
depends: [05c-mcp-policy-enforcement.md, 06-guardrailbinding.md]
related_skills: []
status: draft
last_verified: 2026-04-19
rollback: TODO — document migration path when status flips to current
---

# 16 — Recipe Distribution

> **Status: draft.** This doc is a gate-placeholder. Architect agent
> fills content across 3 rubric iterations (target ≥ 90/100) before
> any spec or controller code references this design.

## Context

_One paragraph. What problem does this design address? `RecipeSource` (OCI-first)
and `Recipe` CRDs enable cosign-signed recipe distribution with admission
validation against workspace tool and model entitlements._

## Open questions (must be answered before `status: current`)

1. What is the OCI image layout for a recipe artifact — what layers, labels,
   and media types does the `RecipeSource` controller expect?
2. How does cosign signature verification happen at admission — webhook that
   calls cosign verify, or a Kyverno policy with an image verifier?
3. What workspace entitlement fields does recipe admission check against
   (tool allow-list, model list, resource limits)?
4. What is the caching strategy for pulled recipe OCI images — stored in the
   local registry (ctlptl), in a PVC, or re-pulled per workspace activation?
5. How does a recipe author publish a new version — OCI push + tag, then a
   `RecipeSource` object update, or an automated release-please step?

## Refs

- [../plans/rubric.md](../plans/rubric.md)
- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)
- [06-guardrailbinding.md](06-guardrailbinding.md)
- [../specs/recipe.operator.keese.ai-v1alpha1.md](../specs/recipe.operator.keese.ai-v1alpha1.md)

TODO(design-gate)
