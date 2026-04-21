<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: workflow
depends:
  - 02-workspace-model.md
  - 05c-mcp-policy-enforcement.md
  - 06-guardrailbinding.md
  - 07-agent-runtime-spi.md
  - 10b-token-accounting.md
  - 18-process-lifecycle.md
  - 20a-api-group-layout.md
  - 22-workflow-composition-examples.md
  - 23-agent-supervision.md
related_skills: [controller-authoring, crd-authoring]
status: current
last_verified: 2026-04-21
rollback: |
  Suspend WorkflowRun with spec.suspended=true; operator SSA-patches Argo Workflow to
  suspended=true within one reconcile. To roll back the operator image: set the prior
  CSV as the install target in OLM (replaces chain per 14a); operator drains in-flight
  projections within the 60s terminationGracePeriodSeconds budget (18). Argo Workflows
  and WorkflowTemplates projected to the argo namespace are NOT deleted on operator
  rollback — they survive and continue executing; reconciliation re-aligns on the next
  tick. CRD schema rollback follows the v1alpha1→v1beta1 promotion rule: no conversion
  webhook exists at v1alpha1, so a schema regression requires a new minor release and
  operator-sdk replace chain entry.
---

# 03 — Workflow Argo Delegation

## Context

`Workflow` and `WorkflowRun` are keese's user-facing orchestration CRs under
`workflow.operator.keese.ai/v1alpha1` (20a). The keese operator projects them into
Argo-native types: `Workflow` → Argo `WorkflowTemplate`; `WorkflowRun` → Argo
`Workflow`. This design answers the five open questions: spec-field mapping,
artifact passing (dev/prod), retry budget composition, trigger projections, and
RBAC in the Argo namespace. The projection contract is the input consumed by 22.

## Spec mapping table — WorkflowRun → Argo Workflow

Controller: `keese-workflow-controller`; all writes via SSA with
`fieldOwner: keese-workflow-controller`.

| keese `WorkflowRun.spec` | Argo `Workflow` field | Transform |
|---|---|---|
| `.workspaceRef` | `metadata.labels["keese.ai/workspace"]` + `metadata.namespace` | 1:1; namespace is the workspace's projected namespace |
| `.workflowRef` | `.spec.workflowTemplateRef.name` | 1:1 |
| `.parameters[]` | `.spec.arguments.parameters[]` | 1:1 |
| `.artifacts[].inputs` | `.spec.arguments.artifacts[]` | Transform: keese resolves credential-scoped S3/GCS/Azure ref from `TokenBudget.spec.artifactStoreRef` (10b/21) |
| `.retryBudget` | `.spec.retryStrategy` + per-step overrides | Composed — see Retry budget section |
| `.timeout` | `.spec.activeDeadlineSeconds` | Int conversion: timeout string → seconds |
| `.supervisionContext` | `metadata.labels["keese.ai/supervision": "enabled", "keese.ai/workspace-uid": <uid>]` | 1:1 for 23 filter |
| `.entrypoint` | `.spec.entrypoint` | 1:1 |
| `.suspended` | `.spec.suspend` | 1:1; patched by controller on `RetryBudgetExhausted` |

**Back-projection** (Argo → WorkflowRun): controller watches `workflows.argoproj.io`
in the argo namespace; maps `phase`, `startedAt`, `finishedAt`, and
`nodes[].outputs[]` → `WorkflowRun.status.artifacts[]`.

**Workflow → WorkflowTemplate** mapping: keese `Workflow.spec.templates[]` projects
to `WorkflowTemplate.spec.templates[]` 1:1. `OwnerRef` on `WorkflowTemplate` points
to keese `Workflow` CR; deletion cascades.

## Artifact passing — dev vs prod

