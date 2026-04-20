<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: memory
depends: [02-workspace-model.md, 04a-openfga-authz-model.md]
related_skills: []
status: draft
last_verified: 2026-04-19
rollback: TODO — document migration path when status flips to current
---

# 15 — Memory Management

> **Status: draft.** This doc is a gate-placeholder. Architect agent
> fills content across 3 rubric iterations (target ≥ 90/100) before
> any spec or controller code references this design.

## Context

_One paragraph. What problem does this design address? The `Memory` CRD uses a
discriminated one-of `spec.provider` for backends (sqlite/redis/qdrant/pgvector/
neo4j/mem0/zep). `SharedMemory` adds cross-workspace sharing gated by ReBAC._

## Open questions (must be answered before `status: current`)

1. What is the discriminated one-of schema for `Memory.spec.provider`, and how
   is the schema enforced (OpenAPIv3 oneOf + CEL, or webhook)?
2. For `SharedMemory`, what OpenFGA tuple grants a workspace read vs. read-write
   access, and who can grant that tuple (tenant-admin only)?
3. How does goose's MCP-memory integration discover and mount the correct
   `Memory` backend — ENV injection, projected file, or ACP extension?
4. What is the backup/restore contract for each provider type (SQLite on PVC,
   Redis with AOF, Qdrant snapshots)?
5. When a `Memory` object is deleted, what is the data retention policy — PVC
   remains, data is wiped, or tenant-configurable?

## Refs

- [../plans/rubric.md](../plans/rubric.md)
- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)
- [02-workspace-model.md](02-workspace-model.md)
- [04a-openfga-authz-model.md](04a-openfga-authz-model.md)
- [../specs/memory.operator.keese.ai-v1alpha1.md](../specs/memory.operator.keese.ai-v1alpha1.md)

TODO(design-gate)
