<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: workflow
depends:
  - 02-workspace-model.md
  - 03-workflow-argo-delegation.md
  - 03b-workflow-trigger-projections.md
  - 05a-envoy-ai-gateway-topology.md
  - 05c-mcp-policy-enforcement.md
  - 06-guardrailbinding.md
  - 09-transport-crd.md
  - 10a-otel-topology.md
  - 10b-token-accounting.md
related_skills: [doc-authoring]
status: current
last_verified: 2026-04-21
rollback: |
  WorkflowRun is immutable after creation; rollback = re-run with corrected spec.
  For output delivery failures: set WorkflowRun.spec.retryOutputs to failed indices;
  controller re-dispatches only those slots. No CRD migration at v1alpha1.
---

# 22 — Workflow Composition Examples

## Context

`Workflow` composes Argo `WorkflowTemplate` projection with pluggable `.spec.triggers[]`
and `.spec.outputs[]`. This doc demonstrates three canonical patterns and resolves all
open questions from iter-1. Full YAML: [22-ii-samples.md](22-ii-samples.md).

**Interactive restriction.** All samples target non-interactive Workspaces
(`spec.interactive: false`). WorkflowRuns against interactive Workspaces are rejected
by admission per 03 iter-2 (`WorkflowRunNotAllowedOnInteractiveWorkspace`).

**Namespace model (03 iter-2).** Argo `Workflow` CRs and step pods run in the
Workspace's namespace — no ephemeral per-run namespaces. Per-run Secret
`keese-wf-<run-id>-creds` in Workspace namespace, owner-ref'd to Argo Workflow for GC.
Artifact path: `keese/<workspace-uid>/<run-id>/<step>/` in the tenant's configured
S3/GCS/Azure/MinIO backend. Cleanup via `ttlStrategy.secondsAfterCompletion: 604800`
(7 d; tenant-overridable).

## Pattern 1 — Cron-triggered autonomous-dev pipeline

```yaml
apiVersion: workflow.operator.keese.ai/v1alpha1
kind: Workflow
metadata:
  name: autonomous-dev-nightly
  namespace: keese-acme
spec:
  workflowTemplateRef:
    name: nightly-dev-template   # Argo WorkflowTemplate in same namespace (03 iter-2)
  timeout: 2h
  triggers:
    - type: cron
      cron: { schedule: "0 2 * * *", timezone: UTC }
  outputs:
    - type: slack
      slack: { secretRef: { name: slack-webhook-dev }, channel: "#dev-autonomous" }
      on: [Succeeded, Failed, Timeout]
    - type: gh-pr
      ghPR: { repo: "keese-ai/keese", credentialRef: { name: github-pat } }
      on: [Succeeded]
```

Steps: `git-pull`, `analyze-issues`, `implement-fix`, `run-tests`, `open-pr`; each runs
a goose agent. Credentials via `BackendSecurityPolicy` (05b); no keys in env vars (rule 05.2).

On `spec.timeout` expiry: controller cancels via SSA; phase → `Timeout`; artifacts retained; `slack` fires.

**Concurrency — `Forbid` variant.** `Workspace.spec.concurrencyPolicy: Forbid` rejects
a new cron run while any run is in-flight (`ConcurrentRunForbidden`; ignored on
interactive Workspaces). Samples default to `Allow` (02 iter-2).

## Pattern 2 — NATS-fanout with dedup

Fan-out: 1 000 articles → 10 parallel `WorkflowRun` instances.

**Dedup.** Each NATS message carries `Nats-Msg-Id`. Controller records IDs in NATS KV
bucket `keese-wf-delivered-<workflow-name>` (24 h TTL). On replay: key present → ACK,
no run. Key absent → write key → create `WorkflowRun` → ACK. Write-before-create makes
crash failure conservative (missing run, not duplicate). KV unavailable → 30 s NACK +
`NATSDedupUnavailable`; fanout pauses. **Flag (09 stub):** 09 will layer `Transport`
abstraction over the direct Consumer reference.

## Pattern 3 — Webhook-triggered PR review

```yaml
triggers:
  - type: webhook
    webhook:
      path: /webhook/github/pr-reviewer
      events: [pull_request.opened, pull_request.synchronize]
      secretRef: { name: github-hmac-secret }  # projected from OpenBao (11)
```

**RBAC (03 iter-2 correction).** No cluster-level `namespaces: create|delete`. Scoped
to tenant namespaces: `get/list/watch` on `workflows`, `workflowtemplates`,
`cronworkflows` (argoproj.io); `create/update/patch/delete` on `workflows.argoproj.io`;
`get` on Secrets `keese-wf-*`.

**Validation:** Extract `X-Hub-Signature-256`; load HMAC key from mounted file (rule 05.7);
constant-time compare → 401 + `WebhookSignatureMismatch` on mismatch; unknown event type →
200 no-op; on match → create `WorkflowRun` in Workspace namespace; dispatch failure → 500 +
`WorkflowDispatchFailed`.

## Outputs — partial-failure handling

Each output projection executes independently. Retry: 3× exponential (1 s, 4 s, 16 s).
After exhaustion: `status = Failed`; `OutputDeliveryFailed` event; continue to next output.

**`PartialSuccess` — top-level enum (decided iter-2).** Rationale: distinct ops semantics
from `Succeeded` — the user should investigate which outputs failed and may retry just those;
first-class visibility in `kubectl get workflowrun`.

