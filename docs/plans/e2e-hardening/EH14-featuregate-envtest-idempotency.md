<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - ../../../.claude/rules/04-kubernetes.md
  - ../../../internal/controller/policy/featuregate_controller.go
  - ../../../internal/controller/policy/suite_test.go
related_skills: [plan-management, controller-authoring]
status: planned
last_verified: 2026-06-09
phase: EH14
model_tier: sonnet
depends_on: []
agent: controller-author
outputs:
  - internal/controller/policy
---

# EH14 — FeatureGate envtest idempotency

**Goal.** Rule 04.16 requires every reconciler's idempotency assertion to run in
**envtest** (CRDs from `config/crd/bases/`, ≥ 3 reconciles, no spec change).
`featuregate_controller_test.go` makes 30 `Reconcile` calls but via a **fake
client**, not the policy envtest suite — so the FeatureGate reconciler is the one
gap in otherwise-complete envtest discipline.

## Deliverables

- Add a FeatureGate idempotency case to the **policy envtest suite**
  (`internal/controller/policy/suite_test.go` + a
  `featuregate_controller_envtest_test.go` or extend the existing envtest test):
  create a `FeatureGate` CR against the envtest API server, reconcile ≥ 3 times,
  assert the world converges with no spurious writes (stable `observedGeneration`,
  stable projection ConfigMap, no error) — mirroring the `tokenbudget` envtest
  idempotency pattern already in the suite.
- Keep the existing fast fake-client unit test (it stays useful); this **adds** the
  envtest-tier assertion rule 04.16 wants.

## Acceptance

- `CGO_ENABLED=0 go test -race -tags=integration ./internal/controller/policy/...`
  green (envtest), including the new FeatureGate idempotency case.
- `make lint` clean; no production-code change (test-only).

## Notes for the agent

- Follow `.claude/agents/controller-author.md` + rule 04.16. Use the existing
  policy `suite_test.go` envtest harness + the `tokenbudget` idempotency test as
  the template. SSA-only if you touch any reconcile path (you should not — this is
  test-only).
- macOS gotcha: `CGO_ENABLED=0` for local envtest runs. Stay inside
  `internal/controller/policy/`.
