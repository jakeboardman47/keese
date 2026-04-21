<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: observability
depends:
  - 04a-openfga-authz-model.md
  - 05a-envoy-ai-gateway-topology.md
  - 06-guardrailbinding.md
  - 10a-otel-topology.md
  - 20a-api-group-layout.md
  - 24-tenant-crd.md
related_skills: []
status: current
last_verified: 2026-04-21
rollback: |
  Set `spec.exhaustionMode: disabled` on all TokenBudgets via
  `kubectl patch tokenbudget -A --type=merge -p '{"spec":{"exhaustionMode":"disabled"}}'`.
  Restore NATS KV `keese-budget-counters` bucket from JetStream snapshot captured before
  the bad deploy. If CR schema changed, run the inverse migration steps documented in
  `docs/plans/migration-token-budget-<slug>.md` and re-apply with the previous controller
  image pinned in the Deployment manifest.
---

# 10b — Token Accounting

## Context

`TokenBudget` is keese's long-window (per-day / per-month) budget CR under
`observability.operator.keese.ai/v1alpha1`, complementing Envoy AI Gateway's short-window
`BackendTrafficPolicy` token-cost filter so both layers enforce independently. Token
consumption flows: OpenFGA audit event → OTEL processor (10a) → NATS KV
`keese-budget-counters`; budget controller writes exhaustion state to `keese-budget-exceeded`
(05a iter-2 locked); ext_authz sets `x-keese-budget-exceeded: true` → Envoy 429.
`Tenant.spec.tokenBudgetRef` (24) and `GuardrailBinding.spec.tokenBudget` (06) reference
this CR. USD billing export delegated to design 21. Unit of account: raw tokens per model
(not USD); `spec.limits[].model` matches `x-envoy-upstream-model-id`; `model: "*"` is an
aggregate cap checked after per-model limits.

## Spec schema

```yaml
apiVersion: observability.operator.keese.ai/v1alpha1
kind: TokenBudget
metadata:
  name: acme-corp-monthly
  namespace: keese-budgets
spec:
  windowDuration: "720h"          # overrides window type; default "720h" (30 days)
  windowAnchor: "2026-04-01T00:00:00Z"
  exhaustionMode: hard            # hard | soft | disabled
  limits:
    - model: "claude-3-5-sonnet"
      inputTokens: 50000000
      outputTokens: 10000000
      totalTokens: 60000000
    - model: "gpt-4o"
      inputTokens: 10000000
      outputTokens: 2000000
      totalTokens: 12000000
    - model: "*"                  # aggregate cap across all models
      totalTokens: 100000000
  pricebookRef:                   # optional; feeds USD billing export (design 21)
    name: "openai-2026q2"
    namespace: keese-billing
status:
  observedGeneration: 1
  phase: Ready                    # Ready | Exhausted | SoftExhausted | ResetFailed
  windowStart: "2026-04-01T00:00:00Z"
  windowEnd: "2026-05-01T00:00:00Z"
  consumedCurrent:
    - model: "claude-3-5-sonnet"
      inputTokens: 12000000
      outputTokens: 3500000
      totalTokens: 15500000
  consumedPrevious:
    - model: "claude-3-5-sonnet"
      totalTokens: 47000000
  conditions:
    - type: Ready
      status: "True"
      reason: WindowActive
```

CRD markers: `// +kubebuilder:subresource:status`; printer columns `Age`, `Ready`,
`Phase`, `WindowEnd`, `Exhaustion`. Namespace-scoped (20a). ReBAC marker on
`spec.limits`: `// +keese:rebac-tuple=budget:can_enforce`.

## Atomic decrement mechanism

OpenFGA `credential.can_use` emits token-accounting event (04a) → OTEL custom processor
(10a, flagged) → atomic CAS `Put` to NATS KV `keese-budget-counters` keys
`workspace/<uid>/<model>/{input,output}`. Budget controller reads counters every 10 s,
updates `status.consumedCurrent`. When `consumed >= limit` for any model or the `*` aggregate,
controller writes `keese-budget-exceeded/workspace/<uid>: true` (05a iter-2 locked). ext_authz
watches that bucket (04c NATS-degraded pattern) and sets `x-keese-budget-exceeded: true`.
No CR-patch on the audit path avoids reconcile storms.

## 429 signaling flow

1. Budget controller sets `keese-budget-exceeded/workspace/<uid>: true`.
2. ext_authz push-watches that key; on receipt sets `x-keese-budget-exceeded: true` in
   the authz response headers (step 5 of 05a ext_authz decision flow).
3. Envoy `local_reply_config` matches header → returns HTTP 429 with
   `Retry-After: <seconds-to-windowEnd>` and `x-keese-limit-source: token-budget`.
4. On budget reset, controller deletes the NATS KV key; ext_authz clears the flag.

## Exhaustion behavior

`spec.exhaustionMode` controls enforcement:

| Mode | Behavior |
|---|---|
| `hard` (default) | Gateway returns 429 for all subsequent calls; in-flight upstream allowed to complete (Envoy pool semantics). |
| `soft` | Accounting continues; `TokenBudgetExhausted` event emitted; `x-keese-budget-soft-warn: true` header on responses; no block. |
| `disabled` | Accounting only; no enforcement. Rollback / monitoring-rollout path. |

`GuardrailBinding.spec.tokenBudget` merge lattice applies `min()` across active bindings;
the effective limit is the most restrictive matching TokenBudget — enforced by the
GuardrailBinding controller before writing to this CR's owning namespace.

