<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: runtime
depends: [02-workspace-model.md, 20-api-group-layout.md]
related_skills: []
status: draft
last_verified: 2026-04-19
rollback: TODO — document migration path when status flips to current
---

# 07 — Agent Runtime SPI

> **Status: draft.** This doc is a gate-placeholder. Architect agent
> fills content across 3 rubric iterations (target ≥ 90/100) before
> any spec or controller code references this design.

## Context

_One paragraph. What problem does this design address? The `AgentRuntime` SPI
defines the Go interface contract that all runtime providers (goose first, then
claude-code/aider/etc.) must satisfy. Interface versioning is enforced via apidiff._

## Open questions (must be answered before `status: current`)

1. What are the exact method signatures in the `AgentRuntime` Go interface, and
   which are required vs. optional capabilities?
2. How is the capability matrix (e.g. `SupportsStreaming`, `SupportsSubAgents`,
   `SupportsMCP`) declared and checked at runtime registration?
3. What is the SemVer policy for the SPI interface — when does a new method
   constitute a minor vs. major (breaking) bump, and how is apidiff integrated
   into CI?
4. Who owns the lifecycle of a running runtime process — the workspace controller,
   a dedicated runtime controller, or the agent pod itself?
5. How does the SPI handle a runtime that crashes mid-reconcile — is it the
   controller's responsibility to restart, or is that delegated to K8s restart policy?

## Refs

- [../plans/rubric.md](../plans/rubric.md)
- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)
- [08a-goose-headless-modes.md](08a-goose-headless-modes.md)
- [../specs/runtime.operator.keese.ai-v1alpha1.md](../specs/runtime.operator.keese.ai-v1alpha1.md)
- [../specs/agent-runtime-spi.md](../specs/agent-runtime-spi.md)

TODO(design-gate)