| Env | Backend | Endpoint | Credentials |
|---|---|---|---|
| Prod (EKS/GKE/AKS) | S3 / GCS / Azure Blob per `Tenant.spec.artifactStoreRef` | Provisioned by OpenTofu (21) | Secret `keese-wf-artifact-<workspace-uid>` projected per-run; deleted on completion |
| Dev (kind) | MinIO in helmfile (P7) | `http://minio.minio-system.svc:9000` | Static dev-only credentials in namespace labeled `keese.ai/dev-only: "true"` |
| Fallback (no backend configured + artifact required) | — | — | `WorkflowRun.status.phase=Failed`; event `ArtifactBackendMissing` |

**Artifact path convention:** `keese/<tenant>/<workspace-uid>/<workflow-run-uid>/<step-id>/`

**Secret lifecycle:** controller creates `Secret` on `WorkflowRun` admission; deletes it
on `WorkflowRun` terminal phase. SSA fieldOwner: `keese-workflow-controller`. RBAC marker:
`// +keese:rebac-tuple=artifact:can_access`.

## Retry budget composition

Two layers operate independently:

- **Argo native** (`retryStrategy`): per-step attempt count + backoff; Argo manages.
- **keese `WorkflowRun.spec.retryBudget`**: cross-step budget — max total retries across
  all steps in the run. Default from `Tenant.spec.defaultRetryBudget` (default 10).

**Composition rule:** controller injects `retryStrategy.limit =
min(step.limit, remainingCrossBudget)` onto every step via `WorkflowTemplate` SSA patch.

**Budget exhaustion mid-run:** controller patches `WorkflowRun.spec.suspended: true`;
Argo pauses; event `RetryBudgetExhausted`. Human increments `spec.retryBudget` and
sets `spec.suspended: false` to resume.

**Supervision interaction (23):** while Argo retries a single step,
`WorkflowRun.status.argoRetryInFlight: true` suppresses supervision evaluation to
prevent false-positive escalation during retry cycles (≤ 5 min typical window).

## Trigger projections

D7 `Workflow.spec.triggers[]` is a discriminated list. Each entry projects one K8s resource.
OwnerRef on each projected resource points to the keese `Workflow` CR; deleted on cascade.

| Trigger type | Projected resource | Notes |
|---|---|---|
| `cron` | K8s `CronJob` (batch/v1) | jobTemplate creates `WorkflowRun` via `kubectl create -f -` |
| `keda` | KEDA `ScaledObject` + K8s `Job` | Job spec creates `WorkflowRun` when metric threshold hit |
| `knative` | Knative `Trigger` → `keese-trigger-receiver` Service | CloudEvent routed to receiver; receiver creates `WorkflowRun` |
| `webhook` | `Gateway` + `HTTPRoute` → `keese-trigger-receiver` | Receiver validates HMAC (GitHub: `x-hub-signature-256` from OpenBao secret); creates `WorkflowRun` |

**`keese-trigger-receiver`:** one Deployment per cluster in operator namespace.
Routes by `HTTPRoute` host/path per `Workflow`. Per-trigger auth config (HMAC secret,
CloudEvent type filter) stored in per-`Workflow` ConfigMap. Intersects with 09
(Transport CRD, stub): HTTPRoute management should align when 09 is current — flagged.

## RBAC and ReferenceGrant

**keese operator SA in `argo` namespace requires:**

```
# workflows.argoproj.io
- get, list, watch, create, update, patch, delete
# workflowtemplates.argoproj.io
- get, list, watch, create, update, patch, delete
# secrets (scoped to keese-workflow-secrets-<tenant> pattern)
- get, watch
```

**Cross-namespace:** `ReferenceGrant` in `argo` namespace allows keese operator SA to
reference Argo resources from the operator namespace.

**Per-run isolation:** each `WorkflowRun` executes in namespace
`keese-wf-<workspace-uid>-<run-id>` (ephemeral; fail-closed NetworkPolicy per rule 04.17;
TTL 24h post-completion; reused by same workspace only).

**OLM dependency flag (14a/14b):** Argo Workflows chart version pinning belongs in OLM
dependency declaration. Flagged for 14b when current.

