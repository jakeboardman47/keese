<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: observability
depends: [10a-otel-topology.md, 05a-envoy-ai-gateway-topology.md]
related_skills: []
status: draft
last_verified: 2026-04-19
rollback: TODO — document migration path when status flips to current
---

# 10b — Token Accounting

> **Status: draft.** This doc is a gate-placeholder. Architect agent
> fills content across 3 rubric iterations (target ≥ 90/100) before
> any spec or controller code references this design.

## Context

_One paragraph. What problem does this design address? `TokenBudget` CR
enforcement combines Envoy AI Gateway cost filtering with OTEL metrics
to decrement budgets, emit alerts, and export billing data._

## Open questions (must be answered before `status: current`)

1. What is the unit of account — raw tokens, cost-weighted tokens, or
   USD-equivalent — and who is authoritative for the exchange rate?
2. How does the `TokenBudget` controller decrement the budget atomically
   when the Envoy AI GW cost filter emits a usage event — direct API patch,
   OTEL processor, or NATS message?
3. What happens when the budget is exhausted mid-request — reject at the
   gateway, allow completion but block future requests, or emit a soft
   warning only?
4. How are budget resets (monthly, per-workspace-cycle) scheduled and
   who triggers them — a CronJob, the workspace controller, or a webhook?
5. What Prometheus metrics and OTEL metric names are emitted for billing
   export, and what is the cardinality risk per tenant × model?

## Refs

- [../plans/rubric.md](../plans/rubric.md)
- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)
- [10a-otel-topology.md](10a-otel-topology.md)
- [05a-envoy-ai-gateway-topology.md](05a-envoy-ai-gateway-topology.md)
- [../specs/observability.operator.keese.ai-v1alpha1.md](../specs/observability.operator.keese.ai-v1alpha1.md)

TODO(design-gate)
