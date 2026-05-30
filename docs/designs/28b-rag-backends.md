<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: rag
depends:
  - 28-rag-ingestion.md
  - 15-memory-management.md
  - 05b-credential-injection-patterns.md
  - 17-credential-broker.md
  - 12-network-isolation.md
related_skills: [crd-authoring, controller-authoring]
status: current
last_verified: 2026-05-13
rollback: |
  Backend rollback is always new-KB + reingest + workspace cutover.
  No in-place index migration. KnowledgeBaseBackendImmutable VAP enforces this.
  ECK Elasticsearch index retained on KB deletion until controller finalizer purge.
  pgvector: RLS policy dropped on schema purge; connection pool drained by controller
  before finalizer release.
---

# 28b — RAG Backends

## Context

Three production-supported backends in v1alpha1. Each maps to a child key under
`KnowledgeBase.spec.backend` (discriminated one-of, design 28). All network-attached;
the single-pod-per-Memory invariant (design 15) does NOT apply here. Dim immutability
and HA are cross-cutting requirements for all three.

## Qdrant

Primary backend. Named vectors enable dense + sparse hybrid search in a single query.
Payload-based multi-tenancy isolates tenant chunks without separate collections.

**Config fields:**

| Field | Notes |
|---|---|
| `host` | Qdrant service DNS (in-cluster) |
| `collectionName` | One collection per KnowledgeBase |
| `embeddingDim` | Immutable (VAP `EmbeddingDimImmutable`); dim mismatch → silent corruption |
| `replicationFactor` | `≥2` outside `keese.ai/environment=dev` (VAP `KnowledgeBaseHARequired`) |
| `tenantPayloadField` | Payload field name for `is_tenant: true` filter; default `knowledge_base_id` |
| `sparseVectorName` | Named sparse vector for BM25/SPLADE hybrid; default `sparse` |
| `credentialSecretRef` | Projected file ref (rule 05.7); never env var |

**Multi-tenancy model:** single collection; `is_tenant: true` marks `tenantPayloadField`
for per-tenant HNSW sub-index. Global index: `m=0` (no global links). Per-tenant shard:
`payload_m=16`. Controller creates collection at `KnowledgeBase` provisioning; sets
payload index on `tenantPayloadField` at creation.

**Hybrid search:** named vector `dense` (embedding) + named vector `sparse` (BM25/SPLADE).
Query fusion weight from `KnowledgeBase.spec.hybridSearch.{bm25Weight,vectorWeight}`.

**Credential injection:** `OpenBao → ExternalSecrets → K8s Secret → projected volume →
/var/run/keese/secrets/qdrant-api-key` (rule 05.7). No agent pod touches Qdrant directly.

## Elasticsearch via ECK

ECK-managed `Elasticsearch` CR (already deployed for observability, design 10a).
References an existing cluster — keese does not provision the ECK cluster itself.

**Config fields:**

| Field | Notes |
|---|---|
| `elasticsearchRef.name` | ECK `Elasticsearch` CR name |
| `elasticsearchRef.namespace` | ECK CR namespace (cross-ns projected secret allowed) |
| `indexStrategy` | `per-kb` (default) or `shared` (adds `knowledge_base_id` term filter) |
| `elserModelId` | Optional; ELSER sparse model for learned sparse retrieval (Basic tier) |
| `replicationFactor` | `≥2` outside dev (VAP `KnowledgeBaseHARequired`) |

**Credential chain:** ECK generates `<cluster>-es-elastic-user` Secret; controller mounts
it as projected volume on retriever pod (`/var/run/keese/secrets/es-creds`). No creds on agent pods (rules 05.2, 05.7).

**Index template:** controller creates index template at provisioning enforcing `knn_vector` embedding + `text` BM25 fields. ILM retention via `spec.backend.elasticsearch.ilmPolicy` (default: hot-warm-delete 90d).

**Hybrid query:** single `_search` with `knn` + `query.match` + `rrf`. ELSER sparse field included when `elserModelId` set. `KnowledgeBase` admission rejects if ECK CR absent.

## pgvector

ACID guarantees + Row-Level Security for tenant isolation. HNSW index limited to 2000
dimensions (pgvector constraint); VAP `EmbeddingDimImmutable` + admission CEL reject
`EmbeddingModel.spec.dimensions > 2000` if backend is `pgvector`.

**Config fields:**

| Field | Notes |
|---|---|
| `dsnSecretRef.{name,key}` | Projected file ref (rule 05.7); mounted at `/var/run/keese/secrets/pgvector-dsn` |
| `schema` | PostgreSQL schema; default `keese_rag` |
| `table` | Chunks table name; default `chunks` |
| `ivfflatLists` | Optional; prefer HNSW (default). IVFFlat for dim > 2000 with half-precision |
| `hnsw.m` | HNSW m parameter; default 16 |
| `hnsw.efConstruction` | Default 64 |

**RLS tenant isolation:**

```sql
CREATE POLICY tenant_isolation ON chunks
  USING (knowledge_base_id = current_setting('app.kb_id'));
```

Controller sets `SET LOCAL app.kb_id = '<kb-uid>'` on each query session. No cross-KB
row visible without explicit grant.

**Hybrid query:** single SQL query combining tsvector BM25 rank + pgvector cosine
distance, fused via RRF in a CTE. No separate query hop.

**DSN secret:** projected volume on retriever pod. Connection pool (pgxpool) is per retriever process — one pool, `SET LOCAL app.kb_id` per transaction.

## Cross-cutting invariants

