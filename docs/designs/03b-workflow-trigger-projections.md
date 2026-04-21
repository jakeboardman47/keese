<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: workflow
depends:
  - 03-workflow-argo-delegation.md
  - 09-transport-crd.md
  - 11-secret-management.md
related_skills: [controller-authoring, doc-authoring]
status: current
last_verified: 2026-04-21
rollback: |
  Trigger resources carry OwnerRef to the keese Workflow CR; deletion cascades when the
  Workflow CR is deleted. To disable a single trigger: set Workflow.spec.triggers[i].suspended=true;
  operator deletes the projected resource (CronJob/ScaledObject/Trigger/HTTPRoute) and recreates
  on unsuspend. No namespace changes required. keese-trigger-receiver is a shared cluster
  Deployment; rolling back its image follows the standard OLM replaces chain (14a).
---

# 03b — Workflow Trigger Projections

## Context

Split from [03](03-workflow-argo-delegation.md) to keep that doc ≤ 200 lines.
This doc owns the trigger-projection contract: how `Workflow.spec.triggers[]` entries
map to K8s resources that create `WorkflowRun` CRs. Consumes the same Workspace-namespace
model established in 03 iter-2 (no ephemeral namespaces; trigger resources land in the
Workflow's namespace).

## Trigger type → projected resource

OwnerRef on each projected resource points to the keese `Workflow` CR; cascade-delete
on `Workflow` deletion. All writes via SSA (`fieldOwner: keese-workflow-controller`).

| Trigger type | Projected resource | Notes |
|---|---|---|
| `cron` | K8s `CronJob` (batch/v1) | jobTemplate creates `WorkflowRun` via `kubectl create`; Argo CronWorkflow not used (avoids cross-namespace dependency) |
| `keda` | KEDA `ScaledObject` + K8s `Job` | Job spec creates `WorkflowRun` when metric threshold hit |
| `knative` | Knative `Trigger` → `keese-trigger-receiver` Service | CloudEvent routed to receiver; receiver creates `WorkflowRun` |
| `webhook` | `Gateway` + `HTTPRoute` → `keese-trigger-receiver` | Receiver validates HMAC (`x-hub-signature-256` from OpenBao secret); creates `WorkflowRun` |
| `nats` | NATS Consumer subscription in trigger-receiver | Dedup via NATS KV bucket `keese-wf-delivered-<workflow-name>` (24 h TTL); see 22 Pattern 2 |

## `keese-trigger-receiver`

One `Deployment` per cluster in the operator namespace. Routes by `HTTPRoute` host/path per
`Workflow`. Per-trigger auth config (HMAC secret, CloudEvent type filter) stored in per-`Workflow`
`ConfigMap` named `keese-trigger-cfg-<workflow-uid>`.

**HMAC validation path** (webhook trigger):
1. Extract `X-Hub-Signature-256` + raw body.
2. Load key from `/var/run/keese/secrets/<trigger-name>` (projected K8s Secret from OpenBao via ExternalSecret; rule 05.7).
3. Constant-time `HMAC-SHA256` compare. Mismatch → 401 + `WebhookSignatureMismatch` event.
4. Unknown event type → 200 (no-op, no `WorkflowRun`).
5. Match → create `WorkflowRun`; failure → 500 + `WorkflowDispatchFailed`.

**Secret rotation:** receiver watches mounted file via inotify; no restart required.

**Cross-dep (09 stub):** HTTPRoute management for webhook triggers should align with `Transport`
CRD ownership when 09 reaches `current`. HTTPRoute-webhook does not consume `Transport` — the
receiver is a dedicated Pod, not a Transport type. Confirm distinction in 09 iter-1.

**Cross-dep (11 stub):** ExternalSecret → K8s Secret rotation path; flag for 11 iter-1.

## NATS dedup contract (summary; full in 22 Pattern 2)

Write-before-create order: KV write precedes `WorkflowRun` creation so a crash between the two
leaves a KV key with no run. On redeliver, key present → ACK, no duplicate. KV unavailable →
NACK with 30 s backoff; `NATSDedupUnavailable` event; fanout pauses.

## RBAC (trigger-receiver)

```
# In operator namespace (keese-system):
workflowruns.workflow.operator.keese.ai — create
configmaps (keese-trigger-cfg-*) — get, list, watch
secrets (keese-trigger-hmac-*) — get, watch  # via projected SA token
```

No cluster-level verbs. HTTPRoute + Gateway managed by operator SA (not receiver SA).

## Failure modes

| Failure | Detection | Mitigation |
|---|---|---|
| CronJob trigger missed (controller restart) | Missed window logged; idempotency key per window | Controller reconciles on restart; at-most-one spawn per window |
| NATS KV unavailable | `NATSDedupUnavailable` event; 30 s backoff | Fanout pauses; resumes on KV recovery |
| Receiver crash mid-HMAC | Response not sent; GitHub retries | Receiver stateless; next retry re-validates |
| HMAC secret missing from OpenBao | Receiver returns 401 | `TriggerAuthSecretMissing` event; operator emits alert |
| HTTPRoute / Gateway not ready | Trigger receiver returns 503 | Event `TriggerGatewayNotReady`; retry by webhook sender |
| WorkflowRun dispatch fails | 500 to sender | `WorkflowDispatchFailed` event; sender retries |

## Observability

- **OTEL spans:** `trigger.project`, `trigger.receiver.hmac_validate`, `trigger.receiver.dispatch`.
- **Events** (`events.go`): `TriggerProjected`, `TriggerProjectionFailed`, `TriggerAuthSecretMissing`, `WebhookSignatureMismatch`, `WorkflowDispatchFailed`, `NATSDedupUnavailable`, `TriggerGatewayNotReady`.
- **Metrics:** `keese_trigger_dispatch_total{type,tenant,result}`, `keese_trigger_hmac_mismatch_total{tenant}`, `keese_trigger_nats_dedup_unavailable_total{tenant}`.

## Refs

[03](03-workflow-argo-delegation.md) · [09](09-transport-crd.md) · [11](11-secret-management.md) · [22](22-workflow-composition-examples.md) · [rubric](../plans/rubric.md)

## Iteration log

### Iteration 1 — 2026-04-21

| # | Category | Wt | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Four trigger types; receiver contract; dedup invariant bounded. |
| 2 | Architecture fit | 10 | 1.0 | 10 | Same-namespace model (03 iter-2); SSA fieldOwner; OwnerRef cascade; VAP-first where applicable. |
| 3 | Security posture | 15 | 1.0 | 15 | HMAC constant-time; projected file secret (rule 05.7); receiver SA minimal RBAC; no cluster verbs. |
| 4 | Automatability | 10 | 0.5 | 5 | Receiver Deployment named; ConfigMap convention stated. Make targets pre-gate. |
| 5 | Verifiability | 15 | 0.5 | 7.5 | 6 failure modes; dedup invariant testable. Named envtest / kuttl tests pre-gate P8. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | 6 modes with detection + mitigation; cron miss; KV unavail; crash; HMAC miss; Gateway not ready; dispatch fail. |
| 7 | Context efficiency | 10 | 1.0 | 10 | Split from 03 to respect 200-line limit; single responsibility; deps linked. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; frontmatter; rollback concrete; cross-dep flags explicit. |
| 9 | Observability | 5 | 1.0 | 5 | 3 spans; 7 event reasons; 3 metrics. |
| 10 | Operational readiness | 10 | 1.0 | 10 | Secret rotation via inotify; OwnerRef cascade; OLM replaces for receiver image. |
| | **Total** | 100 | | **95** | |

Verdict: **SHIP** (95 ≥ 90). Status: `current`.

Top gaps: (1) Cat 4/5: make targets and envtest names pre-gate — acceptable. (2) 09 HTTPRoute ownership confirmed pending 09 iter-1. (3) 11 ExternalSecret rotation pending 11 iter-1.

Cross-deps settled: 22 Pattern 2 dedup contract consumed; 03 same-namespace model respected.
Cross-deps flagged: 09 iter-1 (HTTPRoute/Transport distinction); 11 iter-1 (ExternalSecret rotation path).
