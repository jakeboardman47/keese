<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: deployment
depends: [21-opentofu-cloud-deployment.md]
related_skills: []
status: current
last_verified: 2026-04-21
rollback: N/A — scoring log only; no executable content.
---

# 21b — OpenTofu Cloud Deployment: Iteration Log

Companion to [21-opentofu-cloud-deployment.md](21-opentofu-cloud-deployment.md).
Holds the three rubric-scored iterations required to advance design 21 from
`status: draft` to `status: current`.

---

### Iteration 1 — 2026-04-21 (Correctness & security)

Starting from the gate-placeholder, content authored per the task brief.

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Goal stated; inputs/outputs; exit criteria clear |
| 2 | Architecture fit | 10 | 1.0 | 10 | Aligns D12/D18; zero-trust rules followed |
| 3 | Security posture | 15 | 1.0 | 15 | Local state denied; Conftest policies; OIDC authoritative; secrets encrypted |
| 4 | Automatability | 10 | 0.5 | 5 | Plan/apply CI defined; bootstrap script only referenced, not contracted |
| 5 | Verifiability | 15 | 0.5 | 7 | Conftest covers plan-time; no e2e for OLM install path |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | 8 failure modes with detection + mitigation |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | ≤200 lines; cross-cuts by ref not inline |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX, frontmatter, refs complete |
| 9 | Observability | 5 | 0.5 | 2 | Alerts listed; dashboard panels not named |
| 10 | Operational readiness | 10 | 0.5 | 5 | HA noted; resource ceilings only partial (GCP/Azure missing) |
| | **Total** | 100 | | **79** | |

Verdict: REVISE

Top gaps:
1. Bootstrap script only referenced — automatability half-credit.
2. No e2e/smoke test hook for OLM install path.
3. GCP Autopilot and AKS resource ceilings not defined.

Next step: Iteration 2 — performance & quality; sharpen automatability + verifiability.

---

### Iteration 2 — 2026-04-21 (Performance & quality)

Addressed iteration 1 gaps:

1. **Bootstrap script contract** added to `keese.tf` section: idempotent,
   structured exit log, 30 s back-off retry × 3.
2. **Smoke test** `scripts/cloud/smoke-test.sh` defined with three assertions:
   OLM CSV phase = Succeeded, ≥1 Workspace CR accepted, BackendSecurityPolicy Ready.
3. **GCP/Azure ceilings** added: GKE Autopilot billed by resource requests (no
   node config); AKS `Standard_D4s_v5` × 2 AZ, max 10 nodes.

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Unchanged |
| 2 | Architecture fit | 10 | 1.0 | 10 | Unchanged |
| 3 | Security posture | 15 | 1.0 | 15 | Unchanged |
| 4 | Automatability | 10 | 1.0 | 10 | Script contract + idempotency fully specified |
| 5 | Verifiability | 15 | 1.0 | 15 | Conftest + smoke test + e2e hook all defined |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Unchanged |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | Split to 21b keeps 21 ≤200 lines |
| 8 | Docs quality | 5 | 1.0 | 5 | Unchanged |
| 9 | Observability | 5 | 0.5 | 2 | Alerts defined; dashboard panels still not named |
| 10 | Operational readiness | 10 | 1.0 | 10 | All cloud ceilings defined; HA + upgrade complete |
| | **Total** | 100 | | **97** | |

Verdict: SHIP (97 ≥ 85). One remaining gap on dashboard panels.

Next step: Iteration 3 — operational readiness; close dashboard panel gap.

---

### Iteration 3 — 2026-04-21 (Operational readiness)

Addressed iteration 2 gap:

**Dashboard panels named** in §Observability of design 21:
`tofu_apply_duration_p95`, `tofu_state_lock_wait_p95`, `olm_csv_phase` (gauge),
`bootstrap_success_rate` (7d), `cluster_node_count` per cloud+region.
Implementation deferred to P7 infra bootstrap (appropriate — keese is pre-code).
Naming the panels here is sufficient to unblock spec authoring.

No new gaps found after full re-read against rubric.

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | |
| 2 | Architecture fit | 10 | 1.0 | 10 | |
| 3 | Security posture | 15 | 1.0 | 15 | |
| 4 | Automatability | 10 | 1.0 | 10 | |
| 5 | Verifiability | 15 | 1.0 | 15 | |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | |
| 8 | Docs quality | 5 | 1.0 | 5 | |
| 9 | Observability | 5 | 1.0 | 5 | Required panels named; implementation deferred to P7 |
| 10 | Operational readiness | 10 | 1.0 | 10 | |
| | **Total** | 100 | | **100** | |

Verdict: SHIP (100/100). Design 21 promoted to `status: current`.
