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
  Rollback leaves existing `keese-budget-exceeded` NATS KV keys in place until the
  reconciler clears them or they expire. Schema changes follow the v1alpha1 → v1beta1
  promotion rule; migration doc required at first promotion.
---

# 10b — Token Accounting

## Context

`TokenBudget` is keese's long-window (per-day / per-month) budget CR under
`policy.keese.ai/v1alpha1`, complementing Envoy AI Gateway's short-window
`BackendTrafficPolicy` token-cost filter so both layers enforce independently. Token
consumption is the authoritative counter in Prometheus (via the OTEL pipeline, 10a); NATS KV
`keese-budget-exceeded` carries boolean enforcement signals only (05a iter-2 locked). The
`TokenBudget` controller reconciles consumed-vs-limit by querying Prometheus; on crossover it
writes the NATS KV boolean; ext_authz watches that bucket and returns 429 via Envoy
`local_reply_config`. `Tenant.spec.tokenBudgetRef` (24) and
`GuardrailBinding.spec.tokenBudget` (06) reference this CR. USD billing export delegated to
design 21. Unit of account: raw tokens per model; `spec.limits[].model` matches
`x-envoy-upstream-model-id`; `model: "*"` is an aggregate cap.

## Why Prometheus, not NATS KV for counters

NATS JetStream KV has no atomic INCR/DECR primitive; numeric accounting would require
CAS-based optimistic locking for every tool call (~10k/s per active workspace). Range
queries via `ListObjects` scale poorly compared to PromQL aggregations. Prometheus is
already in the stack (10a), already receives `keese_token_budget_consumed_total` via the
OTEL pipeline, has native cardinality controls, and supports range/window queries via
`increase()`. Separation of concerns: OTEL/Prometheus = time-series consumption data;
NATS KV = boolean propagation of enforcement signals on crossover events only.

## Spec schema

`TokenBudget` is namespace-scoped under `policy.keese.ai/v1alpha1`.
Key fields: `spec.windowDuration` (default `720h`) + `spec.windowAnchor` define the
billing window; `spec.exhaustionMode: hard|soft|disabled`; `spec.limits[]` per
`{model, inputTokens, outputTokens, totalTokens}` where `model: "*"` is an aggregate cap.
`spec.pricebookRef` is optional — when set, the billing-controller (21) converts token
consumption to USD for invoicing; without it only raw token counts are exported.
`status` carries `{observedGeneration, phase, windowStart, windowEnd, consumedCurrent[],
consumedPrevious[], conditions[]}`. Phase values: `Ready | Exhausted | SoftExhausted |
ResetFailed`.

CRD markers: `// +kubebuilder:subresource:status`; printer columns `Age`, `Ready`, `Phase`,
`WindowEnd`, `Exhaustion`. ReBAC marker on `spec.limits`:
`// +keese:rebac-tuple=budget:can_enforce`.

## Atomic decrement and the TokenBudget reconciler

1. Every upstream call at the Envoy AI Gateway emits a completion metric. The gateway's
   token-cost filter writes `keese_token_budget_consumed_total{tenant, workspace, model,
   direction}` with the token count per call. This metric reaches Prometheus via the OTEL
   collector (Tier 1 DaemonSet + Tier 2 Deployment, 10a).
2. The `TokenBudget` controller reconciles every 10 s (configurable). At each tick, for
   each `TokenBudget` CR in scope, for each `spec.limits[i]`:
   - Query Prometheus: `sum(increase(keese_token_budget_consumed_total{tenant=T,
     workspace=W, model=M, direction=D}[<windowDuration>]))` → consumed-in-window.
   - Compare to `spec.limits[i].<input|output|total>Tokens`.
   - Update `status.consumedCurrent[]`.
   - On `consumed >= limit` per `spec.exhaustionMode`:
     - `hard`: write `true` to NATS KV bucket `keese-budget-exceeded` under key
       `workspace/<uid>/<model>` (or `tenant/<name>/aggregate` for aggregate limit).
     - `soft`: emit `TokenBudgetExhausted` event + increment
       `keese_token_budget_exhaustion_events_total{mode=soft}`; no KV write.
     - `disabled`: count-only; no events, no signals.
   - On no longer exceeded (window reset boundary): delete the NATS KV key.
