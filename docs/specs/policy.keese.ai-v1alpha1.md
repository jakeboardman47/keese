<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: spec
category: contract
depends:
  - ../designs/10a-otel-topology.md
  - ../designs/10b-token-accounting.md
  - ../designs/05a-envoy-ai-gateway-topology.md
  - ../designs/04a-openfga-authz-model.md
  - ../designs/06-guardrailbinding.md
  - ../designs/20a-api-group-layout.md
  - ../designs/24-tenant-crd.md
related_skills: [crd-authoring, controller-authoring]
status: current
last_verified: 2026-04-21
regression_lock: false
tests:
  unit: []
  envtest:
    - internal/controller/observability/tokenbudget/suite_test.go
    - internal/controller/observability/tokenbudget/idempotency_test.go
    - internal/controller/observability/tokenbudget/exceeded_transition_test.go
    - internal/controller/observability/tokenbudget/fetch_failure_test.go
    - internal/controller/observability/tokenbudget/window_reset_test.go
  kuttl: []
metrics:
  - keese_token_budget_consumed_total
  - keese_token_budget_limit
  - keese_token_budget_remaining
  - keese_token_budget_exhaustion_events_total
  - keese_token_budget_reset_errors_total
events:
  - BudgetActive
  - BudgetExceeded
  - BudgetReset
  - MetricFetchFailed
  - BudgetSignalWriteFailed
  - BudgetEnforcementUnavailable
  - TooManyBudgets
---

# policy.keese.ai v1alpha1 — spec

## Scope

One kind: **TokenBudget**. Per-tenant or per-workspace long-window token spend cap.
Composed enforcement: Prometheus counter (authoritative) + NATS KV boolean signal +
Envoy AI Gateway rate-limit projection. Billing export delegated to design 21.

## API

**Group / version / kind:** `policy.keese.ai/v1alpha1 / TokenBudget`

**Scope:** Namespace (tenant namespace via Capsule, or workspace namespace).

Markers: `+kubebuilder:object:root=true`, `+kubebuilder:subresource:status`,
`+kubebuilder:resource:scope=Namespaced,shortName=tb`. Printer columns: `Age`,
`Ready` (conditions[Ready].status), `Phase`, `WindowEnd`, `Exhaustion`.

### spec

`scope` — discriminated one-of (CEL XValidation: exactly one of `tenant.name` or
`workspace.name` must be set). `windowDuration string` (Go duration; default `720h`);
`windowAnchor string` (RFC3339; controller advances by windowDuration). `exhaustionMode
string` (`hard|soft|disabled`; default `hard`). `limits[]` — `// +keese:rebac-tuple=budget:can_enforce`;
each entry: `model string` (`"*"` = aggregate cap), `inputTokens int64` (+optional, min 1),
`outputTokens int64` (+optional, min 1), `totalTokens int64` (+optional, min 1).
`pricebookRef.name string` (+optional; feeds billing-controller design 21).

**CEL VAP invariants** (ValidatingAdmissionPolicy `tokenbudget-limits`):

- At least one of `inputTokens`, `outputTokens`, `totalTokens` must be set per limit entry.
- `windowDuration` must match `^[0-9]+(h|d|m)$` and parse to ≥ 1 h.
- `limits` must not be empty.
- Cluster-wide VAP hard ceiling: max 1 200 TokenBudget CRs; warn alert at > 900.

### status

Fields: `observedGeneration int64`; `phase string` (`Ready|Exhausted|SoftExhausted|ResetFailed`);
`windowStart/windowEnd string` (RFC3339); `consumedCurrent[]` and `consumedPrevious[]`
(same shape as limits: `{model, inputTokens, outputTokens, totalTokens}`; previous
populated on window reset, current zeroed); `conditions[]` types: `Ready`, `BudgetExceeded`,
`ResetFailed`.

## Counter store and query pattern

Prometheus is authoritative (10b iter-2, locked). NATS KV carries boolean signal only.

**Metric emitted by Envoy AI Gateway** (via `tokenUsageMetric` extension, cross-cut 05a):

```
keese_token_budget_consumed_total{tenant,workspace,model,direction}
```

OTEL collector Tier 1 DaemonSet scrapes gateway pods; Tier 2 forwards to Prometheus (10a).

**Reconcile query** (one per `spec.limits[i]`, at 10 s interval):

```promql
sum(increase(
  keese_token_budget_consumed_total{
    tenant="<T>", model="<M>", direction=~"input|output"
  }[<windowDuration>]
))
```

On `consumed >= limit`:

- `hard`: write `true` to NATS KV `keese-budget-exceeded` key
  `workspace/<uid>/<model>` (or `tenant/<name>/aggregate`); emit `BudgetExceeded` event.
