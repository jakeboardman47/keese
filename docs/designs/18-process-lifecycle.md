<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: reliability
depends: [02-workspace-model.md, 07-agent-runtime-spi.md]
related_skills: []
status: draft
last_verified: 2026-04-19
rollback: TODO — document migration path when status flips to current
---

# 18 — Process Lifecycle

> **Status: draft.** This doc is a gate-placeholder. Architect agent
> fills content across 3 rubric iterations (target ≥ 90/100) before
> any spec or controller code references this design.

## Context

_One paragraph. What problem does this design address? SIGTERM drain patterns
for controllers (queue drain + leader lease release) and agents (session
checkpoint to PVC/NATS/ES), plus SIGKILL recovery invariants via durable
state, must be codified before any controller is implemented._

## Open questions (must be answered before `status: current`)

1. What is the maximum drain window for a controller pod (configured via
   `terminationGracePeriodSeconds`) and how is it validated in CI?
2. What is the checkpoint format and location for a goose session interrupted
   by SIGTERM — SQLite on PVC, NATS JetStream message, or both?
3. How does idempotent restart work for a controller that was SIGKILLed mid-
   reconcile — what state is re-read from the API server vs. restored from
   durable store?
4. What structured log fields are emitted in the `shutdown` event, and which
   OTEL span attributes carry drain duration and checkpoint location?
5. How do liveness probe parameters (initialDelay, periodSeconds,
   failureThreshold) compose with the drain budget to avoid false-positive
   restarts during graceful shutdown?

## Refs

- [../plans/rubric.md](../plans/rubric.md)
- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)
- [07-agent-runtime-spi.md](07-agent-runtime-spi.md)
- [08a-goose-headless-modes.md](08a-goose-headless-modes.md)

TODO(design-gate)
