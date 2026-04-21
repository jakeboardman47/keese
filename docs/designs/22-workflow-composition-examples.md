<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: workflow
depends:
  - 02-workspace-model.md
  - 03-workflow-argo-delegation.md
  - 05c-mcp-policy-enforcement.md
  - 06-guardrailbinding.md
  - 09-transport-crd.md
  - 10a-otel-topology.md
  - 10b-token-accounting.md
related_skills: [doc-authoring]
status: current
last_verified: 2026-04-21
rollback: |
  WorkflowRun is immutable after creation; rollback is re-run with corrected
  Workflow spec. For output delivery failures: set
  WorkflowRun.spec.retryOutputs to the failed indices; controller re-dispatches
  only those projections. No CRD migration needed at v1alpha1; upgrade path
  follows 03 when it reaches current.
---

# 22 — Workflow Composition Examples

## Context

`Workflow` (D7, D20) composes Argo `WorkflowTemplate` projection with pluggable
`.spec.triggers[]` and `.spec.outputs[]`. This design demonstrates three canonical
patterns — cron-triggered autonomous-dev pipeline, NATS-fanout summarizer/reviewer,
and webhook-triggered PR review — and answers the five open questions from the stub:
trigger YAML, NATS dedup, HMAC authentication, partial-output failure, and the
end-to-end OTEL trace contract. Full YAML samples: [22-ii-samples.md](22-ii-samples.md).

## Pattern 1 — Cron-triggered autonomous-dev pipeline

```yaml
apiVersion: workflow.operator.keese.ai/v1alpha1
kind: Workflow
metadata:
  name: autonomous-dev-nightly
  namespace: keese-acme
spec:
  workflowTemplateRef: nightly-dev-template   # Argo WorkflowTemplate name
  timeout: 2h
  triggers:
    - type: cron
      cron:
        schedule: "0 2 * * *"
        timezone: UTC
  outputs:
    - type: slack
      slack:
        secretRef: { name: slack-webhook-dev }
        channel: "#dev-autonomous"
      on: [Succeeded, Failed, Timeout]
    - type: gh-pr
      ghPR:
        repo: "keese-ai/keese"
        credentialRef: { name: github-pat }
      on: [Succeeded]
```

The Argo `WorkflowTemplate` `nightly-dev-template` defines five sequential steps:
`git-pull`, `analyze-issues`, `implement-fix`, `run-tests`, `open-pr`. Each step
runs a goose agent invocation scoped to the tenant workspace. Credentials flow via
`BackendSecurityPolicy` (05b); no keys in pod env vars (rule 05.2).

On `spec.timeout` expiry: the keese controller cancels the Argo `Workflow` via
SSA patch; `WorkflowRun.status.phase` flips to `Timeout`; any completed Argo steps
persist their artifact outputs in S3 (partial preservation). The `slack` output
fires on `Timeout` per the `on` list.

**Cross-dep flag (03 stub):** field name `workflowTemplateRef` is assumed here; 03
must lock this to the exact key name used in `WorkflowRun.spec` → Argo `Workflow`
projection. If 03 renames it, 22 follows.

## Pattern 2 — NATS-fanout with dedup

Fan-out scenario: 1 000 articles → 10 parallel `WorkflowRun` instances, each
summarizing a batch.

**Dedup mechanism.** Each NATS message carries `Nats-Msg-Id`. The keese trigger
controller records delivered IDs in NATS KV bucket `keese-wf-delivered-<workflow-name>`
with 24 h TTL. On replay, the controller reads the bucket before spawning:

- Key present → ACK message; no new `WorkflowRun`.
- Key absent → write key → create `WorkflowRun` → ACK. Order matters: the KV
  write precedes `WorkflowRun` creation so a crash after KV write but before
  `WorkflowRun.create` results in a KV key with no run. On message redeliver, key
  is present → no duplicate.

**At-least-once guarantee:** if the controller crashes between `WorkflowRun.create`
and NATS ACK, the message redelivers. The dedup key is already present → ACK, no
second run. Net effect: exactly-once `WorkflowRun` per message ID.

**KV unavailable:** controller NACKs with 30 s backoff; emits `NATSDedupUnavailable`
event. Fanout pauses; no runs spawned until KV recovers.

**Cross-dep flag (09 stub):** NATS trigger type relies on `Transport` CRD for
Consumer binding. Until 09 reaches `current`, the trigger controller references the
NATS Consumer directly by name; 09 will layer the `Transport` abstraction.

## Pattern 3 — Webhook-triggered PR review

```yaml
triggers:
  - type: webhook
    webhook:
      path: /webhook/github/pr-reviewer      # mounted on keese-trigger-receiver Service
      events: [pull_request.opened, pull_request.synchronize]
      secretRef: { name: github-hmac-secret } # projected from OpenBao (11)
```

**HTTPRoute** (via `keese-trigger-receiver` Service + Gateway API HTTPRoute) exposes
the path. The `keese-trigger-receiver` Pod validates requests:

