<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: workflow
depends:
  - 02-workspace-model.md          # concurrencyPolicy + interactive fields land in iter-2
  - 04a-openfga-authz-model.md     # iter-5: tenant.allows_messaging + workspace.messageable_from
  - 04b-projected-sa-identity.md   # iter-3: workflowRun audience template required
  - 05c-mcp-policy-enforcement.md
  - 06-guardrailbinding.md
  - 07-agent-runtime-spi.md
  - 09-transport-crd.md            # iter-3: spec.a2a.scope consumes topic-naming + audience contract
  - 10b-token-accounting.md
  - 12-network-isolation.md        # default-deny NP in Workspace namespace now covers Argo pods
  - 18-process-lifecycle.md
  - 20a-api-group-layout.md
  - 22-workflow-composition-examples.md  # iter-2 must absorb same-namespace model
  - 23-agent-supervision.md
  - 25-cross-tenant-agreement.md   # D29: bilateral handshake required for cross-tenant participants
related_skills: [controller-authoring, crd-authoring]
status: current
last_verified: 2026-04-21
rollback: |
  Suspend WorkflowRun with spec.suspended=true; operator SSA-patches Argo Workflow to
  suspended=true within one reconcile. To roll back the operator image: set the prior
  CSV as the install target in OLM (replaces chain per 14a); operator drains in-flight
  projections within the 60s terminationGracePeriodSeconds budget (18). Argo Workflows
  and WorkflowTemplates in the Workspace namespace are NOT deleted on operator rollback
  — they survive and continue executing; reconciliation re-aligns on the next tick.
  Per-run Secrets (keese-wf-<run-id>-creds) are owner-ref'd to the Argo Workflow; GC
  removes them when the Workflow is deleted or after TTL expiry. Per-run JetStream streams
  (keese.tenant.<t>.wf.<r>.>) are owner-ref'd to the Argo Workflow; GC triggers stream
  deletion. CRD schema rollback follows the v1alpha1→v1beta1 promotion rule: no conversion
  webhook exists at v1alpha1.
---

# 03 — Workflow Argo Delegation

## Context

`Workflow` and `WorkflowRun` are keese's user-facing orchestration CRs under
`keese.ai/v1alpha1` (20a). The keese operator projects them into
Argo-native types: `Workflow` → Argo `WorkflowTemplate`; `WorkflowRun` → Argo
`Workflow`. This design answers: spec-field mapping, artifact passing (dev/prod), retry
budget composition, concurrency policy, interactive-workspace mutual exclusion, and RBAC
in the Workspace namespace. Trigger projections: [03b](03b-workflow-trigger-projections.md).

## Namespace model (iter-2 correction)

Argo `Workflow` CRs and step pods run **in the Workspace's namespace**. No ephemeral per-run
namespaces.

| Concern | Detail |
|---|---|
| Argo controller scope | Watches all namespaces labeled `keese.ai/tenant: *` |
| Per-run Secret | `keese-wf-<run-id>-creds` in Workspace namespace; owner-ref to Argo Workflow → GC auto-deletes |
| Artifact path | `keese/<workspace-uid>/<run-id>/<step>/` in tenant S3/GCS/Azure/MinIO backend (21) |
| Parallel run isolation | Distinct Argo Workflow names (`<workflow-name>-<run-id>-<attempt>`); pod name-scoped |
| Cleanup | `ttlStrategy.secondsAfterCompletion: 604800` (7 d default; tenant-overridable); no namespace delete |
| NetworkPolicy | Workspace namespace fail-closed default-deny NP (12) applies to Argo pods automatically |
| RBAC reduction | Operator no longer needs `namespaces: create\|delete` cluster verbs; scoped to `workflows.argoproj.io`, `workflowtemplates.argoproj.io`, `secrets(keese-wf-*)` in tenant namespaces; iter-3 adds NATS JetStream write (see 03c) |
| JetStream stream | `keese.tenant.<t>.wf.<r>.>` provisioned at WorkflowRun create; owner-ref → GC on Workflow delete; `maxAge` matches `WorkflowRun.spec.timeout`; `replicas: 3` |

