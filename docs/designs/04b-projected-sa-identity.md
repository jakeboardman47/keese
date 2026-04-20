<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: authz
depends: [04a-openfga-authz-model.md]
related_skills: []
status: draft
last_verified: 2026-04-19
rollback: TODO — document migration path when status flips to current
---

# 04b — Projected ServiceAccount Identity

> **Status: draft.** This doc is a gate-placeholder. Architect agent
> fills content across 3 rubric iterations (target ≥ 90/100) before
> any spec or controller code references this design.

## Context

_One paragraph. What problem does this design address? Per-tenant projected
ServiceAccount tokens with audience `keese-egress-<tenant>` ensure tight
trust-policy scoping for cloud IAM roles (Bedrock, Vertex AI, Azure OIDC)._

## Open questions (must be answered before `status: current`)

1. What is the token TTL policy per tenant tier, and what mechanism enforces
   refresh before expiry in the gateway pod vs. agent pod?
2. How does OIDC trust anchoring work across cloud providers — is the JWKS
   endpoint the K8s API server's, or a dedicated issuer?
3. What happens when a workspace is moved to a different tenant — must the SA
   token be rotated, and how is the transition window handled?
4. How does the per-tenant audience template (`keese-egress-<tenant>`) get
   projected into `BackendSecurityPolicy.spec.oidc.tokenExchangeServiceAccounts`?
5. What audit trail is emitted for SA token issuance and use, and where does
   it land (ES index, OTEL span)?

## Refs

- [../plans/rubric.md](../plans/rubric.md)
- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)
- [04a-openfga-authz-model.md](04a-openfga-authz-model.md)
- [04c-token-revocation.md](04c-token-revocation.md)
- [05b-credential-injection-patterns.md](05b-credential-injection-patterns.md)

TODO(design-gate)
