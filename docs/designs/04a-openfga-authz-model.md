<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: authz
depends: [01-tenancy-capsule.md, 02-workspace-model.md]
related_skills: []
status: draft
last_verified: 2026-04-19
rollback: TODO — document migration path when status flips to current
---

# 04a — OpenFGA Authorization Model

> **Status: draft.** This doc is a gate-placeholder. Architect agent
> fills content across 3 rubric iterations (target ≥ 90/100) before
> any spec or controller code references this design.

## Context

_One paragraph. What problem does this design address? OpenFGA provides
ReBAC for keese — tuple shapes, relation semantics, and consistency strategy
(eventual vs. HIGHER_CONSISTENCY per-call) must be specified before any
controller writes tuples._

## Open questions (must be answered before `status: current`)

1. What are the canonical tuple shapes for `(user, relation, object)` covering
   tenant membership, workspace access, tool entitlements, and model allow/deny?
2. Which checks require `HIGHER_CONSISTENCY` (synchronous read-your-writes) vs.
   eventual consistency, and what is the per-call latency budget for each tier?
3. How does the OpenFGA authorization model version (model ID) get propagated to
   all controllers and ext_authz callers — ConfigMap, webhook, or operator env?
4. What is the failure-closed contract when OpenFGA is unavailable: deny all, serve
   cached decisions, or circuit-break with a specific HTTP status?
5. At what tuple cardinality does SpiceDB become preferable to OpenFGA, and how
   is that migration path documented?

## Refs

- [../plans/rubric.md](../plans/rubric.md)
- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)
- [04b-projected-sa-identity.md](04b-projected-sa-identity.md)
- [04c-token-revocation.md](04c-token-revocation.md)
- [../specs/egress-authz-protocol.md](../specs/egress-authz-protocol.md)

TODO(design-gate)