## Spec mapping table — WorkflowRun → Argo Workflow

Controller: `keese-workflow-controller`; all writes via SSA with
`fieldOwner: keese-workflow-controller`.

| keese `WorkflowRun.spec` | Argo `Workflow` field | Transform |
|---|---|---|
| `.workspaceRef` | `metadata.labels["keese.ai/workspace"]` + `metadata.namespace` | 1:1; namespace = Workspace namespace |
| `.workflowRef` | `.spec.workflowTemplateRef.name` | 1:1 |
| `.parameters[]` | `.spec.arguments.parameters[]` | 1:1 |
| `.artifacts[].inputs` | `.spec.arguments.artifacts[]` | keese resolves credential-scoped ref from `TokenBudget.spec.artifactStoreRef` (10b/21) |
| `.retryBudget` | `.spec.retryStrategy` + per-step overrides | Composed — see Retry budget section |
| `.timeout` | `.spec.activeDeadlineSeconds` | String → seconds |
| `.supervisionContext` | `metadata.labels["keese.ai/supervision": "enabled"]` | 1:1 for 23 filter |
| `.entrypoint` | `.spec.entrypoint` | 1:1 |
| `.suspended` | `.spec.suspend` | 1:1; patched by controller on `RetryBudgetExhausted` |
| _(implicit)_ | per-step `volumes[].projected.sources[].serviceAccountToken` | Adds `workflowRun` audience (`keese-wf-<run-uid>`); requires 04b iter-3 template; TTL ≤ 600s |

**Back-projection** (Argo → WorkflowRun): controller watches `workflows.argoproj.io`
in all tenant namespaces; maps `phase`, `startedAt`, `finishedAt`, and
`nodes[].outputs[]` → `WorkflowRun.status.artifacts[]`.

**Workflow → WorkflowTemplate** mapping: keese `Workflow.spec.templates[]` projects
to `WorkflowTemplate.spec.templates[]` 1:1. OwnerRef on `WorkflowTemplate` points to
keese `Workflow` CR; deletion cascades.

## Artifact passing — dev vs prod

| Env | Backend | Credentials |
|---|---|---|
| Prod | S3/GCS/Azure Blob per `Tenant.spec.artifactStoreRef` (21) | `keese-wf-<run-id>-creds` in Workspace namespace; owner-ref → GC on Workflow deletion |
| Dev (kind) | MinIO (`http://minio.minio-system.svc:9000`) | Static dev-only creds in namespace labeled `keese.ai/dev-only: "true"` |
| Fallback | — | `WorkflowRun.status.phase=Failed`; event `ArtifactBackendMissing` |

Path: `keese/<workspace-uid>/<run-id>/<step-id>/`. Secret SSA fieldOwner: `keese-workflow-controller`. RBAC marker: `// +keese:rebac-tuple=artifact:can_access`.

## Retry budget composition

Two independent layers: Argo native (`retryStrategy`, per-step) and keese `WorkflowRun.spec.retryBudget` (cross-step; default from `Tenant.spec.defaultRetryBudget`, default 10).

**Composition:** `retryStrategy.limit = min(step.limit, remainingCrossBudget)` injected per step via `WorkflowTemplate` SSA patch. Exhaustion: controller patches `spec.suspended: true`; Argo pauses; `RetryBudgetExhausted` event; human increments budget and clears suspended.

**Supervision (23):** `status.argoRetryInFlight: true` suppresses supervision evaluation during retry cycles.

## Concurrency policy (`Workspace.spec.concurrencyPolicy`)

Flag for **02 iter-2**: field `spec.concurrencyPolicy: Allow|Forbid|Replace` (default `Allow`).

| Value | Semantics |
|---|---|
| `Allow` | Multiple WorkflowRuns run concurrently; each maps to its own Argo Workflow. |
| `Forbid` | Admission rejects new run while any is in-flight (`ConcurrentRunForbidden`). |
| `Replace` | Operator patches prior Argo Workflow `spec.shutdown: Terminate`; waits for terminal phase or `replaceDrainSeconds` (default 60 s); then starts new Workflow; force-terminates on timeout (`ConcurrentRunForced`). |