`WorkflowRun.status.phase: Pending | Running | Succeeded | PartialSuccess | Failed | Timeout`

| Argo result | Output results | `WorkflowRun.status.phase` |
|---|---|---|
| Succeeded | all Succeeded | `Succeeded` |
| Succeeded | ≥ 1 Failed | `PartialSuccess` |
| Failed | any | `Failed` |
| Timeout | any | `Timeout` |

**Selective retry:** `WorkflowRun.spec.retryOutputs: [0, 2]`; controller re-dispatches
only those indices.

**Budget-exhaustion (10b).** Gateway 429 (Prometheus counter crossover → NATS KV signal
→ Envoy `local_reply_config`) → Argo retries per `retryStrategy` → `RetryBudgetExhausted`
→ controller patches `WorkflowRun.spec.suspend: true`. Reference 10b; not duplicated here.

## Observability

Root span `keese.workflow.run` (10a Tier 2); attributes: `workflow.name`, `workflow.run.id`,
`keese.tenant`, `keese.workspace`, `trigger.type`. Child spans: `trigger.activate` →
`argo.workflow.start` → `argo.step` (via `ARGO_TRACE_CONTEXT` env var; propagates to MCP
tool calls) → `output.<type>`. Context injected as `metadata.annotations["keese.ai/traceparent"]`.
Audit: ES `keese-workflow-audit-*` (30 d) + Loki (≥ 1 yr). No tokens or bodies (rule 05.10).

## Trade-offs

| Decision | Rationale |
|---|---|
| NATS KV dedup write-before-create | Conservative failure (missing run, not duplicate) |
| `PartialSuccess` top-level enum | First-class ops visibility; distinct retry semantics |
| Constant-time HMAC compare | Prevents timing-oracle on webhook secret |
| `retryOutputs` field | Avoids re-running Argo for output-only failures |
| Same-namespace (03 iter-2) | Narrower RBAC; single NP; no orphan namespaces |

## Failure modes

| Failure | Detection | Mitigation |
|---|---|---|
| Cron trigger missed (restart) | KEDA fallback; missed window logged | Reconcile on restart; idempotency key per window |
| NATS KV unavailable | `NATSDedupUnavailable`; 30 s NACK | Pauses; resumes on recovery |
| Webhook crash mid-validation | GitHub retries | Stateless receiver; event ID → idempotent `WorkflowRun` name |
| Argo `Workflow` create fails | `Pending`; `ArgoDispatchFailed` | Backoff retry; tenant notified |
| Output sink permanently down | 3 retries; `OutputDeliveryFailed` | `PartialSuccess`; `retryOutputs` recovery |
| Budget exhausted (10b) | 429 → `RetryBudgetExhausted` | `spec.suspend: true`; human increments budget |
| Timeout unenforced (crash) | Argo `activeDeadlineSeconds` mirrors keese timeout | Argo kills independently; keese reconciles on restart |

## Upgrade / rollback

Rollback in frontmatter. `WorkflowRun` immutable after creation. `PartialSuccess` is
a new top-level enum — consumers treat unknown values as non-terminal (forward compat).
`workflowTemplateRef.name` confirmed in 03 iter-2; no migration script needed. v1alpha1 →
v1beta1 follows conversion-webhook path (rule 04.2).

## Refs

[02](02-workspace-model.md) · [03](03-workflow-argo-delegation.md) · [03b](03b-workflow-trigger-projections.md) · [05a](05a-envoy-ai-gateway-topology.md) · [05c](05c-mcp-policy-enforcement.md) · [06](06-guardrailbinding.md) · [09](09-transport-crd.md) · [10a](10a-otel-topology.md) · [10b](10b-token-accounting.md) · [22-ii](22-ii-samples.md) · [rubric](../plans/rubric.md)

## Iteration log

Iter-1 (2026-04-21): 92.5 SHIP. Gaps: ephemeral namespace; `workflowTemplateRef` unconfirmed; `PartialSuccess` position open; RBAC included namespace verbs.

### Iteration 2 — 2026-04-21 — score 95 (SHIP)

| # | Category | Wt | Ratio | Score |
|---|---|---:|---:|---:|
| 1 | Scope clarity | 10 | 1.0 | 10 |
| 2 | Architecture fit | 10 | 1.0 | 10 |
| 3 | Security posture | 15 | 1.0 | 15 |
| 4 | Automatability | 10 | 0.5 | 5 |
| 5 | Verifiability | 15 | 0.5 | 7.5 |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 |
| 7 | Context efficiency | 10 | 1.0 | 10 |
| 8 | Docs quality | 5 | 1.0 | 5 |
| 9 | Observability | 5 | 1.0 | 5 |
| 10 | Operational readiness | 10 | 1.0 | 10 |
| | **Total** | 100 | | **95** |

Verdict: **SHIP**. Status: `current`.

Gaps: (1) Cat 4/5 pre-gate — acceptable. (2) 09 stub: NATS Consumer abstraction pending. (3) 11 stub: ExternalSecret rotation path pending.

Cross-deps settled: 03 iter-2 same-namespace + `workflowTemplateRef.name` + RBAC absorbed; 02 iter-2 `concurrencyPolicy` + `interactive` consumed; 10b 429 path + 05a `local_reply_config` referenced.
