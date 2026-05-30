<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: feature
category: feature
depends: [docs/designs/15-memory-management.md]
implements_specs: [docs/specs/keese.ai-v1alpha1-memory.md]
implements_plans: [docs/plans/demo/tech-debt.md]
source_refs:
  - api/keese/v1alpha1/memory_types.go:1-257
  - api/keese/v1alpha1/sharedmemory_types.go:1-116
  - internal/controller/keese/memory_controller.go:1-241
  - internal/controller/keese/memory_backend.go:1-177
  - internal/controller/keese/memory_redis_backend.go:1-232
  - internal/controller/keese/memory_neo4j_backend.go:1-233
  - internal/controller/keese/memory_zep_backend.go:1-271
  - internal/controller/keese/memory_events.go:1-50
  - config/vap/embedding-dim-immutable.yaml:1-48
  - config/vap/sqlite-single-consumer.yaml:1-74
related_skills: [controller-authoring]
status: implemented
implemented_in_phase: demo-D2
last_verified: 2026-05-29
---

# Memory & SharedMemory

## Summary

Memory and SharedMemory give agent workspaces durable, pluggable storage through a
7-backend discriminated one-of: `sqlite` (default, PVC-backed), `redis`, `qdrant`,
`pgvector`, `neo4j`, `mem0`, and `zep`. For `redis`, `qdrant`, `pgvector`, and `neo4j`,
the connection-coordinate fields (`address`, `endpoint`, `dsnSecretRef`, `uri`) are
**required** by the CRD schema (`minLength: 1`); the in-cluster StatefulSet code paths
in the controller are not reachable via the current `v1alpha1` API — always supply an
external managed endpoint. `sqlite` provisions a PVC directly. `mem0` and `zep` are
external-only. SharedMemory extends the same backend model across workspace boundaries,
writing `memory.reader` / `memory.writer` OpenFGA tuples per consumer workspace, gated
by tenant-admin authz (controller-side enforcement; no admission VAP for this check).

## Behavior

- **Memory CREATE**: controller adds finalizer `finalizers.memory.keese.ai/cleanup`,
  transitions through `Pending → Provisioning → Ready`. A `memory.owner` OpenFGA tuple
  is written for `spec.workspaceRef`.
- **Backend dispatch (in-cluster — unreachable via v1alpha1 API)**: the controller
  contains code to SSA-apply StatefulSet + headless Service when `address`/`uri`/`endpoint`
  is empty, but the CRD schema marks these fields required (`minLength: 1`), so admission
  rejects any sub-struct without them. Redis gets a 1 Gi data PVC; Neo4j gets 5 Gi; Zep
  (self-hosted) 2 Gi — but these paths cannot be triggered via the current API.
- **Backend dispatch (external)**: `address`/`uri`/`endpoint` (required, minLength=1) must
  be set; Provision is a no-op for Redis/Neo4j/Qdrant (health-checks address non-empty);
  pgvector verifies the referenced Secret exists; Zep Cloud/Mem0 health-check via Secret
  existence.
- **HA enforcement**: Redis and Qdrant require `replicas ≥ 2` outside dev namespaces
  (label `keese.ai/env=dev` or `-dev` suffix); violation emits `HAViolation` event and
  sets phase `Degraded` (controller-side enforcement — no admission VAP enforces replica
  minimums for Redis or Qdrant).
- **SharedMemory**: each `spec.sharedWith[]` entry writes `memory.reader` or
  `memory.writer` tuples for the referenced workspace's ServiceAccount. Mutations to
  `sharedWith[]` require tenant-admin role (enforced controller-side via OpenFGA check
  on the reconciler; no admission VAP exists for this guard).
- **Deletion**: finalizer purges OpenFGA tuples before backend deprovision to prevent
  orphaned grants, then removes the finalizer.
- **Idempotency**: converges in ≤ 3 reconciles with no spec change (rule 04.16).
- **VAP — `EmbeddingDimImmutable`**: UPDATE that changes `spec.embeddingDim` on
  `Memory` or `SharedMemory` is rejected at admission (`Deny`); create a new resource
  to reindex.
- **VAP — `SqliteSingleConsumer`**: CREATE/UPDATE with `spec.provider.type=sqlite` and
  any `replicas > 1` sub-field is rejected; SQLite with WAL + RWO PVC requires a single
  consumer pod.

## Configuration surface

Key fields are in `api/keese/v1alpha1/memory_types.go`; full contract in
`docs/specs/keese.ai-v1alpha1-memory.md`.

