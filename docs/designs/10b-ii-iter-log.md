<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: observability
depends: [10b-token-accounting.md]
related_skills: []
status: current
last_verified: 2026-04-21
---

# 10b — Token Accounting — Iteration log

### Iteration 1 — 2026-04-21

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Enforcement + accounting only; billing export explicitly delegated to 21. |
| 2 | Architecture fit | 10 | 1.0 | 10 | NATS KV reuses 04c infra; no CR-patch on audit path; aligns with 05a locked topology. |
| 3 | Security posture | 15 | 1.0 | 15 | `hard` default fail-closed; no tokens in logs (rule 02); `disabled` mode documented risk; ReBAC marker on limits field. |
| 4 | Automatability | 10 | 0.5 | 5 | Rollback patch command given; reset runbook path named; no `make` target or script authored yet (pre-gate). |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Six failure modes; exhaustion modes table; cardinality guard; no envtest/e2e harness authored (10a processor also stub). |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Six failure modes; NATS+controller both down covered; 10a lag case covered; cardinality ceiling hard-stopped by VAP. |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | Within ceiling via split; single responsibility; no inline code dumped. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; frontmatter complete with all deps; rollback concrete; `status: current`. |
| 9 | Observability | 5 | 1.0 | 5 | Five Prom metrics; six events; OTEL span; three alerts; USD metric name declared. |
| 10 | Operational readiness | 10 | 1.0 | 10 | Cardinality ceiling; upgrade/rollback path; reset idempotency; window-boundary procedure; HA via NATS + BackendTrafficPolicy fallback. |
| | **Total** | 100 | | **92.5** | |

Verdict: SHIP (92.5 ≥ 90 iter-1 bar; honest score 93 after rounding). Status: `current`.

Top gaps:
1. `scripts/dev/token-budget-reset-test.sh` — not authored; blocks gate open.
2. Custom OTEL processor in 10a — flagged; blocks live accounting path until 10a `current`.
3. USD billing controller and PriceBook ConfigMap — flagged to design 21.

Next step: 10a iter-1 to unblock the OTEL processor dependency; then author
`scripts/dev/token-budget-reset-test.sh` and envtest suite for `TokenBudget` reconciler.

Cross-deps settled: 05a `keese-budget-exceeded` bucket + `local_reply_config` locked;
06 `min()` merge lattice confirmed; 24 `tokenBudgetRef` semantics confirmed.
Cross-deps flagged: 10a OTEL processor (parallel); 21 USD billing; 22 WorkflowRun 429 pause.
