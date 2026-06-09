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
status: planned
last_verified: 2026-06-09
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
  (step 1) + in-budget 200 fully, mark the over-budget step skipped, add
  `revisit_when_token_metering_live`, and set `status: shipped-with-stubs`.
- Stay inside `tests/e2e/token-budget/` + additive `tests/e2e/lib/` helpers.
