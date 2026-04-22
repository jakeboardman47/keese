<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: spec
category: contract
depends:
  - ../designs/03-workflow-argo-delegation.md
  - ../designs/03b-workflow-trigger-projections.md
  - ../designs/03c-workflow-messaging-plane.md
  - ../designs/04b-projected-sa-identity.md
  - ../designs/22-workflow-composition-examples.md
  - ../designs/22-ii-samples.md
  - ../designs/25-cross-tenant-agreement.md
related_skills: [controller-authoring, crd-authoring]
status: current
last_verified: 2026-04-21
regression_lock: false
tests:
  unit: []
  envtest:
    - workflow_projection_idempotency_test.go
    - workflowrun_projection_idempotency_test.go
    - workflowrun_cta_admission_test.go
    - workflowrun_nats_stream_provision_test.go
    - workflow_audience_injection_test.go
    - workflowrun_concurrency_policy_test.go
    - workflowrun_interactive_workspace_reject_test.go
    - workflowrun_retry_budget_exhaustion_test.go
  kuttl:
    - 00-workflow-template-projection
    - 01-workflowrun-argo-delegation
    - 02-workflowrun-cta-missing-reject
    - 03-workflowrun-retry-budget-exhausted
    - 04-workflowrun-concurrent-forbid
    - 05-workflowrun-partial-success
    - 06-workflowrun-nats-stream-lifecycle
    - 07-workflowrun-suspend-resume
    - 08-workflow-trigger-cron
metrics:
  - keese_workflowrun_phase_total{phase,tenant}
  - keese_workflow_projection_duration_seconds
  - keese_workflowrun_retry_budget_exhausted_total{tenant}
  - keese_artifact_backend_missing_total{tenant}
  - keese_workflowrun_concurrency_replace_drain_seconds{tenant}
  - keese_workflow_nats_stream_provision_duration_seconds
  - keese_workflow_cta_check_duration_seconds
  - keese_workflow_audience_injection_total{result}
events:
  - WorkflowProjected
  - WorkflowRunProjected
  - WorkflowRunFailed
  - ArtifactBackendMissing
  - ArtifactSecretFailed
  - RetryBudgetExhausted
  - ArgoStatusSynced
  - TriggerProjected
  - TriggerProjectionFailed
  - ConcurrentRunForbidden
  - ConcurrentRunForced
  - WorkflowNATSStreamProvisioned
  - WorkflowNATSStreamCleaned
  - MissingWorkflowAudience
  - CrossTenantAgreementMissing
  - OutputDeliveryFailed
---

# workflow.operator.keese.ai v1alpha1 — spec

