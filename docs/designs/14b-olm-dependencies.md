<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: packaging
depends: [14a-olm-channels-upgrades.md]
related_skills: []
status: draft
last_verified: 2026-04-19
rollback: TODO — document migration path when status flips to current
---

# 14b — OLM Dependencies

> **Status: draft.** This doc is a gate-placeholder. Architect agent
> fills content across 3 rubric iterations (target ≥ 90/100) before
> any spec or controller code references this design.

## Context

_One paragraph. What problem does this design address? The keese OLM bundle
declares dependencies on cert-manager, Envoy Gateway, Envoy AI Gateway, Capsule,
NACK, ECK, and Argo Workflows operators. This design covers how dependencies are
expressed and how webhook CA bootstrap works._

## Open questions (must be answered before `status: current`)

1. Which dependencies are expressed as OLM `dependencies` (GVK-based or
   package-based) vs. README-only install prerequisites?
2. How does the webhook CA bootstrap work when cert-manager is a declared OLM
   dependency — what is the ordering guarantee?
3. What happens at install time if a declared dependency operator is already
   installed at an incompatible version — OLM block, warning, or silent proceed?
4. How are dependency version ranges expressed (minimum only, semver range,
   or exact) for each upstream operator?
5. What is the testing strategy for the dependency graph — a multi-operator
   `operator-sdk scorecard` run or a dedicated e2e-kind job?

## Refs

- [../plans/rubric.md](../plans/rubric.md)
- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)
- [14a-olm-channels-upgrades.md](14a-olm-channels-upgrades.md)
- [../references/olm-bundle-authoring.md](../references/olm-bundle-authoring.md)

TODO(design-gate)