- `soft`: emit `BudgetExceeded` event; set `x-keese-budget-soft-warn: true` response
  header; no KV write.
- `disabled`: accounting only; no event, no signal.

On Prometheus unavailable: existing KV signal persists (fail-closed). Controller emits
`MetricFetchFailed` event; increments `keese_token_budget_reset_errors_total`. Back off
exponentially; no false-clear of exceeded signal.

## Envoy rate-limit composition

The TokenBudget controller projects an Envoy `RateLimit` policy via SSA
(`fieldOwner: keese-tokenbudget-controller`) on each reconcile cycle. The policy carries
`remaining = spec.limits[i].<direction>Tokens - consumedCurrent[i].<direction>Tokens` as
the current budget ceiling. Refresh interval matches `spec.windowDuration`. The NATS KV
boolean signal triggers hard 429 independently via `local_reply_config` (05a locked).

## 429 signaling flow

`Agent → Envoy AI Gateway → upstream → usage metric → OTEL Tier 1 → Prometheus`
`TokenBudget ctrl → PromQL → compare → NATS KV write → keese-ext-authz push-watch`
`→ x-keese-budget-exceeded → Envoy local_reply → HTTP 429 (Retry-After: <windowEnd delta>)`

On window reset: controller deletes NATS KV key; clears `BudgetExceeded` condition;
sets `status.phase: Ready`; emits `BudgetReset` event.

## RBAC

Markers on reconciler: `tokenbudgets` verbs `get;list;watch;create;update;patch;delete`;
`tokenbudgets/status` verbs `get;update;patch`; `tokenbudgets/finalizers` verbs `update`;
`events` verbs `create;patch`.

## Finalizer

ID: `finalizers.tokenbudget.keese.ai/envoy-ratelimit-cleanup`

On deletion: SSA-delete projected `RateLimit` policy; delete NATS KV keys for all
`spec.limits[i]` entries; remove finalizer. Idempotent on repeated apply.

## SSA fieldOwner

All writes: `client.FieldOwner("keese-tokenbudget-controller")`.

## Event reasons (events.go)

| Reason | Type | Trigger |
|---|---|---|
| `BudgetActive` | Normal | Phase transitions to Ready |
| `BudgetExceeded` | Warning | consumed >= limit crossover |
| `BudgetReset` | Normal | Window boundary passed; counters reset |
| `MetricFetchFailed` | Warning | Prometheus query error |
| `BudgetSignalWriteFailed` | Warning | NATS KV write failure |
| `BudgetEnforcementUnavailable` | Warning | Controller + NATS both down > 5 min |
| `TooManyBudgets` | Warning | Cluster ceiling > 1 200 CRs |

## Acceptance tests (envtest)

All in `internal/controller/observability/tokenbudget/`; load CRDs from `config/crd/bases/`;
mock Prometheus HTTP client + NATS KV bucket; assert ≥ 3 reconcile idempotency.

- `idempotency_test.go` — no-spec-change reconcile produces identical status; no duplicate events.
- `exceeded_transition_test.go` — mock over-limit: `BudgetExceeded` event + NATS KV write;
  mock under-limit: `BudgetReset` + KV key deleted. Includes clamp-to-zero for negative remaining.
- `fetch_failure_test.go` — Prometheus error: `MetricFetchFailed` event; KV signal unchanged
  (no false-clear); exponential backoff verified.
- `window_reset_test.go` — clock past `windowEnd`: `consumedPrevious` populated; current zeroed;
  KV cleared; `BudgetReset` emitted.

## ReBAC markers

`spec.limits` carries `// +keese:rebac-tuple=budget:can_enforce` (10b locked).
Relation shape documented in `docs/specs/egress-authz-protocol.md`.

## Failure modes

| Failure | Detection | Mitigation |
|---|---|---|
| Prometheus unavailable | `MetricFetchFailed` event; `keese_token_budget_reset_errors_total` | Existing KV signal persists; exponential backoff; no false-clear. |
| NATS KV write fails | `BudgetSignalWriteFailed` event; counter | Retry with backoff; alert at > 1% drop rate. |
| Controller + NATS both down | `BudgetEnforcementUnavailable` event | Short-window `BackendTrafficPolicy` still enforces at gateway. |
| Cardinality ceiling | VAP deny on create; `TooManyBudgets` event | Operator must archive/delete old TokenBudgets. |
| Window reset fails | `status.phase: ResetFailed`; alert | Enforcement holds on stale signal; manual NATS KV delete runbook. |

## Iteration log

Iter-1: 90 → SHIP. Iter-2: 95 → SHIP. Iter-3: 95 → SHIP.
Full table: [policy.keese.ai-v1alpha1-iter-log.md](policy.keese.ai-v1alpha1-iter-log.md).
