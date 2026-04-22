<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: spec
category: iter-log
depends: [observability.operator.keese.ai-v1alpha1.md]
related_skills: []
status: current
last_verified: 2026-04-21
---

# observability.operator.keese.ai v1alpha1 — Iteration log

Companion to [observability.operator.keese.ai-v1alpha1.md](observability.operator.keese.ai-v1alpha1.md).
Split for 200-line ceiling per rule 01 + rule 03.

### Iteration 1 — 2026-04-21

Emphasis: Correctness & security.

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Single kind; bounded inputs/outputs; billing delegated to 21. |
| 2 | Architecture fit | 10 | 1.0 | 10 | Prometheus counter from 10a; NATS boolean from 10b; Envoy composition from 05a; all locked decisions respected. |
| 3 | Security posture | 15 | 1.0 | 15 | `hard` default fail-closed; Prometheus outage = no false-clear; ReBAC marker on limits; no tokens in events; NATS KV boolean only per 10b iter-2. |
| 4 | Automatability | 10 | 0.5 | 5 | Finalizer cleanup path described; SSA fieldOwner named; no `make` target authored yet (pre-gate). |
| 5 | Verifiability | 15 | 1.0 | 15 | Four named envtest files with explicit assertions; mocked interfaces named; CRD loading path specified. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Five failure modes; Prometheus outage fail-closed; NATS+controller both down; cardinality ceiling; reset failure runbook. |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | 200-line ceiling respected via companion split; single responsibility; no inline code blobs. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; complete frontmatter; all deps listed; status current. |
| 9 | Observability | 5 | 1.0 | 5 | Five Prom metrics; seven events; 429 flow diagram; conditions table. |
| 10 | Operational readiness | 10 | 0.5 | 5 | Upgrade/rollback via `exhaustionMode: disabled` described; cardinality ceiling stated; KV key format documented. Envoy RateLimit projection refresh-on-scale-down behavior not yet specified. |
| | **Total** | 100 | | **90** | |

Verdict: SHIP (90 ≥ 90).

Top gaps:
1. Cat 4: No `make` target or bootstrap script for token-budget smoke test (pre-gate acceptable).
2. Cat 10: Envoy RateLimit projection behavior on scale-down (remaining goes negative) not specified.
3. Cat 10: `pricebookRef` interaction with status not fully documented (billing delegated to 21, acceptable).

Next step: Iter 2 — performance & quality: tighten Envoy RateLimit scale-down behavior; add PromQL cost note.

### Iteration 2 — 2026-04-21

Emphasis: Performance & quality.

Gaps addressed:
- Envoy RateLimit scale-down: when `remaining` falls below zero, controller clamps projected
  budget to 0 (not negative); avoids confusing Envoy `local_rate_limit` with a negative token
  value. NATS KV boolean continues to be the hard-stop signal.
- PromQL query cost: one query per `spec.limits[i]` per 10 s reconcile. 100 tenants × 10
  limits → 1 000 queries / 10 s = 100 QPS. Within Prometheus operational range per 10b design.
- Frontmatter `tests.envtest` array populated with four file paths.
- `consumedPrevious` population semantics on window boundary made explicit in main spec.

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Unchanged; billing delegation explicit. |
| 2 | Architecture fit | 10 | 1.0 | 10 | Clamped-to-zero scale-down aligns with Envoy local_rate_limit semantics; no rule violation. |
| 3 | Security posture | 15 | 1.0 | 15 | Unchanged; no new attack surface from clamping. |
| 4 | Automatability | 10 | 0.5 | 5 | Still no `make` target; pre-gate acceptable. |
| 5 | Verifiability | 15 | 1.0 | 15 | Envtest files + assertions complete; clamp behavior testable via exceeded_transition_test. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Unchanged; negative-remaining scenario now handled (clamped). |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | Additions kept inline; no new files needed. |
| 8 | Docs quality | 5 | 1.0 | 5 | Frontmatter updated; status current. |
| 9 | Observability | 5 | 1.0 | 5 | Unchanged. |
| 10 | Operational readiness | 10 | 1.0 | 10 | Scale-down clamping + PromQL cost quantified; upgrade/rollback complete. |
| | **Total** | 100 | | **95** | |

