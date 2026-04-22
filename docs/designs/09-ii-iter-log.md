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

## Iteration 2 — 2026-04-21

Emphasis: **Correctness & security** (pass 2 of 3).

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Hybrid NATS model + 4 a2a auth modes bounded with explicit enforcement points. |
| 2 | Architecture fit | 10 | 1.0 | 10 | workspace-sa uses kubernetes-default OIDCProvider (04b); can_message cross-refs 04a; Workflow controller (03) writes tuples. |
| 3 | Security posture | 15 | 1.0 | 15 | VAP blocks `none` in prod; JWT before OpenFGA; no keys in spec; streamConfig gated by annotation; rule-05 satisfied. |
| 4 | Automatability | 10 | 0.5 | 5 | Admission checks named; make targets pre-gate. |
| 5 | Verifiability | 15 | 0.5 | 7.5 | 10 failure modes; a2a auth testable in envtest (pre-gate). |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Hybrid stream paths split; P8 runbook flagged; a2a deny + unsafe-prod enumerated. |
| 7 | Context efficiency | 10 | 1.0 | 10 | ≤ 200 lines; cross-dep flags section; no inline code blocks. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX + frontmatter; depends includes 04a + 04b; refs valid. |
| 9 | Observability | 5 | 1.0 | 5 | a2a auth span + metric added; 4 new events. |
| 10 | Operational readiness | 10 | 1.0 | 10 | P8 runbook flagged; auto-create finalizer annotation-scoped; rollback unchanged. |
| | **Total** | 100 | | **95** | |

Verdict: **SHIP** (95 ≥ 95). Status held at draft pending iter-3 reframe (workspace-as-boundary model supersedes 4-mode a2a).

Top gaps:
1. Cat 4/5: make targets + envtest pre-gate — unchanged.
2. `workspace#can_message` not yet in model.fga; blocked by 04a iter-5.
3. Per-peer `keese-a2a-<uid>` audience not in 04b audienceTemplates; blocks SA token minting for a2a.

## Iteration 3 — 2026-04-21

Emphasis: **Operational readiness** (pass 3 of 3) + architectural reframe.

Changes from iter-2: dropped `user-oidc` and `none` a2a modes (4 → 2); replaced
`workspace#can_message` with 04a iter-5 `workspace.messageable_from`; audience updated
to `keese-wf-<workflow-run-uid>` (04b iter-3 `workflowRun` template); new `spec.a2a.scope`
field (`intra-tenant | cross-tenant`); NATS-as-primary subsection; VAP guard
`CrossTenantAgreementMissing` for `scope: cross-tenant`; design 25 added to depends.

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Two modes + explicit scope enum; NATS-primary rationale stated; no ambiguous paths remain. |
| 2 | Architecture fit | 10 | 1.0 | 10 | 04a iter-5 relations used correctly; 04b workflowRun audience; 03 iter-3 Workflow controller ownership; D29 CRA guard; all cross-deps flagged. |
| 3 | Security posture | 15 | 1.0 | 15 | `none` and `user-oidc` deleted; `scope: cross-tenant` VAP-blocked without CRA; `messageable_from` tuple is runtime evidence; JWT validation at NATS layer; no keys in spec; rule-05 satisfied. |
| 4 | Automatability | 10 | 0.5 | 5 | VAP and admission checks named; make targets still pre-gate. |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Failure modes consistent with 2-mode model; `CrossTenantAgreementMissing` VAP testable; envtest pre-gate. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | All 10 failure modes updated; `UnsafeTransportForbidden` removed; `CrossTenantAgreementMissing` added; P8 runbook still flagged. |
| 7 | Context efficiency | 10 | 1.0 | 10 | Primary doc 198 lines; score tables in companion; cross-dep flags updated in-place. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX + frontmatter on both files; depends updated (25 added, stale `can_message` ref removed); refs section includes companion. |
| 9 | Observability | 5 | 1.0 | 5 | `scope` label added to a2a auth metric; `CrossTenantAgreementMissing` event replaces `UnsafeTransportForbidden`. |
| 10 | Operational readiness | 10 | 1.0 | 10 | Rollback unchanged; VAP blocks invalid `scope` at admission; CRA bilateral handshake is the upgrade path for cross-tenant. |
| | **Total** | 100 | | **97.5** | |

Verdict: **SHIP** (97.5 ≥ 95). `status` flipped to `current` in primary doc.

Top gaps:
1. Cat 4/5: make targets + envtest remain pre-gate — acceptable; design gate not yet open.
2. 04b iter-3 `workflowRun` audience template in flight — blocks SA minting for a2a and NATS.
3. Design 25 (CrossTenantAgreement) is stub-only — `scope: cross-tenant` VAP guard is
   specced here but CRA admission logic awaits design 25 full authoring.
