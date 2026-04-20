<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: authz
depends: [04a-openfga-authz-model.md, 04b-projected-sa-identity.md]
related_skills: []
status: draft
last_verified: 2026-04-19
rollback: TODO — document migration path when status flips to current
---

# 04c — Token Revocation

> **Status: draft.** This doc is a gate-placeholder. Architect agent
> fills content across 3 rubric iterations (target ≥ 90/100) before
> any spec or controller code references this design.

## Context

_One paragraph. What problem does this design address? When a workspace or
tenant is suspended, cached tokens and OpenFGA tuples must be invalidated
within a defined revocation latency SLO using version-tagged caches and
fail-closed OpenFGA checks._

## Open questions (must be answered before `status: current`)

1. What is the revocation latency SLO (e.g. ≤ 60s for suspension events), and
   how is it measured and alerted on?
2. What version-tag scheme is used for caches so that a single tuple invalidation
   atomically rejects all in-flight requests using the stale token?
3. How does the operator signal the Envoy AI Gateway to flush a per-tenant
   credential cache — HTTP endpoint, ConfigMap bump, or pod annotation?
4. What is the fallback behavior when the OpenFGA revocation check itself times
   out — deny or allow with a warning event?
5. How does the SIGTERM drain window interact with revocation: can an agent
   finish an in-flight step after its token is revoked?

## Refs

- [../plans/rubric.md](../plans/rubric.md)
- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)
- [04a-openfga-authz-model.md](04a-openfga-authz-model.md)
- [04b-projected-sa-identity.md](04b-projected-sa-identity.md)
- [17-credential-broker.md](17-credential-broker.md)

TODO(design-gate)
