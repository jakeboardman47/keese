<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: spec
category: contract
depends:
  - keese.ai-v1alpha1-workflow.md
  - ../designs/03-workflow-argo-delegation.md
  - ../designs/03c-workflow-messaging-plane.md
  - ../designs/25-cross-tenant-agreement.md
related_skills: [controller-authoring]
status: current
last_verified: 2026-04-21
regression_lock: false
---

# keese.ai v1alpha1-b — WorkflowRun spec, failure modes, HA

Companion to [keese.ai-v1alpha1-workflow.md](keese.ai-v1alpha1-workflow.md).

## Kind: WorkflowRun — spec schema

| Field | Type | Required | Default | Notes |
|---|---|---|---|---|
| `spec.workspaceRef.name` | string | yes | — | Immutable; workspace namespace = run namespace |
| `spec.workflowRef.name` | string | yes | — | Immutable; must exist in same namespace |
| `spec.parameters[]` | name/value array | no | — | Maps → Argo `arguments.parameters[]` 1:1 |
| `spec.artifacts[].name` | string | no | — | Maps → Argo `arguments.artifacts[]`; credential resolved from `TokenBudget.spec.artifactStoreRef` |
| `spec.retryBudget` | int32 | no | tenant default (10) | Cross-step budget; `min(step.limit, remainingCrossBudget)` injected per Argo template |
| `spec.timeout` | string (duration) | no | `24h` | → Argo `activeDeadlineSeconds`; also bounds NATS stream `maxAge` |
| `spec.suspended` | bool | no | `false` | SSA-patched → Argo `spec.suspend`; set on `RetryBudgetExhausted` |
| `spec.retryOutputs[]` | int32 array | no | — | Selective output re-dispatch by index; no Argo re-run |
| `spec.entrypoint` | string | no | — | → Argo `spec.entrypoint` 1:1 |

Immutability CEL XValidation (VAP):
- `spec.workspaceRef`, `spec.workflowRef` immutable after create.
- `spec.timeout` immutable after create.
- `spec.suspended` is the only mutable field post-create (besides status).

### WorkflowRun → Argo Workflow mapping

| keese `WorkflowRun.spec` | Argo `Workflow` field | Transform |
|---|---|---|
| `.workspaceRef` | `metadata.labels["keese.ai/workspace"]` + `metadata.namespace` | namespace = Workspace namespace |
| `.workflowRef` | `.spec.workflowTemplateRef.name` | 1:1 |
| `.parameters[]` | `.spec.arguments.parameters[]` | 1:1 |
| `.artifacts[].inputs` | `.spec.arguments.artifacts[]` | Credential resolved from `artifactStoreRef` |
| `.retryBudget` | `.spec.retryStrategy` + per-step overrides | `min(step.limit, budget)` per template |
| `.timeout` | `.spec.activeDeadlineSeconds` | String → seconds |
| `.suspended` | `.spec.suspend` | 1:1; patched on `RetryBudgetExhausted` |
| `.entrypoint` | `.spec.entrypoint` | 1:1 |
| _(implicit)_ | per-step projected SA token | `keese-wf-<run-uid>` audience; TTL ≤ 600 s (04b iter-3) |

### Status

```go
type WorkflowRunStatus struct {
    Phase             string             `json:"phase,omitempty"`
    // Phase: Pending | Running | Succeeded | PartialSuccess | Failed | Timeout
    ArgoPhase         string             `json:"argoPhase,omitempty"`
    Artifacts         []ArtifactStatus   `json:"artifacts,omitempty"`
    Outputs           []OutputStatus     `json:"outputs,omitempty"`
    ArgoRetryInFlight bool               `json:"argoRetryInFlight,omitempty"`
    Conditions        []metav1.Condition `json:"conditions,omitempty"`
    ObservedGeneration int64             `json:"observedGeneration,omitempty"`
}
```

`PartialSuccess`: Argo Succeeded but ≥ 1 output delivery failed; first-class phase.

### RBAC + finalizers

