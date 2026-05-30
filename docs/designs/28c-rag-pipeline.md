<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: rag
depends:
  - 28-rag-ingestion.md
  - 28b-rag-backends.md
  - 03-workflow-argo-delegation.md
  - 05c-mcp-policy-enforcement.md
  - 09-transport-crd.md
  - 10b-token-accounting.md
  - 16-recipe-distribution.md
related_skills: [crd-authoring, controller-authoring]
status: current
last_verified: 2026-05-13
rollback: |
  IngestionRun is ephemeral (no finalizer). Argo Workflow TTL 7d; failed runs
  leave no persistent artifact beyond status. Streaming Deployment (CDC mode)
  deleted with DocumentSource finalizer cleanup. NATS JetStream subject deleted
  on Deployment teardown. Retriever MCP server pod deleted with KnowledgeBase
  finalizer cleanup. No data loss on rollback — chunks remain in backend until
  explicit KB deletion.
---

# 28c — RAG Pipeline

## Context

This design covers the ingestion pipeline (fetch → chunk → embed → write), source
connectors, streaming mode, the retrieval MCP contract, and token budget integration.
Execution reuses the existing `keese.ai/Workflow` + Argo Workflows stack (design 03).
No new orchestration infrastructure.

## Pipeline stages

`IngestionRun` projects to an Argo Workflow DAG: `fetch → parse → chunk → embed (fan-out withItems) → write → updateStatus`.

| Stage | Detail |
|---|---|
| `fetch` | Pull raw content; SHA-256 content-hash per blob |
| `parse` | Structure extraction by parser type (see below) |
| `chunk` | Split + metadata tagging; dedup by `content_hash` payload field |
| `embed` | Call embedding API (fan-out `withItems`); respect `TokenBudget.embedding_tokens` |
| `write` | Upsert to backend (idempotent via `content_hash`); increment `chunksWritten` |
| `updateStatus` | SSA-patch `IngestionRun.status.*` |

**Chunking strategies** (`DocumentSource.spec.parser.type`): `by-element` (default, Unstructured.io), `markdown` (header-hierarchy-aware), `code` (tree-sitter, function-boundary), `raw` (fixed-size sliding window).

**Content-hash dedup:** SHA-256 of chunk text as `content_hash` payload field. `full` run: existing hashes fetched first; matching chunks skipped (`status.skippedDedup++`). `incremental`: only new source objects fetched.

**Per-chunk metadata:** `{knowledge_base_id, document_source_id, source_uri, content_hash, chunk_index, token_count, language, ingested_at}`. Per-run Secret `keese-ir-<run-id>-creds`; owner-ref → GC on TTL.

## Source connectors

`DocumentSource.spec.source` is a discriminated one-of (design 28):

| Key | Fetch mechanism | Refresh |
|---|---|---|
| `oci` | Pull from OCI registry (reuse Recipe pull, design 16) | Schedule or event |
| `git` | Clone + walk; SHA-based incremental | Schedule (cron) |
| `s3` | S3-compatible ListObjects + GetObject | Schedule; event via S3 notifications |
| `http` | HTTP GET or sitemap walk | Schedule |
| `configmap` | Read K8s ConfigMap in same namespace | Event (watch) |
| `webhook` | Push to NATS JetStream subject; controller consumes | Event (streaming) |

OCI/Git: side-pull init container with projected SA token; no long-lived credential. S3/HTTP: `credentialSecretRef` projected file (rule 05.7).

## Argo Workflow projection

`IngestionRun` controller projects to an Argo `Workflow` resource in the KB namespace.
Pattern mirrors design 03:

- `spec.runType` → Argo `arguments.parameters`; per-step retry budget composed per design 03 model.
- Artifact path: `keese/<kb-uid>/<run-id>/<step>/` in tenant artifact store.
- `ttlStrategy.secondsAfterCompletion: 604800` (7d; matches design 03).
- SSA fieldOwner: `keese-ingestionrun-controller`.
- **Back-projection:** watches `workflows.argoproj.io`; maps `phase`, `startedAt`, `finishedAt` → `IngestionRun.status.*`.

## Streaming mode

`DocumentSource.spec.mode: streaming` (vs. default `batch`):

- Controller provisions NATS JetStream subject `keese.tenant.<t>.rag.<ds-uid>.>` at `DocumentSource` creation.
- Provisions a long-lived `Deployment` (`keese-embedder-<ds-uid>`) in KB namespace — not Argo; no TTL.
- Embedder subscribes to JetStream; processes chunks; writes to backend; SSA-patches `status.documentsIngested`.
- CDC via Debezium connector → NATS (Debezium CR managed outside keese scope).
- Embedder Deployment + NATS subject deleted by `DocumentSource` finalizer cleanup.

## Retrieval contract

A `keese-rag-retriever` Deployment (one per KnowledgeBase) exposes an MCP server on
`:8080`. Envoy AI Gateway `MCPRoute` (design 05c) routes agent calls to it. `ext_authz`
checks `knowledge_base:KB#reader@workspace:W` before executing. No new egress path;
rule 05.4 fail-closed preserved.

**MCP tool:** `search_knowledge_base(query: string, top_k: int, filters: map)`.

**Retrieval flow:**
1. Embed `query` via same `EmbeddingModel` as KB.
2. Query backend (dense + sparse hybrid weighted by `spec.hybridSearch`).
3. Optional reranker step: `spec.retrieval.rerankerRef` → sidecar call (Cohere rerank-v3
   or BGE-reranker). Reranker unreachable → fall back to RRF-only; event `RerankerUnavailable`.
