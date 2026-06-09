<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: index
depends: [../README.md, ../rubric.md]
related_skills: [plan-management, conduct]
status: current
last_verified: 2026-06-08
---

# EH — e2e hardening track

Close the e2e testing + coverage gaps found in the 2026-06-08 analysis. Driven as
conductor waves ([ADR 29](../../designs/29-conductor-orchestration.md)).

**Two facts that shape the plan:**

1. The covered behaviors are **implemented** (`cmd/keese-drain`,
   `internal/authz/extauth/check.go`, `internal/controller/policy/ratelimit.go`),
   so these phases write real tests against shipped code — not stubs-for-missing-impl.
   Where a path turns out to be a stub, the phase ships a `kuttl`-skipped /
   `t.Skip` placeholder + a `revisit_when_*` trigger (`shipped-with-stubs`).
2. CI workflow files (`.github/**`) are **protected paths** — `worktree-merge.sh`
   rejects them. Phases that edit CI are `dispatch: manual` (the orchestrator
   authors them on `main`).

## Gap → phase map

| Gap (from analysis) | Phase |
|---|---|
| Nightly e2e never installs kuttl → job fails; pass-by-skip fallback; e2e not on PRs | EH1 |
| Coverage infra (`coverage-targets.yaml`, `coverage-check.sh`) absent; no CI coverage gate | EH2 |
| `sigterm-drain-test.sh` wired into nothing (rule 06-signal §10) | EH3 |
| No live OpenFGA allow/deny decision asserted through the cluster | EH4 |
| `authz.keese.ai` group e2e-uncovered: GuardrailBinding, ToolBinding, WorkspaceTool | EH5 |
| `authz.keese.ai`: CrossTenantAgreement + OIDCProvider e2e | EH6 |
| `policy.keese.ai`: TokenBudget enforcement e2e | EH7 |
| `policy.keese.ai`: FeatureGate behavior e2e | EH8 |
| Workflow / WorkflowRun CRs never reconciled in e2e | EH9 |
| agentruntime-drain uses busybox stand-ins, not real `keese-drain`; orphan prereq | EH10 |
| RecipeSource + RuntimeExtension uncovered | EH11 |
| `test/e2e/` is the unmodified kubebuilder scaffold (no keese coverage) | EH12 |
| Untested SPI packages: `adkgo`, `adkpython`, `podexec` | EH13 |
| `featuregate` idempotency is a fake-client unit test, not in envtest | EH14 |

## Phase index

| Phase | Title | Agent | Dispatch | Depends on | Status |
|---|---|---|---|---|---|
| EH1 | e2e harness reliability + CI wiring | infra-bootstrap | manual | EH2, EH3 | planned |
| EH2 | Coverage enforcement infra | test-engineer | wave | — | complete |
| EH3 | SIGTERM-drain test wiring | test-engineer | wave | — | planned |
| EH4 | Live ReBAC allow/deny e2e (keystone) | test-engineer | wave | — | complete |
| EH5 | GuardrailBinding + ToolBinding + WorkspaceTool e2e | test-engineer | wave | EH4 | planned |
| EH6 | CrossTenantAgreement + OIDCProvider e2e | test-engineer | wave | EH4 | planned |
| EH7 | TokenBudget enforcement e2e | test-engineer | wave | — | planned |
| EH8 | FeatureGate behavior e2e | test-engineer | wave | — | planned |
| EH9 | Workflow + WorkflowRun e2e | test-engineer | wave | — | planned |
| EH10 | Real drain/SIGTERM e2e (replace stand-ins) | test-engineer | wave | EH3 | planned |
| EH11 | RecipeSource + RuntimeExtension e2e | test-engineer | wave | — | planned |
| EH12 | Retire/replace scaffold `test/e2e/` | test-engineer | wave | — | planned |
| EH13 | Runtime SPI unit tests (adkgo/adkpython/podexec) | test-engineer | wave | — | planned |
| EH14 | FeatureGate envtest idempotency | controller-author | wave | — | planned |

## Wave structure (conflict-free batches, ~3/wave)

- **Wave 1:** EH2, EH3, EH4 — foundation + authz keystone (no deps; distinct dirs).
- **Orchestrator (after Wave 1 merges):** EH1 — wire kuttl-install, the new
  coverage gate, and the sigterm job into `.github/**` on `main`.
- **Wave 2:** EH5, EH6 (after EH4), EH7.
- **Wave 3:** EH8, EH9, EH10 (after EH3).
- **Wave 4:** EH11, EH12, EH13, EH14.

Each suite phase writes to its own `tests/e2e/<suite>/` directory, so within a
wave the footprints are disjoint.

## Acceptance (per phase)

- New kuttl suites discovered by `tests/e2e/kuttl-config.yaml`; `make test-e2e`
  green against a bootstrapped kind cluster (or a documented prereq gate via
  `tests/e2e/lib/check-prereqs.sh` when live OpenFGA/OpenBao is required).
- Unit/envtest phases: `make lint && make test` green; `-race` clean.
- Every phase updates its row here + sets its own `status:` on completion.
