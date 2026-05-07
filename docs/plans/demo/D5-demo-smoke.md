<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - ../../references/e2e-smoke.md
  - ../../../scripts/dev/d5-anthropic-smoke.sh
related_skills: [plan-management, test-engineer]
status: in-progress
last_verified: 2026-05-06
---

# D5 — Demo smoke (kind, T1+T2 only)

**Refinement pass:** operational readiness.
**Effort:** ~1 h.
**Owner agent:** `test-engineer`.

## Goal

Prove the end-to-end happy path on a **local kind cluster** — D4 cloud
deploy is deferred. The two assertions kept from the original D5 are:

- **T1.** A goose run inside the session pod returns a code block via a
  successful Anthropic round-trip through the Envoy AI Gateway, and the
  sqlite memory file lands on the session PVC.
- **T2.** Memory survives a pod delete + recreate.

T3 (operator restart smoke), T4 (manual InstallPlan upgrade), T5 (live
demo runbook), and T6 (rollback drill) are dropped from this scope —
they assume D4's cloud cluster + OperatorHub catalog. They reopen when
D4 lands.

## Inputs (already in repo)

- [scripts/dev/e2e-smoke.sh](../../../scripts/dev/e2e-smoke.sh) — phases
  01–08 bring up kind, install bootstrap infra, deploy the operator,
  apply samples, and assert the WorkspaceSession reaches
  `phase=Active`.
- [scripts/dev/d5-anthropic-smoke.sh](../../../scripts/dev/d5-anthropic-smoke.sh)
  — runs T1 + T2 against the running cluster, captures transcripts to
  `.plan-logs/D5-*.txt`.
- [docs/references/e2e-smoke.md](../../references/e2e-smoke.md) — smoke
  reference doc.
- `ANTHROPIC_API_KEY` in `.env.local`. Seeded into OpenBao via
  [scripts/dev/seed-openbao.sh](../../../scripts/dev/seed-openbao.sh) —
  dev-mode auto-unseals on every restart, so no manual `bao operator
  unseal` step is needed for kind. See
  [dev/bootstrap/README.md](../../../dev/bootstrap/README.md) for the
  prod-vs-dev divergence note.

## Tasks

### T1 — End-to-end happy path

```sh
# Bring up kind + operator + Active session pod (10–15 min cold).
bash scripts/dev/e2e-smoke.sh --keep
# Run T1 + T2 against the live cluster.
bash scripts/dev/d5-anthropic-smoke.sh
```

`d5-anthropic-smoke.sh` asserts:

1. The `goose run --text 'Write a Python function …'` exec returns a
   response that mentions `fibonacci`.
2. The Envoy AI Gateway log shows a `POST /v1/messages 200` line added
   during the exec window. Soft-fail if absent (log-level / ext_proc
   routing variants — captured but not blocking).
3. `/var/run/keese/memory/session.db` exists with non-zero size.

**Acceptance.** Exit code `0`. Transcript at
`.plan-logs/D5-happy-path-<timestamp>.txt`.

### T2 — Memory persistence across pod restart

The same script then deletes the session pod, waits for the new pod, and
asks `'What was my previous question?'`. If the response references
`fibonacci`, T2 passes (exit 0). If not, the script exits 2 — a
**soft-fail**, expected until the full Drain/Resume SPI lands per
[tech-debt TD-P1-02](tech-debt.md). The transcript is captured at
`.plan-logs/D5-memory-persist-<timestamp>.txt` for review.

**Acceptance.** Exit code `0` (T2 hard-pass) or `2` (T2 soft-fail
recorded as expected). Anything else (timeout, missing pod, etc.) is a
hard fail.

## Out of scope (→ tech-debt §verification)

- D5-T3 Operator restart smoke — reopens with D4.
- D5-T4 Manual InstallPlan upgrade — depends on OperatorHub catalog.
- D5-T5 Live demo runbook — owner-doc; reopens with D4.
- D5-T6 Rollback drill — depends on the cloud cluster lifecycle.
- Automated kuttl e2e under `tests/e2e/` (TD-P1-07 already shipped a
  progression case; the Anthropic-round-trip case stays here).
- Chaos / network-partition tests (→ TD-P3-08).
- Multi-LLM smoke (→ TD-P2-13).
- Multi-tenant smoke (→ TD-P3-07).
- Performance / latency budget tests.

## Verification

- `.plan-logs/D5-happy-path-<ts>.txt` exists and contains a code block
  in the response section.
- `.plan-logs/D5-memory-persist-<ts>.txt` exists; T2 either references
  the previous topic (hard-pass) or is logged as a soft-fail per the
  Drain/Resume gap.
- [MEMORY.md](../../../MEMORY.md) updated with date + outcome of demo
  dry-run + any new gotchas.

## Failure modes

| Failure | Symptom | Recovery |
|---|---|---|
| Anthropic returns 200 but content empty | gateway stripped headers | check `BackendSecurityPolicy` template injection; D3-T2 |
| Memory persists but is corrupted | sqlite WAL + RWO mid-write | accept; flag as known issue; restart-and-retry. Full fix → TD-P1-02 + TD-P1-09. |
| `e2e-smoke.sh` phase 04 (operator deploy) hangs | Tilt reload loop or operator panic | check `${PLAN_LOGS}/e2e-smoke-tilt.log`; `kubectl describe pod -n keese-system` |
| `goose run` exits non-zero with "no API key" | OpenBao seed didn't pick up `ANTHROPIC_API_KEY` | `kubectl exec -n openbao openbao-0 -- bao kv get keese/tenants/tenant-a/anthropic`; if empty, re-source `.env.local` and rerun `scripts/dev/seed-openbao.sh` |

## Iteration log

### Iteration 1 — 2026-04-25

Original GKE-cloud scope; superseded by Iteration 2.

### Iteration 2 — 2026-05-06

Retargeted to local kind. T3–T6 dropped (D4 deferred). Soft-fail
semantics for T2 added because Drain/Resume SPI (TD-P1-02) is in flight
in parallel.

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Two scripted smoke checks; no manual runbook |
| 2 | Architecture fit | 10 | 1.0 | 10 | Validates rules 04 (k8s), 06-signal (drain) end-to-end |
| 3 | Security posture | 15 | 0.6 | 9 | LLM call path validated; ext_authz exercised post-TD-P1-03 |
| 4 | Automatability | 10 | 1.0 | 10 | Both checks scripted; transcripts captured |
| 5 | Verifiability | 15 | 1.0 | 15 | Exit codes + plan-logs |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Soft-fail explicit for T2 |
| 7 | Context efficiency | 10 | 1.0 | 10 | <200 lines |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX + frontmatter + links |
| 9 | Observability | 5 | 0.6 | 3 | Transcripts + gateway log tail captured |
| 10 | Operational readiness | 10 | 0.7 | 7 | Smoke green is necessary but not sufficient — D4 reopens T3-T6 |
| | **Total** | 100 | | **89** | |

Verdict: SHIP at 89 (rubric ≥ 85 demo target).

Top gaps:
1. T2 soft-fail until TD-P1-02 lands — design tradeoff, not a doc gap.
2. No CI integration — kind smoke runs locally only. Not chasing for D5;
   tech-debt §verification.
3. T3–T6 reopen when D4 lands.

Next step: run the smoke on a developer laptop with `make e2e-smoke && bash scripts/dev/d5-anthropic-smoke.sh`. Capture transcripts; update [MEMORY.md](../../../MEMORY.md).
