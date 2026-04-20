<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: spec
category: contract
depends: [../designs/07-agent-runtime-spi.md, ../designs/18-process-lifecycle.md]
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

# agent-runtime-spi — spec

> **Status: draft.** Authored AFTER all owning design docs reach
> `status: current`. Until then, this is a placeholder.

## Owning design(s)

- [`designs/07-agent-runtime-spi.md`](../designs/07-agent-runtime-spi.md) (status must be current)
- [`designs/18-process-lifecycle.md`](../designs/18-process-lifecycle.md) (status must be current)

## Acceptance test categories (to fill in)

- Interface compliance: goose provider implements all required SPI methods (compile-time).
- Capability matrix: `Capabilities()` returns correct flags for each registered provider.
- apidiff gate: adding a required method to the interface fails apidiff minor-bump check.
- Lifecycle: SIGTERM handler invoked within `terminationGracePeriodSeconds`; exit code 0.
- Checkpoint: session state persisted to SQLite-on-PVC before SIGTERM exit confirmed.

## Ref

- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)

TODO(design-gate)
