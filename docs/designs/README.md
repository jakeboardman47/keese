<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: index
depends: []
related_skills: [doc-authoring]
status: current
last_verified: 2026-05-29
---

# designs/ — WHY

Design docs explain **why** the project is shaped the way it is.
All 62 design docs reached `status: current` with scores ≥ 90/100 — which,
together with the 27 specs, opened the design gate on 2026-04-22. Controller
and API code now lands on `main`.

> **Gate status: OPEN** (since 2026-04-22). See [../plans/README.md](../plans/README.md)
> and the [gate-open audit](../plans/gate-open-audit-2026-04-22.md).

## Index

| # | Doc | Title | Category | Status |
|---|---|---|---|---|
| 01 | [01-tenancy-capsule.md](01-tenancy-capsule.md) | Tenancy via Capsule | tenancy | current |
| 02 | [02-workspace-model.md](02-workspace-model.md) | Workspace Model | workspace | current |
| 02-ii | [02-ii-iter-log.md](02-ii-iter-log.md) | Workspace Model: Iteration Log + Background | workspace | current |
| 03 | [03-workflow-argo-delegation.md](03-workflow-argo-delegation.md) | Workflow Argo Delegation | workflow | current |
| 03b | [03b-workflow-trigger-projections.md](03b-workflow-trigger-projections.md) | Workflow Trigger Projections | workflow | current |
| 03c | [03c-workflow-messaging-plane.md](03c-workflow-messaging-plane.md) | Workflow Messaging Plane: NATS, Audience Injection, CTA Admission | workflow | current |
| 04a | [04a-openfga-authz-model.md](04a-openfga-authz-model.md) | OpenFGA Authorization Model | authz | current |
| 04a-ii | [04a-ii-testplan.md](04a-ii-testplan.md) | OpenFGA Auth Model: Test Plan and CI Automation | authz | current |
| 04a-iii | [04a-iii-iter-log.md](04a-iii-iter-log.md) | OpenFGA Auth Model: Iteration Log (iter-4 + iter-5 detail) | authz | current |
| 04b | [04b-projected-sa-identity.md](04b-projected-sa-identity.md) | Projected ServiceAccount Identity | authz | current |
| 04b-ii | [04b-ii-oidc-trust.md](04b-ii-oidc-trust.md) | OIDC Trust Anchoring Per Cloud | authz | current |
| 04c | [04c-token-revocation.md](04c-token-revocation.md) | Token Revocation | authz | current |
| 05a | [05a-envoy-ai-gateway-topology.md](05a-envoy-ai-gateway-topology.md) | Envoy AI Gateway Topology | egress | current |
| 05a-ii | [05a-ii-iter-log.md](05a-ii-iter-log.md) | Envoy AI Gateway Topology: Iteration Log | egress | current |
| 05b | [05b-credential-injection-patterns.md](05b-credential-injection-patterns.md) | Credential Injection Patterns | egress | current |
| 05b-ii | [05b-ii-bsp-examples.md](05b-ii-bsp-examples.md) | Credential Injection: BSP YAML Examples + Iteration Log | egress | current |
| 05c | [05c-mcp-policy-enforcement.md](05c-mcp-policy-enforcement.md) | MCP Policy Enforcement | egress | current |
| 06 | [06-guardrailbinding.md](06-guardrailbinding.md) | GuardrailBinding | guardrails | current |
| 06-ii | [06-ii-spec-schema.md](06-ii-spec-schema.md) | GuardrailBinding: Spec Schema | guardrails | current |
| 06-iii | [06-iii-samples.md](06-iii-samples.md) | GuardrailBinding: Scope Samples | guardrails | current |
| 07 | [07-agent-runtime-spi.md](07-agent-runtime-spi.md) | Agent Runtime SPI | runtime | current |
| 07b | [07b-agent-runtime-spi.md](07b-agent-runtime-spi.md) | Agent Runtime SPI: Trade-offs, Failure Modes, Upgrade, Observability | runtime | current |
| 08a | [08a-goose-headless-modes.md](08a-goose-headless-modes.md) | Goose Headless Modes | runtime | current |
| 08b | [08b-goose-acp-stdio-k8s.md](08b-goose-acp-stdio-k8s.md) | Goose ACP stdio over K8s | runtime | current |
| 08b-ii | [08b-ii-session-crd-spec.md](08b-ii-session-crd-spec.md) | Goose ACP: WorkspaceSession CRD Spec + Attach Flow | runtime | current |
| 08b-iii | [08b-iii-iter-log.md](08b-iii-iter-log.md) | Goose ACP: Iteration Log | runtime | current |
| 08c | [08c-goose-subagents-limits.md](08c-goose-subagents-limits.md) | Goose Sub-Agents and Limits | runtime | current |
| 09 | [09-transport-crd.md](09-transport-crd.md) | Transport CRD | transport | current |
| 09-ii | [09-ii-iter-log.md](09-ii-iter-log.md) | Transport CRD: Iteration Log | transport | current |
| 10a | [10a-otel-topology.md](10a-otel-topology.md) | OTEL Topology | observability | current |
| 10a-ii | [10a-ii-iter-log.md](10a-ii-iter-log.md) | OTEL Topology: Iteration Log | observability | current |
| 10b | [10b-token-accounting.md](10b-token-accounting.md) | Token Accounting | observability | current |
| 10b-ii | [10b-ii-iter-log.md](10b-ii-iter-log.md) | Token Accounting: Iteration Log | observability | current |
| 11 | [11-secrets-pluggable-vault.md](11-secrets-pluggable-vault.md) | Secrets Pluggable Vault | secrets | current |
| 11-ii | [11-ii-examples.md](11-ii-examples.md) | Secrets: Canonical ExternalSecret YAML Examples + Iteration Log | secrets | current |
| 12 | [12-network-isolation.md](12-network-isolation.md) | Network Isolation | security | current |
| 12-ii | [12-ii-iter-log.md](12-ii-iter-log.md) | Network Isolation: Iteration Log | security | current |
| 13 | [13-cli-tunnel-wireguard.md](13-cli-tunnel-wireguard.md) | CLI Tunnel (WireGuard) | developer-experience | current |
| 13b | [13b-cli-tunnel-ha-ops.md](13b-cli-tunnel-ha-ops.md) | CLI Tunnel: HA + ECMP + Iteration Log | developer-experience | current |
| 14a | [14a-olm-channels-upgrades.md](14a-olm-channels-upgrades.md) | OLM Channels and Upgrades | packaging | current |
| 14a-ii | [14a-ii-iterations.md](14a-ii-iterations.md) | OLM Channels: Iteration Log + kuttl Test Spec | packaging | current |
| 14b | [14b-olm-dependencies.md](14b-olm-dependencies.md) | OLM Dependencies | packaging | current |
| 15 | [15-memory-management.md](15-memory-management.md) | Memory Management | memory | current |
| 16 | [16-recipe-distribution.md](16-recipe-distribution.md) | Recipe Distribution | recipes | current |
| 17 | [17-credential-broker.md](17-credential-broker.md) | Credential Broker | egress | current |
| 18 | [18-process-lifecycle.md](18-process-lifecycle.md) | Process Lifecycle | reliability | current |
| 19 | [19-ide-and-debugging.md](19-ide-and-debugging.md) | IDE and Debugging | developer-experience | current |
| 19b | [19b-iter-log.md](19b-iter-log.md) | IDE and Debugging: Iteration Log | developer-experience | current |
| 20 | [20-api-group-layout.md](20-api-group-layout.md) | API Group Layout (redirect) | api | superseded |
| 20a | [20a-api-group-layout.md](20a-api-group-layout.md) | API Group Layout: Groups, Kinds, Shared Types, Versioning | api | current |
| 20b | [20b-api-group-layout.md](20b-api-group-layout.md) | API Group Layout: Trade-offs, Failure Modes, Rollback, Observability | api | current |
| 21 | [21-opentofu-cloud-deployment.md](21-opentofu-cloud-deployment.md) | OpenTofu Cloud Deployment | deployment | current |
| 21b | [21b-opentofu-iter-log.md](21b-opentofu-iter-log.md) | OpenTofu Cloud Deployment: Iteration Log | deployment | current |
| 22 | [22-workflow-composition-examples.md](22-workflow-composition-examples.md) | Workflow Composition Examples | workflow | current |
| 22-ii | [22-ii-samples.md](22-ii-samples.md) | Workflow Composition Examples: Full YAML Samples | workflow | current |
| 23 | [23-agent-supervision.md](23-agent-supervision.md) | Agent Supervision (Patrol Pattern) | reliability | current |
| 24 | [24-tenant-crd.md](24-tenant-crd.md) | Keese Tenant CRD: Spec, Reconcile, Admission, Migration | tenancy | current |
| 24b | [24b-tenant-crd.md](24b-tenant-crd.md) | Keese Tenant CRD: Trade-offs, Failure Modes, Upgrade, Observability | tenancy | current |
| 25 | [25-cross-tenant-agreement.md](25-cross-tenant-agreement.md) | CrossTenantAgreement CRD (D29) | tenancy | current |
| 25-ii | [25-ii-spec-schema.md](25-ii-spec-schema.md) | CrossTenantAgreement: Spec Schema + VAP CEL | tenancy | current |
| 25-iii | [25-iii-approval-flow.md](25-iii-approval-flow.md) | CrossTenantAgreement: Approval Flow + Failure Modes + Samples | tenancy | current |
| 25-iv | [25-iv-iter-log.md](25-iv-iter-log.md) | CrossTenantAgreement: Iteration Log | tenancy | current |
| 26 | [26-workspace-managed-predicate-adr.md](26-workspace-managed-predicate-adr.md) | Workspace controller predicate ADR | controller | current |
| 27 | [27-feature-gates-openfeature.md](27-feature-gates-openfeature.md) | Feature gates via OpenFeature | lifecycle | current |
| 27-ii | [27-ii-iter-log.md](27-ii-iter-log.md) | Feature gates: Iteration Log | lifecycle | current |
| 27b | [27b-feature-gate-catalog.md](27b-feature-gate-catalog.md) | Feature gate catalog | lifecycle | current |
| 28 | [28-rag-ingestion.md](28-rag-ingestion.md) | RAG Ingestion | rag | current |
| 28b | [28b-rag-backends.md](28b-rag-backends.md) | RAG Backends (Qdrant / Elasticsearch / pgvector) | rag | current |
| 28c | [28c-rag-pipeline.md](28c-rag-pipeline.md) | RAG Pipeline + Retrieval Contract | rag | current |

## Lifecycle

- All docs start at `status: draft`.
- Architect agent fills each doc through 3 rubric iterations (target ≥ 90/100).
- A doc reaches `status: current` only after it scores ≥ 90 on iteration 3.
- `last_verified` is bumped whenever the doc is re-read against current code.
- Conflicting documents are a bug; resolve by retiring one (`status: superseded`).
- Changes to a `current` doc require a new plan iteration before landing.
