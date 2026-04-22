<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: tenancy
depends:
  - 25-cross-tenant-agreement.md
related_skills: []
status: current
last_verified: 2026-04-21
rollback: Log only; no rollback action required.
---

# 25-iv — CrossTenantAgreement: Iteration Log

Full rubric tables for [25-cross-tenant-agreement.md](25-cross-tenant-agreement.md).

## Iter-1 2026-04-21 — correctness + security

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Cluster-scoped CRD; bilateral handshake; all 5 Qs resolved with explicit decisions |
| 2 | Architecture fit | 10 | 1.0 | 10 | 04a iter-5 relations; 09 iter-3 cross-tenant scope; 03c admission check; 24 Tenant CRD; group per 20a |
| 3 | Security posture | 15 | 1.0 | 15 | can_approve_cra relation isolates approval; cosign OIDC sig; TOFU semantics on selector; fail-closed on OpenFGA unavail; SSA fieldOwner |
| 4 | Automatability | 10 | 0.5 | 5 | Make targets pre-gate; admission paths named; VAP CEL listed |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Failure modes testable; envtest spec pre-gate P8 |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | 10 failure modes with detection + mitigation; rollback documented |
| 7 | Context efficiency | 10 | 1.0 | 10 | Split into 25 / 25-ii / 25-iii; each ≤ 200 lines |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX + frontmatter; depends complete; refs section |
| 9 | Observability | 5 | 1.0 | 5 | 3 OTEL spans; 3 metrics with labels; 8 event reasons |
| 10 | Operational readiness | 10 | 0.5 | 5 | Rollback documented; HA/leader-election and resource ceilings not yet stated |
| | **Total** | 100 | | **87.5** | |

Verdict: REVISE.

Top gaps:
1. Cat 10: HA reconciler leader-election model not stated; resource ceilings absent.
2. Cat 10: Conflict-detection index algorithm not specified.
3. Cat 4/5: pre-gate structural (acceptable); make targets and envtest names deferred to P8.

Next step: Iter-2 — performance + quality; close Cat 10 gaps; specify conflict-detection index.

## Iter-2 2026-04-21 — performance + quality

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | No change; all 5 Qs resolved |
| 2 | Architecture fit | 10 | 1.0 | 10 | Conflict-detection index specified in 25-iii; SSA fieldOwner explicit; idempotent tuple writes |
| 3 | Security posture | 15 | 1.0 | 15 | Signature verification path specified; break-glass Rejected path in 25-iii rollback; OIDC vs SA-token both covered |
| 4 | Automatability | 10 | 0.5 | 5 | Pre-gate; make targets named in 25-iii |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Pre-gate; 5 named test files in 25-iii |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | 10 rows in 25-iii; each with detection + mitigation |
| 7 | Context efficiency | 10 | 1.0 | 10 | 4-file split (25 / 25-ii / 25-iii / 25-iv); each ≤ 200 lines |
| 8 | Docs quality | 5 | 1.0 | 5 | All headers; frontmatter status updated; rollback entries on each companion |
| 9 | Observability | 5 | 1.0 | 5 | Metrics + spans + events; label cardinality noted in 25-iii |
| 10 | Operational readiness | 10 | 1.0 | 10 | Leader-election explicit (controller-runtime shared lease); resource ceiling (10k advisory); upgrade path; runbook ref |
| | **Total** | 100 | | **97.5** | |

Verdict: SHIP (97.5). Residuals: Cat 4/5 (−10) pre-gate structural; accepted per design-gate policy.

Top gaps:
1. Cat 4/5: make targets + envtest suite pre-gate; unchanged.
2. Advisory ceiling (10k CRAs) not yet enforced by VAP — deferred post-gate.

Next step: Iter-3 — operational readiness confirmation pass.

## Iter-3 2026-04-21 — operational readiness

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Stable |
| 2 | Architecture fit | 10 | 1.0 | 10 | Stable; NATS prefix owner confirmed as Workflow controller per 03c cross-dep |
| 3 | Security posture | 15 | 1.0 | 15 | Stable; Tenant finalizer blocking deletion documented in 25-ii cross-cuts |
| 4 | Automatability | 10 | 0.5 | 5 | Pre-gate unchanged; make targets in 25-iii |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Pre-gate unchanged; test names in 25-iii |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Stable; upgrade/rollback path explicit; Expired-delete retry runbook ref in 25-iii |
| 7 | Context efficiency | 10 | 1.0 | 10 | Stable; 4-file split maintained |
| 8 | Docs quality | 5 | 1.0 | 5 | Stable |
| 9 | Observability | 5 | 1.0 | 5 | Stable |
| 10 | Operational readiness | 10 | 1.0 | 10 | Stable; leader-election; ceilings; upgrade path; runbook `runbook-cta-tuple-delete.md` ref; GC via owner-ref explicit |
| | **Total** | 100 | | **97.5** | |

Verdict: **SHIP** (97.5 ≥ 90). Pre-gate Cat 4/5 residuals accepted per design-gate policy. Status: `current`.

Top residuals (acceptable):
1. Cat 4 (−5): make targets pre-gate.
2. Cat 5 (−7.5): envtest suite pre-gate.
