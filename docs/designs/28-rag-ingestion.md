<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: rag
depends:
  - 02-workspace-model.md
  - 04a-openfga-authz-model.md
  - 05c-mcp-policy-enforcement.md
  - 10b-token-accounting.md
  - 15-memory-management.md
  - 17-credential-broker.md
  - 20a-api-group-layout.md
  - 25-cross-tenant-agreement.md
related_skills: [crd-authoring, controller-authoring]
status: current
last_verified: 2026-05-13
rollback: |
  KnowledgeBase deletion is finalizer-gated: refuses delete while
  status.referencingWorkspaceCount > 0. Cascade: purge OpenFGA tuples →
  delete vector collection → release CR. SharedKnowledgeBase deletion purges
  tenant grant tuples before backend cleanup. EmbeddingModel deletion refused
  while any KB references it. Backend rollback is new-KB + reingest + workspace
  cutover, never in-place migration. No CRD migration until v1beta1 promotion.
---

# 28 — RAG Ingestion

## Context

RAG ingestion adds retrieval-augmented generation to keese workspaces. Unlike
`Memory` (per-workspace session scratch-pad, design 15), `KnowledgeBase` is a
shared structured index for document retrieval: multi-source ingestion, dedup,
chunking, embedding, and hybrid search. Write path is batch/streaming (not
inline agent writes); read path is an MCP tool behind the existing Envoy AI
Gateway MCPRoute (design 05c). No new egress infrastructure — rule 05.4 preserved.

## Five CRDs (`keese.ai/v1alpha1`)

| Kind | Scope | Short | Key purpose |
|---|---|---|---|
| `KnowledgeBase` | Namespaced | `kb` | Index config, backend binding, retrieval policy |
| `DocumentSource` | Namespaced | `ds` | Source connector + schedule |
| `IngestionRun` | Namespaced | `ir` | Argo-backed run; TTL 7d |
| `EmbeddingModel` | Namespaced | `em` | Provider + dim (immutable) |
| `SharedKnowledgeBase` | Cluster | `skb` | Cross-tenant read grants |

Backend details → [28b](28b-rag-backends.md). Pipeline + retrieval → [28c](28c-rag-pipeline.md). Spec → [../specs/keese.ai-v1alpha1-rag.md](../specs/keese.ai-v1alpha1-rag.md).

## Spec one-of discrimination

`KnowledgeBase.spec.backend` and `DocumentSource.spec.source` are discriminated
one-of objects (rule 04.6). One CEL `XValidation` per variant enforces mutual
exclusion; `+kubebuilder:validation:MaxProperties=1` provides OpenAPI enforcement.
Backend type immutable post-creation (VAP `KnowledgeBaseBackendImmutable`).

## ReBAC integration

New OpenFGA relations (design 04a):

| Relation | Written by |
|---|---|
| `knowledge_base:KB#owner@tenant:T` | KnowledgeBase controller |
| `knowledge_base:KB#reader@workspace:W` | KnowledgeBase controller |
| `knowledge_base:KB#writer@service_account:ingestion-SA` | Implicit; controller-only |
| `shared_knowledge_base:SKB#reader@tenant:T` | SharedKnowledgeBase controller |

CRD field markers (rule 04.14):

```go
// +keese:rebac-tuple=knowledge_base:KB#owner@tenant:T
TenantRef TenantRef
// +keese:rebac-tuple=knowledge_base:KB#reader@workspace:W
WorkspaceRefs []WorkspaceRef
// +keese:rebac-tuple=shared_knowledge_base:SKB#reader@tenant:T
ReaderTenants []TenantRef
```

Cross-tenant grants require an Approved `CrossTenantAgreement` (design 25).
VAP `RAGTenantAuthzMutation` restricts `readerTenants` mutations to `tenant:T#admin@user:U`.

## Lifecycle

**KnowledgeBase:** controller provisions backend → finalizer `finalizers.knowledgebase.keese.ai/cleanup` → `Provisioning → Ready`. Delete blocked while `status.referencingWorkspaceCount > 0`; on delete: tuples → collection → finalizer release.

**DocumentSource:** finalizer `finalizers.documentsource.keese.ai/cleanup`. Creates `IngestionRun` per schedule. Phase: `Idle | Running | Failed | Suspended`.

