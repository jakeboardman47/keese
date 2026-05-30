<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

# Configure memory backends

The `Memory` CR wires a durable, queryable backend to a workspace so agents can persist and retrieve context across sessions.

!!! info "Audience"
    Agent developers and platform engineers configuring per-workspace or shared memory.
    **Prerequisites:** a running keese installation ([Install via OLM](install-olm.md) or
    [Bootstrap a local cluster](bootstrap-local.md)) and at least one
    [Workspace](workspace-session.md).

---

## Overview

`Memory` is a namespaced `keese.ai/v1alpha1` resource. Its `spec.provider` field is a
**discriminated one-of**: exactly one of the seven provider sub-keys may be set, enforced
at admission by a CEL `XValidation` rule. The controller reconciles the desired backend
through a lifecycle FSM (`Pending → Provisioning → Ready ↔ Degraded`) and writes an
OpenFGA ownership tuple linking the Memory to its workspace.

!!! danger "Required fields: address / endpoint / uri / dsnSecretRef"
    For Redis, Qdrant, Neo4j, and pgvector the connection-coordinate field (`address`,
    `endpoint`, `uri`, `dsnSecretRef`) is **required** by the CRD schema (`minLength: 1`).
    Setting it to an empty string or omitting it is rejected at admission. Always supply
    an external managed endpoint. The controller contains in-cluster StatefulSet code paths
    but they are not reachable via the current `v1alpha1` API.

---

## Choosing a backend

```mermaid
flowchart TD
    A([Start: choose a memory backend]) --> B{Need vector<br/>search / RAG?}
    B -- Yes --> C{Graph relationships<br/>matter?}
    B -- No --> D{Need low-latency<br/>key-value cache?}

    C -- Yes --> NEO4J[Neo4j<br/>choose if entity relationships<br/>are first-class]
    C -- No --> VEC{Prefer managed SaaS<br/>or self-hosted?}

    VEC -- Managed --> SAAS{Conversation<br/>memory features needed?}
    SAAS -- Yes, rich API --> MEM0[Mem0<br/>or Zep]
    SAAS -- No --> QDRANT2[Qdrant Cloud<br/>or pgvector RDS]

    VEC -- Self-hosted --> SCALE{High write<br/>throughput?}
    SCALE -- Yes --> QDRANT[Qdrant<br/>optimized for<br/>vector workloads]
    SCALE -- No --> PGV[pgvector<br/>if you already run PostgreSQL]

    D -- Yes --> REDIS[Redis<br/>key-value, session state]
    D -- No --> SQLITE[SQLite on PVC<br/>simplest option — single agent,<br/>no infra dependencies]

    style SQLITE fill:#d4edda,stroke:#28a745
    style REDIS  fill:#cfe2ff,stroke:#0d6efd
    style QDRANT fill:#fff3cd,stroke:#ffc107
    style PGV    fill:#fff3cd,stroke:#ffc107
    style NEO4J  fill:#f8d7da,stroke:#dc3545
    style MEM0   fill:#e2d9f3,stroke:#6f42c1
    style SAAS   fill:#e2d9f3,stroke:#6f42c1
```

| Backend | Best for | In-cluster provisioning | Auth gap |
|---|---|---|---|
| `sqlite` | Single agent, ephemeral or dev workloads | PVC (ReadWriteOnce) | None — file on PVC |
| `redis` | Key-value session cache, low-latency reads | Not reachable — `address` is required by CRD schema | N/A via API |
| `qdrant` | Dense vector search, RAG retrieval | Not reachable — `endpoint` is required by CRD schema | N/A via API |
| `pgvector` | Vector + relational queries, existing Postgres | Not reachable — `dsnSecretRef` is required by CRD schema | N/A via API |
| `neo4j` | Graph memory, entity relationships | Not reachable — `uri` is required by CRD schema | N/A via API |
| `mem0` | Managed semantic memory SaaS | External only | No (projected secret) |
| `zep` | Managed conversation memory SaaS | External only | No (projected secret) |

---

## Provider schema

