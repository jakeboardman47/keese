<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: runtime
depends: [07-agent-runtime-spi.md]
related_skills: []
status: draft
last_verified: 2026-04-19
rollback: TODO — document migration path when status flips to current
---

# 08a — Goose Headless Modes

> **Status: draft.** This doc is a gate-placeholder. Architect agent
> fills content across 3 rubric iterations (target ≥ 90/100) before
> any spec or controller code references this design.

## Context

_One paragraph. What problem does this design address? Goose supports two
headless modes: `goose run --recipe` for discrete bounded tasks and
`goose serve` (ACP server) for long interactive sessions. This design
covers selection criteria and resource sizing for each mode._

## Open questions (must be answered before `status: current`)

1. What criteria determine whether a `Workspace` uses `goose run --recipe`
   vs. `goose serve` ACP mode — session duration, interactivity flag, or
   explicit `spec.mode` field?
2. What are the CPU/memory resource requests and limits for each mode, and
   how do they differ between dev (kind) and production?
3. How does `goose serve` ACP mode expose its endpoint inside the cluster —
   ClusterIP Service, or only via `kubectl exec` bridge?
4. How is the goose binary version pinned in the `AgentRuntime` CR, and what
   is the upgrade path when a new goose release drops?
5. What structured log fields does goose emit that the OTEL collector must
   parse, and how are they mapped to OTEL span attributes?

## Refs

- [../plans/rubric.md](../plans/rubric.md)
- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)
- [07-agent-runtime-spi.md](07-agent-runtime-spi.md)
- [08b-goose-acp-stdio-k8s.md](08b-goose-acp-stdio-k8s.md)
- [08c-goose-subagents-limits.md](08c-goose-subagents-limits.md)

TODO(design-gate)
