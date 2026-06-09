<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - ../../designs/27-feature-gates-openfeature.md
  - ../../designs/27b-feature-gate-catalog.md
  - ../../../internal/controller/policy/featuregate_controller.go
  - ../../../internal/featuregate
related_skills: [plan-management]
status: shipped-with-stubs
last_verified: 2026-06-09
revisit_when_featuregate_effect_observable: true
phase: EH8
model_tier: sonnet
depends_on: []
agent: test-engineer
outputs:
  - tests/e2e/feature-gate
  - tests/e2e/lib
---

# EH8 — FeatureGate behavior e2e

**Goal.** `policy.keese.ai/FeatureGate` (the OpenFeature-backed capability toggle,
designs 27/27b) has no cluster e2e. Prove a gate actually flips observable
controller behavior — not just that the CR reconciles.

## Deliverables

A kuttl suite `tests/e2e/feature-gate/`:

1. **Pick a real gated capability** — read `internal/featuregate/` +
   `docs/designs/27b-feature-gate-catalog.md` + `featuregate_controller.go` and
   choose a gate whose effect is observable on the cluster (a reconcile branch,
   an injected resource, an admission outcome).
2. **Flip it:** apply a `FeatureGate` enabling the capability; assert the gated
   behavior is **present**; patch it disabled; assert the behavior is **absent**
   (or vice-versa). Assert `FeatureGate` reaches Ready + `observedGeneration`.
3. **Default state:** assert the gate's documented default (27b) when no CR is
   applied.

## Acceptance

- Suite green under `make test-e2e`; asserts ≥ 1 gate on/off flip that changes
  observable behavior (not just CR status).

## Notes for the agent

- Test a SHIPPED gate. If no gate's effect is observable in the local bootstrap,
  assert the CR reconcile + provider wiring fully, mark the behavior-flip step
  skipped, add `revisit_when_featuregate_effect_observable`, set
  `status: shipped-with-stubs`, and name the gate you'd assert once it is.
- Stay inside `tests/e2e/feature-gate/` + additive `tests/e2e/lib/` helpers.
