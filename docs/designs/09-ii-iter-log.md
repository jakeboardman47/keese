<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: transport
depends: [09-transport-crd.md]
related_skills: []
status: current
last_verified: 2026-04-21
rollback: n/a — iteration log only; no operational decisions here.
---

# 09-ii — Transport CRD Iteration Log

Full rubric tables for `09-transport-crd.md`. See that doc for decisions.

## Iteration 1 — 2026-04-21

Emphasis: **Correctness & security** (pass 1 of 3).

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Decision sentence; 5 open questions answered with concrete field tables. |
| 2 | Architecture fit | 10 | 1.0 | 10 | Discriminated one-of per rule 04.6; namespace-scoped per 20a; SSA fieldOwner named; no rule violations. |
| 3 | Security posture | 15 | 1.0 | 15 | No credentials in spec; cert-manager referenced not embedded; fail-closed NetworkPolicy gap explicitly flagged to 12; rule 05 compliance stated. |
| 4 | Automatability | 10 | 0.5 | 5 | VAP CEL expressions stated; controller path named; finalizer ID given. Code not yet authored — honest dock; pre-gate acceptable. |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Failure modes table concrete; events.go pattern named; test skeletons implied by rule 04.15. No envtest assertions authored yet — pre-gate acceptable. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | 7 failure modes with detection + mitigation; type-immutability reject; NetworkPolicy cross-dep flagged; buffer overflow mitigated. |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | Doc splits at 200-line boundary (09 + 09-ii); single responsibility; no inline code blobs beyond short CEL. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX header; frontmatter complete; depends lists all 6 dep docs; rollback concrete; last_verified set. |
| 9 | Observability | 5 | 1.0 | 5 | 3 metric families; OTEL spans named; 7 event reasons in events.go table; printer columns listed. |
| 10 | Operational readiness | 10 | 1.0 | 10 | Lifecycle phases defined; mutable vs immutable fields stated; cert rotation via cert-manager native; finalizer scope and stuck-guard stated; NATS stream ownership annotated. |
| | **Total** | 100 | | **92.5** | |

Verdict: **SHIP** (92.5 ≥ 90). `status` flipped to `current`.

Top gaps:
1. Cat 4/5: Controller code and envtest assertions not yet authored — backlog for controller-author agent, pre-gate acceptable.
2. Cat 5: No named test files — same pre-gate backlog.
3. Cross-dep flag: 12 (NetworkPolicy stub) must add NATS:4222 + Envoy:443 egress rules before gate open.

Next step: 12 iter-1 must absorb the NetworkPolicy flag. Spec
`docs/specs/transport.operator.keese.ai-v1alpha1.md` may begin once 09 reaches
`current` (gate rule satisfied).