3. `keese-ext-authz` watches NATS KV `keese-budget-exceeded` via push-watch; on key match
   sets `x-keese-budget-exceeded: true`; Envoy `local_reply_config` returns 429.

Reconciler-to-signal lag is bounded by the reconcile interval (10 s default). During this
window an agent may slightly exceed a limit. This is acceptable: short-window rate-limits
via Envoy `BackendTrafficPolicy` handle per-second overage (05a); long-window budgets
tolerate 10 s crossover lag.

## Prometheus query cost

Each reconcile tick issues one PromQL query per `spec.limits[i]`. A tenant with 10
`TokenBudget` CRs × 3 limits each = 30 queries per reconcile. At 10 s interval → 3 QPS
to Prometheus per tenant. 100 tenants → 300 QPS total. Prometheus handles this at scale.
Cardinality ceiling: max 1K tenants × 10 models × 3 windows × 2 directions = 60K series.
VAP hard ceiling: 1 200 `TokenBudget` CRs cluster-wide (warn at > 900).

## 429 signaling flow

```
Agent call → Envoy → upstream → usage metric → OTEL → Prometheus
TokenBudget reconciler → PromQL query → compare → NATS KV write (on crossover)
keese-ext-authz push-watch → x-keese-budget-exceeded header → Envoy local_reply → 429
```

1. Budget controller writes `keese-budget-exceeded/workspace/<uid>: true`.
2. ext_authz push-watches the bucket; on receipt sets `x-keese-budget-exceeded: true`.
3. Envoy `local_reply_config` matches header → HTTP 429 with
   `Retry-After: <seconds-to-windowEnd>` and `x-keese-limit-source: token-budget`.
4. On window reset: controller deletes NATS KV key; ext_authz clears the flag.

## Exhaustion behavior

| Mode | Behavior |
|---|---|
| `hard` (default) | Gateway returns 429; in-flight upstream call completes (Envoy pool semantics). |
| `soft` | Accounting continues; `TokenBudgetExhausted` event; `x-keese-budget-soft-warn: true` response header; no block. |
| `disabled` | Accounting only; no enforcement. Rollback / monitoring-rollout path. |

`GuardrailBinding.spec.tokenBudget` merge lattice applies `min()` across active bindings;
the effective limit is the most restrictive matching TokenBudget.

## Budget reset and windows

`spec.windowDuration` + `spec.windowAnchor` define the window; controller advances by one
`windowDuration` per elapsed period. `Tenant.spec.billingTimezone` (24) offsets midnight
anchors. On boundary: copy `consumedCurrent → consumedPrevious`; delete NATS KV keys for
`keese-budget-exceeded`; set `status.phase: Ready`. Idempotent: `windowStart < now <
windowEnd` → no-op. On failure: `ResetFailed` phase; enforcement holds on stale signal;
alert `TokenBudgetResetFailed`; runbook: manual NATS KV delete.

## Prometheus metrics

| Metric | Type | Labels |
|---|---|---|
| `keese_token_budget_consumed_total` | Counter | `tenant, workspace, model, direction` |
| `keese_token_budget_limit` | Gauge | `tenant, model, window, direction` |
| `keese_token_budget_remaining` | Gauge | `tenant, model, window, direction` |
| `keese_token_budget_exhaustion_events_total` | Counter | `tenant, workspace, mode` |
| `keese_token_budget_reset_errors_total` | Counter | `tenant, window` |

## Trade-offs

