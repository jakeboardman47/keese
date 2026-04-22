<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: spec
category: contract
depends: [agent-runtime-spi.md]
related_skills: []
status: current
last_verified: 2026-04-21
---

# agent-runtime-spi — iteration log

Full rubric tables for [`agent-runtime-spi.md`](agent-runtime-spi.md).

## Iteration 1 — 2026-04-21 (Correctness & security)

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Goal stated; bounded inputs/outputs/exit criteria; all 10 required items addressed. |
| 2 | Architecture fit | 10 | 1.0 | 10 | Static init() registration; no panic/Fatal; SSA fieldOwner; controller-runtime only. |
| 3 | Security posture | 15 | 1.0 | 15 | No creds in Session; SA token identity only; NetworkPolicy fail-closed; checkpoint scoped per workspace-uid. |
| 4 | Automatability | 10 | 0.5 | 5 | apidiff script named; admission wired to registry; envtest scripts pre-gate (P8). |
| 5 | Verifiability | 15 | 0.5 | 7.5 | 9 named test cases; compile-time interface check via blank var; envtest pre-gate. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | 8 failure modes; partial checkpoint + resume; GUPP breach; StreamEvents stall all covered. |
| 7 | Context efficiency | 10 | 1.0 | 10 | ≤ 200 lines; no inline design content; skills/agents referenced not inlined. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; frontmatter complete; depends all 7 owning designs; events/metrics/tests arrays populated. |
| 9 | Observability | 5 | 1.0 | 5 | OTEL span per method; metrics in frontmatter; events table and frontmatter consistent. |
| 10 | Operational readiness | 10 | 0.5 | 5 | Upgrade/rollback paths present; migration plan gate named; v1beta1 conversion deferral explicit. |
| | **Total** | 100 | | **92.5** | |

Verdict: **SHIP** (92.5 ≥ 90).

Top gaps:
1. Cat 4: `check-runtime-spi-apidiff.sh` not CI-wired (pre-gate, P8).
2. Cat 5: envtest harness pre-gate — authors post-gate with `controller-author` agent.
3. Cat 10: migration doc template not yet authored (triggers on first required method addition).

## Iteration 2 — 2026-04-21 (Performance & quality)

Changes applied: (1) `InjectPrompt`, `InvokeSubAgent`, `Health`, `StreamEvents`
promoted from design references to explicit interface methods with full doc-comment
contracts. (2) `CredentialRotationExpired` named in `RuntimeEvent.Type` vocabulary
(08a flow). (3) `Resume` 60 s deadline explicit; `AgentUnresponsive` typed sentinel
named. (4) Drain six-step sequence numbered in doc-comment. (5) Frontmatter `tests`
arrays populated with all 9 named cases. (6) Compile-time check pattern noted.

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Interface shape complete; no open method questions. |
| 2 | Architecture fit | 10 | 1.0 | 10 | InjectPrompt + InvokeSubAgent wired to 23/08c correctly; fieldOwner invariant preserved. |
| 3 | Security posture | 15 | 1.0 | 15 | CredentialRotationExpired routed via StreamEvents → controller drain; no token in SPI types. |
| 4 | Automatability | 10 | 0.5 | 5 | apidiff gate referenced (CI pre-gate acceptable per task brief). |
| 5 | Verifiability | 15 | 1.0 | 15 | 9 named tests with invariant statements; compile-time blank-var check pattern stated. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Table complete; no new failure modes needed. |
| 7 | Context efficiency | 10 | 1.0 | 10 | ≤ 200 lines; no design content inlined; companion iter log. |
| 8 | Docs quality | 5 | 1.0 | 5 | All frontmatter arrays populated; no broken cross-links. |
| 9 | Observability | 5 | 1.0 | 5 | All 10 method spans named; metrics + events consistent. |
| 10 | Operational readiness | 10 | 0.5 | 5 | Migration plan gate named; Cat 10 pre-gate blocker unchanged. |
| | **Total** | 100 | | **95** | |

Verdict: **SHIP** (95 ≥ 90).

Top gaps (both pre-gate acceptable per task brief):
1. Cat 4: `check-runtime-spi-apidiff.sh` CI wiring (P8 lint phase).
2. Cat 10: `migration-runtime-spi-v2alpha1.md` authors when first required method added.

## Iteration 3 — 2026-04-21 (Operational readiness)

Changes applied: (1) Drain budget derivation formula explicit in doc-comment
(`120 s − 20 s − 10 s = 90 s`) — no implementation guessing. (2) Readiness probe
`/tmp/draining` sentinel cross-referenced in D25+Drain section (rule 06.9). (3)
`CheckpointRef` type in Key types section — implementers do not import CRD types
directly. (4) `HealthReport.Phase` aligned with `AgentRuntime` CR phase values
(`Ready`, `Draining`, `Incompatible`). (5) `regression_lock: false` in frontmatter.
(6) Iteration log split to companion file; main spec within 200-line cap.

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Interface shape, lifecycle, registration, SemVer all bounded; no open questions. |
| 2 | Architecture fit | 10 | 1.0 | 10 | Drain budget derivation unambiguous; probe sentinel cross-ref rule 06.9 explicit. |
| 3 | Security posture | 15 | 1.0 | 15 | No new attack surface; checkpoint path scoped; no secret in any SPI type. |
| 4 | Automatability | 10 | 0.5 | 5 | apidiff CI pre-gate (acceptable per brief); admission wired inline. |
| 5 | Verifiability | 15 | 1.0 | 15 | 9 named tests; invariants machine-checkable; compile-time pattern stated. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Table complete; partial-checkpoint resume path explicit. |
| 7 | Context efficiency | 10 | 1.0 | 10 | Main spec 192 lines; companion for iteration log; skills referenced not inlined. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; frontmatter; all depends listed; status: current on both files. |
| 9 | Observability | 5 | 1.0 | 5 | Span per method; metrics + events arrays complete and consistent. |
| 10 | Operational readiness | 10 | 1.0 | 10 | Budget derivation explicit; probe sentinel linked; migration gate named; rollback in every path. |
| | **Total** | 100 | | **97.5** | |

Verdict: **SHIP** (97.5 ≥ 90). Status: `current`.

Top gaps (both pre-gate acceptable per task brief):
1. Cat 4: `check-runtime-spi-apidiff.sh` CI wiring (P8 lint phase).
2. Cat 5: envtest harness authors post-gate with `controller-author` agent.
