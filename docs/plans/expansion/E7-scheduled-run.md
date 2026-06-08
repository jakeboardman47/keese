<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - E2-a2a-protocol.md
  - ../../../api/keese/v1alpha1/workflow_types.go
  - ../../designs/03-workflow-argo-delegation.md
  - ../../designs/03b-workflow-trigger-projections.md
related_skills: [plan-management, crd-authoring, controller-authoring]
status: planned
last_verified: 2026-05-13
phase: E7
model_tier: sonnet
depends_on: [E2]
agent: crd-author
outputs:
  - api/keese/v1alpha1/
  - internal/controller/keese/scheduledrun/
  - config/crd/bases/
  - PROJECT
  - bundle/
---

# E7 — ScheduledRun CRD

**Refinement pass:** correctness & security.
**Effort:** 2 days. **Owner agent:** `crd-author`.

## Goal

Add `ScheduledRun` as a convenience CRD that projects to a `Workflow` with a cron
trigger. Operators set a workspace ref, a prompt string, and a cron schedule; the
controller creates and manages the underlying Workflow. No new scheduling infrastructure
— this wraps existing Argo cron trigger support in design 03b.

## Inputs

- Workflow types:
  [`api/keese/v1alpha1/workflow_types.go`](../../../api/keese/v1alpha1/workflow_types.go)
- Workflow trigger projections:
  [`docs/designs/03b-workflow-trigger-projections.md`](../../designs/03b-workflow-trigger-projections.md)
- Argo Workflows delegation:
  [`docs/designs/03-workflow-argo-delegation.md`](../../designs/03-workflow-argo-delegation.md)

## Tasks

### T1 — `ScheduledRun` CRD

`api/keese/v1alpha1/scheduledrun_types.go`. Namespaced. ShortName `sr`.

Spec:
- `WorkspaceRef corev1.LocalObjectReference` — target Workspace.
- `Prompt string` — prompt text injected as Workflow step input. VAP
  `ScheduledRunPromptLength`: 1–4096 chars.
- `Schedule string` — standard 5-field cron expression (UTC). VAP
  `ScheduledRunCronValid`: CEL validates cron syntax using a regex.
- `ConcurrencyPolicy ConcurrencyPolicy` — enum `Allow|Forbid|Replace` (default `Forbid`).
- `Suspend *bool` — pauses scheduling without deleting.
- `SuccessHistoryLimit *int32` (default 3).
- `FailedHistoryLimit *int32` (default 3).

Status: `ObservedGeneration`, `Phase`, `LastRunAt`, `NextRunAt`, `Conditions`,
`ActiveWorkflowRun string`.

Printer columns: `Schedule`, `Phase`, `LastRun`, `Age`.

### T2 — Reconciler

`internal/controller/keese/scheduledrun_controller.go`. Reconcile loop:
1. Validate cron expression.
2. SSA-apply a `Workflow` CR with `spec.triggers[0].cron.schedule = Schedule` and
   `spec.inputs[0].prompt = Prompt`.
3. Set owner reference on the Workflow (propagates delete).
4. Watch `WorkflowRun` children for `LastRunAt` / `NextRunAt` status updates.
5. Apply `ConcurrencyPolicy`: if `Forbid`, block new Workflow trigger if an active
   `WorkflowRun` exists.

FieldOwner: `keese-scheduledrun-controller`.

### T3 — Sample + VAPs

`config/samples/scheduledrun_v1alpha1_scheduledrun.yaml` — fires every 5 minutes,
prompt: `"Summarize today's workspace activity."`. Passes dry-run.

VAPs: `ScheduledRunPromptLength`, `ScheduledRunCronValid`.

### T4 — Envtest suite

- `TestScheduledRun_WorkflowCreated`: applying a ScheduledRun creates a Workflow with
  cron trigger.
- `TestScheduledRun_SuspendPreventsRun`: `suspend: true` does not advance `NextRunAt`.
- `TestScheduledRun_ConcurrencyForbid`: second Workflow trigger blocked while first
  WorkflowRun is active.

## Acceptance criteria

- `ScheduledRun` with `*/5 * * * *` creates one `WorkflowRun` per fire.
- `ConcurrencyPolicy: Forbid` blocks overlapping runs.
- `suspend: true` pauses without deletion.
- Envtest suite passes.

## Risks + mitigations

| Risk | Mitigation |
|---|---|
| Argo cron trigger field name drift | Verify against Argo Workflows v3.x API before T2 |
| CEL cron validation is incomplete (5-field only) | Document 7-field cron (with seconds) is unsupported; VAP rejects |
| Owner reference cascade may delete Workflow prematurely | Use `blockOwnerDeletion: false`; ScheduledRun deletion is explicit |

## Refs

- [E2-a2a-protocol.md](E2-a2a-protocol.md)
- [`docs/designs/03b-workflow-trigger-projections.md`](../../designs/03b-workflow-trigger-projections.md)

## Iteration log

### Iteration 1 — 2026-05-13

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | 4 tasks; thin wrapper over existing Workflow |
| 2 | Architecture fit | 10 | 1.0 | 10 | Delegates to Workflow; no new scheduling infra |
| 3 | Security posture | 15 | 1.0 | 15 | Prompt length VAP; no secrets introduced |
| 4 | Automatability | 10 | 1.0 | 10 | make manifests + envtest |
| 5 | Verifiability | 15 | 1.0 | 15 | 3 named envtest tests |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Argo API drift + cron CEL + owner ref |
| 7 | Context efficiency | 10 | 1.0 | 10 | <200 lines |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX + frontmatter |
| 9 | Observability | 5 | 1.0 | 5 | LastRunAt / NextRunAt in status |
| 10 | Operational readiness | 10 | 1.0 | 10 | Suspend + history limits for operational control |
| | **Total** | 100 | | **100** | |

Verdict: SHIP

Top gaps: none blocking. Argo cron field name is an implementation-time lookup.

### Iteration 2 — 2026-05-13

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Stable |
| 2 | Architecture fit | 10 | 1.0 | 10 | Stable |
| 3 | Security posture | 15 | 1.0 | 15 | Stable |
| 4 | Automatability | 10 | 1.0 | 10 | Stable |
| 5 | Verifiability | 15 | 1.0 | 15 | Stable |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Stable |
| 7 | Context efficiency | 10 | 1.0 | 10 | Stable |
| 8 | Docs quality | 5 | 1.0 | 5 | Stable |
| 9 | Observability | 5 | 1.0 | 5 | Stable |
| 10 | Operational readiness | 10 | 1.0 | 10 | Stable |
| | **Total** | 100 | | **100** | |

Verdict: SHIP

### Iteration 3 — 2026-05-13

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Stable |
| 2 | Architecture fit | 10 | 1.0 | 10 | Stable |
| 3 | Security posture | 15 | 1.0 | 15 | Stable |
| 4 | Automatability | 10 | 1.0 | 10 | Stable |
| 5 | Verifiability | 15 | 1.0 | 15 | Stable |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Stable |
| 7 | Context efficiency | 10 | 1.0 | 10 | Stable |
| 8 | Docs quality | 5 | 1.0 | 5 | Stable |
| 9 | Observability | 5 | 1.0 | 5 | Stable |
| 10 | Operational readiness | 10 | 1.0 | 10 | Stable |
| | **Total** | 100 | | **100** | |

Verdict: SHIP