Verdict: SHIP (95 ≥ 90).

Top gaps:
1. Cat 4: `make` target not authored (pre-gate; controller-author backlog).
2. pricebookRef USD status — flagged to design 21; acceptable.
3. kuttl e2e test suite — post-gate; not blocking spec.

Next step: Iter 3 — operational readiness: verify HA, upgrade path, resource ceilings complete.

### Iteration 3 — 2026-04-21

Emphasis: Operational readiness.

Gaps reviewed:
- HA: controller liveness/readiness probes must satisfy rule 06.8. Controller
  `terminationGracePeriodSeconds: 60`; probes: `initialDelaySeconds: 15`,
  `periodSeconds: 10`, `failureThreshold: 6` → 15 + 60 = 75 ≥ 60. Satisfied.
- SIGTERM: reconcile queue drains; NATS KV in-flight writes flushed; OTEL ForceFlush before
  exit. Matches rule 06.2 (operator drain budget 60 s).
- Upgrade: `exhaustionMode` defaults to `hard` for existing CRs via defaulting webhook.
  No Prometheus counter reset on upgrade. NATS KV keys persist through controller restart
  (durable by design, 10b).
- Rollback: `kubectl patch tokenbudget -A --type=merge -p '{"spec":{"exhaustionMode":"disabled"}}'`
  disables enforcement cluster-wide; KV signals drain at next reconcile when under-limit.
- Samples: `config/samples/observability_v1alpha1_tokenbudget_minimal.yaml` (scope: Tenant,
  one limit, hard mode) and `config/samples/observability_v1alpha1_tokenbudget_full.yaml`
  (scope: Workspace, three model limits + aggregate, soft mode, pricebookRef). Both must
  pass `kubectl apply --dry-run=server`.

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Unchanged. |
| 2 | Architecture fit | 10 | 1.0 | 10 | Probe math satisfies rule 06.8; SIGTERM drain matches rule 06.2. |
| 3 | Security posture | 15 | 1.0 | 15 | Unchanged. |
| 4 | Automatability | 10 | 0.5 | 5 | Rollback patch command explicit; samples named; no `make` target yet. |
| 5 | Verifiability | 15 | 1.0 | 15 | Samples named + dry-run requirement stated; four envtest suites complete. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | SIGTERM drain + SIGKILL loss documented (KV durable); probe math verified. |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | Companion split keeps both files within ceiling. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; frontmatter; status: current. |
| 9 | Observability | 5 | 1.0 | 5 | Unchanged. |
| 10 | Operational readiness | 10 | 1.0 | 10 | Probe math; SIGTERM; upgrade/rollback; samples; cardinality ceiling; HA via BackendTrafficPolicy fallback. |
| | **Total** | 100 | | **95** | |

Verdict: SHIP (95 ≥ 90). Status: `current`.

Top residual gaps (pre-gate acceptable):
1. Cat 4: `make` target for token-budget smoke test — controller-author + test-engineer backlog.
2. kuttl e2e suite — post-gate.
3. pricebookRef USD status interaction — design 21 dependency.

Cross-deps settled:
- 10a: Prometheus pipeline + OTEL Tier 1 scrape path locked.
- 10b: NATS KV boolean-only pattern, PromQL query shape, cardinality ceiling — all locked.
- 05a: `keese-budget-exceeded` NATS KV bucket + `local_reply_config` 429 locked.
- 04a: `budget:can_enforce` tuple relation locked.
- 06: `min()` merge lattice across GuardrailBinding.spec.tokenBudget confirmed.
- 24: `Tenant.spec.tokenBudgetRef` semantics confirmed.

Cross-deps flagged:
- 21: pricebookRef USD billing export (stub; not blocking).
- 22: WorkflowRun 429 pause on budget exhaustion (not blocking this spec).
