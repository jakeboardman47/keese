<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: tenancy
depends: []
related_skills: []
status: draft
last_verified: 2026-04-19
rollback: TODO — document migration path when status flips to current
---

# 01 — Tenancy via Capsule

> **Status: draft.** This doc is a gate-placeholder. Architect agent
> fills content across 3 rubric iterations (target ≥ 90/100) before
> any spec or controller code references this design.

## Context

_One paragraph. What problem does this design address? Capsule direct usage
avoids a keese `Tenant` CRD by consuming `capsule.clastix.io/v1beta2/Tenant`
directly. vcluster is opt-in via `Workspace.spec.isolation: hard`._

## Open questions (must be answered before `status: current`)

1. What namespace layout convention should keese enforce per tenant, and how
   does Capsule's `additionalRoleBindings` interact with keese RBAC?
2. When `Workspace.spec.isolation: hard` triggers a vcluster, who owns the
   vcluster lifecycle — keese or the workspace controller?
3. What quota / LimitRange / PSS defaults does keese inject vs. delegate
   entirely to Capsule's `resourceQuota` and `limitRanges` fields?
4. How does Capsule `TenantResource` propagation interact with keese-managed
   `NetworkPolicy` and `BackendSecurityPolicy` objects?
5. What is the upgrade contract when Capsule releases a breaking change to
   `v1beta2/Tenant` while keese is pinned to an older Capsule version?

## Refs

- [../plans/rubric.md](../plans/rubric.md)
- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)
- [20-api-group-layout.md](20-api-group-layout.md)
- [02-workspace-model.md](02-workspace-model.md)

TODO(design-gate)
