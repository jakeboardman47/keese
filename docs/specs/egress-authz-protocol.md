<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: spec
category: contract
depends:
  - ../designs/04a-openfga-authz-model.md
  - ../designs/05a-envoy-ai-gateway-topology.md
  - ../designs/05c-mcp-policy-enforcement.md
related_skills: []
status: draft
last_verified: 2026-04-19
regression_lock: false
tests:
  unit: []
  envtest: []
  kuttl: []
metrics: []
events: []
---

# egress-authz-protocol — spec

> **Status: draft.** Authored AFTER all owning design docs reach
> `status: current`. Until then, this is a placeholder.

## Owning design(s)

- [`designs/04a-openfga-authz-model.md`](../designs/04a-openfga-authz-model.md) (status must be current)
- [`designs/05a-envoy-ai-gateway-topology.md`](../designs/05a-envoy-ai-gateway-topology.md) (status must be current)
- [`designs/05c-mcp-policy-enforcement.md`](../designs/05c-mcp-policy-enforcement.md) (status must be current)

## Acceptance test categories (to fill in)

- ext_authz request/response: shape matches Envoy `CheckRequest` / `CheckResponse` proto.
- OpenFGA check: allowed tool call returns 200; denied tool call returns 403.
- Audit log: every ext_authz decision emits structured log (tuple, SA, host, decision).
- Fail-closed: OpenFGA unavailable returns 503, not 200.
- CEL eval: malformed MCPRoute CEL expression returns 400 with error detail.

## Ref

- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)

TODO(design-gate)
