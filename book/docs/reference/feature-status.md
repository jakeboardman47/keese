<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

# Feature implementation status

A per-feature index of **what is built on `main`**, mirrored from the
machine-readable feature docs under
[`docs/features/`](https://github.com/keese-ai/keese/tree/main/docs/features).
Each feature doc links back to the design, spec, and source that produce it,
and is honest about limitations.

!!! info "Audience"
    Operators and developers deciding what to rely on today. **Prerequisites:**
    none. For the forward-looking view see [Development → Roadmap](../development/roadmap.md).

!!! note "Two documentation trees"
    User-facing prose lives here in the book. The `docs/features/` tree is the
    terse, source-linked **"what is built"** record (one file per feature,
    ≤ 200 lines, machine-readable). Statuses below reflect the state on `main`,
    not release-readiness.

## Core — `keese.ai`

| Feature | Status | Feature doc |
|---|---|---|
| Workspaces & Sessions | ✅ implemented | [workspace.md](https://github.com/keese-ai/keese/blob/main/docs/features/workspace.md) |
| Tenancy & CrossTenantAgreement | ✅ implemented | [tenancy.md](https://github.com/keese-ai/keese/blob/main/docs/features/tenancy.md) |
| Memory & SharedMemory (7 backends) | ✅ implemented | [memory.md](https://github.com/keese-ai/keese/blob/main/docs/features/memory.md) |
| Recipes & RecipeSources | ✅ implemented | [recipe.md](https://github.com/keese-ai/keese/blob/main/docs/features/recipe.md) |
| Transport (messaging plane) | ✅ implemented | [transport.md](https://github.com/keese-ai/keese/blob/main/docs/features/transport.md) |
| Workflows & WorkflowRuns | ✅ implemented | [workflow.md](https://github.com/keese-ai/keese/blob/main/docs/features/workflow.md) |
| Agent Runtime SPI & Goose provider | ✅ implemented | [agent-runtime-spi.md](https://github.com/keese-ai/keese/blob/main/docs/features/agent-runtime-spi.md) |
| ADK Python & Go runtimes | 🚧 stub (E0) | [adk-runtimes.md](https://github.com/keese-ai/keese/blob/main/docs/features/adk-runtimes.md) |

## Access control — `authz.keese.ai`

| Feature | Status | Feature doc |
|---|---|---|
| GuardrailBinding | ✅ implemented | [guardrailbinding.md](https://github.com/keese-ai/keese/blob/main/docs/features/guardrailbinding.md) |
| Egress ext_authz (keese-authz) | ✅ implemented | [ext-authz.md](https://github.com/keese-ai/keese/blob/main/docs/features/ext-authz.md) |
| OIDCProvider | ✅ implemented | [oidc-provider.md](https://github.com/keese-ai/keese/blob/main/docs/features/oidc-provider.md) |

## Constraints & platform — `policy.keese.ai` + cross-cutting

| Feature | Status | Feature doc |
|---|---|---|
| TokenBudget | ✅ implemented | [token-budget.md](https://github.com/keese-ai/keese/blob/main/docs/features/token-budget.md) |
| Feature Gates | ✅ implemented | [feature-gates.md](https://github.com/keese-ai/keese/blob/main/docs/features/feature-gates.md) |
| Supply-chain admission (cosign) | ✅ implemented | [cosign-webhook.md](https://github.com/keese-ai/keese/blob/main/docs/features/cosign-webhook.md) |
| ValidatingAdmissionPolicies (5) | ✅ implemented | [admission-policies.md](https://github.com/keese-ai/keese/blob/main/docs/features/admission-policies.md) |

## Production caveats you must know

These are real, current limitations — see each feature doc and the
[roadmap](../development/roadmap.md) for detail.

!!! danger "Not production-ready as shipped"
    - **In-cluster memory backends run unauthenticated.** Redis, Qdrant, Neo4j,
      and pgvector StatefulSets do not mount `credentialSecretRef`; only Zep
      self-hosted mounts a projected credential. Use external/managed endpoints
      for production. See [Concepts → Memory](../concepts/memory.md).
    - **WorkflowRun NATS stream cleanup is bugged** — streams are not deleted on
      WorkflowRun deletion (`workflowrun_controller.go`). See
      [Concepts → Workflows](../concepts/workflows.md).
    - **OIDCProvider cache-flush is a no-op** in this release (`FakeCacheFlusher`);
      do not rely on it for zero-downtime key rotation.

!!! warning "Planned — not yet implemented"
    - **ADK Python / ADK Go runtimes** — registered stubs; all SPI methods return
      `ErrUnsupported`. Goose is the only usable runtime today.
    - **RAG** (KnowledgeBase / DocumentSource / IngestionRun / EmbeddingModel) —
      designs current, spec current, no controller code.
    - **Agent supervision ladder**, **gateway-side NATS-KV token enforcement**,
      **Workflow output sinks**, and **ToolBinding/WorkspaceTool controllers**
      are designed but not yet implemented.

## See also

- [Development → Roadmap](../development/roadmap.md) — built vs. remaining, with tracks.
- [Reference → API overview](api/index.md) — the CRD field reference.
- [`docs/features/`](https://github.com/keese-ai/keese/tree/main/docs/features) — the source-linked feature docs.
