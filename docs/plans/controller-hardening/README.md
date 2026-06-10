<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: index
depends: [../README.md, ../e2e-hardening/README.md]
related_skills: [plan-management, conduct, controller-authoring]
status: current
last_verified: 2026-06-10
---

# CH — controller-hardening track

Close the 8 production controller/spec follow-ups the e2e-hardening track
surfaced (see the 2026-06-09/10 MEMORY entries). Driven as conductor waves.

**This track changes production reconciler + authz behavior** — unlike the
test-only e2e track, every phase gets a harder diff review, SSA-only writes
(rule 04.7), and the authz/pipeline phases are design-gated.

## Gap → phase map

| Follow-up (from e2e findings) | Phase |
|---|---|
| `FakeNatsSignaler` `-race` (red integration CI) | CH1 |
| `podexec.go` context-timeout data race | CH2 |
| `Workflow.status.runCount` never written | CH6 |
| default-binding name mismatch (`keese-default` vs `keese.ai-default`) | CH8 |
| `enabled_in` tuple unwired on workspace bind | CH3 |
| bootstrap overlays (OLM/cosign + `goose-runtime` load) | CH7 |
| cross-tenant `messageable_from` resolution + OpenFGA model | CH4 |
| token-cost metering pipeline (OTEL → rate-limiter) | CH5 |

## Phase index

| Phase | Title | Agent | Depends | Status |
|---|---|---|---|---|
| CH1 | Fix FakeNatsSignaler data race (unblock -race CI) | controller-author | — | complete |
| CH2 | Fix podexec context-timeout data race | controller-author | — | complete |
| CH6 | Write Workflow.status.runCount | controller-author | — | complete |
| CH9 | Dedup the keese envtest harness (suite won't compile) | test-engineer | — | complete |
| CH8 | Fix default GuardrailBinding name mismatch | controller-author | — | complete |
| CH3 | Wire enabled_in tuple on workspace bind | controller-author | — | complete |
| CH7 | Bootstrap overlays (OLM/cosign + goose-runtime) | infra-bootstrap | — | shipped-with-stubs |
| CH4 | Cross-tenant messageable_from + OpenFGA model | rebac-modeler | — | complete |
| CH5 | Token-cost metering pipeline (design ADR — ADR 30) | architect | — | complete |
| CH5a | keese-token-meter OTEL processor (`cmd/token-meter/`) | implementer | — | complete |
| CH5b | Wire the meter into the Tier-1 OTEL collector + bootstrap | infra-bootstrap | CH5a | shipped-with-stubs |
| CH5c | Un-stub the TokenBudget reconciler vs the live series | controller-author | CH5b | planned |
| CH5d | Flip EH7's metering e2e (real tokens → 429) | test-engineer | CH5c | planned |

## Wave structure

- **Wave 1:** CH1, CH2, CH6 — safe, conflict-free fixes (policy / podexec /
  keese-workflow), high-value (CH1 unblocks the red `-race` integration job).
- **Wave 2:** CH3, CH7, CH8.
- **Wave 3 (design-gated):** CH4 (opus `rebac-modeler` — authz decision + tuple
  shapes), CH5 (architect ADR **before** any implementation — new OTEL→rate-limit
  pipeline). Flipping CH3/CH4/CH7 live also clears several e2e `shipped-with-stubs`
  gates (`enabled_in`, `cross_tenant`, `drain_image`, `featuregate_effect`).

## Acceptance (per phase)

- `make lint` clean; `CGO_ENABLED=0 go test -race [-tags=integration] ./<pkg>/...`
  green; SSA-only controller writes; `make manifests generate bundle` + commit if
  any `*_types.go` changes.
- Each phase updates its row here + sets its own `status:` on completion.