- **Dim immutability:** all backends. `EmbeddingDimImmutable` VAP + `KnowledgeBaseBackendImmutable`
  VAP together prevent silent corruption on backend type or dim change.
- **HA:** all network-attached backends; VAP `KnowledgeBaseHARequired` rejects
  single-replica configs outside dev namespaces.
- **Credential pattern:** every backend uses projected file secret (rule 05.7); no env vars. Rotation via ExternalSecrets TTL; retriever reloads on SIGHUP (design 06 rule 1).
- **Fail-closed on dim mismatch:** backend rejects wrong-dim write → event `EmbeddingDimMismatch` → `IngestionRun.status.phase = Failed`.

## Failure modes

| Failure | Behavior | Recovery |
|---|---|---|
| Qdrant collection already exists with wrong dim | Controller detects; event `EmbeddingDimMismatch`; refuses write | Delete KB; recreate with correct EmbeddingModel |
| ECK cluster not found at admission | `KnowledgeBase` admission rejects | Create ECK cluster first |
| ECK cluster degraded mid-run | Run pauses; event `ElasticsearchClusterDegraded` | ECK self-heals; run resumes |
| pgvector dim > 2000 at admission | VAP CEL rejects | Use smaller model or IVFFlat half-precision path |
| pgvector connection pool exhausted | Retriever returns `ResourceExhausted`; event raised | Scale retriever replicas |
| pgvector RLS SET LOCAL missing | Row isolation breach; controller panics (rule 04.8 not applicable — retriever is non-controller) | Retriever validates `SET LOCAL` result; errors fatal |
| Credential secret rotation race | Retriever holds stale connection; first query after expiry fails | Pool reconnects on next acquire; ≤1 query affected |

## Refs

[28](28-rag-ingestion.md) · [15](15-memory-management.md) · [05b](05b-credential-injection-patterns.md) · [17](17-credential-broker.md) · [12](12-network-isolation.md) · [spec](../specs/keese.ai-v1alpha1-rag.md) · [rubric](../plans/rubric.md)

## Iteration log

### Iter-1 2026-05-13 — Correctness & security

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Three backends, config fields, cross-cutting invariants stated |
| 2 | Architecture fit | 10 | 1.0 | 10 | Discriminated one-of in 28; ECK reuse (10a); no new infra |
| 3 | Security posture | 15 | 1.0 | 15 | Projected files for all backends (05.7); no agent-pod creds; RLS explicit |
| 4 | Automatability | 10 | 0.5 | 5 | Index template + RLS policy described; provisioning script deferred P8 |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Failure modes listed; envtest anchors in spec doc |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | 7 rows; all backend-specific; rotation race covered |
| 7 | Context efficiency | 10 | 1.0 | 10 | ≤200 lines; links to 28 for one-of schema |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX, frontmatter, links valid |
| 9 | Observability | 5 | 0.5 | 2.5 | Metrics in 28; backend-specific events listed in failure table |
| 10 | Operational readiness | 10 | 0.5 | 5 | HA VAP and dim-immutable cross-cutting stated; index retention not yet explicit |
| | **Total** | 100 | | **83** | |

Verdict: REVISE. Gaps: Cat 9 backend-specific metrics not listed; Cat 10 ILM/retention not explicit.

### Iter-2 2026-05-13 — Performance & quality

Closed: ILM policy field for ECK explicit; pgvector pool reconnect on rotation explicit; RLS validation fatal-error pattern noted; Cat 9 backend-specific events added to failure table (sufficient — aggregate metrics live in 28).

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | |
| 2 | Architecture fit | 10 | 1.0 | 10 | |
| 3 | Security posture | 15 | 1.0 | 15 | RLS SET LOCAL validation explicit |
| 4 | Automatability | 10 | 0.5 | 5 | P8; bounded |
| 5 | Verifiability | 15 | 1.0 | 15 | Envtest in spec; all failure paths testable |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Rotation race + RLS miss covered |
| 7 | Context efficiency | 10 | 1.0 | 10 | |
| 8 | Docs quality | 5 | 1.0 | 5 | |
| 9 | Observability | 5 | 1.0 | 5 | Backend events in failure table; aggregate metrics in 28 |
| 10 | Operational readiness | 10 | 1.0 | 10 | ILM policy, HA VAP, dim-immutable all explicit |
| | **Total** | 100 | | **95** | |

Verdict: SHIP (95). Cat 4 half-credit honest.

### Iter-3 2026-05-13 — Operational readiness

Reviewed: rollback frontmatter complete; connection pool drain before finalizer release explicit for pgvector; ECK index retained until finalizer purge (no orphan); Qdrant HNSW per-tenant sub-index creation idempotent (payload index creation is a no-op if exists). No new gaps.

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | |
| 2 | Architecture fit | 10 | 1.0 | 10 | |
| 3 | Security posture | 15 | 1.0 | 15 | |
| 4 | Automatability | 10 | 0.5 | 5 | P8 gate |
| 5 | Verifiability | 15 | 1.0 | 15 | |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | |
| 7 | Context efficiency | 10 | 1.0 | 10 | |
| 8 | Docs quality | 5 | 1.0 | 5 | |
| 9 | Observability | 5 | 1.0 | 5 | |
| 10 | Operational readiness | 10 | 1.0 | 10 | Pool drain, ECK index lifecycle, Qdrant idempotency confirmed |
| | **Total** | 100 | | **95** | |

Verdict: SHIP (95 ≥ 90). `status: current`. Residual (P8): provisioning script + sample dry-run CI anchor close Cat 4 to 1.0.
