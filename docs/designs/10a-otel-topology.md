<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: observability
depends: [02-workspace-model.md]
related_skills: []
status: draft
last_verified: 2026-04-19
rollback: TODO — document migration path when status flips to current
---

# 10a — OTEL Topology

> **Status: draft.** This doc is a gate-placeholder. Architect agent
> fills content across 3 rubric iterations (target ≥ 90/100) before
> any spec or controller code references this design.

## Context

_One paragraph. What problem does this design address? The OTEL collector
deployment model — pipelines, sampling strategy, and exporters (traces to
Elastic APM, metrics+logs to Elasticsearch) — must be specified before any
controller emits telemetry._

## Open questions (must be answered before `status: current`)

1. What sampling strategy (head-based, tail-based, or hybrid) is used for
   traces, and what is the sampling rate in dev vs. production?
2. How does the OTEL collector deployment (Deployment vs. DaemonSet) affect
   tail-sampling correctness when spans cross nodes?
3. What is the fallback exporter when Elastic APM takes > 30s to boot —
   Jaeger, debug stdout, or drop?
4. How are per-tenant trace contexts isolated so tenant-A traces cannot
   be queried by tenant-B in Kibana?
5. What OTEL resource attributes are mandatory on every span emitted by keese
   controllers (`k8s.namespace.name`, `service.name`, etc.)?

## Refs

- [../plans/rubric.md](../plans/rubric.md)
- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)
- [10b-token-accounting.md](10b-token-accounting.md)
- [../specs/observability.operator.keese.ai-v1alpha1.md](../specs/observability.operator.keese.ai-v1alpha1.md)

TODO(design-gate)
