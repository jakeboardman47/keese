<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - ../../designs/30-token-metering-pipeline.md
  - CH5b-meter-bootstrap-wiring.md
  - ../../../internal/controller/policy/tokenbudget_controller.go
related_skills: [plan-management, controller-authoring]
status: complete
last_verified: 2026-06-10
revisit_when_adr30_current: true
phase: CH5c
model_tier: sonnet
depends_on: [CH5b]
agent: controller-author
outputs:
  - internal/controller/policy
  - docs/specs/policy.keese.ai-v1alpha1.md
---

# CH5c — Un-stub the TokenBudget reconciler vs the live series

**Goal.** With the meter (CH5a/CH5b) feeding `keese_token_budget_consumed_total`
into Prometheus, complete the enforcement loop: the `TokenBudget` reconciler queries
the live series, compares to `spec.limits`, and flips the NATS-KV exceeded signal on
crossover (which ext_authz already watches → 429). Today this consume→compare step
is effectively a no-op (consumed read 0).

## Deliverables

1. Reconcile path: run `increase(keese_token_budget_consumed_total{…}[windowDuration])`
   per `(tenant, model, direction)`, compare to `spec.limits[i]`, write the exceeded
   bucket on crossover; respect `exhaustionMode` (hard/soft) + `windowStart` reset.
   **Fail-open** on a Prometheus fetch error (never false-clear an existing exceeded
   signal — ADR 30 §failure-modes); status is derived (rule 04.4).
2. Envtest with a **mock Prometheus** (table-driven: under/at/over budget, window
   reset, fetch-error fail-open, soft-vs-hard). Runs in the consolidated keese... no
   — the **policy** envtest suite.
3. **Additive spec fix:** replace the vague `tokenUsageMetric` note at
   `docs/specs/policy.keese.ai-v1alpha1.md:91` with a ref to ADR 30 (the spec is
   `current`; ADR 30 is `draft`, so land this only once ADR 30 reaches `current`, or
   note the dependency).

## Acceptance

- `CGO_ENABLED=0 go test -race -count=1 -tags=integration ./internal/controller/policy/...`
  green incl. the new metering cases; SSA-only writes; `make lint` clean.

## Notes for the agent

- Read ADR 30 for the exact PromQL + fail-open rule. Stay inside
  `internal/controller/policy/` + the one spec line. **Never run bare `git stash`/
  `pop`/`reset`/`checkout <branch>`** (hits the shared checkout). CH5d flips the e2e.
