<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: egress
depends: [05a-envoy-ai-gateway-topology.md, 04b-projected-sa-identity.md]
related_skills: []
status: draft
last_verified: 2026-04-19
rollback: TODO — document migration path when status flips to current
---

# 05b — Credential Injection Patterns

> **Status: draft.** This doc is a gate-placeholder. Architect agent
> fills content across 3 rubric iterations (target ≥ 90/100) before
> any spec or controller code references this design.

## Context

_One paragraph. What problem does this design address? The three-table
decomposition (Route → Backend, BackendSecurityPolicy → credential source,
OpenFGA tuple → allow/deny) governs how static, OIDC-STS, and dynamic
credentials are injected at the Envoy layer without agent pods holding secrets._

## Open questions (must be answered before `status: current`)

1. What are the exact `BackendSecurityPolicy` field combinations for each
   credential type (static API key, AWS OIDC, GCP WI, Azure Entra)?
2. How does the three-table decomposition handle credential rotation without
   dropping in-flight requests — what is the drain window during a secret
   version bump?
3. For tenants using static API keys, where does the key live (OpenBao path),
   and what ExternalSecret CR template projects it to the gateway-referenced
   K8s Secret?
4. What is the precedence when both a workspace-level and tenant-level
   `BackendSecurityPolicy` reference the same upstream backend?
5. How are non-AI upstreams (e.g. GitHub PAT, Jira token) handled — vault-agent
   sidecar file mount or the same `BackendSecurityPolicy` pattern?

## Refs

- [../plans/rubric.md](../plans/rubric.md)
- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)
- [05a-envoy-ai-gateway-topology.md](05a-envoy-ai-gateway-topology.md)
- [17-credential-broker.md](17-credential-broker.md)
- [11-secrets-pluggable-vault.md](11-secrets-pluggable-vault.md)

TODO(design-gate)