| Field | Notes |
|---|---|
| `spec.provider.type` | Required discriminant; one of 7 backends |
| `spec.provider.<backend>` | Backend-specific config; CEL `exists_one` XValidation enforces one-of |
| `spec.workspaceRef` | Owning workspace; drives `memory.owner` OpenFGA tuple |
| `spec.embeddingDim` | Optional; immutable after creation (`embedding-dim-immutable` VAP-enforced, range 1–65536) |
| `spec.provider.redis.replicas` | HA guard: ≥ 2 outside dev namespaces |
| `spec.provider.sqlite.reclaimPolicy` | `Retain` (default) or `Delete` on Memory deletion |
| `spec.sharedWith[].access` | `reader` (default) or `writer`; controls OpenFGA tuple relation |

## Observability

Event reasons are defined in `internal/controller/keese/memory_events.go:9-50`.

| Event reason | Type | When |
|---|---|---|
| `ProvisioningStarted` | Normal | Backend StatefulSet/Service first created |
| `ProvisioningSucceeded` | Normal | Backend confirmed healthy; phase → `Ready` |
| `ProvisioningFailed` | Warning | Backend provisioning error; phase → `Degraded` |
| `DeprovisioningStarted` | Normal | Deletion finalizer begins cleanup |
| `DeprovisioningSucceeded` | Normal | Cleanup complete; finalizer removed |
| `DeprovisioningFailed` | Warning | Backend deprovision error |
| `RebacSyncSucceeded` | Normal | OpenFGA tuples written |
| `RebacSyncFailed` | Warning | OpenFGA tuple write failed; phase → `Degraded` |
| `RebacPurgeFailed` | Warning | OpenFGA tuple delete failed during cleanup |
| `HAViolation` | Warning | Replicas < 2 outside dev namespace |
| `Degraded` | Warning | Backend unhealthy check returned false |
| `Ready` | Normal | Phase transitions to `Ready` |

Status fields: `status.phase` (`Pending`/`Provisioning`/`Ready`/`Degraded`/`Terminating`),
`status.conditions[Ready]`, `status.observedGeneration`, `status.backendProvisioned`,
`status.rebacTupleCount`. Printer columns: `Age`, `Ready`, `Phase`, `Provider`.

## Known limitations

**API/controller mismatch — in-cluster StatefulSet paths not reachable**: the controller
implements in-cluster StatefulSet provisioning for Redis, Qdrant, Neo4j, and pgvector
when `address`/`endpoint`/`uri`/`dsnSecretRef` is empty, but the CRD schema marks these
fields **required** (`minLength: 1`). Admission rejects any sub-struct without them, so
the in-cluster paths are dead code from the API perspective. Additionally, those paths
would run **unauthenticated** — Neo4j hardcodes `NEO4J_AUTH=none`
(`memory_neo4j_backend.go:208`); Redis and Qdrant StatefulSets carry no auth. Only Zep
self-hosted mounts a projected credential at `/var/run/keese/secrets/zep` per rule 05.7
(`memory_zep_backend.go:222-234`). Always supply external/managed endpoints.

- Controller does not TCP-probe external endpoints (rule 05.4 — no controller-tier
  egress); `Healthy()` returns `true` if an address/URI is non-empty.
- SQLite is single-consumer only; multi-replica scenarios require a network-attached
  backend.
- SharedMemory `sharedWith[]` mutations are enforced controller-side via an OpenFGA
  `tenant#admin` relation check; there is no admission VAP for this guard and none is
  planned (CEL cannot perform cross-resource OpenFGA lookups).
- `mem0` backend provisions only an ExternalSecret; no Healthy probe beyond ESO CRD
  presence.

## Change history

- TD-P1-09 (2026-05-06): SQLite single-pod invariant documented in spec; RWO PVC +
  per-Memory UID-named PVC enforced by controller.
- TD-P2-08 (2026-05-07): `EmbeddingDimImmutable` and `SqliteSingleConsumer` VAPs
  shipped in `config/vap/`.
- TD-P2-12 (2026-05-07): Six backend provisioners (redis, qdrant, pgvector, neo4j,
  mem0, zep) implemented; `MultiBackendProvisioner` dispatches all 7 types.

## References

- Design: `docs/designs/15-memory-management.md`
- Spec: `docs/specs/keese.ai-v1alpha1-memory.md`
- Plan: `docs/plans/demo/tech-debt.md` (TD-P1-09, TD-P2-08, TD-P2-12)
- Source: `internal/controller/keese/memory_controller.go`,
  `internal/controller/keese/memory_*_backend.go`,
  `config/vap/embedding-dim-immutable.yaml`,
  `config/vap/sqlite-single-consumer.yaml`