Webhook counts in-flight runs (`status.phase` not in `{Succeeded, Failed, Error}`) and applies policy. Concurrent runs share the same `TokenBudget` CR — no splitting.

## Interactive vs non-interactive Workspaces

`Workspace.spec.interactive` (flag for **02 iter-2**): boolean, immutable after create (VAP), default `false`.

- `true` → attach-path only; WorkflowRuns rejected.
- `false` (default) → WorkflowRun path; attach rejected.

VAP rejects `WorkflowRun` creation when `referenced Workspace.spec.interactive == true` with reason
`WorkflowRunNotAllowedOnInteractiveWorkspace`. Changing path requires a new Workspace.

## Trade-offs

| Decision | Rationale |
|---|---|
| keese wraps Argo (not exposes) | Hides Argo churn; enables multi-engine future (D20) |
| Same-namespace (iter-2) vs. ephemeral per-run | Narrower RBAC; single NP; Capsule consistency; no orphan namespaces; Argo names unique per run |
| concurrencyPolicy Allow/Forbid/Replace | K8s CronJob convention; drain-window prevents unbounded queuing |
| Interactive mutual exclusion via VAP | One type per Workspace; no runtime mode-switch; simplest invariant |
| One trigger receiver per cluster | Simpler HA; tenant isolation via HMAC + namespace |

## Failure modes

| Failure | Detection | Mitigation |
|---|---|---|
| `WorkflowTemplate` SSA patch fails | Controller error event | Retry; prior template retained; `WorkflowProjectionFailed` |
| `Workflow` projection fails | `WorkflowRun.status.phase=Failed` | Controller retries with backoff |
| `ArtifactBackendMissing` | On `WorkflowRun` create | Fail fast before Argo resource created; `ArtifactBackendMissing` event |
| Artifact Secret creation fails | Controller error | Retry; run stays `Pending`; `ArtifactSecretFailed` event |
| Argo watch disconnects | Watch error | Re-establish; SSA idempotent; `ArgoWatchDisconnected` alert |
| `Forbid` race (two simultaneous submits) | Admission serialized | `ConcurrentRunForbidden` on later request |
| `Replace` drain timeout exceeded | `replaceDrainSeconds` elapsed | Force-terminate; `ConcurrentRunForced`; prior artifacts in S3 |
| RetryBudget exhausted | Controller patches `spec.suspended` | Argo pauses; human increments budget; no data loss |
| WorkflowRun on interactive Workspace | VAP reject | `WorkflowRunNotAllowedOnInteractiveWorkspace` |
| `MissingWorkflowAudience` | 04b iter-3 template absent at projection time | Admission fail; WorkflowRun stays Pending; event raised |
| `CrossTenantAgreementMissing` | Cross-tenant participant; no Approved CRA | Admission rejects; surfaces missing pair in message; ReBAC enforcement at transport (09) |
| `NATSStreamCreateFailed` | JetStream unavailable at WorkflowRun create | Controller retries with backoff; WorkflowRun stays Pending |
| `NATSStreamDeleteFailed` | JetStream unavailable at Workflow terminal/deletion | Controller retries; stream retained until next reconcile |

## Observability

- **OTEL spans:** `workflow.project.template`, `workflow.project.run`, `workflow.status.sync`, `workflow.concurrency.replace_drain`, `workflow.nats.stream.provision`, `workflow.cta.check`.
- **Events** (`events.go`): `WorkflowProjected`, `WorkflowRunProjected`, `WorkflowRunFailed`, `ArtifactBackendMissing`, `ArtifactSecretFailed`, `RetryBudgetExhausted`, `ArgoStatusSynced`, `TriggerProjected`, `TriggerProjectionFailed`, `TriggerAuthSecretMissing`, `ConcurrentRunForbidden`, `ConcurrentRunForced`, `WorkflowNATSStreamProvisioned`, `WorkflowNATSStreamCleaned`, `MissingWorkflowAudience`, `CrossTenantAgreementMissing`.
- **Metrics:** `keese_workflowrun_phase_total{phase,tenant}`, `keese_workflow_projection_duration_seconds`, `keese_workflowrun_retry_budget_exhausted_total{tenant}`, `keese_artifact_backend_missing_total{tenant}`, `keese_workflowrun_concurrency_replace_drain_seconds{tenant}`, `keese_workflow_nats_stream_provision_duration_seconds`, `keese_workflow_cta_check_duration_seconds`, `keese_workflow_audience_injection_total{result}`.
- **Printer columns (04.5):** `Workflow` — `Age`, `Ready`, `Phase`, `RunCount`; `WorkflowRun` — `Age`, `Ready`, `Phase`, `ArgoPhase`.

