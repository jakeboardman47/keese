<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - ../../specs/keese.ai-v1alpha1-recipe.md
  - ../../../internal/controller/keese/recipesource_controller.go
  - ../../../internal/controller/keese/runtimeextension_controller.go
related_skills: [plan-management]
status: planned
last_verified: 2026-06-09
phase: EH11
model_tier: sonnet
depends_on: []
agent: test-engineer
outputs:
  - tests/e2e/recipe-source
  - tests/e2e/runtime-extension
---

# EH11 — RecipeSource + RuntimeExtension e2e

**Goal.** `RecipeSource` and `RuntimeExtension` have **no** e2e coverage. Cover
the two shipped reconcilers
`internal/controller/keese/{recipesource,runtimeextension}_controller.go`.

## Deliverables

1. **`tests/e2e/recipe-source/`** — apply a `RecipeSource` (OCI or git per
   `recipesource_git.go`); assert it reaches Ready and the source is
   fetched/cached + `status` reflects the resolved recipes. Assert the
   `finalizers.recipe.keese.ai/cache-cleanup`-style cleanup on delete (check the
   actual finalizer in the controller).
2. **`tests/e2e/runtime-extension/`** — apply a `RuntimeExtension` referencing an
   `AgentRuntime`; assert reconcile + that the `RuntimeExtension`-enabled OpenFGA
   tuple is written on workspace create and removed on teardown (per the runtime
   spec's `ExtensionTupleWritten`/`ExtensionTupleDeleted` events). Assert
   owner-ref to the AgentRuntime + `observedGeneration`.

Prereq-gate via `tests/e2e/lib/check-prereqs.sh`.

## Acceptance

- Both suites green under `make test-e2e` on a bootstrapped cluster; skip cleanly
  on placeholder prereqs.

## Notes for the agent

- Read the controllers + spec for the **actual** finalizer IDs, status fields, and
  event reasons — test shipped reality, not assumptions. If OCI fetch or the
  extension tuple write needs infra not in the bootstrap, fully assert the
  CR-reconcile + status layer, mark the infra step skipped, add a
  `revisit_when_*` trigger, and set `status: shipped-with-stubs`.
- Stay inside your two output dirs + additive `tests/e2e/lib/` (source EH4 helpers).