| Option | Decision | Rationale |
|---|---|---|
| Raw tokens vs. USD | Raw tokens | Prices change; tokens are observable; USD is billing-layer (design 21). |
| Prometheus vs. NATS KV for counters | Prometheus | No atomic INCR in KV; PromQL range queries are native; already in stack (10a). |
| NATS KV for signals | Boolean only | Atomic boolean set/delete is KV's natural fit; reuses 04c infrastructure. |
| Controller read-interval vs. real-time | 10 s interval | Avoids N×M KV watches; lag acceptable for long-window budgets; short-window left to BackendTrafficPolicy. |
| `hard` default exhaustion | Yes | Fail-closed; `soft` and `disabled` are explicit opt-outs. |
| `*` aggregate wildcard | Yes | Prevents model-substitution evasion; modeled after GuardrailBinding merge lattice. |

## Failure modes

| Failure | Detection | Mitigation |
|---|---|---|
| Prometheus unavailable during reconcile | Controller error; `keese_token_budget_reset_errors_total` | Back off with exponential retry; existing `keese-budget-exceeded` signal stays until Prometheus recovers; no false-clear. |
| Budget reset fails | `status.phase: ResetFailed`; `TokenBudgetResetFailed` alert | Enforcement continues; manual NATS KV delete runbook. |
| NATS KV signal write fails | Controller error counter | Retry with backoff; emit `BudgetSignalWriteFailed` event; alert at > 1% drop rate. |
| Budget controller + NATS both down | No signal; no exhaustion enforcement | Short-window BackendTrafficPolicy still enforces at gateway; `BudgetEnforcementUnavailable` alert. |
| ext_authz NATS watch lost | `keese_extauthz_degraded_seconds_total` | Resubscribe on reconnect; existing exceeded keys persist. |
| Cardinality ceiling reached | VAP deny on create; `TooManyBudgets` event | Operator must archive/delete old TokenBudgets. |

## Upgrade / rollback

Upgrade: `spec.exhaustionMode` defaults to `hard` for existing CRs via defaulting webhook.
Controller upgrade does not reset Prometheus counters or NATS KV signals. Schema changes
follow v1alpha1 → v1beta1 promotion rule (04-kubernetes.md rule 2); migration doc required
at first promotion. Rollback: set `exhaustionMode: disabled`; existing KV signals drain
naturally at next reconcile when Prometheus shows under-limit.

## Observability

Events (in `internal/controller/observability/tokenbudget/events.go`):
`TokenBudgetExhausted`, `TokenBudgetResetFailed`, `BudgetSignalWriteFailed`,
`BudgetEnforcementUnavailable`, `TooManyBudgets`.
OTEL span: `budget.reconcile` (`tenant`, `workspace`, `model`, `consumed`, `limit`,
`phase`). Alerts: `TokenBudgetResetFailed` (any), `BudgetSignalWriteFailed` (> 1% rate),
`BudgetEnforcementUnavailable` (NATS + controller both down > 5 min).
`pricebookRef` feeds USD export metric `keese_tokens_usd_total{tenant,workspace,model}`
via billing-controller (design 21, flagged).

## Refs

- [04a](04a-openfga-authz-model.md) — `credential.can_use` token-accounting event
- [04c](04c-token-revocation.md) — NATS KV patterns (`keese-revocation-version`); reused
- [05a](05a-envoy-ai-gateway-topology.md) — `keese-budget-exceeded` bucket + 429 flow locked
- [06](06-guardrailbinding.md) — `spec.tokenBudget` merge lattice
- [10a](10a-otel-topology.md) — Prometheus authoritative counter source + OTEL pipeline
- [20a](20a-api-group-layout.md) — `policy.keese.ai/v1alpha1`
- [21](21-opentofu-cloud-infra.md) — USD billing export (stub; flagged)
- [24](24-tenant-crd.md) — `Tenant.spec.tokenBudgetRef`; `billingTimezone`
- [../plans/rubric.md](../plans/rubric.md)

## Iteration log
Iter-1: 92.5 → SHIP. Iter-2: 95.0 → SHIP. Full table: [10b-ii-iter-log.md](10b-ii-iter-log.md).
