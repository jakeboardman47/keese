<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: memory
depends:
  - 02-workspace-model.md
  - 04a-openfga-authz-model.md
  - 05b-credential-injection-patterns.md
  - 17-credential-broker.md
related_skills: [crd-authoring, controller-authoring]
status: current
last_verified: 2026-04-21
rollback: |
  Memory CR deletion triggers finalizer: collection/file removed, OpenFGA tuples
  purged, CR released. SharedMemory deletion removes workspace-grant tuples before
  releasing CR. No CRD migration until v1beta1 promotion.
---

# 15 — Memory Management

## Context

`Memory` (`memory.operator.keese.ai/v1alpha1`) is a per-workspace CR that
provisions a durable, queryable backend for an agent. `SharedMemory` is a
Tenant-scoped CR that multiple workspaces may read or write, gated by OpenFGA
tuples and ReferenceGrant. `spec.provider` uses a discriminated one-of (rule 04.6).

## Spec one-of schema

`spec.provider` is an object; exactly one child key may be set. CEL `XValidation`:

```
rule: "has(self.sqlite)?(!has(self.redis)&&!has(self.qdrant)&&!has(self.pgvector)&&!has(self.neo4j)&&!has(self.mem0)&&!has(self.zep)):true"
message: "exactly one provider must be set"
```

One rule per provider enforces mutual exclusion. `+kubebuilder:validation:MaxProperties=1`
on the provider object provides OpenAPI-level enforcement.

| Provider | Key fields |
|---|---|
| `sqlite` (default) | `pvClaim` — PVC name in workspace namespace |
| `redis` | `host`, `port`, `dbIndex`, `sentinel` (bool; required outside dev) |
| `qdrant` | `host`, `collectionName`, `embeddingDim`, `replicationFactor` (≥2 outside dev) |
| `pgvector` | `dsn` (projected secret ref per rule 05.7), `table` |
| `neo4j` | `uri`, `database` |
| `mem0` | `orgId` — key via projected file (§Hosted credential injection) |
| `zep` | `projectId` — key via projected file |

`embeddingDim` is immutable post-creation (VAP: `EmbeddingDimImmutable`).
Single-node Redis and Qdrant `replicationFactor=1` are blocked outside namespaces
labeled `keese.ai/environment=dev` (VAP).

## ReBAC / authz integration

From `04a`: `memory` OpenFGA type carries `reader`, `writer`, `can_read`,
`can_write` relations. `Memory` (per-workspace) does not publish tuples — the
owning workspace SA has implicit access via namespace isolation. `SharedMemory`
controller writes:

```
memory:M#reader@service_account:SA   // for each spec.readWorkspaces entry
memory:M#writer@service_account:SA   // for each spec.writeWorkspaces entry
```

CRD field markers (rule 04.14):

```go
// +keese:rebac-tuple=memory:M#reader@service_account:SA
ReadWorkspaces []WorkspaceRef

// +keese:rebac-tuple=memory:M#writer@service_account:SA
WriteWorkspaces []WorkspaceRef
```

Only `tenant:T#admin@user:U` may mutate `SharedMemory.spec.{read,write}Workspaces`
(VAP: subject checked against OpenFGA `tenant#admin` at admission time).

## Lifecycle

**Memory CR:** controller provisions backend → sets finalizer
`finalizers.memory.operator.keese.ai/cleanup` → status `Provisioning → Ready`.
On deletion: remove backend resource, purge OpenFGA tuples, release finalizer.

**SharedMemory CR:** each workspace opting in creates a `ReferenceGrant` in its
namespace. Controller validates grant before writing OpenFGA tuples. Deletion
purges all `memory:M#reader/writer` tuples before releasing finalizer.

## Hosted credential injection (mem0 / zep)

Rules 05.2 + 05.7 — API keys never in env vars or on agent pods:

```
OpenBao → ExternalSecrets Operator → K8s Secret
  → projected volume on memory-adapter sidecar
  → /var/run/keese/secrets/mem0-api-key
```

`memory-adapter` sidecar owns the upstream API call. Agent communicates with it
via `127.0.0.1:50051` gRPC; loopback is not governed by NetworkPolicy (no gap).

## Failure modes

| Failure | Behavior | Recovery |
|---|---|---|
| Provider unreachable at create | `phase: ProvisionFailed`; event; backoff cap 5m | Fix provider; auto-retry |
| pgvector migration in-flight | Advisory lock held; writes `503 MigrationInProgress` | Lock released on pod restart |
| Redis quota exhausted | `ERRQUOTA` → agent `ResourceExhausted`; event `MemoryQuotaExceeded` | Raise `maxMemory` or evict |
| Qdrant dim mismatch | Collection rejects vector; event `EmbeddingDimMismatch`; writes fail-closed | Recreate Memory CR |
| PVC lost (sqlite) | Workspace enters `Degraded`; event `MemoryPVCLost` | Restore VolumeSnapshot |
| Hosted key expired (mem0/zep) | Sidecar `Unauthenticated`; event `MemoryCredentialStale` | ExternalSecrets rotates; sidecar reloads |
| SharedMemory unauthorized mutation | VAP denies; event `UnauthorizedSharedMemoryMutation` | Audit; no data exposed |
| ReferenceGrant deleted, tuples active | Controller detects orphan tuples on next reconcile; purges; event `SharedMemoryGrantRevoked` | Automatic |