## Budget reset and windows

`spec.windowDuration` + `spec.windowAnchor` define the window; controller advances by one
`windowDuration` per elapsed period. `Tenant.spec.billingTimezone` (24) offsets midnight
anchors. On boundary: copy `consumedCurrent → consumedPrevious`; zero counters; delete NATS
KV keys for `keese-budget-counters` and `keese-budget-exceeded`; set `status.phase: Ready`.
Idempotent: `windowStart < now < windowEnd` → no-op. On failure: `ResetFailed` phase;
enforcement holds on stale counter; alert `TokenBudgetResetFailed`; runbook: manual NATS
KV patch. Cardinality guard: warn at > 900 TokenBudgets; VAP hard-rejects past 1 200.

## Prometheus metrics

| Metric | Type | Labels |
|---|---|---|
| `keese_token_budget_consumed_total` | Counter | `tenant, workspace, model, direction` |
| `keese_token_budget_limit` | Gauge | `tenant, model, window, direction` |
| `keese_token_budget_remaining` | Gauge | `tenant, model, window, direction` |
| `keese_token_budget_exhaustion_events_total` | Counter | `tenant, workspace, mode` |
| `keese_token_budget_reset_errors_total` | Counter | `tenant, window` |

Cardinality ceiling: max 1K tenants × 10 models × 3 windows × 2 directions = 60K series.

## Trade-offs

| Option | Decision | Rationale |
|---|---|---|
| Raw tokens vs. USD | Raw tokens | Prices change; tokens are observable; USD is billing-layer (design 21). |
| OTEL processor → NATS KV vs. CR patch | NATS KV | CR patching from audit path causes reconcile storms; NATS CAS is atomic and sub-ms. |
| Controller read-interval vs. real-time | 10 s interval | Avoids N×M KV watches; 10 s lag acceptable for long-window budgets; short-window left to BackendTrafficPolicy. |
| `hard` default exhaustion | Yes | Fail-closed; `soft` and `disabled` are explicit opt-outs. |
| `*` aggregate wildcard | Yes | Prevents model-substitution evasion; modeled after GuardrailBinding merge lattice. |

## Failure modes

| Failure | Detection | Mitigation |
|---|---|---|
| NATS KV counter write fails | OTEL processor error counter | Processor retries with backoff; emits `TokenAccountingDropped` event; alert at >1% drop rate |
| Budget controller NATS watch lost | `keese_extauthz_degraded_seconds_total` | Resubscribe on reconnect; existing exceeded keys persist until TTL/delete |
| Budget reset fails | `status.phase: ResetFailed`; `TokenBudgetResetFailed` alert | Enforcement continues; manual NATS KV patch runbook |
| 10a processor pipeline not available | Counter accumulates in NATS KV; consumed lags | Budget lags reality; recovery on 10a restore; `TokenAccountingLagging` alert if delta > 5% |
| Cardinality ceiling reached | VAP deny on create; `TooManyBudgets` event | Operator must archive/delete old TokenBudgets |
| NATS + controller both down | No decrement; no exhaustion signal | Short-window BackendTrafficPolicy still enforces at gateway; log alert `BudgetEnforcementUnavailable` |

## Upgrade / rollback

Upgrade: new `spec.exhaustionMode` field defaults to `hard` for existing CRs via
defaulting webhook. Upgrading the controller does not reset counters. Schema changes
follow `v1alpha1 → v1beta1` promotion rule (04-kubernetes.md rule 2); migration doc
required at first promotion. Rollback: set `exhaustionMode: disabled`; restore NATS KV
snapshot; document in `docs/plans/migration-token-budget-<slug>.md`.

## Observability

Events (in `internal/controller/observability/tokenbudget/events.go`):
`TokenBudgetExhausted`, `TokenBudgetResetFailed`, `TokenAccountingDropped`,
`TokenAccountingLagging`, `BudgetEnforcementUnavailable`, `TooManyBudgets`.
OTEL span: `budget.reconcile` (`tenant`, `workspace`, `model`, `consumed`, `limit`,
`phase`). Alerts: `TokenBudgetResetFailed` (any), `TokenAccountingDropped` (> 1% rate),
`BudgetEnforcementUnavailable` (NATS + controller both down > 5 min).
`pricebookRef` feeds USD export metric `keese_tokens_usd_total{tenant,workspace,model}`
via billing-controller (design 21, flagged).

## Refs

- [04a](04a-openfga-authz-model.md) — `credential.can_use` emits token-accounting event
- [04c](04c-token-revocation.md) — NATS KV patterns (keese-revocation-version); reused
- [05a](05a-envoy-ai-gateway-topology.md) — `keese-budget-exceeded` bucket + 429 flow locked
- [06](06-guardrailbinding.md) — `spec.tokenBudget` merge lattice
- [10a](10a-otel-topology.md) — OTEL processor pipeline (stub; flagged dependency)
- [20a](20a-api-group-layout.md) — `observability.operator.keese.ai/v1alpha1`
- [21](21-opentofu-cloud-infra.md) — USD billing export (stub; flagged)
- [22](22-workflow-composition-examples.md) — WorkflowRun 429 pause (stub; flagged)
- [24](24-tenant-crd.md) — `Tenant.spec.tokenBudgetRef`; `billingTimezone`
- [../plans/rubric.md](../plans/rubric.md)

## Iteration log

Iter-1 score 92.5 → SHIP. Full table: [10b-ii-iter-log.md](10b-ii-iter-log.md).