```mermaid
erDiagram
    Memory {
        string name PK
        string workspaceRef FK
        int    embeddingDim "immutable after create"
    }

    MemoryProvider {
        enum type "sqlite|redis|qdrant|pgvector|neo4j|mem0|zep"
    }

    SQLiteConfig {
        string storageSize   "default 1Gi"
        string storageClassName
        enum   reclaimPolicy "Retain|Delete (default Retain)"
    }

    RedisConfig {
        string address       "host:port (required)"
        int    replicas      "default 1; >=2 outside dev (controller-enforced)"
        string credentialSecretRef
    }

    QdrantConfig {
        string collectionName "required"
        string endpoint      "required — gRPC endpoint"
        int    replicas      "default 1; >=2 outside dev (controller-enforced)"
        string credentialSecretRef
    }

    PGVectorConfig {
        string dsnSecretRef  "required — Secret name holding the PostgreSQL DSN"
        string tableName     "default keese_memory"
    }

    Neo4jConfig {
        string uri           "required — Bolt URI"
        string credentialSecretRef
    }

    Mem0Config {
        string apiEndpoint   "optional SaaS override"
        string credentialSecretRef "required"
    }

    ZepConfig {
        string apiEndpoint   "optional SaaS override"
        string credentialSecretRef "required"
    }

    Memory ||--|| MemoryProvider : "spec.provider"
    MemoryProvider ||--o| SQLiteConfig   : "sqlite (one-of)"
    MemoryProvider ||--o| RedisConfig    : "redis (one-of)"
    MemoryProvider ||--o| QdrantConfig   : "qdrant (one-of)"
    MemoryProvider ||--o| PGVectorConfig : "pgvector (one-of)"
    MemoryProvider ||--o| Neo4jConfig    : "neo4j (one-of)"
    MemoryProvider ||--o| Mem0Config     : "mem0 (one-of)"
    MemoryProvider ||--o| ZepConfig      : "zep (one-of)"
```

The CEL rule in the CRD enforces that **exactly one** provider sub-field is populated.
Setting two fields simultaneously will be rejected at admission.

---

## Backend reference

### SQLite (recommended for local dev)

The controller SSA-applies a single `ReadWriteOnce` PVC named `<memory-name>-memory`.
No external dependency.

```yaml
apiVersion: keese.ai/v1alpha1
kind: Memory
metadata:
  name: agent-mem
  namespace: my-workspace-ns
spec:
  workspaceRef: my-workspace
  provider:
    type: sqlite
    sqlite:
      storageSize: "2Gi"
      storageClassName: standard
      reclaimPolicy: Retain   # PVC survives Memory deletion
```

`reclaimPolicy: Delete` removes the PVC when the Memory CR is deleted. The default is
`Retain` so data survives accidental CR deletion.

### Redis

`spec.provider.redis.address` is **required** (`minLength: 1` in the CRD schema).
There is no schema-valid way to trigger in-cluster StatefulSet provisioning by leaving
`address` empty — the API server rejects a Redis sub-struct without `address`.
Always supply a valid `host:port`.

```yaml
# External Redis (the only schema-valid configuration)
spec:
  provider:
    type: redis
    redis:
      address: "redis.infra.svc.cluster.local:6379"
      credentialSecretRef: redis-creds   # projected at /var/run/keese/secrets/redis-creds
```

!!! note "In-cluster StatefulSet path"
    The controller implements in-cluster Redis StatefulSet provisioning, but the CRD schema
    requires `address` to be non-empty — omitting it is rejected at admission. The in-cluster
    path is not reachable via the current `v1alpha1` API. For local dev, use a Redis instance
    deployed separately (e.g. via Helmfile) and reference it by address.

!!! warning "HA validation"
    The Memory controller emits an `HAViolation` event and sets the Memory phase to
    `Degraded` when Redis or Qdrant `replicas` is less than 2 outside namespaces
    labelled `keese.ai/environment=dev`. This is **controller-side enforcement**, not a
    `ValidatingAdmissionPolicy`. The only memory-related VAPs are `EmbeddingDimImmutable`
    (blocks changes to `spec.embeddingDim` after creation) and `SqliteSingleConsumer`
    (enforces a single replica for SQLite-backed Memory). Neither VAP enforces replica
    minimums for Redis or Qdrant.