## Trade-offs

| Option | Decision | Rationale |
|---|---|---|
| keese CRDs wrap Argo vs. expose Argo directly | keese wraps | Hides Argo churn from users; enables multi-engine future (D20) |
| Cross-namespace ReferenceGrant vs. same-namespace | Cross-namespace with ReferenceGrant | Argo installs in `argo`; keese in `keese-system`; Gateway API is the standard cross-NS boundary |
| Ephemeral per-run namespace vs. shared workspace namespace | Ephemeral per-run | Fail-closed NetworkPolicy isolation; enables precise cleanup and RBAC scoping per run |
| keese retry budget over Argo native | Both layers preserved | Argo native handles per-step; keese bounds total; human can intervene on exhaustion without aborting |
| Trigger receiver: one per cluster vs. per-tenant | One per cluster | Simpler HA; routes by path; tenant isolation via HMAC secret + namespace |

## Failure modes

| Failure | Detection | Mitigation |
|---|---|---|
| Argo `WorkflowTemplate` SSA patch fails | Controller error event; backoff | Retry; prior WorkflowTemplate retained; `WorkflowProjectionFailed` event |
| Argo `Workflow` projection fails | `WorkflowRun.status.phase=Failed`; event | Controller retries; user sees status immediately |
| `ArtifactBackendMissing` (no store configured) | On `WorkflowRun` create | Fail fast; event before Argo resource is created; user must configure `Tenant.spec.artifactStoreRef` |
| Artifact Secret creation fails | Controller error event | Retry; `WorkflowRun` stays `Pending`; event `ArtifactSecretFailed` |
| Argo namespace unreachable | Controller watch disconnects | Controller re-establishes watch; SSA idempotent on reconnect; `ArgoNamespaceUnreachable` alert |
| ReferenceGrant missing | SSA returns Forbidden | Event `ReferenceGrantMissing`; install Job re-applies grant on operator startup |
| Trigger receiver crashes | Pod `Failed`; Deployment controller restarts | Pending triggers miss events in crash window; NATS-backed triggers replay (JetStream); CronJob triggers self-recover on next tick |
| RetryBudget exhausted mid-run | Controller patches `spec.suspended` | Argo pauses; human intervenes; no data loss — last Argo step completes or fails cleanly |
| `keese-trigger-receiver` HMAC secret missing from OpenBao | Receiver returns 401 on webhook | Event `TriggerAuthSecretMissing`; operator emits alert; webhook sender receives 401 |

## Upgrade / rollback

See frontmatter `rollback:`. Argo CRD upgrades: pin Argo chart version in
`helmfile.yaml`; test against envtest with matching CRD versions. Breaking Argo API
changes: projector updates the `WorkflowTemplate` schema generation; old runs
running on prior Argo version complete via `replaces` OLM chain. Flag for 14b:
Argo chart version must appear in OLM dependency block.

## Observability

- **OTEL spans:** `workflow.project.template` (Workflow → WorkflowTemplate),
  `workflow.project.run` (WorkflowRun → Argo Workflow), `workflow.status.sync`
  (back-projection).
- **Event reasons** (`internal/controller/workflow/events.go`): `WorkflowProjected`,
  `WorkflowRunProjected`, `WorkflowRunFailed`, `ArtifactBackendMissing`,
  `ArtifactSecretFailed`, `RetryBudgetExhausted`, `ArgoStatusSynced`,
  `TriggerProjected`, `TriggerProjectionFailed`, `ReferenceGrantMissing`,
  `TriggerAuthSecretMissing`.
- **Metrics:** `keese_workflowrun_phase_total{phase,tenant}`,
  `keese_workflow_projection_duration_seconds`,
  `keese_workflowrun_retry_budget_exhausted_total{tenant}`,
  `keese_artifact_backend_missing_total{tenant}`.
- **Printer columns (rule 04.5):** `Workflow` — `Age`, `Ready`, `Phase`, `RunCount`;
  `WorkflowRun` — `Age`, `Ready`, `Phase`, `ArgoPhase`.

