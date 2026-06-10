<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: observability
depends: [30-token-metering-pipeline.md]
related_skills: []
status: draft
last_verified: 2026-06-10
---

# 30 — Token-cost metering pipeline — Iteration log

## Iteration 1 — 2026-06-10

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Goal in one sentence (close the missing meter hop); explicitly does NOT re-litigate locked 10b/05a. |
| 2 | Architecture fit | 10 | 1.0 | 10 | Reuses Tier-1 DaemonSet + Prometheus + NATS KV; adds no CRD/store; honors 10b/05a `current` decisions; no D1–D26 conflict. |
| 3 | Security posture | 15 | 1.0 | 15 | Reads at the single authoritative egress hop (rule 05.4); rejects agent-span source (threat model); budgets fail-open justified vs 05 security controls that fail-closed independently. |
| 4 | Automatability | 10 | 0.5 | 5 | Rollback is an SSA ConfigMap patch; phase table is concrete; meter binary/wiring not yet authored (design-only phase by design). |
| 5 | Verifiability | 15 | 0.5 | 7.5 | CH5c envtest + CH5d e2e named with assertions; no harness authored yet (this phase is design-only). |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Five failure modes; retry double-count solved via request-id dedup; gateway restart via `increase()` reset handling; fail-open/closed split justified. |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | 168 lines; single responsibility (the one missing hop); no code dumped; refs not inline. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; frontmatter complete; all refs resolve; `status: draft`. |
| 9 | Observability | 5 | 1.0 | 5 | Contract series + two self-telemetry metrics; reuses locked span + events; no new span needed. |
| 10 | Operational readiness | 10 | 1.0 | 10 | Additive/reversible rollback; sequential phase plan with footprints; HA via existing DaemonSet + BTP fallback. |
| | **Total** | 100 | | **95.0** | |

Verdict: SHIP-as-draft (95.0 ≥ 90). Status stays `draft` per CH5 phase plan
(ADR scored to `current` when its implementation phases land + harness authored).

Top gaps:
1. Meter binary `cmd/token-meter/` not authored — CH5a.
2. Reconciler PromQL client still stubbed (EH7) — CH5c un-stubs once CH5b emits the live series.
3. Spec line 91 (`tokenUsageMetric` hand-wave) needs the additive ref-to-30 clarification — CH5c.

Next step: dispatch CH5a (infra-bootstrap) to author the meter processor, then
CH5b wiring, CH5c reconciler un-stub, CH5d e2e flip of EH7.

Cross-deps settled: 10b Prometheus-authoritative loop unchanged; 05a 429 flow +
short-window BTP fallback unchanged; 10a Tier-1 DaemonSet hosts the meter.
Cross-deps flagged: EH7 `revisit_when_token_metering_live` flips at CH5d; spec
`policy.keese.ai-v1alpha1.md` line 91 additive clarification at CH5c.