### Qdrant

Both `spec.provider.qdrant.collectionName` and `spec.provider.qdrant.endpoint` are
**required** (`minLength: 1` in the CRD schema). There is no schema-valid way to trigger
in-cluster StatefulSet provisioning by leaving `endpoint` empty — the API server rejects
a Qdrant sub-struct without both fields.

```yaml
# External Qdrant Cloud (or self-managed Qdrant)
spec:
  embeddingDim: 1536          # immutable; must match your embedding model
  provider:
    type: qdrant
    qdrant:
      collectionName: agent-memory
      endpoint: "https://xyz.qdrant.tech:6334"
      credentialSecretRef: qdrant-apikey
```

!!! note "In-cluster StatefulSet path"
    The controller implements in-cluster Qdrant StatefulSet/QdrantCluster provisioning, but
    the CRD schema requires `endpoint` and `collectionName` to be non-empty — omitting them
    is rejected at admission. The in-cluster path is not reachable via the current `v1alpha1`
    API. For local dev, deploy Qdrant separately and reference it by endpoint.

!!! warning "embeddingDim is immutable"
    `spec.embeddingDim` is validated immutable by the `EmbeddingDimImmutable`
    `ValidatingAdmissionPolicy`. Changing the dimensionality after creation requires
    deleting and recreating the Memory CR. If the collection already exists in Qdrant
    with a different dimension, writes will fail with an `EmbeddingDimMismatch` event.

### pgvector

`spec.provider.pgvector.dsnSecretRef` is **required** (`minLength: 1` in the CRD schema).
There is no schema-valid way to trigger in-cluster CNPG/StatefulSet provisioning by
omitting `dsnSecretRef` — the API server rejects a pgvector sub-struct without it.
Always supply the name of a Secret holding the PostgreSQL DSN.

```yaml
# External Postgres with pgvector extension (the only schema-valid configuration)
spec:
  embeddingDim: 1536
  provider:
    type: pgvector
    pgvector:
      dsnSecretRef: rds-dsn     # Secret name; mounted as projected file per rule 05.7
      tableName: agent_memory
```

!!! note "In-cluster CNPG/StatefulSet path"
    The controller implements in-cluster CloudNativePG and StatefulSet fallback provisioning,
    but the CRD schema requires `dsnSecretRef` to be non-empty — omitting it is rejected at
    admission. The in-cluster path is not reachable via the current `v1alpha1` API. For local
    dev, provision a Postgres instance separately (e.g. via Helmfile) and reference it via
    `dsnSecretRef`.

### Neo4j

`spec.provider.neo4j.uri` is **required** (`minLength: 1` in the CRD schema). There is
no schema-valid way to trigger in-cluster StatefulSet provisioning by leaving `uri` empty
— the API server rejects a Neo4j sub-struct without it. Always supply a valid Bolt URI.

```yaml
# External Neo4j AuraDB (the only schema-valid configuration)
spec:
  provider:
    type: neo4j
    neo4j:
      uri: "bolt+s://xxxxx.databases.neo4j.io:7687"
      credentialSecretRef: neo4j-creds
```

!!! note "In-cluster StatefulSet path"
    The controller implements an in-cluster `neo4j:5-community` StatefulSet with
    `NEO4J_AUTH=none`, but the CRD schema requires `uri` to be non-empty — omitting it is
    rejected at admission. The in-cluster path is not reachable via the current `v1alpha1`
    API. For local dev, deploy Neo4j separately and reference it by URI.

### Mem0

Mem0 is a SaaS-only backend. `credentialSecretRef` is **required**. The Secret must
be populated by ExternalSecrets Operator from OpenBao (or another vault); it is mounted
as a projected file at `/var/run/keese/secrets/<name>` on the memory-adapter sidecar.

```yaml
spec:
  provider:
    type: mem0
    mem0:
      credentialSecretRef: mem0-apikey   # required
      # apiEndpoint: https://api.mem0.ai  # override for self-hosted Mem0
```

### Zep

Same pattern as Mem0.

