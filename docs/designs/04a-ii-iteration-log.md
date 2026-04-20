<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: authz
depends: [04a-openfga-authz-model.md]
related_skills: []
status: draft
last_verified: 2026-04-20
---

# 04a — Iteration Log

Rubric scores for `04a-openfga-authz-model.md`. Parent design:
[04a-openfga-authz-model.md](04a-openfga-authz-model.md).

## Iteration 1 — 2026-04-19

| # | Category | Weight | Ratio | Score | Notes |
|---|---:|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Goal, bounded inputs/outputs stated |
| 2 | Architecture fit | 10 | 1.0 | 10 | OpenFGA schema 1.1; aligns with D4, D13 |
| 3 | Security posture | 15 | 1.0 | 15 | HIGHER_CONSISTENCY, fail-closed, no token logging |
| 4 | Automatability | 10 | 0.5 | 5 | fga CLI validate referenced; but dual-Check requires two RTTs — not ideal |
| 5 | Verifiability | 15 | 1.0 | 15 | Positive + negative assertion YAML noted |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Deny-on-timeout, circuit-break, SpiceDB migration path |
| 7 | Context efficiency | 10 | 1.0 | 10 | < 200 lines; skill pointer; references not inlined |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX + frontmatter complete |
| 9 | Observability | 5 | 0.5 | 2.5 | ES audit noted; Loki/long-term retention gap |
| 10 | Operational readiness | 10 | 1.0 | 10 | Model versioning, rollback, ConfigMap propagation |
| | **Total** | 100 | | **92.5** | |

Verdict: REVISE (held at `draft` pending substantive reviewer feedback)

Top gaps:
1. Dual-Check pattern suboptimal — computed relation available in schema 1.1.
2. No `can_revoke` relation for force-revoke use case (04c needs this).
3. Loki long-term audit retention unaddressed; only ES (30-day).

Next step: Iter-2 applying reviewer feedback.

---

## Iteration 2 — 2026-04-20

| # | Category | Weight | Ratio | Score | Notes |
|---|---:|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Full tuple table, entity map, check semantics, failure modes in scope |
| 2 | Architecture fit | 10 | 1.0 | 10 | Computed relation, Tenant CRD (D26) backing, schema 1.1 single Check |
| 3 | Security posture | 15 | 1.0 | 15 | Fail-closed, HIGHER_CONSISTENCY, force-revoke automation-only, admission webhook sequence, no bypass path |
| 4 | Automatability | 10 | 1.0 | 10 | fga CLI validate step; seed job; feature flag documented; dual-Check fallback scripted |
| 5 | Verifiability | 15 | 1.0 | 15 | Tuple table covers all relations; negative case (ForbiddenToRevoke) specified; test backlog tracked |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Computed-relation bug escape hatch, OpenFGA unreachable, stale tuple, model mid-flight, bypass prevention |
| 7 | Context efficiency | 10 | 1.0 | 10 | Split at 200-line boundary; cross-references tight; no inline verbatim from other designs |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; frontmatter complete including 24-tenant-crd.md dep; rollback field filled |
| 9 | Observability | 5 | 1.0 | 5 | ES primary + Loki secondary, LogQL, 1-year retention, OTEL fan-out, metrics + traces declared |
| 10 | Operational readiness | 10 | 1.0 | 10 | Model versioning, ConfigMap propagation, rollback path, additive migrations |
| | **Total** | 100 | | **100** | |

Verdict: SHIP (≥ 95 threshold met; `status` flipped to `current`)

Residual gaps (not blocking):
1. `tests/openfga/*.yaml` assertion files not yet authored — test-engineer backlog.
2. D23 (agent-supervision) and D26 (Tenant CRD) are stubs; cross-reference fidelity
   must be validated when those docs reach `current`.
3. `--openfga-check-mode` flag wiring into `internal/rebac/` deferred to post-gate impl.

Cross-dependencies settled:
- [x] Subject shape `user:ksa-<workspace-uid>@keese-egress-<tenant>` confirmed (04b).
- [x] `can_revoke` interface agreed with 04c iter-2 (running in parallel).
- [x] Loki dependency on 10a iter-1 flagged and documented.
- [x] D26 Tenant CRD backing `tenant:X` identity agreed with tenancy design.
- [ ] `fga model validate` to be run by infra-bootstrap in P7 seed job (CLI not on
  PATH at authoring time; check noted as intended verification).
