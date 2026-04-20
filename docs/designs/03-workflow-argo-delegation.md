<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: workflow
depends: [02-workspace-model.md]
related_skills: []
status: draft
last_verified: 2026-04-19
rollback: TODO — document migration path when status flips to current
---

# 03 — Workflow Argo Delegation

> **Status: draft.** This doc is a gate-placeholder. Architect agent
> fills content across 3 rubric iterations (target ≥ 90/100) before
> any spec or controller code references this design.

## Context

_One paragraph. What problem does this design address? A keese `Workflow` CR
projects an Argo `WorkflowTemplate`; a `WorkflowRun` maps to an Argo `Workflow`.
This design covers artifact passing, retry budgets, and trigger/output composition._

## Open questions (must be answered before `status: current`)

1. What is the exact mapping between `WorkflowRun.spec` fields and an Argo
   `Workflow` manifest — which fields are projected 1:1 vs. transformed?
2. How does artifact passing work between Argo steps when the artifact backend
   (S3/GCS/Azure) is not available in dev (kind)?
3. What retry budget semantics does keese impose on top of Argo's native
   `retryStrategy`, and how are conflicts resolved?
4. How do `.spec.triggers[]` projections (CronJob / KEDA / Knative / webhook)
   reference and activate the generated `WorkflowTemplate`?
5. What RBAC does the keese operator need in the Argo namespace, and how is
   cross-namespace `WorkflowTemplate` access gated?

## Refs

- [../plans/rubric.md](../plans/rubric.md)
- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)
- [02-workspace-model.md](02-workspace-model.md)
- [../specs/workflow.operator.keese.ai-v1alpha1.md](../specs/workflow.operator.keese.ai-v1alpha1.md)

TODO(design-gate)
