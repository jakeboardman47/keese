<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - EH4-rebac-decision-e2e.md
  - ../../specs/authz.keese.ai-v1alpha1-guardrail.md
  - ../../../internal/controller/authz/guardrailbinding_controller.go
related_skills: [plan-management]
status: planned
last_verified: 2026-06-09
phase: EH5
model_tier: sonnet
depends_on: [EH4]
agent: test-engineer
outputs:
  - tests/e2e/authz-guardrails
  - tests/e2e/lib
---

# EH5 — GuardrailBinding + ToolBinding + WorkspaceTool e2e

**Goal.** The entire `authz.keese.ai` tooling/guardrail surface has no cluster
e2e. Cover the three reconcilers that ship in
`internal/controller/authz/{guardrailbinding,toolbinding,workspacetool}_controller.go`.

## Deliverables

A kuttl suite `tests/e2e/authz-guardrails/`:

1. **GuardrailBinding reconcile + default-inherit:** apply a `GuardrailBinding`
   scoped to a workspace; assert it reaches its Ready condition and its status
   reflects the merged guardrail set. Assert **default-inherit** (rule: a
   workspace with no explicit binding inherits the namespace/tenant `default`
   binding) — the load-bearing invariant from
   `docs/designs/06-guardrailbinding.md`.
2. **ToolBinding + WorkspaceTool → tuples:** apply a `ToolBinding` /
   `WorkspaceTool` granting a tool; assert reconcile + that the resulting OpenFGA
   tuple makes the tool **allowed** (reuse EH4's allow path: granted tool → 200),
   and that revoking the binding flips it to denied (403). This ties the CRD layer
   to the live ext_authz decision.
3. **Status/events:** assert the controllers emit their finite-table event reasons
   (rule 04.11) and set `observedGeneration`.

Prereq-gate via `tests/e2e/lib/check-prereqs.sh` (+ `check-extauth.sh` from EH4).

## Acceptance

- Suite green under `make test-e2e` on a bootstrapped cluster with a seeded store;
  skips cleanly when prereqs are placeholders.
- Asserts default-inherit + at least one tool allow→deny driven by a binding.

## Notes for the agent

- Test SHIPPED behavior. The **gateway-side** guardrail enforcement (Presidio /
  LlamaGuard ext_proc) may not be live in the bootstrap — if so, mark that step
  skipped, add `revisit_when_guardrail_extproc_live`, set
  `status: shipped-with-stubs`, and cover the CRD-reconcile + tuple layer (which
  is live) in full.
- Stay inside `tests/e2e/authz-guardrails/` + additive `tests/e2e/lib/` helpers.
  Reuse EH4's request-firing + audit patterns; don't duplicate them — source them.
