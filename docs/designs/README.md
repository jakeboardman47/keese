<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: index
depends: []
related_skills: [doc-authoring]
status: current
last_verified: 2026-04-19
---

# designs/ — WHY

Design docs explain **why** the project is shaped the way it is.
All 37 design docs must reach `status: current` and score ≥ 90/100
before any spec is authored and before any controller code is written.

> **Gate status: CLOSED.** See [../plans/README.md](../plans/README.md).

## Index

| # | Doc | Title | Category | Status |
|---|---|---|---|---|
| 01 | [01-tenancy-capsule.md](01-tenancy-capsule.md) | Tenancy via Capsule | tenancy | current |
| 02 | [02-workspace-model.md](02-workspace-model.md) | Workspace Model | workspace | draft |
| 03 | [03-workflow-argo-delegation.md](03-workflow-argo-delegation.md) | Workflow Argo Delegation | workflow | draft |
| 04a | [04a-openfga-authz-model.md](04a-openfga-authz-model.md) | OpenFGA Authorization Model | authz | current |
| 04a-ii | [04a-ii-testplan.md](04a-ii-testplan.md) | OpenFGA Auth Model: Test Plan and CI Automation | authz | current |
| 04b | [04b-projected-sa-identity.md](04b-projected-sa-identity.md) | Projected ServiceAccount Identity | authz | current |
| 04b-ii | [04b-ii-oidc-trust.md](04b-ii-oidc-trust.md) | OIDC Trust Anchoring Per Cloud | authz | current |
| 04c | [04c-token-revocation.md](04c-token-revocation.md) | Token Revocation | authz | current |
| 05a | [05a-envoy-ai-gateway-topology.md](05a-envoy-ai-gateway-topology.md) | Envoy AI Gateway Topology | egress | current |
| 05a-ii | [05a-ii-iter-log.md](05a-ii-iter-log.md) | Envoy AI Gateway Topology: Iteration Log | egress | current |
| 05b | [05b-credential-injection-patterns.md](05b-credential-injection-patterns.md) | Credential Injection Patterns | egress | draft |
| 05c | [05c-mcp-policy-enforcement.md](05c-mcp-policy-enforcement.md) | MCP Policy Enforcement | egress | draft |
| 06 | [06-guardrailbinding.md](06-guardrailbinding.md) | GuardrailBinding | guardrails | draft |
| 07 | [07-agent-runtime-spi.md](07-agent-runtime-spi.md) | Agent Runtime SPI | runtime | draft |
| 08a | [08a-goose-headless-modes.md](08a-goose-headless-modes.md) | Goose Headless Modes | runtime | draft |
| 08b | [08b-goose-acp-stdio-k8s.md](08b-goose-acp-stdio-k8s.md) | Goose ACP stdio over K8s | runtime | draft |
| 08c | [08c-goose-subagents-limits.md](08c-goose-subagents-limits.md) | Goose Sub-Agents and Limits | runtime | draft |
| 09 | [09-transport-crd.md](09-transport-crd.md) | Transport CRD | transport | draft |
| 10a | [10a-otel-topology.md](10a-otel-topology.md) | OTEL Topology | observability | draft |
| 10b | [10b-token-accounting.md](10b-token-accounting.md) | Token Accounting | observability | draft |
| 11 | [11-secrets-pluggable-vault.md](11-secrets-pluggable-vault.md) | Secrets Pluggable Vault | secrets | draft |
| 12 | [12-network-isolation.md](12-network-isolation.md) | Network Isolation | security | draft |
| 13 | [13-cli-tunnel-wireguard.md](13-cli-tunnel-wireguard.md) | CLI Tunnel (WireGuard) | developer-experience | draft |
| 14a | [14a-olm-channels-upgrades.md](14a-olm-channels-upgrades.md) | OLM Channels and Upgrades | packaging | draft |
| 14b | [14b-olm-dependencies.md](14b-olm-dependencies.md) | OLM Dependencies | packaging | draft |
| 15 | [15-memory-management.md](15-memory-management.md) | Memory Management | memory | draft |
| 16 | [16-recipe-distribution.md](16-recipe-distribution.md) | Recipe Distribution | recipes | draft |
| 17 | [17-credential-broker.md](17-credential-broker.md) | Credential Broker | egress | draft |
| 18 | [18-process-lifecycle.md](18-process-lifecycle.md) | Process Lifecycle | reliability | draft |
| 19 | [19-ide-and-debugging.md](19-ide-and-debugging.md) | IDE and Debugging | developer-experience | draft |
| 20 | [20-api-group-layout.md](20-api-group-layout.md) | API Group Layout (redirect) | api | superseded |
| 20a | [20a-api-group-layout.md](20a-api-group-layout.md) | API Group Layout: Groups, Kinds, Shared Types, Versioning | api | current |
| 20b | [20b-api-group-layout.md](20b-api-group-layout.md) | API Group Layout: Trade-offs, Failure Modes, Rollback, Observability | api | current |
| 21 | [21-opentofu-cloud-deployment.md](21-opentofu-cloud-deployment.md) | OpenTofu Cloud Deployment | deployment | draft |
| 22 | [22-workflow-composition-examples.md](22-workflow-composition-examples.md) | Workflow Composition Examples | workflow | draft |
| 23 | [23-agent-supervision.md](23-agent-supervision.md) | Agent Supervision (Patrol Pattern) | reliability | draft |
| 24 | [24-tenant-crd.md](24-tenant-crd.md) | Keese Tenant CRD: Spec, Reconcile, Admission, Migration | tenancy | current |
| 24b | [24b-tenant-crd.md](24b-tenant-crd.md) | Keese Tenant CRD: Trade-offs, Failure Modes, Upgrade, Observability | tenancy | current |

## Lifecycle

- All docs start at `status: draft`.
- Architect agent fills each doc through 3 rubric iterations (target ≥ 90/100).
- A doc reaches `status: current` only after it scores ≥ 90 on iteration 3.
- `last_verified` is bumped whenever the doc is re-read against current code.
- Conflicting documents are a bug; resolve by retiring one (`status: superseded`).
- Changes to a `current` doc require a new plan iteration before landing.
