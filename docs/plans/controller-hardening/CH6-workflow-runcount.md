<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - ../../specs/keese.ai-v1alpha1-workflow.md
  - ../../specs/keese.ai-v1alpha1-workflow-b.md
  - ../../../internal/controller/keese/workflow_controller.go
  - ../../../internal/controller/keese/workflowrun_controller.go
related_skills: [plan-management, controller-authoring]
status: planned
last_verified: 2026-06-10
phase: CH6
model_tier: sonnet
depends_on: []
agent: controller-author
outputs:
  - internal/controller/keese
---

# CH6 — Write Workflow.status.runCount

**Goal.** `Workflow.status.runCount` is defined on the type and exposed as a
printer column, but the reconciler **never writes it** (EH9 had to skip its
increment assertion). Wire it so it reflects reality.

## Deliverables

1. Compute `status.runCount` in the Workflow reconcile path: count the
   `WorkflowRun`s owned by the `Workflow` (list by owner-ref / label), and write
   it via **Server-Side Apply** (rule 04.7, `fieldOwner = keese-workflow-controller`).
   Decide the semantics from the spec — total runs vs active runs — and match
   `keese.ai-v1alpha1-workflow.md`; if the spec is silent/ambiguous, use **total
   runs created** and note it.
2. Respect rule 04.4 — `status.runCount` is derived state; it must not feed the
   next reconcile decision (no spec/status coupling). Trigger a Workflow requeue
   on WorkflowRun create/delete (owner watch) so the count stays fresh.
3. No `*_types.go` change expected (the field already exists). If you must touch
   types, run `make manifests generate bundle` and commit the artifacts.

## Acceptance

- An envtest case: create a `Workflow`, create N `WorkflowRun`s under it, assert
  `status.runCount == N`; delete one, assert it updates; assert idempotency over
  ≥ 3 reconciles (rule 04.16).
- `CGO_ENABLED=0 go test -race -count=1 -tags=integration ./internal/controller/keese/...`
  green; `make lint` clean.

## Notes for the agent

- SSA-only writes; do not break the existing Workflow→Argo projection or the
  `finalizers.workflow.keese.ai/cascade` path. Stay inside
  `internal/controller/keese/` (workflow / workflowrun controllers + their tests).
- macOS gotcha: `CGO_ENABLED=0`. This unblocks the EH9 `revisit_when_workflow_run_count_live`
  gate — leave a note in your SUMMARY so the e2e step can be un-skipped later.