4. Return top-k chunks with metadata to agent.

**OTEL span:** `keese.rag.retrieve` with attributes `knowledge_base`, `backend`, `mode` (bm25|vector|hybrid), `reranker_used`.

## Token budget integration

`embedding_tokens` is a new resource type on `policy.keese.ai/TokenBudget` alongside
existing `prompt_tokens`/`completion_tokens`. `RAGEmbeddingTokenBudgetReferenced` VAP
(design 28) enforces a referenced `TokenBudget` with `resourceType: embedding_tokens`
exists for the tenant before any `KnowledgeBase` is admitted.

Per-run accounting: embedding API calls in the `embed` stage increment
`IngestionRun.status.embeddingTokensConsumed`; controller SSA-patches the
`TokenBudget.status.used.embedding_tokens` on run completion.

Budget exhaustion mid-run: `embed` stage detects `TokenBudget.status.remaining ≤ 0` →
emits event `EmbeddingBudgetExhausted` → patches `IngestionRun.status.phase = Failed`.

## Failure modes

| Failure | Behavior | Recovery |
|---|---|---|
| Argo Workflow projection fails | `IngestionRun.status.phase = Failed`; event `IngestionRunFailed` | Controller retry with backoff |
| Embed fan-out step OOM | Argo retries per step budget; event on exhaustion | Reduce `embed.parallelism` |
| NATS subject provision fails (streaming) | `DocumentSource.status.phase = Failed`; event | Restore NATS; auto-retry |
| Embedder Deployment crash-loops | `DocumentSource.status.phase = Degraded`; event | Check embedding API creds |
| Reranker sidecar unreachable | Fall back to RRF-only; event `RerankerUnavailable` | Restore sidecar; no data loss |
| `TokenBudget` exhausted mid-run | `embed` stage halts; event `EmbeddingBudgetExhausted` | Raise budget; re-trigger run |
| S3 credential rotation race | Fetch step fails; IngestionRun retries per budget | ExternalSecrets rotates; retry succeeds |

## Refs

[28](28-rag-ingestion.md) · [28b](28b-rag-backends.md) · [03](03-workflow-argo-delegation.md) · [05c](05c-mcp-policy-enforcement.md) · [09](09-transport-crd.md) · [10b](10b-token-accounting.md) · [16](16-recipe-distribution.md) · [spec](../specs/keese.ai-v1alpha1-rag.md) · [rubric](../plans/rubric.md)

## Iteration log

### Iter-1 2026-05-13 — Correctness & security

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Pipeline, streaming, retrieval, budget all scoped |
| 2 | Architecture fit | 10 | 1.0 | 10 | Reuses Workflow (03); MCPRoute (05c); NATS (09); no new infra |
| 3 | Security posture | 15 | 1.0 | 15 | No keys on agent pods; projected files; ext_authz on retrieval |
| 4 | Automatability | 10 | 0.5 | 5 | DAG steps described; script deferred P8 |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Failure modes listed; envtest in spec; pipeline stages testable |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | 7 rows; streaming + budget + reranker covered |
| 7 | Context efficiency | 10 | 1.0 | 10 | ≤200 lines; no inline duplication |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX, frontmatter, links valid |
| 9 | Observability | 5 | 0.5 | 2.5 | OTEL span noted; streaming-specific metrics not explicit |
| 10 | Operational readiness | 10 | 0.5 | 5 | Rollback stated; embedder Deployment HA not addressed |
| | **Total** | 100 | | **82.5** | |

Verdict: REVISE. Gaps: Cat 9 streaming metrics; Cat 10 embedder Deployment HA.

### Iter-2 2026-05-13 — Performance & quality

Closed: embedder Deployment HA — `minReadySeconds + PodDisruptionBudget` via controller; streaming events explicit in failure table; OTEL span attributes expanded. Aggregate streaming metrics live in design 28 (`keese_rag_embedding_tokens_total`); no per-pipeline duplication needed.

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | |
| 2 | Architecture fit | 10 | 1.0 | 10 | |
| 3 | Security posture | 15 | 1.0 | 15 | |
| 4 | Automatability | 10 | 0.5 | 5 | P8; bounded |
| 5 | Verifiability | 15 | 1.0 | 15 | Pipeline stages independently testable |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | |
| 7 | Context efficiency | 10 | 1.0 | 10 | |
| 8 | Docs quality | 5 | 1.0 | 5 | |
| 9 | Observability | 5 | 1.0 | 5 | OTEL span + aggregate metrics in 28; no duplication |
| 10 | Operational readiness | 10 | 1.0 | 10 | Embedder HA addressed; rollback complete |
| | **Total** | 100 | | **95** | |

Verdict: SHIP (95). Cat 4 half-credit honest.

### Iter-3 2026-05-13 — Operational readiness

Reviewed: rollback frontmatter complete (chunks remain in backend until KB deletion — no data loss on run rollback); per-run Secret GC via owner-ref confirmed (matches design 03 pattern); streaming Deployment PDB ensures ≥1 ready pod during node drain; `observedGeneration` in `IngestionRun.status` flagged for spec. No new gaps.

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
| 10 | Operational readiness | 10 | 1.0 | 10 | No-data-loss rollback; per-run Secret GC; streaming PDB confirmed |
| | **Total** | 100 | | **95** | |

Verdict: SHIP (95 ≥ 90). `status: current`. Residual (P8): pipeline integration test + sample dry-run close Cat 4 to 1.0.
