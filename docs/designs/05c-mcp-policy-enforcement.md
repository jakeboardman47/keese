<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: egress
depends: [05a-envoy-ai-gateway-topology.md, 04a-openfga-authz-model.md]
related_skills: []
status: draft
last_verified: 2026-04-19
rollback: TODO — document migration path when status flips to current
---

# 05c — MCP Policy Enforcement

> **Status: draft.** This doc is a gate-placeholder. Architect agent
> fills content across 3 rubric iterations (target ≥ 90/100) before
> any spec or controller code references this design.

## Context

_One paragraph. What problem does this design address? CEL per-tool policies
in `MCPRoute` rules enforce which MCP tools an agent may call. This design
covers the `request.mcp.tool`/`method` CEL semantics and how a `ToolAllowList`
ConfigMap projects to `MCPRoute` admission rules._

## Open questions (must be answered before `status: current`)

1. What is the full CEL variable schema exposed in `MCPRoute` rule expressions
   (e.g. `request.mcp.tool`, `request.mcp.arguments`, `source.namespace`)?
2. How does the `ToolAllowList` ConfigMap schema look, and what controller or
   webhook translates it to `MCPRoute` CEL rules atomically?
3. What is the audit log format emitted per MCP tool call, and how is it
   routed to the OTEL collector / ES index?
4. When a CEL expression compilation error occurs at `MCPRoute` admission, what
   is the fallback — reject all MCP calls or allow-all with an alert?
5. How does per-tool rate limiting interact with `TokenBudget` enforcement at
   the Envoy level?

## Refs

- [../plans/rubric.md](../plans/rubric.md)
- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)
- [05a-envoy-ai-gateway-topology.md](05a-envoy-ai-gateway-topology.md)
- [06-guardrailbinding.md](06-guardrailbinding.md)
- [../specs/egress-authz-protocol.md](../specs/egress-authz-protocol.md)

TODO(design-gate)
