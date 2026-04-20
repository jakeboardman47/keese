<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: log
depends: []
related_skills: [testing-envtest, testing-e2e-kind]
status: current
last_verified: 2026-04-19
---

# Flake Log

Tracks tests quarantined per the flake policy in
[`.claude/rules/06-testing.md`](../../.claude/rules/06-testing.md).

## Policy recap

- Two non-deterministic failures on `main` within a rolling 7-day window →
  quarantine immediately.
- Every quarantined test gets an owner and a due date in this log.
- A test quarantined for more than two phases without a plan to fix it is
  **deleted**, not perpetuated.

## Active quarantine

| Test | Phase | Tier (unit\|envtest\|kuttl\|e2e-kind) | Tag | Owner | Quarantined | Due | Root cause (hypothesis) |
|------|-------|---------------------------------------|-----|-------|-------------|-----|--------------------------|
| _none yet_ | — | — | | | | | |

## Resolved

| Test | Phase | Tier (unit\|envtest\|kuttl\|e2e-kind) | Quarantined | Resolved | Fix (commit) |
|------|-------|---------------------------------------|-------------|----------|--------------|
| _none yet_ | — | — | | | |

## Adding an entry

1. Tag the test with the language/framework's skip-or-flake mechanism.
2. Commit: `test(<scope>): quarantine <test name> — see docs/plans/flake-log.md`.
3. Append a row to **Active quarantine** with Phase, Tier, owner + due date (≤ 14 days).
4. File a plan task in the owning phase doc to root-cause.

## Resolving an entry

1. Remove the quarantine marker.
2. Run the test locally 20 times before declaring fixed. For integration tests
   also run under the race detector.
3. Move the row from **Active** to **Resolved** with the fix commit.

## Tier definitions (per plan P4)

| Tier | Description |
|---|---|
| `unit` | Pure Go unit tests; no cluster; fakes/mocks only. |
| `envtest` | envtest API server; controller reconcile loops; `Eventually` assertions. |
| `kuttl` | Black-box kubectl-based step tests against a running cluster. |
| `e2e-kind` | Full stack on kind; requires P7 bootstrap; nightly + tag triggered. |