1. Extract `X-Hub-Signature-256` header and raw body.
2. Load HMAC key from mounted K8s Secret at `/var/run/keese/secrets/github-hmac`
   (rule 05.7 — projected file, not env var).
3. Compute `HMAC-SHA256(key, body)`; constant-time compare. Mismatch → HTTP 401;
   emit `WebhookSignatureMismatch` event.
4. Parse event type; unknown type → HTTP 200 (acknowledged, no-op).
5. On match: create `WorkflowRun` with `spec.parameters.prNumber`, `spec.parameters.sha`.
6. Dispatch failure → HTTP 500; emit `WorkflowDispatchFailed`.

**Secret rotation:** keese controller watches OpenBao path
`keese/tenants/<tenant>/triggers/<workflow-name>/github-secret` via ExternalSecret CR
(D10/11); K8s Secret updates; receiver reads via inotify-watch on the mounted file path.
No service restart required.

**Cross-dep flag (09 stub):** HTTPRoute-webhook trigger coexists with `Transport`
CRD but does not consume it — the receiver is a dedicated Pod, not a Transport type.
This distinction must be confirmed when 09 reaches `current`.

**Cross-dep flag (11 stub):** Secret rotation path above assumes ExternalSecret
projects OpenBao secret to K8s Secret. Until 11 reaches `current`, the rotation
mechanism is manually patched; flag for 11 iter-1.

## Outputs — partial-failure handling

Each output projection executes independently after the Argo `Workflow` completes.
Sequential execution; a failure on one does not skip others.

| Output type | Delivery |
|---|---|
| `slack` | Slack webhook via in-cluster proxy Service (rule 05.5) |
| `gh-pr` | GitHub API via Envoy AI Gateway (credential in BSP) |
| `nats-stream` | JetStream publish; at-least-once |
| `s3` | Artifact copy; idempotent via object key |
| `knative-sink` | CloudEvent to Knative Sink |

**Retry:** each output retries 3× with exponential backoff (1 s, 4 s, 16 s). After
three failures: `WorkflowRun.status.outputs[i].status = Failed`; emit
`OutputDeliveryFailed` event; continue to next output.

**Phase resolution:**

| Argo result | Output results | `WorkflowRun.status.phase` |
|---|---|---|
| Succeeded | all Succeeded | `Succeeded` |
| Succeeded | ≥ 1 Failed | `PartialSuccess` |
| Failed | any | `Failed` |
| Timeout | any | `Timeout` |

**Selective retry:** `WorkflowRun.spec.retryOutputs: [0, 2]` (zero-based indices);
controller re-dispatches only those output slots on next reconcile.

## Observability — one OTEL trace per WorkflowRun

Root span `keese.workflow.run` created at `WorkflowRun` admission (10a Tier 2):

| Attribute | Value |
|---|---|
| `workflow.name` | `Workflow.metadata.name` |
| `workflow.run.id` | `WorkflowRun.metadata.uid` |
| `keese.tenant` | tenant label |
| `keese.workspace` | workspace ref |
| `trigger.type` | `cron`, `nats`, `webhook` |

Child spans (in order):
- `keese.workflow.trigger.activate` — trigger fires; labels vary by type.
- `keese.workflow.argo.workflow.start` — Argo `Workflow` created.
- `keese.workflow.argo.step` — one per Argo step; propagated via `ARGO_TRACE_CONTEXT`
  env var read by the keese Argo executor wrapper; steps propagate to MCP tool calls
  at the gateway layer (05c `keese.mcp.tool_call` child spans).
- `keese.workflow.output.<type>` — one per output slot; `output.index`, `output.type`,
  `output.status` attributes.

Trace context header `traceparent` injected into the Argo `Workflow` spec as
`metadata.annotations["keese.ai/traceparent"]`; executor wrapper reads and sets
as OTEL context before any tool call.

Audit destination: ES `keese-workflow-audit-*` (30-day ILM) + Loki
`{job="keese-workflow", workflow="<name>"}` (≥ 1-year). No tokens or bodies
logged (rule 05.10).

## Trade-offs

| Decision | Rationale |
|---|---|
| NATS KV dedup (write-before-create order) | Write-before-create makes the failure mode conservative (missing run) rather than duplicating; re-triggering is explicit |
| `PartialSuccess` phase enum | Distinguishes Argo success from full delivery success; avoids silent output loss |
| Constant-time HMAC compare | Prevents timing-oracle attacks on the webhook secret |
| `retryOutputs` field on WorkflowRun | Avoids re-running Argo workflow just to retry a Slack post; targeted recovery |
| `ARGO_TRACE_CONTEXT` env injection | Avoids patching Argo; wrapper reads annotation; zero upstream Argo changes |

## Failure modes

