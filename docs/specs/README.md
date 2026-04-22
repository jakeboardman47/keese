<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: index
depends: [../designs/README.md]
related_skills: [doc-authoring]
status: current
last_verified: 2026-04-21
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
| [workspace.operator.keese.ai-v1alpha1.md](workspace.operator.keese.ai-v1alpha1.md) | `Workspace`, `WorkspaceShare`, `WorkspaceSession` | 01, 02, 02-ii, 08b, 08b-ii, 12, 04b | current |
| [workspace.operator.keese.ai-v1alpha1-ii-workspace.md](workspace.operator.keese.ai-v1alpha1-ii-workspace.md) | `Workspace` CRD detail | 02, 02-ii, 12, 04b | current |
| [workspace.operator.keese.ai-v1alpha1-ii-share.md](workspace.operator.keese.ai-v1alpha1-ii-share.md) | `WorkspaceShare` CRD detail | 02, 04a | current |
| [workspace.operator.keese.ai-v1alpha1-ii-session.md](workspace.operator.keese.ai-v1alpha1-ii-session.md) | `WorkspaceSession` CRD detail | 08b, 08b-ii | current |
| [workspace.operator.keese.ai-v1alpha1-ii-iter-log.md](workspace.operator.keese.ai-v1alpha1-ii-iter-log.md) | Workspace spec iteration log | — | current |
| [workflow.operator.keese.ai-v1alpha1.md](workflow.operator.keese.ai-v1alpha1.md) | `Workflow` | 03, 22 | current |
| [workflow.operator.keese.ai-v1alpha1-b.md](workflow.operator.keese.ai-v1alpha1-b.md) | `WorkflowRun` + cross-tenant admission | 03, 03c, 22, 25 | current |
| [runtime.operator.keese.ai-v1alpha1.md](runtime.operator.keese.ai-v1alpha1.md) | `AgentRuntime`, `RuntimeExtension` | 07, 07b, 08a, 08b, 08c, 16, 04a | current |
| [runtime.operator.keese.ai-v1alpha1b-iter-log.md](runtime.operator.keese.ai-v1alpha1b-iter-log.md) | Runtime spec iteration log | — | current |
| [memory.operator.keese.ai-v1alpha1.md](memory.operator.keese.ai-v1alpha1.md) | `Memory`, `SharedMemory` | 15, 04a | current |
| [recipe.operator.keese.ai-v1alpha1.md](recipe.operator.keese.ai-v1alpha1.md) | `Recipe`, `RecipeSource` | 16, 06, 08a | current |
| [guardrail.operator.keese.ai-v1alpha1.md](guardrail.operator.keese.ai-v1alpha1.md) | `GuardrailBinding` | 06, 06-ii, 06-iii, 05c | current |
| [observability.operator.keese.ai-v1alpha1.md](observability.operator.keese.ai-v1alpha1.md) | `TokenBudget` | 10a, 10b | current |
| [observability.operator.keese.ai-v1alpha1-iter-log.md](observability.operator.keese.ai-v1alpha1-iter-log.md) | Observability spec iteration log | — | current |
| [transport.operator.keese.ai-v1alpha1.md](transport.operator.keese.ai-v1alpha1.md) | `Transport` | 09, 09-ii, 04a, 04b, 03c, 25 | current |
| [transport-ii-iter-log.md](transport-ii-iter-log.md) | Transport spec iteration log | — | current |
| [tenancy.operator.keese.ai-v1alpha1.md](tenancy.operator.keese.ai-v1alpha1.md) | `Tenant` (D26), `CrossTenantAgreement` (D29) | 24, 24b, 25, 25-ii, 25-iii, 04a | draft |
| [authz.operator.keese.ai-v1alpha1.md](authz.operator.keese.ai-v1alpha1.md) | `OIDCProvider` (D28) | 04b, 04b-ii, 04a | draft |
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
