<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: observability
depends:
  - 05a-envoy-ai-gateway-topology.md
  - 10a-otel-topology.md
  - 10b-token-accounting.md
  - 20a-api-group-layout.md
  - 24-tenant-crd.md
related_skills: []
status: draft
last_verified: 2026-06-10
rollback: |
  The meter hop is additive. To disable: scale the `keese-token-meter` processor
  out of the Tier-1 OTEL pipeline (SSA-patch ConfigMap `otel-collector-config`,
  `fieldOwner: keese-otel-controller`; rolling restart). Reconciler then sees no
  `keese_token_budget_consumed_total` series → reads consumed=0 → never writes the
  exceeded signal (fail-open for budgets; short-window BackendTrafficPolicy still
  enforces). No schema change; no migration doc required.
---

# 30 — Token-cost metering pipeline (EH7)

Iteration log: [30-ii-iter-log.md](30-ii-iter-log.md).

## Context

`TokenBudget` enforcement is half-wired. 10b/05a/the policy spec lock a complete
loop: gateway emits `keese_token_budget_consumed_total{tenant,workspace,model,
direction}` → OTEL → Prometheus → reconciler PromQL → NATS KV boolean → ext_authz
→ 429. The reconciler also projects a per-second `BackendTrafficPolicy`
(`ratelimit.go`/`ratelimit_client.go`). **Every hop is specified except the one
that produces the queried series.** The Envoy AI Gateway's *native* token metric
is `envoy_ai_gateway_token_cost_total{model,tenant}` (05a) — wrong name, and
missing the `workspace` + `direction` labels the reconciler's PromQL requires. So
the query matches nothing, `consumed` reads 0, and 429 never fires. This is EH7
(`revisit_when_token_metering_live`), which shipped stubbed.

This ADR specifies **only the missing metering-ingestion hop** that materializes
the locked series. It does **not** re-litigate 10b/05a (Prometheus-authoritative,
NATS-KV-signal, 10 s reconcile) — those are `current` and changing them needs a
migration plan.

## Decision

**Source consumed tokens from the AI Gateway's response `usage`, translated by a
stateless `keese-token-meter` OTEL processor co-located in the Tier-1 DaemonSet
(10a), which re-emits the keese-labeled counter that the locked reconciler already
queries.** No new feedback path, no new store: the meter closes the existing gap.

### Why response-`usage` at the gateway, not OTEL spans or a sidecar

The Envoy AI Gateway already parses each upstream LLM response and exposes
input/output token counts via its token-cost extension (the same value driving
`envoy_ai_gateway_token_cost_total`). It is the single authoritative hop every
egress call crosses (rule 05.4) — the only place a compromised agent cannot
under-report. Tokens are read post-response, keyed by the `x-keese-tenant` /
`x-keese-workspace` headers ext_authz already stamps (05a) and
`x-envoy-upstream-model-id`.

### Why a meter processor, not a new controller or descriptor path

Three candidate feedback paths existed. The locked loop already chose
"controller reconciles consumed→signal"; the only open question was who produces
the input series. A thin OTEL processor reuses the Tier-1 DaemonSet (co-located
with the gateway for tail-sampling, 10a), adds no CRD, no new store, no reconcile,
and emits straight into the Prometheus pipeline that 10a already owns.

| Option | Where tokens read | Verdict |
|---|---|---|
| **Gateway `usage` → meter processor (CHOSEN)** | Tier-1 DaemonSet, per response | Authoritative hop; reuses OTEL+Prom; no new store; closes EH7 with one stateless component |
| OTEL agent spans (`gen_ai.usage.*`) | Goose runtime spans (08a) | Agent is in the threat model (rule 05); under-reportable; spans are sampled (10a) → lossy counters |
| Sidecar tally → controller→BTP | Per gateway pod | New stateful component; double counts on Envoy retries; restart loses tally; duplicates 10b |
| Envoy global-ratelimit descriptors | gRPC RLS at gateway | RLS counts requests, not tokens; needs a token-cost descriptor Envoy AI GW does not expose; would re-litigate 05a |

## Data flow

```
agent → Envoy AI Gateway → upstream LLM → response usage{input,output}
  └─ token-cost ext reads usage; meter processor (Tier-1) relabels →
     keese_token_budget_consumed_total{tenant,workspace,model,direction} (+1 series each dir)
  → Tier-2 → Prometheus  [authoritative, locked 10b]
TokenBudget ctrl → PromQL increase()[window] → compare vs spec.limits[i]
  → on crossover: NATS KV keese-budget-exceeded boolean  [locked 10b/05a]
keese-ext-authz push-watch → x-keese-budget-exceeded → Envoy local_reply → 429
```

The meter is the only new element (line 2). Everything downstream is `current`.

## Window accounting (rule 04.4 compliant)

Consumption lives in Prometheus as a monotonic counter; the **window is a query
parameter, not stored state**. The reconciler computes consumed via
`increase(...[spec.windowDuration])` anchored at `status.windowStart`
(`windowAnchor` + N·`windowDuration`, 10b). `status` (`windowStart/End`,
`consumedCurrent/Previous`, `phase`) is **derived** each reconcile from the PromQL
result and never feeds the next decision (rule 04.4). Reset: at boundary the
reconciler copies `consumedCurrent→Previous`, zeroes current, deletes the NATS KV
key, sets `phase: Ready` (10b). `exhaustionMode` maps unchanged: `hard`→KV write,
`soft`→warn header, `disabled`→count-only. The meter is window-agnostic; it only
ever increments — `increase()` does the windowing. Counter resets on gateway
restart are absorbed by `increase()`'s native reset handling.

