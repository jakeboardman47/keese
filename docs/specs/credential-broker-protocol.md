<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: spec
category: contract
depends:
  - ../designs/17-credential-broker.md
  - ../designs/05b-credential-injection-patterns.md
  - ../designs/04c-token-revocation.md
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

# credential-broker-protocol — spec

> **Status: draft.** Authored AFTER all owning design docs reach
> `status: current`. Until then, this is a placeholder.

## Owning design(s)

- [`designs/17-credential-broker.md`](../designs/17-credential-broker.md) (status must be current)
- [`designs/05b-credential-injection-patterns.md`](../designs/05b-credential-injection-patterns.md) (status must be current)
- [`designs/04c-token-revocation.md`](../designs/04c-token-revocation.md) (status must be current)

## Acceptance test categories (to fill in)

- Cache hit: second request for same (audience, role) served from pod-local cache.
- Refresh at 70% TTL: background refresh triggered before threshold; no request dropped.
- Fail-closed at 95% TTL: expired credential returns 503, not stale token.
- Revocation flush: revocation signal empties per-pod cache within revocation SLO.
- OpenBao failure table: each failure mode produces correct action per design 17.

## Ref

- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)

TODO(design-gate)
