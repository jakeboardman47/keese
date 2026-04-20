<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: egress
depends: [04a-openfga-authz-model.md, 04b-projected-sa-identity.md]
related_skills: []
status: draft
last_verified: 2026-04-19
rollback: TODO — document migration path when status flips to current
---

# 05a — Envoy AI Gateway Topology

> **Status: draft.** This doc is a gate-placeholder. Architect agent
> fills content across 3 rubric iterations (target ≥ 90/100) before
> any spec or controller code references this design.

## Context

_One paragraph. What problem does this design address? Envoy AI Gateway v0.5.x
provides `MCPRoute`, `AIGatewayRoute`, `BackendSecurityPolicy`, and token-cost
rate limiting. This design covers deployment topology (per-cluster vs. optional
per-tenant) and how ext_authz is wired to OpenFGA._

## Open questions (must be answered before `status: current`)

1. Should the Envoy AI Gateway be deployed per-cluster (shared) or optionally
   per-tenant for isolation, and what drives the opt-in decision?
2. How are `MCPRoute` and `AIGatewayRoute` CRD versions pinned by digest in the
   operator CSV, and what is the upgrade strategy when Envoy AI GW releases?
3. What is the ext_authz cluster name and the exact request/response format
   the keese OpenFGA adapter must implement?
4. How does token-cost rate limiting interact with `TokenBudget` enforcement —
   which layer is authoritative on budget exhaustion?
5. What observability (OTEL spans, Prom metrics) does the gateway emit, and how
   does the operator's OTEL collector pipeline consume them?

## Refs

- [../plans/rubric.md](../plans/rubric.md)
- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)
- [05b-credential-injection-patterns.md](05b-credential-injection-patterns.md)
- [05c-mcp-policy-enforcement.md](05c-mcp-policy-enforcement.md)
- [10b-token-accounting.md](10b-token-accounting.md)

TODO(design-gate)
