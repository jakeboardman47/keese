<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: spec
category: contract
depends: [../designs/07-agent-runtime-spi.md, ../designs/08a-goose-headless-modes.md]
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

# runtime.operator.keese.ai v1alpha1 — spec

> **Status: draft.** Authored AFTER all owning design docs reach
> `status: current`. Until then, this is a placeholder.

## Owning design(s)

- [`designs/07-agent-runtime-spi.md`](../designs/07-agent-runtime-spi.md) (status must be current)
- [`designs/08a-goose-headless-modes.md`](../designs/08a-goose-headless-modes.md) (status must be current)
- [`designs/08b-goose-acp-stdio-k8s.md`](../designs/08b-goose-acp-stdio-k8s.md) (status must be current)
- [`designs/08c-goose-subagents-limits.md`](../designs/08c-goose-subagents-limits.md) (status must be current)

## Acceptance test categories (to fill in)

- Schema validation: `AgentRuntime.spec.provider` required; `RuntimeExtension` ref valid.
- Controller idempotency: runtime pod stable across 3 reconcile passes.
- Admission: VAP rejects `kind` field mutation after creation.
- SPI capability check: goose runtime satisfies all required interface methods.
- Sub-agent limit: 11th sub-agent spawn rejected with correct event.

## Ref

- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)

TODO(design-gate)
