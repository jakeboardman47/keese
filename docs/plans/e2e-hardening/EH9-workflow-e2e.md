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
related_skills: [plan-management]
status: shipped-with-stubs
last_verified: 2026-06-09
revisit_when_workflow_run_count_live: true
revisit_when_workflow_triggers_live: true
phase: EH9
model_tier: sonnet
depends_on: []
agent: test-engineer
outputs:
  - tests/e2e/workflow
  - tests/e2e/lib
---

# EH9 — Workflow + WorkflowRun e2e

**Goal.** No suite creates real `Workflow`/`WorkflowRun` CRs — the
`non-interactive-launcher` suite exercises a hand-rolled Job mirror, not the
Workflow controller's Argo projection. Cover the real reconcilers
(`internal/controller/keese/{workflow,workflowrun}_controller.go`).

## Deliverables

A kuttl suite `tests/e2e/workflow/`:

1. **Workflow projection:** apply a `Workflow` CR; assert it reaches Ready and
   projects its Argo artifact (`WorkflowTemplate` / `CronWorkflow` per
   `spec.triggers`) with the expected guardrail/messaging wiring.
2. **WorkflowRun lifecycle:** create (or trigger) a `WorkflowRun`; assert it
   projects an Argo `Workflow`, runs, and reaches a **terminal phase**
   (Succeeded), and that the owning `Workflow.status.runCount` increments.
3. **Concurrency + cascade:** assert the `concurrencyPolicy` (`Allow`/`Forbid`/
   `Replace`) behavior on a second run, and the `finalizers.workflow.keese.ai/
   cascade` cleans up runs on `Workflow` delete.

## Acceptance

- Suite green under `make test-e2e` on a cluster with Argo Workflows (bootstrap
  dep); asserts Argo projection + a `WorkflowRun` Succeeded + cascade.

## Notes for the agent

- Argo Workflows is a `dev/bootstrap/helmfile.yaml` release. If a projection path
  (e.g. a specific trigger/output sink) isn't live, assert the core
  Workflow→Argo→WorkflowRun→Succeeded path fully, mark the extra step skipped, add
  `revisit_when_workflow_triggers_live`, set `status: shipped-with-stubs`.
- Stay inside `tests/e2e/workflow/` + additive `tests/e2e/lib/` helpers.
