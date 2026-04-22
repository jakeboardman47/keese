<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: developer-experience
depends: [19-ide-and-debugging.md]
related_skills: [doc-authoring]
status: current
last_verified: 2026-04-21
rollback: N/A — iteration log only.
---

# 19b — IDE and Debugging: Iteration Log

Full rubric tables for [19-ide-and-debugging.md](19-ide-and-debugging.md).

## Iteration 1 — 2026-04-21

Focus: Correctness & security.

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Two flows, named sessions, failure modes all in scope |
| 2 | Architecture fit | 10 | 1.0 | 10 | SYS_PTRACE not privileged; ACP via kubectl exec; 08b auth chain honoured |
| 3 | Security posture | 15 | 0.5 | 7.5 | dlv NetworkPolicy break-glass justified but D12 still draft — gap |
| 4 | Automatability | 10 | 1.0 | 10 | make ide-config, make dlv-attach, make port-forward-debugpy all scripted |
| 5 | Verifiability | 15 | 0.5 | 7.5 | No smoke test for dlv connectivity or ACP attach |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Six failure modes documented with mitigations |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | ≤200 lines; no config reproduced; skills pointed |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX, frontmatter, rollback complete |
| 9 | Observability | 5 | 1.0 | 5 | Inherits 08b metrics; debugpy gap noted |
| 10 | Operational readiness | 10 | 0.5 | 5 | Debug overlay dev-only enforced; no plugin upgrade path |
| | **Total** | 100 | | **80** | |

Verdict: REVISE

Top gaps:
1. Verifiability: no smoke test for dlv or ACP attach.
2. Operational readiness: keese-jetbrains plugin versioning not defined.
3. Security: break-glass depends on D12 which is draft.

Next step: Iter 2 — smoke tests, plugin versioning, D12 cross-ref with VAP guard.

---

## Iteration 2 — 2026-04-21

Focus: Performance & quality.

Changes: Added `scripts/dev/dlv-attach-smoke.sh` (verifies port-forward alive, dlv API
responds to `ListGoroutines`) and `scripts/dev/acp-attach-smoke.sh` (dry-run
`WorkspaceSession` CR, checks bridge ready). keese-jetbrains plugin distributed as GitHub
release asset (`.zip`), version-pinned in `dev/ide/goland/plugins/version.txt`; `make
ide-config` downloads if absent. dlv egress NetworkPolicy exception scoped to
`kind-dev` clusters only via label `keese.ai/env=dev`; VAP blocks patch on any
cluster without that label, closing D12-draft gap procedurally.

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Unchanged |
| 2 | Architecture fit | 10 | 1.0 | 10 | VAP-guarded dev-only patch aligns with rule 05 |
| 3 | Security posture | 15 | 1.0 | 15 | VAP label guard + break-glass annotation; D12 draft gap closed procedurally |
| 4 | Automatability | 10 | 1.0 | 10 | Smoke scripts added; plugin download scripted |
| 5 | Verifiability | 15 | 1.0 | 15 | dlv + ACP smoke tests; SIGTERM drain test (rule 06.10) applies |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Unchanged |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | ≤200 lines maintained via 19b split |
| 8 | Docs quality | 5 | 1.0 | 5 | Unchanged |
| 9 | Observability | 5 | 1.0 | 5 | Unchanged |
| 10 | Operational readiness | 10 | 0.5 | 5 | Plugin upgrade path defined; mid-session rollback absent |
| | **Total** | 100 | | **95** | |

Verdict: SHIP

Top gaps:
1. Plugin mid-session upgrade not handled (acceptable at v1alpha1).
2. D12 remains draft — revisit when current.
3. debugpy metrics minimal (debug-only flow, low severity).

Next step: Iter 3 — operational readiness pass; plugin rollback; status → current.

---

## Iteration 3 — 2026-04-21

Focus: Operational readiness.

Changes: Plugin mid-session upgrade: GoLand ACP plugin updates take effect only after
IDE restart; bridge version pinned in `version.txt`; mismatch detected at connect time
via `X-Keese-Bridge-Version` header (08b), returning `UNSUPPORTED_VERSION` — IDE plugin
shows upgrade prompt. `make ide-config` warns if D12 NetworkPolicy exception is present
on a non-dev cluster context (`keese.ai/env` label absent).

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
| 9 | Observability | 5 | 1.0 | 5 | |
| 10 | Operational readiness | 10 | 1.0 | 10 | Plugin rollback defined; non-dev guard added |
| | **Total** | 100 | | **100** | |

Verdict: SHIP — status promoted to `current`.

Residual notes (non-blocking):
- D12 still draft; when current, verify NetworkPolicy exception text matches D19.
- debugpy metrics minimal; acceptable for debug-only flow.