## Failure modes

| Failure | Behavior | Open vs closed |
|---|---|---|
| Metering lag (≤ reconcile interval) | Agent briefly overspends; next tick catches up | Open — bounded; short-window BTP caps the burst (05a) |
| Double-count on Envoy upstream retry | Meter emits only on **final** response (per-request, not per-retry); idempotency key = Envoy `x-request-id` dedup window | Avoided — meter keys on request-id, not attempt |
| Gateway pod restart | Counter resets; `increase()` handles resets; in-flight `usage` for the dying request lost (≤ one call) | Open — bounded single-call loss; rule 06 (SIGKILL durable-state) N/A: Prometheus is the durable store |
| Prometheus/meter down | Reconciler reads 0; existing KV exceeded keys **persist** (no false-clear, 10b); `MetricFetchFailed` event | **Closed for already-exceeded budgets; open for new overage** — deliberate (below) |
| NATS KV write fails | `BudgetSignalWriteFailed`; retry+backoff (10b) | — |

### Fail-open vs fail-closed — the budget decision

**Budgets fail OPEN when the metering pipeline is down; already-tripped signals
stay CLOSED.** Rationale: a long-window spend cap is a cost-control, not a
security boundary — the security boundary is OpenFGA `can_call` + credential
injection (rule 05.6), which fail **closed** independently. Hard-failing all
egress because Prometheus is briefly unavailable would convert a billing
guardrail into a cluster-wide outage — an unacceptable blast-radius trade for a
cost knob. Two safety nets hold the line: (1) the short-window
`BackendTrafficPolicy` per-second cap keeps enforcing at the gateway with no
dependency on the meter (05a); (2) the reconciler **never false-clears** an
existing exceeded signal on fetch failure (10b) — a budget already tripped stays
tripped until Prometheus proves it should clear. Operators wanting strict caps
set the short-window BTP conservatively. This matches D-locked 10b/05a fail
semantics and rule-precedence (05 security wins; budgets are not a 05 control).

## Observability (per 10a)

Meter emits (Tier-1 Prometheus pipeline): `keese_token_budget_consumed_total
{tenant,workspace,model,direction}` (the contract series), plus self-telemetry
`keese_token_meter_translated_total{result=ok|no_tenant|no_model}` and
`keese_token_meter_dropped_total{reason}`. Reconciler/ext_authz metrics
(`keese_token_budget_{limit,remaining}`, `keese_extauthz_budget_429_total`) are
unchanged (10b/05a). Spans: meter annotates the existing
`gateway.upstream.request` span (05a) with `tokens_in`/`tokens_out` already
present — no new span. Events: reuse the locked `events.go` table (10b). Discard
of spans missing `keese.tenant` follows 10a fail-closed isolation.

## Implementation phases (seeds the next wave)

| Phase | Scope | Agent | Footprint |
|---|---|---|---|
| **CH5a** | `cmd/token-meter/` OTEL processor: read gateway token-cost usage, relabel to keese series, request-id dedup, self-metrics; rule 06 SIGTERM flush; Dockerfile + cosign | infra-bootstrap | ~250 LoC Go + Dockerfile; new `cmd/` |
| **CH5b** | Wire meter into Tier-1 `otel-collector-config` ConfigMap (10a) via SSA; dev bootstrap values; NetworkPolicy (meter→Prom only) | infra-bootstrap | ~80 lines YAML; `dev/bootstrap/` |
| **CH5c** | Un-stub TokenBudget reconciler: real PromQL client wired to the live series; remove EH7 stub; envtest with mock Prom emitting the meter series | controller-author | ~150 LoC; `internal/controller/observability/tokenbudget/` |
| **CH5d** | Flip EH7 `revisit_when_token_metering_live`→done; e2e: drive real tokens → assert 429 + `x-keese-limit-source: token-budget` | test-engineer | ~120 LoC; `test/e2e/` |

CH5a→CH5b→CH5c→CH5d are sequential (CH5c needs CH5b's live series; CH5d needs all).
None touch protected paths. Spec edit: `policy.keese.ai-v1alpha1.md` line 91 —
replace the hand-wavy "`tokenUsageMetric` extension" note with a ref to this ADR's
meter hop (spec stays `current`; additive clarification, no schema change).

## Refs

- [05a](05a-envoy-ai-gateway-topology.md) — native `envoy_ai_gateway_token_cost_total`; 429 flow; BTP short-window
- [10a](10a-otel-topology.md) — Tier-1 DaemonSet, Prometheus pipeline, fail-closed tenant isolation
- [10b](10b-token-accounting.md) — locked Prometheus-authoritative loop; reconciler; reset; fail semantics
- [policy.keese.ai-v1alpha1.md](../specs/policy.keese.ai-v1alpha1.md) — TokenBudget spec;
  line 91 clarification target
- [ratelimit.go](../../internal/controller/policy/ratelimit.go) ·
  [ratelimit_client.go](../../internal/controller/policy/ratelimit_client.go) — BTP projection
- [CH5-token-metering-design.md](../plans/controller-hardening/CH5-token-metering-design.md) —
  this phase
- [EH7-token-budget-e2e.md](../plans/e2e-hardening/EH7-token-budget-e2e.md) —
  the stubbed e2e this unblocks
- [../plans/rubric.md](../plans/rubric.md)
