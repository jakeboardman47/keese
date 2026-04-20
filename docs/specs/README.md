<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: index
depends: [../designs/README.md]
related_skills: [doc-authoring]
status: draft
last_verified: 2026-04-19
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
| [workspace.operator.keese.ai-v1alpha1.md](workspace.operator.keese.ai-v1alpha1.md) | `Workspace`, `WorkspaceShare` | 01, 02 | draft |
| [workflow.operator.keese.ai-v1alpha1.md](workflow.operator.keese.ai-v1alpha1.md) | `Workflow`, `WorkflowRun` | 03, 22 | draft |
| [runtime.operator.keese.ai-v1alpha1.md](runtime.operator.keese.ai-v1alpha1.md) | `AgentRuntime`, `RuntimeExtension` | 07, 08a, 08b, 08c | draft |
| [memory.operator.keese.ai-v1alpha1.md](memory.operator.keese.ai-v1alpha1.md) | `Memory`, `SharedMemory` | 15 | draft |
| [recipe.operator.keese.ai-v1alpha1.md](recipe.operator.keese.ai-v1alpha1.md) | `Recipe`, `RecipeSource` | 16 | draft |
| [guardrail.operator.keese.ai-v1alpha1.md](guardrail.operator.keese.ai-v1alpha1.md) | `GuardrailBinding` | 06, 05c | draft |
| [observability.operator.keese.ai-v1alpha1.md](observability.operator.keese.ai-v1alpha1.md) | `TokenBudget` | 10a, 10b | draft |
| [transport.operator.keese.ai-v1alpha1.md](transport.operator.keese.ai-v1alpha1.md) | `Transport` | 09 | draft |
| [egress-authz-protocol.md](egress-authz-protocol.md) | ext_authz contract (cross-cutting) | 04a, 05a, 05c | draft |
| [agent-runtime-spi.md](agent-runtime-spi.md) | Go SPI interface contract | 07, 18 | draft |
| [credential-broker-protocol.md](credential-broker-protocol.md) | Credential broker contract | 17, 05b, 04c | draft |

## Lifecycle

- `status: draft` — placeholder; owning designs not yet `current`.
- `status: current` — designs are `current`; spec content authored and reviewed.
- `status: implemented` — all acceptance tests exist and pass; `regression_lock: true`.
- Downgrading from `implemented` → `current` requires a `docs/plans/migration-<slug>.md`.