| Failure | Detection | Mitigation |
|---|---|---|
| Cron trigger missed (controller restart) | KEDA ScaledJob fallback (CronJob semantics); missed window logged | Controller reconciles on restart; at-most-one spawn per window via idempotency key |
| NATS KV unavailable during fanout | `NATSDedupUnavailable` event; 30 s NACK backoff | Fanout pauses; no data loss; resumes on KV recovery |
| Webhook receiver crash mid-validation | HMAC check incomplete; response not sent; GitHub retries | Receiver stateless; next retry from GitHub re-validates; idempotency via `WorkflowRun` name derived from event ID |
| Argo `Workflow` create fails | `WorkflowRun` stuck in `Pending`; `ArgoDispatchFailed` event | Controller retries with backoff (max 1000 s per rule 04); tenant notified |
| Output sink permanently down | 3 retries exhausted; `OutputDeliveryFailed` | `PartialSuccess` phase; `retryOutputs` for manual recovery |
| Trace context lost (annotation missing) | Argo executor wrapper logs `MissingTraceContext`; span orphaned | Root span still present; step spans miss parent linkage; non-blocking |
| `WorkflowRun.spec.timeout` not enforced (controller crash) | Argo native `activeDeadlineSeconds` mirrors the keese timeout | Argo kills workflow independently; keese reconciles phase on restart |

## Upgrade / rollback

Rollback in frontmatter. `WorkflowRun` CRs are immutable after creation; no
migration needed for running instances. `PartialSuccess` phase is a new enum value —
controllers consuming `phase` must treat unknown values as non-terminal (forward
compatibility). When 03 reaches `current`, `workflowTemplateRef` field name must be
reconciled with whatever 03 standardizes; one-time `jq` migration script if the key
name differs. v1alpha1 → v1beta1 follows the standard conversion-webhook path (rule
04.2).

## Refs

- [02](02-workspace-model.md) — `spec.resumeFrom`; Workspace FSM
- [03](03-workflow-argo-delegation.md) — Argo projection contract (stub; flagged)
- [05c](05c-mcp-policy-enforcement.md) — per-tool CEL policy on Argo step tool calls
- [06](06-guardrailbinding.md) — tenant binding shapes tool access for workflow steps
- [09](09-transport-crd.md) — Transport CRD (stub; NATS + webhook flags above)
- [10a](10a-otel-topology.md) — OTEL collector; trace fan-out to ES + Loki
- [10b](10b-token-accounting.md) — `WorkflowRun` 429 pause on budget exhaustion (flagged in 10b)
- [22-ii-samples.md](22-ii-samples.md) — full annotated YAML samples (≥ 50 lines each)
- [../plans/rubric.md](../plans/rubric.md)

## Iteration log

### Iteration 1 — 2026-04-21

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Five open questions answered; three concrete patterns; bounded inputs/outputs. |
| 2 | Architecture fit | 10 | 1.0 | 10 | D7 triggers/outputs; D20 Argo delegation; SSA + fieldOwner; compose-over-replicate. |
| 3 | Security posture | 15 | 1.0 | 15 | HMAC constant-time; projected file secrets; no keys in env; BSP credential routing; dedup conservative failure mode. |
| 4 | Automatability | 10 | 0.5 | 5 | Mechanisms described; controller code pre-gate. `retryOutputs` semantics are runnable via kubectl patch. |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Failure modes enumerated; dedup invariant stated; phase resolution table concrete. Named test files absent (pre-gate). |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | 7 failure modes; cron miss; KV unavail; receiver crash; Argo dispatch; output sink; trace loss; timeout fallback. |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | ≤ 200 lines; full YAML in 22-ii-samples.md; single responsibility; all deps linked. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; frontmatter complete; depends lists all 7; rollback concrete. |
| 9 | Observability | 5 | 1.0 | 5 | Root span + 4 child span types; attributes table; audit destinations stated. |
| 10 | Operational readiness | 10 | 1.0 | 10 | PartialSuccess forward-compat note; timeout mirror; retryOutputs for recovery; v1beta1 path. |
| | **Total** | 100 | | **92.5** | |

Verdict: **SHIP** (92.5 ≥ 90). Status: `current`.

Top gaps:
1. Cat 4/5: controller code and named envtest / kuttl tests absent — acceptable pre-gate; flag for spec phase.
2. Cross-dep 03 (stub): `workflowTemplateRef` field name unconfirmed; flag for 03 iter-1 resolution.
3. Cross-dep 09 (stub): NATS Consumer binding abstraction pending; flag for 09 iter-1.

Cross-deps settled: 02 `spec.resumeFrom` consumed; 05c MCP span propagation via `ARGO_TRACE_CONTEXT`; 06 merge lattice gates tool access; 10a Tier 2 trace root span; 10b `WorkflowRun` 429 pause referenced.

Cross-deps flagged: 03 field-name drift risk; 09 Transport abstraction; 11 secret rotation mechanism.

Open questions (carry to iter 2):
1. Does 03 name the WorkflowTemplate reference field `workflowTemplateRef` or `templateRef`?
2. Does 09 wrap the NATS Consumer inside a `Transport` object, or does the trigger reference the Consumer directly?
3. `PartialSuccess` — should this be a sub-phase of `Succeeded` or a top-level enum value?