## Cross-dep flags

- **22 iter-2 (required):** absorb same-namespace model; drop ephemeral-namespace references; `workflowTemplateRef` field confirmed.
- **02 iter-2 (required):** add `spec.concurrencyPolicy` + `spec.interactive` to Workspace spec; VAP immutability on `interactive`.
- **03b:** trigger projections split to [03b](03b-workflow-trigger-projections.md).
- **04a iter-5 (LANDED 2026-04-21):** new `tenant.allows_messaging` + `workspace.messageable_from` ReBAC relations enable cross-tenant authz; runtime enforcement delegated to transport (09).
- **04b iter-3 (LANDED 2026-04-21):** `workflowRun` audience template (`keese-wf-<run-uid>`) required for per-step SA-token audience injection; absence raises `MissingWorkflowAudience`.
- **09 iter-3 (LANDED 2026-04-21):** `spec.a2a.scope: intra-tenant | cross-tenant` consumes the NATS topic-naming + audience contract defined in [03c](03c-workflow-messaging-plane.md).
- **D29 / design 25 (stub):** CrossTenantAgreement CRD required for cross-tenant peer admission check. Cross-tenant peers are derived **implicitly** by the controller from `Workflow.spec.templates[]` `transportRef`s with `scope: cross-tenant` (Q2(b) decision 2026-04-21) — no new top-level WorkflowRun spec field is required.
- **14b (stub):** Argo chart version OLM dependency pin.

## Messaging plane responsibilities

NATS topic provisioning, per-WorkflowRun SA audience injection, CrossTenantAgreement admission, and stream teardown are detailed in [03c — Workflow Messaging Plane](03c-workflow-messaging-plane.md).

## Refs

[02](02-workspace-model.md) · [03b](03b-workflow-trigger-projections.md) · [03c](03c-workflow-messaging-plane.md) · [04a](04a-openfga-authz-model.md) · [04b](04b-projected-sa-identity.md) · [05c](05c-mcp-policy-enforcement.md) · [06](06-guardrailbinding.md) · [07](07-agent-runtime-spi.md) · [09](09-transport-crd.md) · [10b](10b-token-accounting.md) · [12](12-network-isolation.md) · [18](18-process-lifecycle.md) · [20a](20a-api-group-layout.md) · [22](22-workflow-composition-examples.md) · [23](23-agent-supervision.md) · [25](25-cross-tenant-agreement.md) · [rubric](../plans/rubric.md)

## Iteration log

### Iteration 1 — 2026-04-21 — score 92.5 (SHIP) — baseline; 5 open questions answered; ephemeral namespace model (corrected in iter-2); Cat 4/5 pre-gate gaps remain.

### Iteration 2 — 2026-04-21 — score 95 (SHIP) — full table in [03c](03c-workflow-messaging-plane.md)

Same-namespace model; RBAC narrowed; concurrencyPolicy + interactive mutual exclusion. Cat 4/5 pre-gate gaps remain. Cross-deps flagged: 02 iter-2, 22 iter-2, 14b, 09.

### Iteration 3 — 2026-04-21 — score 96 (SHIP) — full scoring table + notes in [03c](03c-workflow-messaging-plane.md)

Adds messaging plane responsibilities: NATS topic provisioning, per-run audience injection, CrossTenantAgreement admission (peers derived implicitly from `transportRef`s with `scope: cross-tenant` per Q2(b) 2026-04-21), stream teardown. Depends updated (04a, 04b, 09, 25). Failure modes +4; events +4; metrics +3; OTEL spans +2.
