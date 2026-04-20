<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: transport
depends: [20-api-group-layout.md]
related_skills: []
status: draft
last_verified: 2026-04-19
rollback: TODO — document migration path when status flips to current
---

# 09 — Transport CRD

> **Status: draft.** This doc is a gate-placeholder. Architect agent
> fills content across 3 rubric iterations (target ≥ 90/100) before
> any spec or controller code references this design.

## Context

_One paragraph. What problem does this design address? The `Transport` CRD
uses a `spec.type` discriminator (`nats|a2a|mcp|stdio`) to provide pluggable
messaging semantics, delivery guarantees, and TLS configuration via cert-manager._

## Open questions (must be answered before `status: current`)

1. What are the exact `spec` sub-fields for each transport type, and how does
   the discriminated one-of schema validation work in OpenAPIv3?
2. What delivery guarantee does each transport type promise — at-least-once,
   at-most-once, exactly-once — and who owns deduplication?
3. How does TLS certificate provisioning via cert-manager work for each type:
   is the `Certificate` CR created by the Transport controller or referenced
   by name?
4. What is the `Transport` lifecycle — can its `spec.type` be changed after
   creation, or is it immutable (VAP-enforced)?
5. How does a `Workflow` or `WorkspaceShare` reference a `Transport` object —
   by name in the same namespace, or cross-namespace via `ReferenceGrant`?

## Refs

- [../plans/rubric.md](../plans/rubric.md)
- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)
- [08b-goose-acp-stdio-k8s.md](08b-goose-acp-stdio-k8s.md)
- [12-network-isolation.md](12-network-isolation.md)
- [../specs/transport.operator.keese.ai-v1alpha1.md](../specs/transport.operator.keese.ai-v1alpha1.md)

TODO(design-gate)