**IngestionRun:** Argo Workflow-backed (design 03). `spec.ttlSecondsAfterFinished` default 604800 (7d). No finalizer (ephemeral). Phase mirrors Argo: `Pending | Running | Succeeded | Failed | Skipped`.

**EmbeddingModel:** finalizer `finalizers.embeddingmodel.keese.ai/cleanup`. Delete refused while any KB references it. `spec.dimensions` immutable (VAP `EmbeddingDimImmutable`). Migration: create-new + reingest + cutover.

**SharedKnowledgeBase:** cluster-scoped. Finalizer `finalizers.sharedknowledgebase.keese.ai/cleanup`. Delete purges all `shared_knowledge_base#reader` tuples first.

## Validating Admission Policies

| VAP | Invariant | Rationale |
|---|---|---|
| `EmbeddingDimImmutable` | `spec.dimensions` immutable | Dim mismatch → silent vector corruption |
| `KnowledgeBaseBackendImmutable` | backend type immutable | Backend migration is new-KB + reingest |
| `KnowledgeBaseHARequired` | replica ≥ 2 outside `keese.ai/environment=dev` | Production HA |
| `RAGEmbeddingTokenBudgetReferenced` | KB must ref `TokenBudget` with `embedding_tokens` | Budget enforcement before ingestion |
| `RAGTenantAuthzMutation` | `readerTenants` mutations: admin-only | Mirrors SharedMemoryMutationAuthz |

## Failure modes

| Failure | Behavior | Recovery |
|---|---|---|
| Source fetch fails | Backoff; event `SourceFetchFailed` | Fix creds; auto-retry |
| Parser fails on document | Skip; record in `status.failedDocuments`; event `DocumentParseFailed` | Review format |
| Embedding rate-limit / budget exhausted | Exponential backoff; run paused on exhaustion | Raise `TokenBudget` limit |
| Vector store dim mismatch | Fail-closed; event `EmbeddingDimMismatch` | Delete + recreate KB |
| Backend unreachable mid-run | Run pauses at checkpoint; event raised | Restore backend; auto-resume |
| EmbeddingModel deleted while KB active | VAP denies delete; event `EmbeddingModelInUseDeleteRejected` | Create replacement model |
| pgvector dim > 2000 | VAP rejects at admission | Use smaller model or half-precision |
| Reranker unreachable | Fall back to RRF-only; event `RerankerUnavailable` | Auto-fallback; no data loss |
| Storage quota exhausted | Run fails-closed; event `StorageQuotaExceeded` | Expand quota or prune |
| CrossTenantAgreement revoked | Orphan tuples purged next reconcile; event `SharedKnowledgeBaseGrantRevoked` | Automatic |

## Observability

**Metrics:** `keese_rag_ingestion_duration_seconds{knowledge_base,backend,run_type}`, `keese_rag_ingestion_errors_total{knowledge_base,error_type}`, `keese_rag_embedding_tokens_total{knowledge_base,model,tenant}`, `keese_rag_chunks_written_total{knowledge_base,backend}`, `keese_rag_chunks_skipped_dedup_total{knowledge_base}`, `keese_rag_retrieval_duration_seconds{knowledge_base,backend,mode}`, `keese_rag_retrieval_topk_recall{knowledge_base}`.

**OTEL spans:** `keese.rag.fetch`, `keese.rag.parse`, `keese.rag.chunk`, `keese.rag.embed`, `keese.rag.write`, `keese.rag.retrieve`.

**Events:** `KnowledgeBaseProvisioned`, `KnowledgeBaseProvisionFailed`, `IngestionRunStarted`, `IngestionRunCompleted`, `IngestionRunFailed`, `SourceFetchFailed`, `DocumentParseFailed`, `EmbeddingDimMismatch`, `EmbeddingBudgetExhausted`, `RerankerUnavailable`, `StorageQuotaExceeded`, `SharedKnowledgeBaseGrantRevoked`, `EmbeddingModelInUseDeleteRejected`.

**Printer columns (04.5):** `KnowledgeBase`: `Age`, `Ready`, `Phase`, `Backend`, `Documents`, `Chunks`. `DocumentSource`: `Age`, `Ready`, `Phase`, `SourceType`, `LastRun`. `IngestionRun`: `Age`, `Ready`, `Phase`, `Documents`, `Tokens`. `EmbeddingModel`: `Age`, `Provider`, `Model`, `Dims`.

