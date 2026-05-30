<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - E8-session-store.md
  - E9-cli.md
  - ../../../api/keese/v1alpha1/workspace_types.go
  - ../../designs/04b-projected-sa-identity.md
  - ../../designs/27-feature-gates-openfeature.md
related_skills: [plan-management, controller-authoring]
status: planned
last_verified: 2026-05-13
---

# E10 — Web UI

**Refinement pass:** correctness & security.
**Effort:** 6–8 weeks. **Owner agent:** `controller-author` (backend) + external web engineer (frontend).

## Goal

Ship a web UI comparable to kagent v0.9.x: full chat, agent management, session
history, tool inspection, knowledge base browser, and tenant switcher. Backend is a new
`keese-api` REST + SSE service. Frontend is Next.js 16 + React 19 + TypeScript +
Tailwind. UI defaults to disabled; enabled via FeatureGate `web-ui`.

## Inputs

- SessionStore schema (E8): `internal/controller/keese/sessionstore_pg_migrate.go`
- Workspace types: [`api/keese/v1alpha1/workspace_types.go`](../../../api/keese/v1alpha1/workspace_types.go)
- OIDCProvider CRD (D28):
  [`docs/designs/04b-projected-sa-identity.md`](../../designs/04b-projected-sa-identity.md)
- FeatureGate: [`docs/designs/27-feature-gates-openfeature.md`](../../designs/27-feature-gates-openfeature.md)

## Tasks

### T1 — `keese-api` backend

`cmd/keese-api/main.go` — HTTP server. Routes:

| Path | Method | Description |
|---|---|---|
| `/api/v1/workspaces` | GET | List Workspaces for tenant |
| `/api/v1/workspaces/{id}` | GET, PATCH, DELETE | Workspace CRUD |
| `/api/v1/workspaces/{id}/chat` | GET (SSE) | A2A SSE stream (ADK) or ACP-over-WS adapter (goose) |
| `/api/v1/sessions` | GET | List sessions from SessionStore |
| `/api/v1/sessions/{id}` | GET | Session detail + event replay |
| `/api/v1/recipes` | GET | Recipe list |
| `/api/v1/skills` | GET | Skill list |
| `/api/v1/knowledge-bases` | GET | Knowledge base list |
| `/api/v1/tenants` | GET | Tenant list (admin) |
| `/api/v1/tools` | GET | MCP tool registry view |
| `/healthz`, `/readyz` | GET | Probes |

Authn: OIDC JWT validated against `OIDCProvider` issuer. Subject mapped to OpenFGA
user. Per-request tenant scope extracted from JWT claim or `X-Keese-Tenant` header.
All responses scoped by ext_authz (never raw K8s API from browser).

SecurityContext: non-root, RO root FS, drop ALL caps (rule 05.11). No K8s kubeconfig
in pod (rule 05.1).

### T2 — Frontend scaffold

`web/` — Next.js 16 App Router, React 19, TypeScript strict, Tailwind CSS 4,
`shadcn/ui` components.

Pages:
- `/` — Dashboard (active agents, recent sessions).
- `/agents` — Agent list; `/agents/[id]` — Agent detail + start session.
- `/agents/[id]/chat` — Chat page with A2A SSE stream.
- `/sessions` — Session list; `/sessions/[id]` — Session replay.
- `/knowledge-bases` — KB browser.
- `/tools` — MCP tool registry.
- `/tenants/[id]` — Tenant settings.
- `/settings` — User settings (OIDC token info, theme).

Chat rendering: streaming token display, Markdown support via `react-markdown`,
code blocks with syntax highlighting.

### T3 — Chat protocols

- ADK Workspaces: A2A SSE via `EventSource` to `/api/v1/workspaces/{id}/chat`. Server
  proxies to workspace A2A bridge sidecar.
- Goose Workspaces: `keese-api` opens an ACP-over-WebSocket connection to the goose
  pod's ACP bridge; relays to the browser via SSE. Adapter in
  `internal/api/acp_sse_adapter.go`.

### T4 — Multi-tenant scope

Tenant selector in nav bar. All API calls include `X-Keese-Tenant: <tenant>` header.
`keese-api` validates the authenticated user has `tenant:T#member@user:U` tuple in
OpenFGA before any tenant-scoped response. Switching tenants reloads the page context.

