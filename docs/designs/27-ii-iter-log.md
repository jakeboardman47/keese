<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: lifecycle
depends: [27-feature-gates-openfeature.md]
related_skills: [doc-authoring, score-plan]
status: current
last_verified: 2026-05-06
---

# 27-ii — Feature gates: iteration log

Companion to [27-feature-gates-openfeature.md](27-feature-gates-openfeature.md).
Rubric: [docs/plans/rubric.md](../plans/rubric.md).

## Iteration 1 — 2026-05-06 (Correctness & Security)

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Decision sentence + 11 bounded sections |
| 2 | Architecture fit | 10 | 1.0 | 10 | Honors rules 04.1, 04.2, 04.7, 06; new kind under existing `policy.keese.ai` |
| 3 | Security posture | 15 | 0.5 | 7.5 | RBAC explicit; threat: a privileged actor could write the projected CM directly without going through the CRD |
| 4 | Automatability | 10 | 0.5 | 5 | `kubectl patch` documented; no `make` targets / no list/diff scripts named |
| 5 | Verifiability | 15 | 1.0 | 15 | Unit + envtest + kuttl all named with concrete acceptance |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | F1–F6 with detection + mitigation each |
| 7 | Context efficiency | 10 | 1.0 | 10 | 198 lines; iteration log split out |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX, frontmatter, links resolve |
| 9 | Observability | 5 | 1.0 | 5 | Three metrics + structured transition event |
| 10 | Operational readiness | 10 | 0.5 | 5 | Restart-required + drift SLO + last-good cache present; no rollback path for the gate plumbing itself |
| | **Total** | 100 | | **82.5** | |

Verdict: REVISE

Top gaps:
1. Security — CM-tamper threat unaddressed (kyverno policy + cosign trust root).
2. Automatability — no `make featuregate-*` targets named.
3. Operational readiness — no kill-switch / rollback for the plumbing.

Next step: iteration 2 lands kyverno policy reference, two `make`
targets, and a §11 Rollback section.

## Iteration 2 — 2026-05-06 (Operational Readiness)

Additions to the main doc:
- §6 now references kyverno `ClusterPolicy` at
  `config/featuregates/policy.yaml` denying CM writes from any SA
  other than `keese-controller-manager`, anchored on the rule-05.12
  + TD-P1-04 cosign trust root.
- §6 now names `make featuregate-list` + `make featuregate-diff` as
  the operator-facing tooling.
- §11 Rollback documents the kill switch (`kubectl delete cm` +
  scale controller to 0) and the safe-default behavior (alpha→off,
  beta→on, GA paths unchanged) when the projection is missing.

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | |
| 2 | Architecture fit | 10 | 1.0 | 10 | |
| 3 | Security posture | 15 | 1.0 | 15 | kyverno policy + cosign trust root close the CM-tamper gap |
| 4 | Automatability | 10 | 1.0 | 10 | Two `make` targets named with `scripts/` referent |
| 5 | Verifiability | 15 | 1.0 | 15 | |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | |
| 7 | Context efficiency | 10 | 1.0 | 10 | Held at 198 lines |
| 8 | Docs quality | 5 | 1.0 | 5 | |
| 9 | Observability | 5 | 1.0 | 5 | |
| 10 | Operational readiness | 10 | 1.0 | 10 | Kill switch + safe-default rollback behavior documented |
| | **Total** | 100 | | **100** | |

Verdict: SHIP

No top-3 gaps remain at iteration 2. Iteration 3 (Performance &
Quality) is unnecessary — the design has no perf-critical hot path
beyond the sub-µs map lookup, which is implementation detail for
27b.

## Next steps (after architect approval)

1. Promote main doc `status: draft → current`, bump
   `last_verified`, add to `docs/designs/README.md` index.
2. Add a CLAUDE.md task table row: "Toggle a keese capability via
   FeatureGate" → load 27 first, then 27-ii.
3. Author `docs/specs/policy.keese.ai-v1alpha1.md` FeatureGate
   section.
4. Implementation plan: `docs/plans/td-feature-gates-openfeature.md`
   with phased delivery (gate plumbing → cosign retrofit →
   keese-authz → OTEL → recipe-source → guardrail → rest).
5. Author `27b-feature-gate-catalog.md` enumerating every gate the
   project ships, owners, and stage history.