Group: `workflow.operator.keese.ai` · Version: `v1alpha1`
Kinds: **Workflow**, **WorkflowRun** · Namespace: tenant namespace (Workspace's namespace)
Companion: [workflow-v1alpha1-b.md](workflow.operator.keese.ai-v1alpha1-b.md)

## Kind: Workflow

Projects to Argo `WorkflowTemplate` in the same namespace (03 iter-2).
Owner-referenced to `Tenant` CR; deletion cascades to `WorkflowTemplate`.
SSA fieldOwner: `keese-workflow-controller`.

### CRD markers

```go
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=wf
// +kubebuilder:printcolumn:name=Age,type=date,JSONPath=.metadata.creationTimestamp
// +kubebuilder:printcolumn:name=Ready,type=string,JSONPath=.status.conditions[?(@.type=="Ready")].status
// +kubebuilder:printcolumn:name=Phase,type=string,JSONPath=.status.phase
// +kubebuilder:printcolumn:name=RunCount,type=integer,JSONPath=.status.runCount
// +keese:rebac-tuple=workspace:messageable_from
```

### Spec schema

| Field | Type | Required | Default | Notes |
|---|---|---|---|---|
| `spec.workflowTemplateRef.name` | string | yes | — | Argo `WorkflowTemplate` in same namespace; 1:1 |
| `spec.timeout` | string (duration) | no | `24h` | Maps → Argo `activeDeadlineSeconds` |
| `spec.templates[]` | object array | no | — | Step definitions; each may carry `transportRef` |
| `spec.templates[].name` | string | yes | — | Step template name |
| `spec.templates[].transportRef` | LocalObjectRef | no | — | `transport.operator.keese.ai/v1alpha1/Transport`; scope read by CTA check |
| `spec.triggers[]` | object array | no | — | See [03b](../designs/03b-workflow-trigger-projections.md) |
| `spec.outputs[]` | object array | no | — | Delivery sinks; see 22 §Outputs |
| `spec.suspended` | bool | no | `false` | Pauses trigger projection and run admission |

CEL XValidation: `spec.timeout` must parse as Go duration when set.

### Spec → Argo WorkflowTemplate mapping

| keese `Workflow.spec` | Argo `WorkflowTemplate.spec` | Transform |
|---|---|---|
| `workflowTemplateRef.name` | name of the template resource | Projected by name; owner-ref on `WorkflowTemplate` |
| `templates[]` | `templates[]` | 1:1 projection; `transportRef` stripped before projection |
| `timeout` | `activeDeadlineSeconds` | String → seconds |

### Status

```go
type WorkflowStatus struct {
    Phase            string             `json:"phase,omitempty"`
    RunCount         int32              `json:"runCount,omitempty"`
    Conditions       []metav1.Condition `json:"conditions,omitempty"`
    ObservedGeneration int64            `json:"observedGeneration,omitempty"`
}
```

### RBAC + finalizer

```go
// +kubebuilder:rbac:groups=workflow.operator.keese.ai,resources=workflows,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=argoproj.io,resources=workflowtemplates,verbs=get;list;watch;create;update;patch;delete
// Finalizer: finalizers.workflow.operator.keese.ai/workflowtemplate-gc
```

### Acceptance tests — Workflow (≥ 4)

| Suite | Scenario | Assertion |
|---|---|---|
| envtest | `workflow_projection_idempotency_test.go` | `WorkflowTemplate` SSA outcome identical across 3 reconciles with no spec change |
| envtest | `workflow_audience_injection_test.go` | Per-step projected SA token added with `keese-wf-<run-uid>` audience; TTL ≤ 600 s |
| kuttl | `00-workflow-template-projection` | Apply `Workflow`; assert `WorkflowTemplate` exists in same namespace |
| kuttl | `08-workflow-trigger-cron` | Apply cron trigger; assert `CronJob` created in same namespace with `WorkflowRun` jobTemplate |

## Kind: WorkflowRun

Projects to Argo `Workflow`. Per-run namespace = Workspace's namespace (no ephemeral
namespaces; 03 iter-2). SSA fieldOwner: `keese-workflowrun-controller`.

### CRD markers

```go
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=wfr
// +kubebuilder:printcolumn:name=Age,type=date,JSONPath=.metadata.creationTimestamp
// +kubebuilder:printcolumn:name=Ready,type=string,JSONPath=.status.conditions[?(@.type=="Ready")].status
// +kubebuilder:printcolumn:name=Phase,type=string,JSONPath=.status.phase
// +kubebuilder:printcolumn:name=ArgoPhase,type=string,JSONPath=.status.argoPhase
// +keese:rebac-tuple=workspace:messageable_from
```

Companion doc continues: [workflow-v1alpha1-b.md](workflow.operator.keese.ai-v1alpha1-b.md)

## Iteration log

Full rubric tables in [companion](workflow.operator.keese.ai-v1alpha1-b.md) §Iteration log.

- **Iter-1 2026-04-21** — 92.5 REVISE. Gaps: companion not written; HA ceilings absent; Cat 4/5 pre-gate structural.
- **Iter-2 2026-04-21** — 97.5 REVISE. Companion written; 14 failure modes; ≥ 4 envtest + ≥ 4 kuttl per kind; HA ceilings. Cat 4 pre-gate residual.
- **Iter-3 2026-04-21** — 97.5 **SHIP**. Operational readiness: SIGTERM drain, rollback pointer, leader-election IDs confirmed. Cat 4 (−5) pre-gate; all other cats full.
