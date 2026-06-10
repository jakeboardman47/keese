<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - ../../specs/policy.keese.ai-v1alpha1.md
  - ../../designs/10a-otel-topology.md
  - ../../../internal/controller/policy/ratelimit.go
related_skills: [plan-management, doc-authoring]
status: complete
last_verified: 2026-06-10
phase: CH5
model_tier: opus
depends_on: []
agent: architect
dispatch: design
outputs:
  - docs/designs/30-token-metering-pipeline.md
  - docs/designs/README.md
---

# CH5 — Token-cost metering pipeline (design ADR — no code)

**Goal.** `TokenBudget` enforcement only half-works: the controller *projects* a
rate-limit (`ratelimit.go`/`ratelimit_client.go` → Envoy `BackendTrafficPolicy`),
but **nothing feeds consumed tokens back** to the rate-limiter, so over-budget
(429) enforcement can't fire (EH7 had to stub it). This phase is **design only** —
write the ADR; do **not** implement.

## Deliverable

`docs/designs/30-token-metering-pipeline.md` (status `draft`, scored later), plus its
row in `docs/designs/README.md`. The ADR must decide + justify:

1. **Source of truth for consumed tokens** — LLM-response `usage` (input/output
   tokens) as seen by the Envoy AI Gateway, vs OTEL spans, vs a sidecar tally.
   Where is it measured exactly (which filter / hop)?
2. **The feedback path** — how a per-`(tenant, model, window)` running total reaches
   the rate-limiter's decision: Envoy global rate-limit service descriptors? A
   controller that reconciles consumed→`BackendTrafficPolicy`? An OTEL processor
   (design 10a) writing to a store the limiter reads? Pick one; show the data flow.
3. **Window accounting** — how `windowStart`/`windowDuration` + `exhaustionMode`
   (hard/soft) map onto the chosen mechanism; reset semantics; rule 04.4 (status is
   derived) compliance.
4. **Failure modes** — metering lag, double-count on retries, gateway restart,
   counter store unavailable (fail-open vs fail-closed for budgets — justify).
5. **Observability** — the metrics/spans the pipeline emits (`keese_token_budget_
   consumed_total{tenant,model}` etc., per 10a).
6. **Implementation phases** — break the build into follow-on CH phases
   (controller-author / infra-bootstrap) with rough footprints, so this ADR seeds
   the next wave.

## Acceptance

- ADR follows `docs/designs/` conventions (SPDX + frontmatter, ≤ 200 lines, refs).
- A clear chosen mechanism with trade-offs vs ≥ 2 alternatives; concrete data-flow;
  failure modes; the implementation-phase breakdown.
- **No production/code changes** — `internal/`, `api/`, `config/` untouched.

## Notes for the agent (architect)

- This unblocks EH7's `revisit_when_token_metering_live` once the design is built.
  Read `ratelimit.go` + `policy.keese.ai-v1alpha1.md` (TokenBudget spec) +
  `10a-otel-topology.md` + `05a-envoy-ai-gateway-topology.md` for the existing
  rails. Prefer a mechanism that reuses the AI Gateway's native token-cost rate
  limiting if it exists.
- **Never run bare `git stash`/`pop`/`reset`/`checkout <branch>`** (hits the shared
  checkout). Design only — propose, don't implement.
