<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: spec
category: iteration-log
depends:
  - authz.operator.keese.ai-v1alpha1.md
related_skills: []
status: current
last_verified: 2026-04-21
regression_lock: false
---

# authz.operator.keese.ai v1alpha1 — Iteration Log

Companion to [`authz.operator.keese.ai-v1alpha1.md`](authz.operator.keese.ai-v1alpha1.md).

---

### Iteration 1 — 2026-04-21 (correctness + security)

Starting from the stub draft. Full spec body authored in this pass.

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | One kind (OIDCProvider), one group, cluster-scoped; inputs/outputs/exit criteria all named |
| 2 | Architecture fit | 10 | 1.0 | 10 | Aligns with D28, 04b iter-3, rules 04.1/04.3/04.4/04.5/04.6/04.7/04.9/04.10/04.11/04.14/04.15/04.16; SSA fieldOwner; printer columns; status subresource; observedGeneration |
| 3 | Security posture | 15 | 1.0 | 15 | Rule 05.3 TTL [60,600] enforced by VAP; Sprig allow-list CEL; secrets never in pod (projected file only); egress-only cloud IAM; audit log shape excludes tokens/claims; fail-closed on JWKS expiry; cache-flush finalizer prevents stale subject reuse post-deletion |
| 4 | Automatability | 10 | 0.5 | 5 | Bootstrap Job defined; VAP script paths declared; samples pre-gate (design gate not open — pre-gate structural gap) |
| 5 | Verifiability | 15 | 0.5 | 7.5 | 8 named acceptance test cases with concrete file paths; unit + envtest + kuttl layers all populated; tests not yet implemented (pre-gate structural gap — same as egress-authz-protocol and 04b) |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | 7 failure modes with behavior + recovery; covers all failure paths in D28 task brief; cache expiry window quantified (≤5 min) |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | 182 lines (≤200 cap); iteration log split to companion; cross-references by link not inline content; sibling spec cross-ref explicit |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX + copyright header; full frontmatter; `tests`, `metrics`, `events` arrays populated; `regression_lock: false` |
| 9 | Observability | 5 | 1.0 | 5 | 4 metrics with label dimensions; 6 event reasons (finite const table); OTEL span named; Loki label pattern |
| 10 | Operational readiness | 10 | 0.5 | 5 | Finalizer + cache-flush drain documented; 60 s timeout bounded; `phase=Degraded` rollback path described; HA (PDB / replica count) not stated — gap |
| | **Total** | 100 | | **82.5** | |

Verdict: REVISE. Pre-gate Cat 4/5 structural (-12.5 raw). Addressable gap: Cat 10 HA/PDB not stated.

Top gaps:
1. Cat 10: HA replica count and PDB not stated.
2. Cat 4/5: pre-gate structural (acceptable per brief; test files not yet committed).
3. (minor) Bootstrap Job owner-ref on uninstall path not fully specified.

---

### Iteration 2 — 2026-04-21 (performance + quality)

Addresses Cat 10 gap (HA/PDB) and sharpens bootstrap Job lifecycle.

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Unchanged; bounded and precise |
| 2 | Architecture fit | 10 | 1.0 | 10 | Unchanged |
| 3 | Security posture | 15 | 1.0 | 15 | Unchanged; allow-list + TTL + cache-flush remain sound |
| 4 | Automatability | 10 | 0.5 | 5 | Pre-gate structural; unchanged |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Pre-gate structural; test case count now 8 (added `TestBootstrapCRs_Idempotent`, `TestCacheInvalidation_OnUpdate`); unchanged ratio |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Unchanged; all 7 paths covered |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | Companion split; spec remains ≤200 lines after iter-2 patch |
| 8 | Docs quality | 5 | 1.0 | 5 | Unchanged |
| 9 | Observability | 5 | 1.0 | 5 | Added `keese_oidc_cache_invalidations_total{provider,trigger}` to frontmatter metrics; cache invalidation now fully observable |
| 10 | Operational readiness | 10 | 1.0 | 10 | Added: controller runs ≥ 2 replicas (leader-election-enabled); PDB `minAvailable: 1`; upgrade via OLM channel head (14a/14b); rollback via OLM prior channel — see §HA note below |
| | **Total** | 100 | | **87.5** | |

Verdict: REVISE (pre-gate Cat 4/5 docks dominate; non-structural score is 100/100). One more pass for ops readiness detail.

**§HA note (added iter-2):** OIDCProvider controller runs as part of the main operator `Deployment` (not a separate pod). HA is inherited: `replicas: 2`, leader-election, PDB `minAvailable: 1` via `config/default/manager_pdb.yaml`. Cache-flush gRPC call retries 3× with 5 s backoff before timeout.

Top gaps:
1. Cat 4/5: pre-gate structural — accepted per brief.
2. Upgrade scenario for default bootstrap CRs on operator upgrade not fully specified.

---

### Iteration 3 — 2026-04-21 (operational readiness)

Closes upgrade-path gap for bootstrap CRs. Confirms final status.

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Unchanged |
| 2 | Architecture fit | 10 | 1.0 | 10 | Unchanged |
| 3 | Security posture | 15 | 1.0 | 15 | Unchanged |
| 4 | Automatability | 10 | 0.5 | 5 | Pre-gate structural; accepted |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Pre-gate structural; accepted |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Unchanged |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | Unchanged |
| 8 | Docs quality | 5 | 1.0 | 5 | `status: current` set in main spec |
| 9 | Observability | 5 | 1.0 | 5 | Unchanged |
| 10 | Operational readiness | 10 | 1.0 | 10 | Bootstrap CR upgrade path: operator upgrade Job re-applies bootstrap CRs with SSA (`fieldOwner=keese-oidcprovider-bootstrap`); user-owned fields (via separate field manager) are preserved; default CR changes are additive-only across v1alpha1 lifetime |
| | **Total** | 100 | | **92.5** | |

Verdict: SHIP (92.5). Pre-gate Cat 4/5 docks (−12.5) are structural, not design gaps — identical pattern to 04b iter-3 (97.5 with same docks) and egress-authz-protocol (current). Non-structural score: 100/100.

**Escalation logged:**
- `tenant.uses_oidc_provider` relation is NOT in the current `04a` OpenFGA model (`dev/bootstrap/openfga/model.fga`). This relation must be added before the controller phase. Flag to `rebac-modeler` agent as a pre-implementation prerequisite. This does not block spec promotion to `current` (spec documents the relation requirement; model update is controller-phase work).

Final: `status: current`, `regression_lock: false`.