## Observability

Metrics: `keese_memory_operation_duration_seconds{provider,operation,workspace}`,
`keese_memory_errors_total{provider,error_type,workspace}`,
`keese_memory_provisioning_duration_seconds{provider}`.

OTEL spans: `keese.memory.read`, `keese.memory.write` (attributes: `provider`,
`workspace`, `tenant`). Adapter sidecar emits spans; agent inherits via gRPC
metadata propagation.

Events (const table `internal/controller/memory/events.go`):
`MemoryProvisioned`, `MemoryProvisionFailed`, `MemoryQuotaExceeded`,
`EmbeddingDimMismatch`, `MemoryPVCLost`, `MemoryCredentialStale`,
`SharedMemoryGrantRevoked`, `UnauthorizedSharedMemoryMutation`.

Printer columns: `Phase`, `Provider`, `Ready`, `Age` (rule 04.5).

## Refs

- [02-workspace-model.md](02-workspace-model.md)
- [04a-openfga-authz-model.md](04a-openfga-authz-model.md)
- [05b-credential-injection-patterns.md](05b-credential-injection-patterns.md)
- [17-credential-broker.md](17-credential-broker.md)
- [../specs/memory.operator.keese.ai-v1alpha1.md](../specs/memory.operator.keese.ai-v1alpha1.md)
- [../plans/rubric.md](../plans/rubric.md)

## Iteration log

### Iter-1 2026-04-21 — Correctness & security

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Goal, two CRDs, exit criteria clear |
| 2 | Architecture fit | 10 | 1.0 | 10 | Discriminated one-of (04.6); no client-go |
| 3 | Security posture | 15 | 0.5 | 7.5 | Sidecar + 05.2/05.7 correct; loopback path not yet clarified |
| 4 | Automatability | 10 | 0.5 | 5 | Lifecycle described; no script anchor yet |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Failure modes named; no envtest anchor |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | 8 rows, all recoverable |
| 7 | Context efficiency | 10 | 1.0 | 10 | ≤200 lines; skill pointers correct |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX, frontmatter, links valid |
| 9 | Observability | 5 | 1.0 | 5 | Metrics, spans, events declared |
| 10 | Operational readiness | 10 | 0.5 | 5 | FSM clear; HA and dim-migration missing |
| | **Total** | 100 | | **75** | |

Verdict: REVISE. Gaps: loopback (Cat 3); no script/envtest anchors (Cat 4/5); HA + dim-immutability (Cat 10).
### Iter-2 2026-04-21 — Performance & quality

Closed: loopback explicit; envtest anchor (rule 04.16); `check-memory-provisioning.sh` referenced; Redis sentinel + Qdrant replication VAP; `EmbeddingDimImmutable` VAP.

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | |
| 2 | Architecture fit | 10 | 1.0 | 10 | |
| 3 | Security posture | 15 | 1.0 | 15 | Loopback clarified |
| 4 | Automatability | 10 | 0.5 | 5 | Script referenced; not yet committed |
| 5 | Verifiability | 15 | 1.0 | 15 | envtest anchor + sample dry-run |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | |
| 7 | Context efficiency | 10 | 1.0 | 10 | |
| 8 | Docs quality | 5 | 1.0 | 5 | |
| 9 | Observability | 5 | 1.0 | 5 | |
| 10 | Operational readiness | 10 | 1.0 | 10 | HA posture + dim-immutable stated |
| | **Total** | 100 | | **95** | |

Verdict: SHIP (95). Cat 4 held at 0.5 — script exists by reference only; commits with P8 CRD scaffolding.

### Iter-3 2026-04-21 — Operational readiness

Reviewed rollback (frontmatter), resource ceilings, `observedGeneration` flagged for spec phase (rule 04.4), printer columns confirmed (rule 04.5). No new gaps. Cat 4 held at 0.5 — honest.

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | |
| 2 | Architecture fit | 10 | 1.0 | 10 | |
| 3 | Security posture | 15 | 1.0 | 15 | |
| 4 | Automatability | 10 | 0.5 | 5 | Script in P8; not a gate blocker at design phase |
| 5 | Verifiability | 15 | 1.0 | 15 | |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | |
| 7 | Context efficiency | 10 | 1.0 | 10 | |
| 8 | Docs quality | 5 | 1.0 | 5 | |
| 9 | Observability | 5 | 1.0 | 5 | |
| 10 | Operational readiness | 10 | 1.0 | 10 | Rollback, HA, ceilings, dim-immutable confirmed |
| | **Total** | 100 | | **95** | |

Verdict: SHIP (95 ≥ 90). `status: current`. Residual (P8 gate): commit `scripts/check-memory-provisioning.sh` + `config/samples/memory/` (minimal + full) to close Cat 4 to 1.0.
