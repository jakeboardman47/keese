<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - ../../designs/30-token-metering-pipeline.md
  - ../../designs/10a-otel-topology.md
related_skills: [plan-management, controller-authoring, signal-handling]
status: planned
last_verified: 2026-06-10
phase: CH5a
model_tier: sonnet
depends_on: []
agent: implementer
outputs:
  - cmd/token-meter
---

# CH5a — keese-token-meter OTEL processor

**Goal.** Build the one missing hop ADR 30 identifies: a **stateless** processor
that reads the Envoy AI Gateway's per-response token `usage` and relabels it into
the contract series the TokenBudget reconciler's PromQL already queries —
`keese_token_budget_consumed_total{tenant,workspace,model,direction}`.

## Deliverables

- `cmd/token-meter/` — an OTEL/metrics processor (or collector processor plugin per
  ADR 30 §mechanism): consume the gateway `envoy_ai_gateway_token_cost_total` /
  response `usage`, relabel to the keese series (add `workspace` + `direction`,
  rename), **dedup on Envoy `x-request-id`** emitting only on the final response
  (no double-count on retries), expose self-metrics, structured logging.
- Rule 06 SIGTERM: flush in-flight counts on shutdown, exit 0 in budget, structured
  `shutdown` event; a `cmd/token-meter` SIGTERM test (per rule 06 §10).
- `Dockerfile` (distroless, digest-pinned base) + cosign-signable; no secrets.

## Acceptance

- `CGO_ENABLED=0 go test -race ./cmd/token-meter/...` green, incl. the SIGTERM +
  request-id-dedup + relabel tests. `make lint` clean. `go build ./cmd/...` clean.
- Output series carries exactly `{tenant,workspace,model,direction}` per ADR 30.

## Notes for the agent

- Read ADR 30 (`30-token-metering-pipeline.md`) for the exact label contract + the
  fail-open posture (this component never gates traffic). Stay inside `cmd/token-meter/`.
- **Never run bare `git stash`/`pop`/`reset`/`checkout <branch>`** (hits the shared
  checkout). macOS gotcha: `CGO_ENABLED=0`. CH5b wires it into the collector.
