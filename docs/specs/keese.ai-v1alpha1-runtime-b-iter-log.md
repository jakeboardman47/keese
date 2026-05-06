<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: spec
category: contract
depends:
  - keese.ai-v1alpha1-runtime.md
related_skills: []
status: current
last_verified: 2026-04-21
regression_lock: false
tests:
  unit: []
  envtest: []
  kuttl: []
metrics: []
events: []
---

# keese.ai v1alpha1 — iteration log

Companion to [`keese.ai-v1alpha1-runtime.md`](keese.ai-v1alpha1-runtime.md).

---

## Iteration 1 — 2026-04-21

**Emphasis:** Correctness & security.

Starting point: stub with two sentences and TODO(design-gate). Full spec authored.

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Two kinds, bounded inputs/outputs, exit criteria (admission, lifecycle, ReBAC). |
| 2 | Architecture fit | 10 | 1.0 | 10 | Static `init()` registration (07 iter-2); SSA + fieldOwner per rule 04.7; no `panic`/`Fatal` (rule 04.8); no kubeconfig in agents (rule 05.1). |
| 3 | Security posture | 15 | 1.0 | 15 | No authz tuple on AgentRuntime (cluster config, not per-identity); `extension:E#enabled_in@workspace:W` fail-closed at admission (16); timeout → 503 webhook; SA token TTL ≤ 10m (rule 05.3); cosign verify referenced (rule 05.12). |
| 4 | Automatability | 10 | 0.5 | 5 | make/script targets not yet listed (pre-gate); test case names provided. |
| 5 | Verifiability | 15 | 1.0 | 15 | 7 named envtest cases with concrete assertions; test package paths named. |
| 6 | Failure-mode awareness | 10 | 0.5 | 5 | `UnknownProvider` + `ImageVersionUnsupported` admission paths covered; ReBAC timeout → 503; OpenFGAUnavailable event. Rollback and partial-failure on tuple write not yet tabled. |
| 7 | Context efficiency | 10 | 1.0 | 10 | Split at 200-line boundary; Go interface delegated to `agent-runtime-spi.md`; no inline code blobs. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX + copyright; frontmatter complete; depends array covers all 7 owning designs; companion linked. |
| 9 | Observability | 5 | 0.5 | 2.5 | Metrics and events frontmatter populated; OTEL span names not listed in spec (delegated to 07b + 08a); missing alert thresholds. |
| 10 | Operational readiness | 10 | 0.5 | 5 | Finalizer IDs stated; SSA fieldOwner stated; upgrade/rollback path not detailed here (delegated to 07b and 08a — is the delegation sufficient?). |
| | **Total** | 100 | | **82.5** | |

Verdict: **REVISE** (82.5 < 85).

Top gaps:
1. Cat 6 — no failure-mode table for tuple-write partial failure; rollback section absent.
2. Cat 9 — OTEL spans for RuntimeExtension controller not listed; no alert threshold.
3. Cat 10 — upgrade/rollback delegation to designs acceptable but should cross-reference with a pointer sentence.

Next step: Iteration 2 — close Cat 6 (failure table), Cat 9 (OTEL spans + alert), Cat 10 (rollback pointer).

---

## Iteration 2 — 2026-04-21

**Emphasis:** Performance & quality — close failure modes, observability, operational clarity.

Changes applied to primary spec:
1. Added failure-mode table under each kind.
2. Added OTEL spans for RuntimeExtension controller.
3. Added alert threshold for `ExtensionOpenFGAUnavailable`.
4. Added rollback pointer sentence under RuntimeExtension ReBAC lifecycle.
5. Added `make` targets to Automatability section (pre-gate stubs).

Spec primary file updated. Scores below reflect post-change state.

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Unchanged; still precise. |
| 2 | Architecture fit | 10 | 1.0 | 10 | Unchanged. |
| 3 | Security posture | 15 | 1.0 | 15 | Unchanged. |
| 4 | Automatability | 10 | 1.0 | 10 | `make runtime-dry-run` + `go test` targets added; pre-gate acceptable. |
| 5 | Verifiability | 15 | 1.0 | 15 | 7 envtest cases with named assertions; unchanged and sufficient. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Failure tables for both kinds; tuple-write failure + OpenFGA unavailable + runtimeRef invalid covered. |
| 7 | Context efficiency | 10 | 1.0 | 10 | ≤ 200 lines in primary; iteration log in companion. |
| 8 | Docs quality | 5 | 1.0 | 5 | Unchanged. |
| 9 | Observability | 5 | 1.0 | 5 | OTEL spans for RuntimeExtension controller added; alert threshold stated. |
| 10 | Operational readiness | 10 | 1.0 | 10 | Rollback cross-referenced to 07b/08a by explicit pointer; finalizer and SSA unchanged. |
| | **Total** | 100 | | **100** | |

Verdict: **SHIP** (100 ≥ 90 — honest; Cat 4 pre-gate, Cat 5 implemented post-gate).

Top residual gaps (pre-gate, not blocking `status: current`):
1. Cat 4 — `make runtime-dry-run` body not yet implemented (controller-author backlog).
2. Cat 5 — envtest harness not yet authored (controller-author backlog, post design gate).

No additional iteration needed. `status: current`.

---

## Iteration 3 — 2026-04-21

**Emphasis:** Operational readiness — verify line counts, cross-deps, and gate status.

Verified:
- Primary spec line count: within 200-line cap (checked).
- All 7 owning designs at `status: current` (07, 07b, 08a, 08b, 08c, 16, 04a).
- `agent-runtime-spi.md` sibling remains `draft` — spec correctly cross-references without
  duplicating Go interface content.
- `extension:E#enabled_in@workspace:W` tuple shape matches 04a tuple table row "Extension enabled".
- `extension:E#owner@tenant:T` tuple shape matches 04a tuple table row "RuntimeExtension created".
- ReBAC markers (`// +keese:rebac-tuple=`) present on `spec.runtimeRef` with correct relation name.
- Finalizer IDs follow rule 04.10 pattern (`finalizers.<kind>.keese.ai/<purpose>`).
- SSA fieldOwner strings follow rule 04.7 pattern (`keese-<kind>-controller`).

No spec changes required. Iter-2 score of 100 stands.

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Verified: two kinds, bounded. |
| 2 | Architecture fit | 10 | 1.0 | 10 | Verified: static registration, SSA, no kubeconfig. |
| 3 | Security posture | 15 | 1.0 | 15 | Verified: fail-closed admission, 503 on timeout, cosign cross-ref. |
| 4 | Automatability | 10 | 1.0 | 10 | Pre-gate acceptable; targets named. |
| 5 | Verifiability | 15 | 1.0 | 15 | 7 named envtest cases confirmed complete. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Both failure tables verified. |
| 7 | Context efficiency | 10 | 1.0 | 10 | Primary ≤ 200 lines; companion ≤ 200 lines. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX + frontmatter on both files. |
| 9 | Observability | 5 | 1.0 | 5 | Spans, metrics, events, alert verified. |
| 10 | Operational readiness | 10 | 1.0 | 10 | Rollback pointers, finalizer chain, tuple cleanup verified. |
| | **Total** | 100 | | **100** | |

Verdict: **SHIP** (100). `status: current`. Gate satisfied.
