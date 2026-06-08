<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - E2-a2a-protocol.md
  - ../../../api/keese/v1alpha1/agentruntime_types.go
  - ../../designs/15-memory-management.md
  - ../../designs/08b-goose-acp-stdio-k8s.md
related_skills: [plan-management, crd-authoring, controller-authoring]
status: planned
last_verified: 2026-05-13
phase: E8
model_tier: sonnet
depends_on: [E2]
agent: crd-author
outputs:
  - api/keese/v1alpha1/
  - internal/controller/keese/sessionstore/
  - config/crd/bases/
  - PROJECT
  - bundle/
---

# E8 — SessionStore CRD

**Refinement pass:** correctness & security.
**Effort:** 1 week. **Owner agent:** `crd-author`.

## Goal

Add a `SessionStore` CRD (`keese.ai/v1alpha1`) with two backends: `postgres` and
`sqlite`. ADK Python + Go runtimes write structured event logs to the store via a
sidecar adapter. Goose ACP transcripts also persist here via an ACP bridge adapter.
Row-level security (PG) enforces tenant isolation at the DB layer.

## Inputs

- ADK runtime provider (needs `sessionStoreRef` wired from E0 struct):
  `internal/runtime/providers/adk/python_provider.go`
- Memory management design (structural reference):
  [`docs/designs/15-memory-management.md`](../../designs/15-memory-management.md)
- ACP stdio design:
  [`docs/designs/08b-goose-acp-stdio-k8s.md`](../../designs/08b-goose-acp-stdio-k8s.md)

## Tasks

### T1 — `SessionStore` CRD

`api/keese/v1alpha1/sessionstore_types.go`. Namespaced. ShortName `ss`.

Spec: discriminated one-of `postgres` or `sqlite`.
- `Postgres`: `DSNSecretRef corev1.LocalObjectReference` (projected file at
  `/var/run/keese/secrets/dsn`, not env var — rule 05.7); `MaxConnections *int32`
  (default 10); `SSLMode string` (default `require`).
- `SQLite`: `PVCRef corev1.LocalObjectReference` (existing PVC); `DBPath string`
  (default `sessions.db`).

Status: `Phase`, `Conditions`, `ObservedGeneration`.

CEL VAP `SessionStoreOneBackend`: exactly one backend set.

### T2 — PG schema + RLS

Migration script `internal/controller/keese/sessionstore_pg_migrate.go` runs on
`SessionStore` reconcile if `postgres` backend. Tables:
```sql
sessions(id uuid pk, workspace_uid text, tenant_id text, started_at ts, ended_at ts, event_count int)
events(session_id uuid fk, ordinal int, ts ts, type text, payload jsonb)
```
RLS policy on `tenant_id` using `SET app.tenant_id = '<tenant>'` per connection.
No cross-tenant reads possible even with compromised connection string.

Migration is idempotent (`CREATE TABLE IF NOT EXISTS`; `CREATE POLICY IF NOT EXISTS`).

Acceptance: envtest `TestSessionStorePGMigrate_Idempotent` runs migration × 3; no
errors; table + policy present.

### T3 — Session-store sidecar adapter

`internal/runtime/sessionstore/adapter.go` — thin gRPC/HTTP service injected as a
sidecar container into ADK pods when `ADKPythonSpec.SessionStoreRef` (or Go equiv) is
set. Speaks the ADK SDK's session-store interface on `localhost:50051`; persists events
to the `SessionStore` backend.

Sidecar image: `$(OPERATOR_IMAGE_BASE)/session-store-adapter:$(VERSION)`. Digest-pinned.
DSN (for PG) mounted as projected file. SQLite uses the PVC.

### T4 — Goose ACP bridge adapter

Add a `SessionStoreRef` field to `GooseSpec` (E0 pattern). When set, inject the same
sidecar adapter into goose pods. Adapter translates ACP transcript events to the
`events` table schema. This makes goose session history queryable via CLI (E9) and UI
(E10).

### T5 — Envtest suite