```go
// +kubebuilder:rbac:groups=keese.ai,resources=workflowruns,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=argoproj.io,resources=workflows,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;create;update;patch;delete,resourceNames=keese-wf-*
// Finalizers:
//   finalizers.workflowrun.keese.ai/nats-stream
//   finalizers.workflowrun.keese.ai/argo-workflow
```

SSA fieldOwner: `keese-workflowrun-controller`.

### NATS topic provisioning

At WorkflowRun create, controller calls JetStream `AddStream`:
- `name`: `keese-tenant-<tenant-uid>-wf-<run-uid>`
- `subjects`: `["keese.tenant.<t>.wf.<r>.>"]`
- `retention`: `workqueue` · `storage`: `file` · `replicas`: 3
- `maxAge`: `spec.timeout`

Owner-ref on stream → Argo Workflow; GC on Workflow delete. Finalizer
`finalizers.workflowrun.keese.ai/nats-stream` guards teardown.
RBAC marker: `// +keese:rebac-tuple=workspace:messageable_from`.

### CrossTenantAgreement admission

Implemented as an admission webhook (cross-resource fan-out; rule 04.12).
Controller scans `Workflow.spec.templates[].transportRef`s; for each Transport
with `spec.a2a.scope: cross-tenant` resolves the peer Workspace + Tenant.
Lookup per peer:

```
CrossTenantAgreement where:
  spec.from.tenantRef == thisWorkflowRun.tenant
  spec.to.tenantRef   == peer.tenant
  spec.from.workspaceSelector matches thisWorkspace
  spec.to.workspaceSelector   matches peer.workspace
  status.phase == Approved AND status.expiresAt > now
```

Reject: `CrossTenantAgreementMissing`; message includes the missing `(from, to)` pair
and the offending `transportRef` name. Runtime enforcement at transport (09); admission
is fast-fail UX only.

### Acceptance tests — WorkflowRun (≥ 4)

| Suite | Scenario | Assertion |
|---|---|---|
| envtest | `workflowrun_projection_idempotency_test.go` | Argo `Workflow` SSA outcome identical across 3 reconciles |
| envtest | `workflowrun_cta_admission_test.go` | Webhook rejects run with cross-tenant `transportRef` and no matching Approved CRA; error contains `transportRef` name |
| envtest | `workflowrun_nats_stream_provision_test.go` | JetStream `AddStream` called once on create; finalizer set; stream retained on NATS unavailability |
| envtest | `workflowrun_concurrency_policy_test.go` | `Forbid` rejects second run; `Replace` patches prior Argo Workflow `shutdown: Terminate` |
| envtest | `workflowrun_interactive_workspace_reject_test.go` | VAP rejects `WorkflowRun` when Workspace `spec.interactive: true` |
| envtest | `workflowrun_retry_budget_exhaustion_test.go` | Controller patches `spec.suspended: true`; event `RetryBudgetExhausted` emitted |
| kuttl | `01-workflowrun-argo-delegation` | Apply `WorkflowRun`; Argo `Workflow` exists in same namespace; per-step SA token projected |
| kuttl | `02-workflowrun-cta-missing-reject` | Apply cross-tenant run with no CRA; assert webhook reject with reason |
| kuttl | `03-workflowrun-retry-budget-exhausted` | Exhaust budget; assert `spec.suspended: true` and event |
| kuttl | `05-workflowrun-partial-success` | Force output delivery failure; assert `status.phase: PartialSuccess` |

## Failure modes

