<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: testing
status: shipped-with-stubs
last_verified: 2026-06-09
phase: EH9
revisit_when_workflow_run_count_live: true
revisit_when_workflow_triggers_live: true
---

# tests/e2e/workflow/

EH9 — end-to-end coverage of the **real** `Workflow` and `WorkflowRun`
reconcilers (`internal/controller/keese/{workflow,workflowrun}_controller.go`),
not the hand-rolled Job mirror that `non-interactive-launcher/` exercises.

Self-contained: provisions its own `wfe2e` Tenant / `wf-e2e` namespace /
`wf-ws` Workspace so it never collides with other `tests/e2e/*` cases on the
shared kind-keese cluster.

## Prerequisites

A bootstrapped cluster with Argo Workflows live (the
`dev/bootstrap/helmfile.yaml` `argo-workflows` release, cluster-scoped
workflow-controller):

```sh
make kind-up && make bootstrap-infra
```

The argosay step image (`argoproj/argosay:v2`) must be pullable from the
node.

## Steps

| Step | Asserts |
|---|---|
| `00-setup` / `00-assert` | Tenant Active + Workspace exists. |
| `01-workflow` / `01-assert` | **Workflow projection** — Workflow Ready/phase=Ready; Argo `WorkflowTemplate argo-wft-wf-demo` projected; Cron trigger → `CronJob keese-wf-wf-demo-cron`. |
| `02-workflowrun` / `02-assert` | **WorkflowRun → Argo** — `status.argoWorkflowName=keese-wfr-wfr-demo`; the Argo `Workflow` exists. |
| `03-assert` | **Terminal Succeeded** — live Argo runs argosay to completion; `syncArgoStatus` back-projects phase=Succeeded + Ready=True. |
| `04-concurrency-setup` / `04-assert` | A long-running `wf-block` run goes Running. |
| `05-concurrency-second` / `05-assert` | **concurrencyPolicy=Forbid** — 2nd run stays Pending, reason `ConcurrentRunForbidden`, no Argo Workflow projected. |
| `06-assert` | `finalizers.workflow.keese.ai/cascade` installed on `wf-demo`. |
| `07-cascade-delete` / `07-assert` | **Cascade GC** — delete `wf-demo`; Workflow + run + owner-ref'd Argo `WorkflowTemplate` all removed. |
| `08-teardown` | Cleans up the `wf-block` concurrency fixtures (re-runnable). |

## Shipped-with-stubs

This suite ships `status: shipped-with-stubs` for one deliberately-skipped
assertion. Both revisit triggers below are also recorded in the suite
frontmatter.

- **`Workflow.status.runCount` increment** (`03-assert.yaml`, skipped).
  The field is defined + printed (`workflow_types.go`) but the live
  `WorkflowReconciler` never assigns it. Asserting `runCount: 1` would fail
  against the real controller, so the terminal-Succeeded path is asserted
  fully and the count is left unverified.
  **revisit_when_workflow_run_count_live**: once the controller counts its
  WorkflowRuns, add `status.runCount: 1` to step 03.

- **Non-Cron trigger projections** (Knative `Trigger`, `HTTPRoute`,
  `NATSSubscription`). Only the **Cron → CronJob** projection is asserted
  here because it has no extra runtime dependency. The Knative/Gateway/KEDA
  backends are not part of the minimal Argo-only precondition this suite
  targets (and `NATSSubscription` is a controller no-op pending the KEDA
  dep-conflict, `workflow_controller.go §228`).
  **revisit_when_workflow_triggers_live**: when Knative Eventing + Gateway
  API + KEDA are in the bootstrap, add per-trigger projection steps.
