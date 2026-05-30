<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: spec
category: contract
depends:
  - ../designs/28-rag-ingestion.md
  - ../designs/28b-rag-backends.md
  - ../designs/28c-rag-pipeline.md
  - ../designs/04a-openfga-authz-model.md
  - ../designs/15-memory-management.md
related_skills: [crd-authoring, controller-authoring]
status: current
last_verified: 2026-05-29
regression_lock: false
tests:
  unit: [internal/controller/rag/knowledgebase_controller_test.go, internal/controller/rag/documentsource_controller_test.go, internal/controller/rag/ingestionrun_controller_test.go, internal/controller/rag/embeddingmodel_controller_test.go, internal/controller/rag/sharedknowledgebase_controller_test.go]
  envtest: [internal/controller/rag/suite_test.go]
  kuttl: [test/e2e/kuttl/rag/]
metrics: [keese_rag_ingestion_duration_seconds{knowledge_base,backend,run_type}, keese_rag_ingestion_errors_total{knowledge_base,error_type}, keese_rag_embedding_tokens_total{knowledge_base,model,tenant}, keese_rag_chunks_written_total{knowledge_base,backend}, keese_rag_chunks_skipped_dedup_total{knowledge_base}, keese_rag_retrieval_duration_seconds{knowledge_base,backend,mode}, keese_rag_retrieval_topk_recall{knowledge_base}]
events: [KnowledgeBaseProvisioned, KnowledgeBaseProvisionFailed, IngestionRunStarted, IngestionRunCompleted, IngestionRunFailed, SourceFetchFailed, DocumentParseFailed, EmbeddingDimMismatch, EmbeddingBudgetExhausted, RerankerUnavailable, StorageQuotaExceeded, SharedKnowledgeBaseGrantRevoked, EmbeddingModelInUseDeleteRejected]
---

# keese.ai v1alpha1 — RAG spec

**Goal.** Five CRDs (`KnowledgeBase`, `DocumentSource`, `IngestionRun`, `EmbeddingModel`,
`SharedKnowledgeBase`) provide document ingestion, embedding, and retrieval with zero-trust
credential handling and OpenFGA ReBAC enforcement.

Owning designs: [28](../designs/28-rag-ingestion.md) · [28b](../designs/28b-rag-backends.md) · [28c](../designs/28c-rag-pipeline.md).

## KnowledgeBase CRD

**`spec.backend`** — discriminated one-of; `+kubebuilder:validation:MaxProperties=1`. CEL rule enforces mutual exclusion across `qdrant | elasticsearch | pgvector` (one rule per variant, mirrors design 15 pattern).

| Field | Notes |
|---|---|
| `spec.tenantRef` | `// +keese:rebac-tuple=knowledge_base:KB#owner@tenant:T` |
| `spec.embeddingModelRef` | Must reference existing `EmbeddingModel` in same namespace |
| `spec.backend` | Discriminated one-of: `qdrant \| elasticsearch \| pgvector` |
| `spec.chunking.{strategy,maxTokens,overlap}` | Default strategy `by-element` |
| `spec.hybridSearch.{enabled,bm25Weight,vectorWeight}` | Weights sum = 1.0 (CEL) |
| `spec.retrieval.{rerankerRef,topK,maxFilterChars}` | `rerankerRef` optional |
| `spec.workspaceRefs[]` | `// +keese:rebac-tuple=knowledge_base:KB#reader@workspace:W` |

**Status:** `observedGeneration` (rule 04.4); `phase`: `Provisioning | Ready | ProvisionFailed | Degraded | Terminating`; `documentCount`, `chunkCount`, `lastIndexedAt`, `embeddingTokensConsumed`, `referencingWorkspaceCount`.

**Printer columns (04.5):** `Age`, `Ready`, `Phase`, `Backend`, `Documents`, `Chunks`.

**VAPs:** `EmbeddingDimImmutable`, `KnowledgeBaseBackendImmutable`, `KnowledgeBaseHARequired`, `RAGEmbeddingTokenBudgetReferenced`.

**RBAC markers:**

```go
// +kubebuilder:rbac:groups=keese.ai,resources=knowledgebases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=keese.ai,resources=knowledgebases/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=keese.ai,resources=knowledgebases/finalizers,verbs=update
```

**Finalizer:** `finalizers.knowledgebase.keese.ai/cleanup`. Deletion order: purge OpenFGA tuples → delete backend collection/index → release. Blocked while `status.referencingWorkspaceCount > 0`. **SSA field owner:** `keese-knowledgebase-controller`.

## DocumentSource CRD

**`spec.source`** — discriminated one-of; `+kubebuilder:validation:MaxProperties=1`. Keys: `oci | git | s3 | http | configmap | webhook`.

