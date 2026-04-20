<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: workflow
depends: [03-workflow-argo-delegation.md, 09-transport-crd.md]
related_skills: []
status: draft
last_verified: 2026-04-19
rollback: TODO — document migration path when status flips to current
---

# 22 — Workflow Composition Examples

> **Status: draft.** This doc is a gate-placeholder. Architect agent
> fills content across 3 rubric iterations (target ≥ 90/100) before
> any spec or controller code references this design.

## Context

_One paragraph. What problem does this design address? Concrete `Workflow` patterns
demonstrate how `.triggers[]`, `.outputs[]`, Argo, NATS, and Knative compose:
cron-triggered autonomous-dev pipeline, webhook-triggered PR review, NATS-fanout
summarizer/reviewer._

## Open questions (must be answered before `status: current`)

1. For the cron-triggered autonomous-dev pipeline, what is the exact YAML for
   `spec.triggers[0]` projecting a CronJob, and what Argo `WorkflowTemplate`
   template does it invoke?
2. For the NATS-fanout pattern, how does keese ensure at-least-once delivery
   and prevent duplicate Argo `Workflow` spawns when a NATS message is replayed?
3. For the webhook-triggered PR review, how does the HTTPRoute-webhook trigger
   authenticate the GitHub webhook (HMAC signature), and what K8s resource
   receives and validates it?
4. How do `.spec.outputs[]` projections (Knative Sink / NATS stream / Slack /
   S3 / gh-PR) handle partial failure when one output sink is unavailable?
5. What is the observability contract for a composed workflow — one root OTEL
   trace spanning all triggers, Argo steps, and output deliveries?

## Refs

- [../plans/rubric.md](../plans/rubric.md)
- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)
- [03-workflow-argo-delegation.md](03-workflow-argo-delegation.md)
- [09-transport-crd.md](09-transport-crd.md)
- [../specs/workflow.operator.keese.ai-v1alpha1.md](../specs/workflow.operator.keese.ai-v1alpha1.md)

TODO(design-gate)
