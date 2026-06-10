<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - ../../designs/30-token-metering-pipeline.md
  - CH5a-token-meter-processor.md
related_skills: [plan-management, infra-bootstrap]
status: planned
last_verified: 2026-06-10
phase: CH5b
model_tier: sonnet
depends_on: [CH5a]
agent: infra-bootstrap
outputs:
  - dev/bootstrap
  - config
---

# CH5b — Wire keese-token-meter into the Tier-1 OTEL collector

**Goal.** Deploy the CH5a meter into the metering pipeline so its relabeled series
reaches Prometheus (the authoritative store the TokenBudget reconciler reads, per
ADR 30 + 10b).

## Deliverables

- Wire `keese-token-meter` into the **Tier-1** `otel-collector-config` (DaemonSet,
  design 10a) via SSA, per ADR 30's topology; add the dev bootstrap values +
  `make bootstrap-infra` apply.
- A fail-closed **NetworkPolicy**: the meter may egress **only** to Prometheus
  (and scrape the gateway), nothing else (rule 05.5 — no wildcards).
- Pin the meter image; document the load (`make` target reuse from CH7's
  `e2e-images-load` pattern).

## Acceptance

- `kustomize build` / `helmfile template` render clean; `kubeconform` valid on the
  rendered manifests; NetworkPolicy enumerates exact endpoints. `make lint` clean.
- On a bootstrapped kind cluster, the meter pod runs and the keese consumed series
  appears in Prometheus (or, no live cluster: structurally validated, with the live
  check documented as the nightly path).

## Notes for the agent

- Read ADR 30 + 10a for the tier topology. Stay inside `dev/bootstrap/` + `config/`
  (collector config + NetworkPolicy). Do NOT touch `internal/`, `cmd/` (CH5a owns
  the binary), `.github/**`, protected paths.
- **Never run bare `git stash`/`pop`/`reset`/`checkout <branch>`** (hits the shared
  checkout). CH5c un-stubs the reconciler against the now-live series.