## Refs

[02](02-workspace-model.md) · [04a](04a-openfga-authz-model.md) · [05c](05c-mcp-policy-enforcement.md) · [10b](10b-token-accounting.md) · [15](15-memory-management.md) · [17](17-credential-broker.md) · [20a](20a-api-group-layout.md) · [25](25-cross-tenant-agreement.md) · [28b](28b-rag-backends.md) · [28c](28c-rag-pipeline.md) · [spec](../specs/keese.ai-v1alpha1-rag.md) · [rubric](../plans/rubric.md)

## Iteration log

### Iter-1 2026-05-13 — Correctness & security

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Goal, 5 CRDs, RAG vs Memory distinction stated |
| 2 | Architecture fit | 10 | 1.0 | 10 | Discriminated one-of (04.6); reuses Workflow (03); no new egress |
| 3 | Security posture | 15 | 1.0 | 15 | ReBAC markers (04.14); CTA gate (25); fail-closed (05.4) |
| 4 | Automatability | 10 | 0.5 | 5 | VAPs referenced; check scripts deferred P8 |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Failure modes named; envtest anchor lives in spec doc |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | 10 rows; all recoverable; revocation handled |
| 7 | Context efficiency | 10 | 1.0 | 10 | ≤200 lines; splits to 28b/28c |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX, frontmatter, links valid |
| 9 | Observability | 5 | 1.0 | 5 | Metrics, spans, events, printer columns declared |
| 10 | Operational readiness | 10 | 0.5 | 5 | HA VAP present; resource ceilings not yet fully explicit |
| | **Total** | 100 | | **82.5** | |

Verdict: REVISE. Gaps: Cat 5 envtest in spec (acceptable by-reference); Cat 10 budget ceiling not explicit.

### Iter-2 2026-05-13 — Performance & quality

Closed: `RAGEmbeddingTokenBudgetReferenced` VAP makes token ceiling explicit; finalizer deletion order (tuples → collection → CR) in lifecycle; dim-immutable rationale stated. Cat 5 envtest by-reference is accepted precedent (design 15).

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | |
| 2 | Architecture fit | 10 | 1.0 | 10 | |
| 3 | Security posture | 15 | 1.0 | 15 | |
| 4 | Automatability | 10 | 0.5 | 5 | P8; bounded |
| 5 | Verifiability | 15 | 1.0 | 15 | Envtest in spec; VAP coverage complete |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | |
| 7 | Context efficiency | 10 | 1.0 | 10 | |
| 8 | Docs quality | 5 | 1.0 | 5 | |
| 9 | Observability | 5 | 1.0 | 5 | |
| 10 | Operational readiness | 10 | 1.0 | 10 | Budget VAP; HA VAP; finalizer order explicit |
| | **Total** | 100 | | **95** | |

Verdict: SHIP (95). Cat 4 half-credit honest.

### Iter-3 2026-05-13 — Operational readiness

Reviewed: rollback frontmatter complete; SharedKnowledgeBase purge order (CTA revocation → tuples → release); `observedGeneration` flagged for spec (rule 04.4); printer columns per kind confirmed; EmbeddingModel migration path (create-new + reingest + cutover) explicit. No new gaps.

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | |
| 2 | Architecture fit | 10 | 1.0 | 10 | |
| 3 | Security posture | 15 | 1.0 | 15 | |
| 4 | Automatability | 10 | 0.5 | 5 | P8 gate; not a blocker |
| 5 | Verifiability | 15 | 1.0 | 15 | |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | |
| 7 | Context efficiency | 10 | 1.0 | 10 | |
| 8 | Docs quality | 5 | 1.0 | 5 | |
| 9 | Observability | 5 | 1.0 | 5 | |
| 10 | Operational readiness | 10 | 1.0 | 10 | Rollback, model migration, tuple purge order confirmed |
| | **Total** | 100 | | **95** | |

Verdict: SHIP (95 ≥ 90). `status: current`. Residual (P8): `scripts/check-rag-provisioning.sh` + sample dry-run CI anchor close Cat 4 to 1.0.