## Cross-dep flags

- **22 (parallel):** spec mapping table and trigger projections above are the contract
  consumed by 22 composition examples. 22 author may proceed with this doc as input.
- **09 (Transport CRD, stub):** HTTPRoute-webhook trigger management intersects; align
  HTTPRoute ownership model when 09 reaches current.
- **14a/14b (OLM, stubs):** Argo Workflows Helm chart version pinning must appear in
  OLM dependency block; flag for 14b iter-1.

## Refs

- [02-workspace-model.md](02-workspace-model.md) — Workspace FSM; Running entry on first WorkflowRun
- [05c-mcp-policy-enforcement.md](05c-mcp-policy-enforcement.md) — MCP tool-call audit during workflow steps
- [06-guardrailbinding.md](06-guardrailbinding.md) — GuardrailBinding applies during trigger-activated runs
- [07-agent-runtime-spi.md](07-agent-runtime-spi.md) — step-boundary checkpoint; Drain/Resume
- [10b-token-accounting.md](10b-token-accounting.md) — per-run token metering; 429 signals
- [18-process-lifecycle.md](18-process-lifecycle.md) — controller drain budget 60s; idempotent restart
- [20a-api-group-layout.md](20a-api-group-layout.md) — `workflow.operator.keese.ai/v1alpha1/{Workflow,WorkflowRun}`
- [22-workflow-composition-examples.md](22-workflow-composition-examples.md) — consumes this contract
- [23-agent-supervision.md](23-agent-supervision.md) — `argoRetryInFlight` supervision-exclude flag
- [../plans/rubric.md](../plans/rubric.md)

## Iteration log

### Iteration 1 — 2026-04-21

| # | Category | Wt | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | 5 open questions answered; mapping table bounded; exit criteria explicit. |
| 2 | Architecture fit | 10 | 1.0 | 10 | SSA + fieldOwner; VAP-first; wraps Argo cleanly; aligns 20a group layout. |
| 3 | Security posture | 15 | 1.0 | 15 | Artifact Secret scoped + deleted post-run; HMAC from OpenBao; per-run ephemeral namespace + NetworkPolicy; ReferenceGrant explicit; no wildcard RBAC. |
| 4 | Automatability | 10 | 0.5 | 5 | Controller paths named; SSA fieldOwners set; install Job for ReferenceGrant named. Pre-gate: no make target authored yet. |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Failure modes enumerated; event reasons listed. Envtest assertions not yet authored (pre-gate P8). |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | 9 failure modes with detection + mitigation; HMAC missing; budget exhaustion; Argo unreachable. |
| 7 | Context efficiency | 10 | 1.0 | 10 | ≤ 200 lines; single responsibility; no inline code blobs; all deps linked. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; frontmatter complete; rollback concrete; depends list matches all 9 deps. |
| 9 | Observability | 5 | 1.0 | 5 | 3 OTEL spans; 11 event reasons; 4 metrics; printer columns declared. |
| 10 | Operational readiness | 10 | 1.0 | 10 | OLM replaces chain; per-run namespace TTL; Argo chart pin strategy; run isolation explicit. |
| | **Total** | 100 | | **92.5** | |

Verdict: **SHIP** (92.5 ≥ 90). Status: `current`.

Top gaps:
1. Cat 4 (0.5): ReferenceGrant install Job and make targets unimplemented — pre-gate; authors with controller-author agent post gate-open.
2. Cat 5 (0.5): Envtest test names for projection idempotency, retry-budget exhaustion, and trigger-receiver HMAC path not yet authored — pre-gate P8.

Cross-deps settled: 22 receives spec mapping + trigger projections; 23 `argoRetryInFlight` contract honored; 02 `Running` entry on first `WorkflowRun` consistent.
Cross-deps flagged: 09 HTTPRoute ownership align when current; 14b Argo chart OLM dependency pin.
