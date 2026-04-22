<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: packaging
depends: [14a-olm-channels-upgrades.md]
related_skills: [validate-bundle]
status: current
last_verified: 2026-04-21
---

# 14a-ii — OLM Channels: Rubric Iterations and Test Assertions

Companion to [14a-olm-channels-upgrades.md](14a-olm-channels-upgrades.md).
Contains the full rubric iteration log and the kuttl test assertion spec
referenced by the main design.

## kuttl Upgrade Test Suite (`test/e2e/olm-upgrade/`)

**Step 1–6 (standard upgrade):**

1. Install `keese.v0.0.1` bundle into an envtest-backed OLM cluster.
2. Apply a synthetic `keese.v0.0.2` bundle with one additive optional CRD field.
3. `keese.v0.0.2` CSV carries `spec.replaces: keese.v0.0.1`.
4. Approve the generated `InstallPlan`.
5. Assert CSV `keese.v0.0.2` reaches `phase: Succeeded`.
6. Assert existing `Workspace` CR is readable with no unexpected conditions.
   Assert `keese_olm_upgrade_total` counter incremented by 1.

**Step 7 (skipRange):**

7. Install `keese.v0.1.0` bundle with
   `olm.skipRange: '>=0.0.2 <0.1.0'` against a cluster still on
   `v0.0.1` (simulating skipped bad releases `v0.0.2`, `v0.0.3`).
   Assert OLM generates an InstallPlan for `v0.1.0` directly. Approve.
   Assert CSV reaches `Succeeded`. Assert existing Workspace CR readable.

**Step 8 (rollback):**

8. With `v0.1.0` CSV `Succeeded`, delete the CSV and set
   `Subscription.spec.startingCSV: keese.v0.0.1`. Assert OLM generates
   a new `InstallPlan` for `v0.0.1`. Approve. Assert CSV reaches
   `Succeeded`. Assert `Workspace` CR has
   `status.conditions[type=Ready].status: True`.

## cosign Tamper Test (`test/e2e/bundle-sign/`)

Uses a test bundle image with a valid digest but a forged attestation.
Asserts `scripts/bundle-sign-verify.sh` exits 1 and prints
`FAIL: signature verification`. Runs in CI via `make test-bundle-sign`.

## Iteration Log

### Iteration 1 — 2026-04-21 (Correctness & Security)

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Channels, graph, CRD compat, signing, rollback all bounded |
| 2 | Architecture fit | 10 | 1.0 | 10 | Aligns with rule 04.2, 04.13, 05.12; no violations |
| 3 | Security posture | 15 | 0.5 | 8 | cosign + pre-install webhook documented; catalog tamper threat light |
| 4 | Automatability | 10 | 0.5 | 5 | set-csv-replaces.sh named but signature unspecified |
| 5 | Verifiability | 15 | 0.5 | 8 | CI gate named; no envtest/kuttl upgrade assertion |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | 7 failure modes with detection + mitigation |
| 7 | Context efficiency | 10 | 1.0 | 10 | Under 200 lines; skill pointers present |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX, frontmatter, links complete |
| 9 | Observability | 5 | 1.0 | 5 | Counter + histogram + structured log event |
| 10 | Operational readiness | 10 | 0.5 | 5 | Rollback runbook present; webhook HA not quantified |
| | **Total** | 100 | | **76** | |

Verdict: REVISE

Top gaps:
1. Automatability: script signatures unspecified.
2. Verifiability: no kuttl/envtest assertion spec.
3. Operational readiness: conversion webhook HA not quantified.

### Iteration 2 — 2026-04-21 (Performance & Quality)

Additions: concrete script signatures, webhook HA spec, kuttl test
assertions (steps 1–6 above).

**Script contracts added to main doc:**
- `scripts/set-csv-replaces.sh <new-version> <prev-version>` — idempotent.
- `scripts/bundle-sign-verify.sh <bundle-image-digest>` — exits non-zero on
  cosign verify failure; required status check before `make catalog-push`.

**Webhook HA spec added to main doc:**
2 replicas, `PodDisruptionBudget minAvailable: 1`,
`terminationGracePeriodSeconds: 30`, `failurePolicy: Fail`.

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | |
| 2 | Architecture fit | 10 | 1.0 | 10 | |
| 3 | Security posture | 15 | 1.0 | 15 | cosign verify + failPolicy:Fail + signed digest; tamper asserted in test |
| 4 | Automatability | 10 | 1.0 | 10 | Both scripts have concrete signatures; CI wiring named |
| 5 | Verifiability | 15 | 0.5 | 8 | Standard upgrade kuttl spec present; skipRange + rollback + cosign tamper pending |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | |
| 7 | Context efficiency | 10 | 1.0 | 10 | |
| 8 | Docs quality | 5 | 1.0 | 5 | |
| 9 | Observability | 5 | 1.0 | 5 | |
| 10 | Operational readiness | 10 | 1.0 | 10 | Webhook HA quantified; resource ceilings match existing CSV |
| | **Total** | 100 | | **93** | |

Verdict: SHIP

Top gaps (resolved in iteration 3):
1. Verifiability: skipRange kuttl assertion missing.
2. Verifiability: rollback kuttl assertion missing.
3. Verifiability: cosign tamper test missing.

### Iteration 3 — 2026-04-21 (Operational Readiness)

Additions: steps 7 (skipRange) and 8 (rollback) in the kuttl suite above;
cosign tamper test spec above.

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | |
| 2 | Architecture fit | 10 | 1.0 | 10 | |
| 3 | Security posture | 15 | 1.0 | 15 | |
| 4 | Automatability | 10 | 1.0 | 10 | |
| 5 | Verifiability | 15 | 1.0 | 15 | skipRange, rollback, cosign tamper all asserted |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | |
| 7 | Context efficiency | 10 | 1.0 | 10 | |
| 8 | Docs quality | 5 | 1.0 | 5 | |
| 9 | Observability | 5 | 1.0 | 5 | |
| 10 | Operational readiness | 10 | 1.0 | 10 | |
| | **Total** | 100 | | **100** | |

Verdict: SHIP
