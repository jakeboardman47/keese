<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: testing
depends: []
related_skills: []
status: draft
last_verified: 2026-04-19
---

# envtest + kuttl Harness

> **Status: draft.** Stub — fill in after P4 designs reach `status: current`.

## Contents (to expand)

1. **envtest setup** — `setup-envtest use`, `KUBEBUILDER_ASSETS`, CRD install path.
2. **Controller idempotency pattern** — 3-reconcile `Eventually` loop (10s/250ms).
3. **kuttl test layout** — `TestSuite`, `TestStep`, assertion file conventions.
4. **Fake client vs. envtest** — when each is appropriate; fake limitations.
5. **Race detector** — `go test -race` for integration; required before quarantine removal.

## Refs

- [../plans/rubric.md](../plans/rubric.md)
- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)

TODO(design-gate)
