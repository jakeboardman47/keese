<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: runtime
depends: [08a-goose-headless-modes.md, 04a-openfga-authz-model.md]
related_skills: []
status: draft
last_verified: 2026-04-19
rollback: TODO — document migration path when status flips to current
---

# 08c — Goose Sub-Agents and Limits

> **Status: draft.** This doc is a gate-placeholder. Architect agent
> fills content across 3 rubric iterations (target ≥ 90/100) before
> any spec or controller code references this design.

## Context

_One paragraph. What problem does this design address? Goose supports spawning
sub-agents; keese caps this at 10 concurrent sub-agents per workspace and
enforces the cap via ReBAC-at-spawn using OpenFGA tuples._

## Open questions (must be answered before `status: current`)

1. What OpenFGA tuple shape represents "workspace W has N active sub-agents"
   and how is the counter incremented/decremented atomically?
2. When the 10-agent ceiling is hit, what is the caller-visible error (HTTP
   status, ACP error code) and what event is emitted on the `Workspace`?
3. How does the ceiling apply per-workspace vs. per-tenant vs. per-cluster —
   is there a hierarchical quota, and who sets each level?
4. What happens to orphaned sub-agent processes when the parent agent is
   SIGTERMed — are they adopted, killed, or checkpointed independently?
5. How does the 10-agent cap interact with `TokenBudget` — does each
   sub-agent share or split the budget of the parent workspace?

## Refs

- [../plans/rubric.md](../plans/rubric.md)
- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)
- [08a-goose-headless-modes.md](08a-goose-headless-modes.md)
- [04a-openfga-authz-model.md](04a-openfga-authz-model.md)
- [10b-token-accounting.md](10b-token-accounting.md)

TODO(design-gate)