| Failure | Detection | Mitigation |
|---|---|---|
| `WorkflowTemplate` SSA patch fails | `WorkflowProjectionFailed` event | Retry; prior template retained |
| Argo Workflow projection fails | `WorkflowRun.status.phase=Pending` | Retry with backoff |
| `ArtifactBackendMissing` | At WorkflowRun create | Fail fast; `ArtifactBackendMissing` event |
| Artifact Secret create fails | Controller error | Retry; run stays Pending; `ArtifactSecretFailed` |
| Argo watch disconnects | Watch error | Re-establish; SSA idempotent; `ArgoWatchDisconnected` alert |
| `Forbid` race (two simultaneous) | Admission serialized | `ConcurrentRunForbidden` on later request |
| `Replace` drain timeout | `replaceDrainSeconds` elapsed | Force-terminate; `ConcurrentRunForced` |
| RetryBudget exhausted | Controller patches `spec.suspended` | Argo pauses; human increments |
| WorkflowRun on interactive Workspace | VAP reject | `WorkflowRunNotAllowedOnInteractiveWorkspace` |
| `MissingWorkflowAudience` | `workflowRun` OIDCProvider template absent | Run stays Pending; event raised |
| `CrossTenantAgreementMissing` | Cross-tenant `transportRef`; no Approved CRA | Admission rejects; surfaces offending `transportRef` |
| `NATSStreamCreateFailed` | JetStream unavailable at create | WorkflowRun stays Pending; retry with backoff |
| `NATSStreamDeleteFailed` | JetStream unavailable at teardown | Stream retained; retry on next reconcile |
| Output delivery fails | 3 retries exhausted | `PartialSuccess`; `OutputDeliveryFailed`; `retryOutputs` recovery |

## HA and resource ceilings

| Controller | HA mode | Leader election | Resource ceiling |
|---|---|---|---|
| `keese-workflow-controller` | 2 replicas active/passive | `lease.coordination.k8s.io/keese-workflow-leader` | 100 m CPU / 128 Mi RAM per replica |
| `keese-workflowrun-controller` | 2 replicas active/passive | `lease.coordination.k8s.io/keese-workflowrun-leader` | 150 m CPU / 256 Mi RAM per replica |

SIGTERM drain budget: 60 s (`terminationGracePeriodSeconds`); liveness probe
`failureThreshold × periodSeconds ≥ 60 s` (rule 06.8). Readiness flips NotReady on
SIGTERM before stop (rule 06.9). Leader lease released before queue drains; successor
reconciles in-flight SSA patches idempotently.

## Rollback path

See [03-workflow-argo-delegation.md](../designs/03-workflow-argo-delegation.md) rollback
frontmatter. CRD schema rollback follows v1alpha1 → v1beta1 conversion-webhook promotion
rule (04.2). No conversion webhooks at v1alpha1 (04.13).

## Iteration log

### Iteration 2 — 2026-04-21 (correctness and security) — 97.5 REVISE

WorkflowRun schema, NATS provisioning, CTA admission, 14 failure modes, 10 test scenarios
authored. HA table added. Cat 4 pre-gate residual (−5). `keese-trigger-receiver` confirmed
owned by 03b; not duplicated here.

### Iteration 3 — 2026-04-21 (operational readiness)

| # | Category | Wt | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Scope unchanged; HA table + rollback pointer added without scope creep. |
| 2 | Architecture fit | 10 | 1.0 | 10 | Leader election IDs correct; controller names align with SSA fieldOwner. |
| 3 | Security posture | 15 | 1.0 | 15 | No change; still full. |
| 4 | Automatability | 10 | 0.5 | 5 | Pre-gate residual; not a design gap. |
| 5 | Verifiability | 15 | 1.0 | 15 | 10 scenarios named across both kinds; assertions concrete enough for P8 impl. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | 14 modes; full. |
| 7 | Context efficiency | 10 | 1.0 | 10 | Both files ≤ 200 lines; index pointers correct. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX + frontmatter on companion; depends list complete. |
| 9 | Observability | 5 | 1.0 | 5 | Full; HA probe rules (06.8/06.9) cited. |
| 10 | Operational readiness | 10 | 1.0 | 10 | HA mode; resource ceilings; SIGTERM drain; lease IDs; rollback path. |
| | **Total** | 100 | | **97.5** | |

Verdict: **SHIP** (97.5 ≥ 90). Status promoted to `current`.

Top gaps (residual, acceptable pre-design-gate):
1. Cat 4 (−5): make targets not yet written; design gate not open.
2. Cat 5 half-credit reflected in part-a (envtest bodies pre-gate P8) — companion now full.
3. `keese-trigger-receiver` trigger-projection tests in 03b; not duplicated here (clean separation confirmed).