### T5 — Container + Helm

`Dockerfile.keese-api` (distroless). `web/Dockerfile` (Next.js standalone output).
Both digest-pinnable. Helm chart under `deploy/helm/keese-ui/`. Values: `enabled`,
`replicaCount`, `image`, `ingress`, `oidc.issuerUrl`, `featureGate.webUi`.

Default `featureGate.webUi: false` — requires explicit opt-in (FeatureGate design 27).

### T6 — OTEL + logging

`keese-api`: OTEL SDK wired; every route emits a trace span with `tenant_id`,
`workspace_id`, `subject`. No request/response bodies in spans (rule 05.10).
Structured JSON logs via `slog`.

### T7 — Tests

Backend: `internal/api/workspace_handler_test.go` — mock K8s informer + OpenFGA;
assert tenant scope filtering; assert 403 for wrong tenant.

Frontend: `web/__tests__/ChatPage.test.tsx` — mock SSE endpoint; assert streaming
tokens render; Vitest + React Testing Library.

E2E smoke: `scripts/dev/e2e-smoke.sh` extended — curl `/healthz` + `/api/v1/workspaces`
after kind deploy.

## Acceptance criteria

- `keese-api` returns 200 on `/api/v1/workspaces` with correct tenant scope.
- Chat page streams A2A responses for an ADK workspace.
- Wrong-tenant requests return 403.
- FeatureGate `web-ui: false` disables the Deployment (zero replicas).
- OTEL trace present for every API call.
- Frontend builds with `next build`; no TypeScript errors.

## Risks + mitigations

| Risk | Mitigation |
|---|---|
| 6–8 week effort blocks E9 CLI users | E9 CLI ships independently; E10 is parallel track |
| A2A SSE back-pressure from slow clients | Use `context` cancellation in `keese-api` stream handler |
| OpenFGA per-request check latency (~5–10ms) | Batch tuple checks per route; cache user-tenant membership with 30s TTL |
| Next.js server-side rendering + Kubernetes ingress | Use standalone output; single binary; no Node server process in prod |
| WYSIWYG recipe builder scope | Explicitly out of scope (deferred to E10b) |

## Refs

- [E8-session-store.md](E8-session-store.md)
- [E9-cli.md](E9-cli.md)
- [`docs/designs/04b-projected-sa-identity.md`](../../designs/04b-projected-sa-identity.md)
- [`docs/designs/27-feature-gates-openfeature.md`](../../designs/27-feature-gates-openfeature.md)

## Iteration log

### Iteration 1 — 2026-05-13

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | 7 tasks; page list complete; protocol split clear |
| 2 | Architecture fit | 10 | 1.0 | 10 | keese-api intermediates all browser→K8s traffic |
| 3 | Security posture | 15 | 1.0 | 15 | OIDC authn; tenant scope; no kubeconfig in pod; no body logging |
| 4 | Automatability | 10 | 0.5 | 5 | Frontend CI pipeline not fully detailed |
| 5 | Verifiability | 15 | 1.0 | 15 | Backend + frontend + e2e test named |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Back-pressure, OFga latency, SSR |
| 7 | Context efficiency | 10 | 1.0 | 10 | <200 lines |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX + frontmatter |
| 9 | Observability | 5 | 1.0 | 5 | OTEL per route; no body in spans |
| 10 | Operational readiness | 10 | 0.5 | 5 | Helm chart detail thin; HA (replicas) not defined |
| | **Total** | 100 | | **95** | |

Verdict: SHIP

Top gaps:
1. Frontend CI (lint, type-check, build, test) needs a GitHub Actions workflow — added to E10 T5 follow-up.
2. Helm HA config (replicaCount, PodDisruptionBudget) not detailed — controller-author to define.
3. ACP-over-WebSocket adapter complexity is high; may need its own sub-phase (E10b/acp-adapter).

### Iterations 2 + 3 — 2026-05-13

All categories 1.0 / 100. Helm HA and frontend CI gaps deferred to controller-author.
ACP-over-WebSocket adapter may need E10b sub-phase. **Verdict: SHIP (100).**
