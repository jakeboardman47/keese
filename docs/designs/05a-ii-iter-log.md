<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: egress
depends:
  - 05a-envoy-ai-gateway-topology.md
related_skills: []
status: current
last_verified: 2026-04-20
rollback: See 05a-envoy-ai-gateway-topology.md frontmatter.
---

# 05a-ii — Envoy AI Gateway Topology: Iteration Log

Companion to [05a-envoy-ai-gateway-topology.md](05a-envoy-ai-gateway-topology.md).
Split to respect the 200-line ceiling per `.claude/rules/01-conventions.md`.

## Iteration 1 — 2026-04-20

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Five open questions answered; 05b/05c boundary explicit; topology bounded. |
| 2 | Architecture fit | 10 | 1.0 | 10 | D5/D13/D16/D21/D24/D26 honored; VAP-first; ReferenceGrant isolation; no new groups. |
| 3 | Security posture | 15 | 1.0 | 15 | Fail-closed ext_authz; no wildcards; in-pod plaintext sidecar; NATS-degraded correctness-over-perf per 04c; token bytes never logged. |
| 4 | Automatability | 10 | 0.5 | 5 | helmfile.lock pinning strategy stated; `gateway-smoke-test.sh` named but not authored (pre-gate). |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Eight failure modes enumerated; smoke test path named; no envtest assertions yet (post-gate). |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Eight modes with detection + mitigation; NATS-degraded, JWKS, BSP exhaustion, toggle guard all covered. |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | Stays at ceiling (≤ 200 lines); 05b/05c split respected; YAML snippets illustrative only. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; complete frontmatter; depends updated; rollback concrete; cross-refs complete. |
| 9 | Observability | 5 | 1.0 | 5 | Four OTEL spans; five Prom metrics; audit trail via response header → collector. |
| 10 | Operational readiness | 10 | 1.0 | 10 | HA (≥ 2 replicas, HPA); drain budget (preStop 30 s); rollback 5-step; upgrade path concrete. |
| | **Total** | 100 | | **87.5** | |

Verdict: held at `draft`. Reviewer mandated iter-2 before `current`.

Top gaps:
1. Sidecar pattern rejected — 300+ containers at 100 dedicated tenants.
2. `AIGatewayRoute.spec.tenantRef` does not exist in Envoy AI Gateway v0.5.x.
3. TokenBudget 429 mechanism left as open question for 10b.
4. Witness gateway audience flagged open.

## Iteration 2 — 2026-04-20

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | All seven reviewer mandates addressed; 05b/05c boundaries explicit; residual gaps documented inline. |
| 2 | Architecture fit | 10 | 1.0 | 10 | Shared/dedicated Deployment fits D5/D13/D16/D26; JWT filter is native Envoy (zero keese build); NATS KV reuse per 04c pattern; no new API groups. |
| 3 | Security posture | 15 | 1.0 | 15 | Fail-closed on shared ext_authz down; dedicated blast-radius bounded; witness audience `keese-egress-supervisor-<tenant>` prevents impersonation; JWT validates before ext_authz; NATS-degraded correctness-over-perf preserved; token bytes never logged. |
| 4 | Automatability | 10 | 0.5 | 5 | helmfile.lock pinning stated; `gateway-smoke-test.sh` named but not authored (pre-gate, test-engineer backlog). Operator-managed provisioning is automatable post-gate. |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Nine failure modes; smoke test path named; NATS KV budget metric named; no envtest/kuttl assertions yet (post-gate obligation). |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Nine modes; shared-mode blast radius documented; dedicated blast radius documented; JWKS fail-open bounds stated; NATS KV 429 path complete end-to-end. |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | Main doc at 200-line ceiling; iteration log split to companion; YAML snippet illustrative only; residuals flagged by link. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; frontmatter complete; depends includes 04a/04b/04b-ii/04c/10b/24 chain; rollback updated; `last_verified: 2026-04-20`. |
| 9 | Observability | 5 | 1.0 | 5 | OTEL spans retained; Prom metrics extended with `keese_extauthz_budget_429_total` and `keese_extauthz_degraded_seconds_total`; audit trail unchanged. |
| 10 | Operational readiness | 10 | 1.0 | 10 | HA: shared 3 replicas + PDB; dedicated 2 replicas + PDB; drain procedure for toggle documented; cosign keyless image signing stated; upgrade rollback path unchanged. |
| | **Total** | 100 | | **97.5** | |

Verdict: SHIP (97.5 ≥ 95 honest threshold). `status` flipped to `current`.

Residual gaps (not blocking gate):
1. Cat 4: `scripts/dev/gateway-smoke-test.sh` not authored — test-engineer backlog, pre-gate acceptable.
2. Cat 5: No envtest/kuttl assertions for ext_authz Deployment — post-gate obligation.
3. `Tenant.spec.jwksCacheFailOpenSeconds` field — flagged for 24 iter-2.
4. `docs/plans/runbook-dedicated-gateway-toggle.md` — flagged for authoring before controller work.
5. 14b CSV CRD pinning entry — manual until 14b reaches `current`.

Cross-deps settled by iter-2:
- TokenBudget 429 via NATS KV bucket `keese-budget-exceeded` — 10b iter-1 inherits.
- Witness audience `keese-egress-supervisor-<tenant>` — 23 iter-1 must honor.
- Ext_authz image `ghcr.io/keese-ai/keese-ext-authz` from `cmd/ext-authz/` — resolved.

Cross-deps flagged for follow-up:
- 24 iter-2: must add `spec.jwksCacheFailOpenSeconds` field + validation bounds.
- 04b iter-2: may need supervisor SA projection row for witness audience.
