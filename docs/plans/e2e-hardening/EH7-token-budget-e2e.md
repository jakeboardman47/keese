<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - ../../specs/policy.keese.ai-v1alpha1.md
  - ../../../internal/controller/policy/tokenbudget_controller.go
  - ../../../internal/controller/policy/ratelimit.go
related_skills: [plan-management]
status: shipped-with-stubs
last_verified: 2026-06-10
revisit_when_metering_fully_live: true
phase: EH7
model_tier: sonnet
depends_on: []
agent: test-engineer
outputs:
  - tests/e2e/token-budget
  - tests/e2e/lib
---

# EH7 — TokenBudget enforcement e2e

**Goal.** `policy.keese.ai/TokenBudget` has no cluster e2e. Its reconciler ships
(`internal/controller/policy/tokenbudget_controller.go` +
`ratelimit.go`/`ratelimit_client.go`) and projects token-cost rate limiting onto
the Envoy AI Gateway. Prove the budget is actually enforced.

## Deliverables

A kuttl suite `tests/e2e/token-budget/`:

1. **Projection:** apply a `TokenBudget` with a small `limitTokens`; assert the
   controller reaches Ready and projects the rate-limit config (assert the
   downstream artifact `ratelimit.go` writes — the Envoy rate-limit / OTEL
   processor config object — exists and reflects the budget).
2. **Enforcement (in-budget):** fire a request through the gateway from a
   workspace pod (reuse EH4's request helper) under the budget → **HTTP 200**.
3. **Enforcement (over-budget):** drive consumption past `limitTokens` (loop
   requests or seed the consumed counter) → assert the gateway returns **HTTP 429**
   (rate-limited) and the `TokenBudget` status reflects exhaustion.
4. **Window reset:** assert the budget recovers after its accounting window
   (or, if the window is long, assert the status `windowStart` advances).

Prereq-gate via `tests/e2e/lib/check-prereqs.sh` (gateway + metering required).

## Acceptance

- Suite green under `make test-e2e` on a bootstrapped cluster; skips cleanly on
  placeholder prereqs.
- Asserts in-budget 200 + over-budget 429 (or a documented stub if the metering
  pipeline isn't live in the bootstrap).

## Notes for the agent

- Test SHIPPED behavior. The token-cost **metering** path (OTEL processor feeding
  consumed tokens back to the rate-limiter) may not be wired in the local
  bootstrap. If over-budget enforcement can't be driven live, assert the projection
  (step 1) + in-budget 200 fully, mark the over-budget step skipped, add a revisit
  trigger, and set `status: shipped-with-stubs`.
- Stay inside `tests/e2e/token-budget/` + additive `tests/e2e/lib/` helpers.

## CH5d update (2026-06-10) — over-budget assert wired, still stub-gated

[CH5d](../controller-hardening/CH5d-metering-e2e.md) un-stubbed the over-budget
step against the now-complete enforcement loop (CH5c reconciler queries the live
`keese_token_budget_consumed_total` series → flips the NATS-KV exceeded signal →
ext_authz `local_reply` 429). The suite now **asserts** step 3 fully:

- **429 + `x-keese-limit-source: token-budget`** — the long-window TokenBudget
  signal (ADR 30 / 05a / 10b), distinct from the gateway's short-window
  `gateway-token-rate`. `enforce.sh` polls response headers
  (`fire_request_headers` / `poll_limit_source`) for it.
- **`status.phase=Exhausted`** on the `TokenBudget`.
- **Window-reset recovery** — `status.windowStart` advances (polled, no
  `sleep`-as-assertion) and a request 200s again.

**Why it still ships-with-stubs.** The full live path depends on
[CH5b](../controller-hardening/CH5b-meter-bootstrap-wiring.md)'s two remaining
stubs, which gate the live series and therefore the whole downstream 429:

| CH5b trigger | What must land to flip |
|---|---|
| `revisit_when_meter_image_live` | Build + kind-load the `ghcr.io/keese-ai/keese-token-meter:dev` image (`make token-meter-load`) so the `keese-token-meter` Deployment in `monitoring` runs (not ImagePullBackOff). |
| `revisit_when_collector_ingest_shaping` | The Tier-1 OTEL collector image shapes its OTLP token-cost datapoint into the meter's `/ingest` JSON record, so the consumed series materializes. |

`tests/e2e/lib/check-metering-fully-live.sh` collapses both into the umbrella
precondition **`revisit_when_metering_fully_live`** and `enforce.sh` skips
steps 3-4 cleanly (exit 2) until they are met — no fake pass (rule 06). When both
CH5b triggers land, the over-budget step runs unmodified and the gate passes;
flip this doc to `status: complete` and drop `revisit_when_metering_fully_live`
at that point.
