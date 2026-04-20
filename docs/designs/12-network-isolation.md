<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: security
depends: [01-tenancy-capsule.md, 02-workspace-model.md]
related_skills: []
status: draft
last_verified: 2026-04-19
rollback: TODO — document migration path when status flips to current
---

# 12 — Network Isolation

> **Status: draft.** This doc is a gate-placeholder. Architect agent
> fills content across 3 rubric iterations (target ≥ 90/100) before
> any spec or controller code references this design.

## Context

_One paragraph. What problem does this design address? Default-deny
`NetworkPolicy` per workspace plus `ReferenceGrant`-based opt-in for
cross-namespace sharing ensures network isolation without manual policy
authoring by workspace operators._

## Open questions (must be answered before `status: current`)

1. What is the exact default-deny `NetworkPolicy` template the workspace
   controller applies, and what egress rules does it open for the Envoy
   AI Gateway and cert-manager?
2. How does `ReferenceGrant` govern cross-namespace `WorkspaceShare` network
   access — is it a Gateway API `ReferenceGrant` or a keese custom resource?
3. What happens when a workspace needs egress to an external API that is not
   routed through Envoy AI Gateway — is there an escape hatch, and what audit
   trail does it leave?
4. How does network isolation compose with Capsule's `networkPolicies`
   field — who wins on conflict, and what is the merge strategy?
5. What is the `NetworkPolicy` behaviour on kind (CNI: kindnet) vs. production
   (CNI: Calico/Cilium), and how is this tested in CI?

## Refs

- [../plans/rubric.md](../plans/rubric.md)
- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)
- [01-tenancy-capsule.md](01-tenancy-capsule.md)
- [05a-envoy-ai-gateway-topology.md](05a-envoy-ai-gateway-topology.md)
- [09-transport-crd.md](09-transport-crd.md)

TODO(design-gate)
