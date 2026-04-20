<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: egress
depends: [05b-credential-injection-patterns.md, 11-secrets-pluggable-vault.md, 04c-token-revocation.md]
related_skills: []
status: draft
last_verified: 2026-04-19
rollback: TODO — document migration path when status flips to current
---

# 17 — Credential Broker

> **Status: draft.** This doc is a gate-placeholder. Architect agent
> fills content across 3 rubric iterations (target ≥ 90/100) before
> any spec or controller code references this design.

## Context

_One paragraph. What problem does this design address? Per-gateway-pod credential
caching with tiered refresh (70% TTL), fail-closed past 95% TTL, and an explicit
failure table covering OpenBao down, OpenFGA down, and STS failure scenarios._

## Open questions (must be answered before `status: current`)

1. What are the caching tiers — in-memory (per-request), per-gateway-pod (keyed
   by audience+role), and distributed (Redis/NATS KV) — and when is each used?
2. What is the exact failure table: for each failure mode (OpenBao down, OpenFGA
   down, STS timeout, network partition), what is the credential broker's action?
3. How is the 70% TTL refresh triggered — background goroutine, Envoy callback,
   or an external controller reconcile loop?
4. When the broker hits the 95% TTL threshold without a successful refresh, what
   is the observable signal — event on which object, alert, metric?
5. How does the credential broker interact with token revocation (design 04c) —
   does a revocation signal flush the per-pod cache synchronously?

## Refs

- [../plans/rubric.md](../plans/rubric.md)
- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)
- [05b-credential-injection-patterns.md](05b-credential-injection-patterns.md)
- [04c-token-revocation.md](04c-token-revocation.md)
- [../specs/credential-broker-protocol.md](../specs/credential-broker-protocol.md)

TODO(design-gate)