| Field | Notes |
|---|---|
| `spec.knowledgeBaseRef` | `// +keese:rebac-tuple=knowledge_base:KB#writer@service_account:ingestion-SA` |
| `spec.source` | Discriminated one-of; credential via projected file (rule 05.7) |
| `spec.schedule.{type,cron}` | `type: cron | event | manual` |
| `spec.parser.{type,options}` | `type: by-element | markdown | code | raw` |

**Status:** `observedGeneration`; `phase`: `Idle | Running | Failed | Suspended`; `lastRunAt`, `documentsKnown`, `documentsIngested`. **Printer columns:** `Age`, `Ready`, `Phase`, `SourceType`, `LastRun`. **Finalizer:** `finalizers.documentsource.keese.ai/cleanup` (tears down streaming Deployment + NATS subject). **SSA field owner:** `keese-documentsource-controller`.

## IngestionRun CRD

**Spec:** `documentSourceRef`, `runType: full | incremental | dryRun`, `retryBudget` (default 5), `ttlSecondsAfterFinished` (default 604800). Argo Workflow-backed (design 03c); per-run Secret `keese-ir-<run-id>-creds`; owner-ref → GC.

**Status:** `observedGeneration`; `phase`: `Pending | Running | Succeeded | Failed | Skipped`; `processedDocuments`, `skippedDedup`, `failedDocuments`, `chunksWritten`, `embeddingTokensConsumed`. **Printer columns:** `Age`, `Ready`, `Phase`, `Documents`, `Tokens`. **No finalizer** (ephemeral). **SSA field owner:** `keese-ingestionrun-controller`.

## EmbeddingModel CRD

**`spec.endpoint`** — discriminated one-of: `hostedRef | localRef`.

| Field | Notes |
|---|---|
| `spec.provider` | `openai \| cohere \| local \| voyage \| nvidia \| jina` |
| `spec.dimensions` | **Immutable** (VAP `EmbeddingDimImmutable`); `> 2000` rejected for pgvector (CEL) |
| `spec.maxContextTokens` | Advisory; enforced by chunker |
| `spec.languages[]` | BCP-47 codes |
| `spec.endpoint` | `hostedRef`: credential broker (design 17). `localRef`: in-cluster Service |

**Status:** `observedGeneration`; `phase`: `Ready | Failed`. **Printer columns:** `Age`, `Provider`, `Model`, `Dims`. **Finalizer:** `finalizers.embeddingmodel.keese.ai/cleanup` — deletion refused while any KB references it; event `EmbeddingModelInUseDeleteRejected`. **SSA field owner:** `keese-embeddingmodel-controller`.

## SharedKnowledgeBase CRD

Cluster-scoped; mirrors `SharedMemory` (design 15).

```go
// +kubebuilder:resource:scope=Cluster
// +keese:rebac-tuple=shared_knowledge_base:SKB#reader@tenant:T
ReaderTenants []TenantRef
```

All grants require Approved `CrossTenantAgreement` (design 25). **VAP `RAGTenantAuthzMutation`** — `readerTenants` mutations: `tenant:T#admin@user:U` only. **Finalizer:** `finalizers.sharedknowledgebase.keese.ai/cleanup` — purge all `shared_knowledge_base#reader` tuples before releasing. **SSA field owner:** `keese-sharedknowledgebase-controller`.

## Acceptance tests

Samples: `config/samples/rag/{knowledgebase,documentsource,ingestionrun,embeddingmodel,sharedknowledgebase}/{minimal,full}.yaml` — all pass `kubectl apply --dry-run=server` (rule 04.15).

### KnowledgeBase envtest

| Test | Assertion |
|---|---|
| `TestKB_Idempotency` | 3 reconciles → status stable; no duplicate OpenFGA writes |
| `TestKB_BackendOneOf_CEL` | Two backend keys → admission reject |
| `TestKB_BackendImmutable_VAP` | Update backend type post-create → VAP deny |
| `TestKB_FinalizerBlockedWhileWorkspaceRefs` | Delete with `referencingWorkspaceCount > 0` → blocked |
| `TestKB_FinalizerCleanupRace` | Concurrent delete + reconcile → finalizer released exactly once |
| `TestKB_HARequired_VAP` | `replicationFactor=1` in non-dev namespace → VAP deny |

### DocumentSource envtest

| Test | Assertion |
|---|---|
| `TestDS_Idempotency` | 3 reconciles → no duplicate IngestionRun creates |
| `TestDS_SourceOneOf_CEL` | Two source keys → admission reject |
| `TestDS_StreamingDeploymentProvisioned` | `mode: streaming` → Deployment + NATS subject present |
| `TestDS_FinalizerCleanupRace` | Concurrent delete → Deployment + subject purged; finalizer released once |
| `TestDS_CredentialNeverEnvVar` | S3 pod spec → zero env vars carrying secret material |
| `TestDS_IncrementalRun_Dedup` | Same content submitted twice → `skippedDedup = 1` |

