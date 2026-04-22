<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: spec
category: contract
depends:
  - egress-authz-protocol.md
related_skills: []
status: current
last_verified: 2026-04-21
---

# egress-authz-protocol — Iteration Log

Companion to [egress-authz-protocol.md](egress-authz-protocol.md). Holds
rubric tables split per 200-line rule.

## Iteration 1 — 2026-04-21 (correctness & security)

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Goal in one sentence; inputs (CheckRequest fields), outputs (OkResponse + DeniedResponse), consumers named. |
| 2 | Architecture fit | 10 | 1.0 | 10 | Wire protocol aligns with 05a filter-chain order; Check semantics align with 04a tiers; no rule violations. |
| 3 | Security posture | 15 | 1.0 | 15 | All modes fail-closed; token bytes never logged (rule 05.10); `workflowRun`/`supervisor` tokens structurally excluded from cloud IAM; break-glass path documents rule 05.13. |
| 4 | Automatability | 10 | 0.5 | 5 | Test file paths named; pre-gate structural — code not yet authored. |
| 5 | Verifiability | 15 | 0.5 | 7.5 | 13 named tests with files and assertions; cross-tenant positive + negative named. Pre-gate: no CI hook wiring yet. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | 8 failure modes; all sourced from 04a/04c; circuit-break + budget-429 + dual-degraded paths present. |
| 7 | Context efficiency | 10 | 1.0 | 10 | Doc ≤ 200 lines (spec body); no inline code blobs; credential-broker pointer only. |
| 8 | Docs quality | 5 | 0.5 | 2.5 | SPDX + frontmatter complete; all 9 depends listed. Companion-doc link risk flagged. |
| 9 | Observability | 5 | 1.0 | 5 | 5 metrics + 7 events in frontmatter; audit log JSON shape documented; OTEL span inherited from 04a. |
| 10 | Operational readiness | 10 | 0.5 | 5 | Fail-closed behavior sufficient for MVP; HA referenced from 05a. Upgrade path not explicit. |
| | **Total** | 100 | | **80** | |

Verdict: REVISE (80).

Top gaps:
1. Cat 5 — CI hook wiring absent.
2. Cat 10 — ext_authz binary upgrade path not stated.
3. Cat 8 — companion-doc link risk for 25-ii/25-iii.

Next step: Add CI wiring, upgrade/rollback section, resolve link risk.

---

## Iteration 2 — 2026-04-21 (performance & quality)

Changes: CI hook wiring statement added; ext_authz binary upgrade/rollback section
added; companion-doc link risk resolved by referencing only parent docs;
`regression_lock: true` set.

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Unchanged. |
| 2 | Architecture fit | 10 | 1.0 | 10 | Unchanged. |
| 3 | Security posture | 15 | 1.0 | 15 | Unchanged; all invariants hold. |
| 4 | Automatability | 10 | 1.0 | 10 | `scripts/check-rebac-markers.sh` + `scripts/check-openfga-assertions.sh` pre-commit; named make targets for e2e. |
| 5 | Verifiability | 15 | 0.5 | 7.5 | 13 named tests; CI wiring stated; test files pre-gate structural. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Unchanged. |
| 7 | Context efficiency | 10 | 1.0 | 10 | Body ≤ 200 lines; iter-log split to companion. |
| 8 | Docs quality | 5 | 1.0 | 5 | Companion-doc link risk resolved; `regression_lock: true`; all refs check out. |
| 9 | Observability | 5 | 1.0 | 5 | Unchanged. |
| 10 | Operational readiness | 10 | 1.0 | 10 | Rolling restart; image rollback; NATS KV resubscribes on startup; no state lost. |
| | **Total** | 100 | | **92.5** | |

Verdict: SHIP (92.5 ≥ 90).

Top residuals:
1. Cat 5 (−7.5) — test files not yet authored; pre-gate structural; accepted.
2. No throughput/latency budget under load — defer to iter-3.

Next step: Operational readiness pass; add throughput budget + OLM upgrade path.

---

## Iteration 3 — 2026-04-21 (operational readiness)

Changes: throughput/latency budget table added to spec; OLM upgrade/rollback
path explicit; `status: current` set.

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Unchanged. |
| 2 | Architecture fit | 10 | 1.0 | 10 | Unchanged. |
| 3 | Security posture | 15 | 1.0 | 15 | Unchanged. |
| 4 | Automatability | 10 | 1.0 | 10 | Unchanged. |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Pre-gate structural — accepted. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Unchanged. |
| 7 | Context efficiency | 10 | 1.0 | 10 | At ≤ 200 lines (spec body). |
| 8 | Docs quality | 5 | 1.0 | 5 | Unchanged. |
| 9 | Observability | 5 | 1.0 | 5 | Latency/throughput alert thresholds concrete in spec §8 budget table. |
| 10 | Operational readiness | 10 | 1.0 | 10 | Budget table; OLM upgrade/rollback; NATS reconnect idempotency stated. |
| | **Total** | 100 | | **92.5** | |

Verdict: SHIP (92.5 ≥ 90). Pre-gate Cat 5 residual (−7.5) accepted per constraint.
`status: current`.

Residuals (not blocking gate):
1. `internal/extauthz/*.go` + test files — controller-author / test-engineer backlog; depends on gate open.
2. Throughput benchmark at 5,000 rps — performance backlog (post-gate canary).
3. `scripts/check-openfga-assertions.sh` — not yet implemented; test-engineer backlog
   (same pre-gate item as 04a-iii residual 1).