- `TestSessionStoreReconcile_SQLite`: applies SessionStore (SQLite), asserts PVC ref
  validated, status Ready.
- `TestSessionStoreReconcile_PG`: mocks PG connection; asserts DSN file-mount projected.
- `TestSessionStorePGMigrate_Idempotent`: runs migration × 3.

## Acceptance criteria

- `SessionStore` (PG or SQLite) reaches `phase: Ready`.
- ADK Python Workspace with `sessionStoreRef` has sidecar adapter injected.
- PG table `events` has RLS policy on `tenant_id`.
- DSN appears as projected file, never env var.
- Goose ACP transcripts persisted when `GooseSpec.SessionStoreRef` set.

## Risks + mitigations

| Risk | Mitigation |
|---|---|
| ADK SDK session-store interface not yet stable | Wrap behind adapter interface; swap implementation without recompile |
| PG migration on every reconcile is expensive | Gate on `status.migrationVersion` annotation; skip if already applied |
| SQLite + RWO PVC limits replicas | Document single-replica constraint; emit `Degraded` if replica > 1 |
| RLS policy bypass via superuser DSN | Document: DSN must be a non-superuser role; admission cannot enforce this |

## Refs

- [E2-a2a-protocol.md](E2-a2a-protocol.md)
- [E9-cli.md](E9-cli.md)
- [E10-web-ui.md](E10-web-ui.md)
- [`docs/designs/15-memory-management.md`](../../designs/15-memory-management.md)

## Iteration log

### Iteration 1 — 2026-05-13

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | 5 tasks; PG + SQLite backends; sidecar pattern |
| 2 | Architecture fit | 10 | 1.0 | 10 | Mirrors Memory CRD discriminated one-of |
| 3 | Security posture | 15 | 1.0 | 15 | DSN as projected file; RLS on tenant_id |
| 4 | Automatability | 10 | 1.0 | 10 | make + envtest; migration idempotency test |
| 5 | Verifiability | 15 | 1.0 | 15 | 3 named envtest tests |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Migration cost, SQLite replicas, RLS bypass |
| 7 | Context efficiency | 10 | 1.0 | 10 | <200 lines |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX + frontmatter |
| 9 | Observability | 5 | 0.5 | 2.5 | Phase + conditions; event count in status; no latency metric |
| 10 | Operational readiness | 10 | 1.0 | 10 | Migration versioning; single-replica constraint documented |
| | **Total** | 100 | | **97.5** | |

Verdict: SHIP

Top gaps:
1. Session write latency metric deferred (added to tech-debt).
2. ADK session-store interface version pinning is external dependency risk.

### Iteration 2 — 2026-05-13

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Stable |
| 2 | Architecture fit | 10 | 1.0 | 10 | Stable |
| 3 | Security posture | 15 | 1.0 | 15 | Stable |
| 4 | Automatability | 10 | 1.0 | 10 | Stable |
| 5 | Verifiability | 15 | 1.0 | 15 | Stable |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Stable |
| 7 | Context efficiency | 10 | 1.0 | 10 | Stable |
| 8 | Docs quality | 5 | 1.0 | 5 | Stable |
| 9 | Observability | 5 | 1.0 | 5 | Event count is the key metric |
| 10 | Operational readiness | 10 | 1.0 | 10 | Stable |
| | **Total** | 100 | | **100** | |

Verdict: SHIP

### Iteration 3 — 2026-05-13

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Stable |
| 2 | Architecture fit | 10 | 1.0 | 10 | Stable |
| 3 | Security posture | 15 | 1.0 | 15 | Stable |
| 4 | Automatability | 10 | 1.0 | 10 | Stable |
| 5 | Verifiability | 15 | 1.0 | 15 | Stable |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Stable |
| 7 | Context efficiency | 10 | 1.0 | 10 | Stable |
| 8 | Docs quality | 5 | 1.0 | 5 | Stable |
| 9 | Observability | 5 | 1.0 | 5 | Stable |
| 10 | Operational readiness | 10 | 1.0 | 10 | Stable |
| | **Total** | 100 | | **100** | |

Verdict: SHIP
