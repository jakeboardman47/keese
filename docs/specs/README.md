<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: index
depends: [../designs/README.md]
related_skills: [doc-authoring]
status: current
last_verified: 2026-06-08
---

# specs/ — WHAT (testable)

Specs describe **what** the project does in terms concrete enough to be parsed
by test harnesses. Each spec is keyed to one or more design docs.

> **Gate reminder: specs cannot promote from `draft` until ALL dependent design
> docs in their `depends:` frontmatter list reach `status: current`.**
> See [../plans/README.md](../plans/README.md) and [../designs/README.md](../designs/README.md).

## Index

| Spec | Kind / Contract | Owning designs | Status |
|---|---|---|---|
| [keese.ai-v1alpha1-workspace.md](keese.ai-v1alpha1-workspace.md) | `Workspace`, `WorkspaceShare`, `WorkspaceSession` | 01, 02, 02-ii, 08b, 08b-ii, 12, 04b | current |
| [keese.ai-v1alpha1-workspace-ii-workspace.md](keese.ai-v1alpha1-workspace-ii-workspace.md) | `Workspace` CRD detail | 02, 02-ii, 12, 04b | current |
| [keese.ai-v1alpha1-workspace-ii-share.md](keese.ai-v1alpha1-workspace-ii-share.md) | `WorkspaceShare` CRD detail | 02, 04a | current |
| [keese.ai-v1alpha1-workspace-ii-session.md](keese.ai-v1alpha1-workspace-ii-session.md) | `WorkspaceSession` CRD detail | 08b, 08b-ii | current |
| [keese.ai-v1alpha1-workspace-ii-iter-log.md](keese.ai-v1alpha1-workspace-ii-iter-log.md) | Workspace spec iteration log | — | current |
| [keese.ai-v1alpha1-workflow.md](keese.ai-v1alpha1-workflow.md) | `Workflow` | 03, 22 | current |
| [keese.ai-v1alpha1-workflow-b.md](keese.ai-v1alpha1-workflow-b.md) | `WorkflowRun` + cross-tenant admission | 03, 03c, 22, 25 | current |
| [keese.ai-v1alpha1-runtime.md](keese.ai-v1alpha1-runtime.md) | `AgentRuntime`, `RuntimeExtension` | 07, 07b, 08a, 08b, 08c, 16, 04a | current |
| [keese.ai-v1alpha1-runtime-b-iter-log.md](keese.ai-v1alpha1-runtime-b-iter-log.md) | Runtime spec iteration log | — | current |
| [keese.ai-v1alpha1-memory.md](keese.ai-v1alpha1-memory.md) | `Memory`, `SharedMemory` | 15, 04a | current |
| [keese.ai-v1alpha1-rag.md](keese.ai-v1alpha1-rag.md) | `KnowledgeBase`, `DocumentSource`, `IngestionRun`, `EmbeddingModel`, `SharedKnowledgeBase` | 28, 28b, 28c, 04a, 15 | current |
| [keese.ai-v1alpha1-recipe.md](keese.ai-v1alpha1-recipe.md) | `Recipe`, `RecipeSource` | 16, 06, 08a | current |
| [authz.keese.ai-v1alpha1-guardrail.md](authz.keese.ai-v1alpha1-guardrail.md) | `GuardrailBinding` | 06, 06-ii, 06-iii, 05c | current |
| [policy.keese.ai-v1alpha1.md](policy.keese.ai-v1alpha1.md) | `TokenBudget` | 10a, 10b | current |
| [policy.keese.ai-v1alpha1-iter-log.md](policy.keese.ai-v1alpha1-iter-log.md) | Observability/policy spec iteration log | — | current |
| [keese.ai-v1alpha1-transport.md](keese.ai-v1alpha1-transport.md) | `Transport` | 09, 09-ii, 04a, 04b, 03c, 25 | current |
| [transport-ii-iter-log.md](transport-ii-iter-log.md) | Transport spec iteration log | — | current |
| [keese.ai-v1alpha1-tenancy.md](keese.ai-v1alpha1-tenancy.md) | `Tenant` (D26), `CrossTenantAgreement` (D29) | 24, 24b, 25, 25-ii, 25-iii, 04a, 01 | current |
| [keese.ai-v1alpha1-tenancy-ii-tenant.md](keese.ai-v1alpha1-tenancy-ii-tenant.md) | `Tenant` CRD detail | 24, 24b, 01 | current |
| [keese.ai-v1alpha1-tenancy-ii-cra.md](keese.ai-v1alpha1-tenancy-ii-cra.md) | `CrossTenantAgreement` CRD detail (note: CTA lives in `authz.keese.ai` at runtime; spec remains here for historical continuity) | 25, 25-ii, 25-iii, 04a | current |
| [keese.ai-v1alpha1-tenancy-iter-log.md](keese.ai-v1alpha1-tenancy-iter-log.md) | Tenancy spec iteration log | — | current |
| [authz.keese.ai-v1alpha1.md](authz.keese.ai-v1alpha1.md) | `OIDCProvider` (D28), `GuardrailBinding`, `CrossTenantAgreement` authz aspects | 04b, 04b-ii, 04a | current |
| [authz.keese.ai-v1alpha1-ii-iter-log.md](authz.keese.ai-v1alpha1-ii-iter-log.md) | Authz spec iteration log | — | current |
| [egress-authz-protocol.md](egress-authz-protocol.md) | ext_authz contract (cross-cutting) | 04a, 04a-ii, 04a-iii, 04b, 04b-ii, 04c, 05a, 05b, 05c, 25 | current |
| [egress-authz-protocol-iter-log.md](egress-authz-protocol-iter-log.md) | Egress authz protocol iteration log | — | current |
| [agent-runtime-spi.md](agent-runtime-spi.md) | Go SPI interface contract | 07, 07b, 08a, 08b, 08c, 18, 23 | current |
| [agent-runtime-spi-iter.md](agent-runtime-spi-iter.md) | Agent runtime SPI iteration log | — | current |
| [credential-broker-protocol.md](credential-broker-protocol.md) | Credential broker contract | 17, 05b, 05b-ii, 04c, 04b, 04b-ii | current |

## Lifecycle

- `status: draft` — placeholder; owning designs not yet `current`.
- `status: current` — designs are `current`; spec content authored and reviewed.
- `status: implemented` — all acceptance tests exist and pass; `regression_lock: true`.
- Downgrading from `implemented` → `current` requires a `docs/plans/migration-<slug>.md`.