### IngestionRun envtest

| Test | Assertion |
|---|---|
| `TestIR_Idempotency` | 3 reconciles → Argo Workflow not recreated if exists |
| `TestIR_ArgoBacked_DAG` | Argo Workflow has all 6 pipeline steps |
| `TestIR_TTL_Applied` | `ttlStrategy.secondsAfterCompletion = 604800` on Argo Workflow |
| `TestIR_BudgetExhausted` | `TokenBudget.embedding_tokens = 0` → phase Failed; event `EmbeddingBudgetExhausted` |
| `TestIR_DryRun_NoWrite` | `runType: dryRun` → no backend write; `chunksWritten = 0` |
| `TestIR_StatusBackProjection` | Argo Workflow phase → `IngestionRun.status.phase` synced |

### EmbeddingModel envtest

| Test | Assertion |
|---|---|
| `TestEM_Idempotency` | 3 reconciles → status stable |
| `TestEM_DimImmutable_VAP` | Update `spec.dimensions` → VAP deny |
| `TestEM_DeleteBlockedWhileReferenced` | KB references it → finalizer blocks delete; event raised |
| `TestEM_PgvectorDimLimit_VAP` | `dimensions > 2000` + pgvector KB → VAP deny |
| `TestEM_LocalRef_Endpoint` | `endpoint.localRef` → retriever uses in-cluster Service URL |
| `TestEM_CredentialNeverEnvVar` | Hosted endpoint pod spec → zero secret env vars |

### SharedKnowledgeBase envtest

| Test | Assertion |
|---|---|
| `TestSKB_Idempotency` | 3 reconciles → tuples stable; no extra OpenFGA writes |
| `TestSKB_CTA_Required` | No Approved CTA → controller rejects tuple write |
| `TestSKB_ReaderTupleWritten` | `readerTenants` entry → tuple `shared_knowledge_base:SKB#reader@tenant:T` present |
| `TestSKB_FinalizerCleanupRace` | Concurrent delete → all tuples purged; finalizer released once |
| `TestSKB_UnauthorizedMutation_VAP` | Non-admin patches `readerTenants` → VAP deny + event |
| `TestSKB_GrantRevoked` | CTA revoked → orphan tuples purged; event `SharedKnowledgeBaseGrantRevoked` |

## Iteration log

### Iter-1 2026-05-13 — Correctness & security

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Five CRDs, goal, test anchors, exit criteria |
| 2 | Architecture fit | 10 | 1.0 | 10 | SSA fieldOwner per kind; discriminated one-of; no client-go |
| 3 | Security posture | 15 | 1.0 | 15 | ReBAC markers on all authz fields; VAP chain; no credential env vars |
| 4 | Automatability | 10 | 0.5 | 5 | Sample dry-run referenced; check script deferred P8 |
| 5 | Verifiability | 15 | 1.0 | 15 | 6 envtest per CRD; VAP + race + credential + budget tests named |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Finalizer order; deletion guards; streaming teardown |
| 7 | Context efficiency | 10 | 1.0 | 10 | ≤200 lines; links to designs; no inline duplication |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX, frontmatter complete; status: draft |
| 9 | Observability | 5 | 1.0 | 5 | Metrics, events in frontmatter; printer columns per kind |
| 10 | Operational readiness | 10 | 1.0 | 10 | observedGeneration, finalizer order, deletion guards stated |
| | **Total** | 100 | | **95** | |

Verdict: SHIP (95). Cat 4 half-credit honest. `status: draft` until owning designs `current`.

### Iter-2 2026-05-13 — Performance & quality

CEL placement idiomatic; RBAC markers cover finalizers verb; SSA fieldOwner strings match kind; test names import-ready. `referencingWorkspaceCount` guard wording matches finalizer semantics. No new gaps. Score unchanged 95.

### Iter-3 2026-05-13 — Operational readiness

Finalizer deletion order (tuples → backend → CR) consistent across all five CRDs. `IngestionRun` no-finalizer rationale explicit (ephemeral; owner-ref GC). Streaming Deployment teardown via `DocumentSource` finalizer — no orphan. `regression_lock: false` until first production rollout. Score unchanged 95.

Verdict: SHIP (95 ≥ 90). `status: draft` flips to `current` once designs 28/28b/28c confirmed `current` (done this session). Residual (P8): check script + sample dry-run CI anchor close Cat 4 to 1.0.