```yaml
spec:
  provider:
    type: zep
    zep:
      credentialSecretRef: zep-apikey    # required
      # apiEndpoint: https://api.getzep.com
```

---

## Attaching a Memory to a Workspace

The `Memory.spec.workspaceRef` identifies the owning workspace. The controller writes
an OpenFGA `memory.owner` tuple for that workspace. To make the memory accessible to the
agent runtime, list it in `Workspace.spec.memoryRefs`:

```yaml
apiVersion: keese.ai/v1alpha1
kind: Workspace
metadata:
  name: my-workspace
  namespace: my-workspace-ns
spec:
  runtimeRef:
    name: goose-default   # must reference an AgentRuntime in Ready phase; adkPython/adkGo currently enter Degraded immediately
  tenantRef:
    name: acme
    namespace: acme
  memoryRefs:
    - name: agent-mem   # must be in the same namespace
```

The Workspace controller does not validate that the referenced Memory is Ready; the
agent runtime receives the Memory name and resolves the backend endpoint at startup. If
the Memory is in `Degraded` phase the agent will fail to initialise its memory adapter.

---

## Memory lifecycle

The controller FSM:

1. **Pending** — CR created, finalizer `finalizers.memory.keese.ai/cleanup` not yet added.
2. **Provisioning** — finalizer present; `Backend.Provision` called (SSA-applies the
   backing resource).
3. **Ready** — `Backend.Healthy` returns true and the OpenFGA owner tuple is written.
4. **Degraded** — provisioning failed, backend unhealthy, or ReBAC sync failed.
   The controller retries with exponential backoff (cap 5 minutes).
5. **Terminating** — deletion timestamp set; ReBAC tuples purged first, then
   `Backend.Deprovision` called, then finalizer removed.

Check the status after applying:

```bash
kubectl get mem agent-mem -n my-workspace-ns -o wide
# NAME        PHASE   PROVIDER   READY   AGE
# agent-mem   Ready   sqlite     True    2m
```

View events for provisioning failures:

```bash
kubectl describe mem agent-mem -n my-workspace-ns
```

Key event reasons: `ProvisioningSucceeded`, `ProvisioningFailed`, `Degraded`,
`RebacSyncFailed`, `DeprovisioningSucceeded`.

---

## Credential injection for external backends

Secrets for external backends (Redis, Qdrant, Neo4j, Mem0, Zep) must be mounted as
projected files — never as environment variables (rule 05.7). The recommended flow:

```
OpenBao / Cloud Secret Manager
  → ExternalSecrets Operator → K8s Secret
      → projected volume on memory-adapter sidecar
          → /var/run/keese/secrets/<credentialSecretRef>
```

Create the Secret (for local dev only — production uses ExternalSecrets):

```bash
kubectl create secret generic qdrant-apikey \
  --from-literal=api-key=<your-key> \
  -n my-workspace-ns
```

Then reference the Secret name in `spec.provider.<backend>.credentialSecretRef`.

!!! note "pgvector DSN secret"
    For `pgvector`, the entire DSN string (e.g. `postgres://user:pass@host:5432/db`)
    should be in the Secret, not just the password. Reference it via `dsnSecretRef`.

---

## Observability

!!! warning "Planned — not yet implemented"
    The memory controller does not currently emit Prometheus metrics or OTEL spans.
    The metrics and spans below (`keese_memory_operation_duration_seconds`,
    `keese_memory_errors_total`, `keese_memory_provisioning_duration_seconds`,
    and `keese.memory.read` / `keese.memory.write` OTEL spans) are the **target**
    observability surface but are not yet registered or emitted by the controller.

    In the interim, use `kubectl describe mem <name>` to read controller events and
    phase transitions. See [Observability setup](observability-setup.md) for wiring
    once these metrics land.

---

## See also

- [Concepts: Memory](../concepts/memory.md) — architecture and SharedMemory
- [RAG ingestion](rag-ingestion.md) — ingest documents into a vector Memory
- [Observability setup](observability-setup.md) — wire memory metrics to ECK
- [Create a workspace & attach a session](workspace-session.md) — end-to-end workspace flow
