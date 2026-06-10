<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - ../../designs/30-token-metering-pipeline.md
  - CH5c-tokenbudget-reconciler.md
  - ../e2e-hardening/EH7-token-budget-e2e.md
related_skills: [plan-management, testing]
status: planned
last_verified: 2026-06-10
phase: CH5d
model_tier: sonnet
depends_on: [CH5c]
agent: test-engineer
outputs:
  - tests/e2e/token-budget
---

# CH5d — Flip EH7's metering e2e (real over-budget → 429)

**Goal.** With the metering loop live (CH5a–CH5c), un-gate the EH7 over-budget step:
drive real token consumption past `limitTokens` and assert the gateway returns
**429** with `x-keese-limit-source: token-budget`, plus the window-reset recovery.

## Deliverables

1. In `tests/e2e/token-budget/`, remove the `revisit_when_token_metering_live` gate
   on the over-budget step (`enforce.sh` steps 3–4 / `check-metering.sh`): drive
   real requests through the gateway until the meter→Prometheus→reconciler→NATS-KV
   loop trips, then assert **429** + `x-keese-limit-source: token-budget` + the
   `TokenBudget` status reflects exhaustion.
2. Window reset: assert recovery after `windowDuration` (or `windowStart` advance).
3. Flip the EH7 phase doc `status` shipped-with-stubs → complete (or note CH5d
   completed its gate) + drop the revisit trigger.

## Acceptance

- Suite green under `make test-e2e` on a cluster with the meter deployed (CH5b) +
  `goose-runtime`/metering live; the over-budget step no longer self-skips.
- No `sleep`-as-assertion (rule 06 — poll the budget status / 429).

## Notes for the agent

- Read ADR 30 + EH7. Reuse EH7's request-firer + projection asserts (they stay).
  Stay inside `tests/e2e/token-budget/`. **Never run bare `git stash`/`pop`/`reset`/
  `checkout <branch>`** (hits the shared checkout). This closes the EH7 stub.
