<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - ../../../internal/controller/keese
related_skills: [plan-management, controller-authoring, testing]
status: complete
last_verified: 2026-06-10
phase: CH9
model_tier: sonnet
depends_on: []
agent: test-engineer
outputs:
  - internal/controller/keese
---

# CH9 — Dedup the keese envtest harness (suite won't compile)

**Goal.** `ce2436e` merged 7 controllers into `package keese` but left **7
`*_suite_test.go` files each redeclaring** package-level `func TestControllers`,
`ctx`, `cancel`, `testEnv`, `cfg`, `k8sClient`, and `getFirstFoundEnvTestBinaryDir`.
That's duplicate-symbol — the `keese` package **test binary does not compile**, so
`go test -race -tags=integration ./internal/controller/keese/...` **build-fails on
`main`** (the integration CI job is red on a compile error, and CH6's new envtest
can't run). Surfaced by CH6.

## Deliverables

1. **One shared envtest harness** for `package keese`: a single `suite_test.go`
   with the lone `TestControllers` / `RunSpecs`, the shared `ctx`/`cancel`/
   `testEnv`/`cfg`/`k8sClient`, the env-bin helper, and **one `BeforeSuite` that
   registers every keese reconciler** in the test manager (workspace, workspacesession,
   workspaceshare, memory, recipe, recipesource, runtime, tenancy, transport,
   workflow, workflowrun, …). Load CRDs from `config/crd/bases/`.
2. Remove the duplicate package-level harness declarations from the other
   `*_suite_test.go` files, **keeping their `Describe`/`It` spec blocks** (move the
   specs into per-controller `*_test.go` or keep the files with only their specs).
3. Ensure no cross-suite state bleed (unique namespaces/names per spec; the
   existing suites already mostly do this).

## Acceptance

- `CGO_ENABLED=0 go test -race -count=1 -tags=integration ./internal/controller/keese/...`
  **compiles and is green** — every controller's envtest specs run under the one
  suite (including CH6's `RunCount derivation` spec). `make lint` clean.
- No production code change (test harness only).

## Notes for the agent

- This unblocks the keese integration CI job + CH6's runCount envtest + lets EH9's
  `revisit_when_workflow_run_count_live` flip live. Stay inside
  `internal/controller/keese/`. macOS gotcha: `CGO_ENABLED=0`.
- Watch for per-suite `SetupWithManager` differences (predicates, owner watches —
  e.g. the workflow controller's new `Watches(&WorkflowRun{})` from CH6); register
  each reconciler exactly as its own `SetupWithManager` does.
