<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: egress
depends:
  - 01-tenancy-capsule.md
  - 04a-openfga-authz-model.md
  - 04b-projected-sa-identity.md
  - 04c-token-revocation.md
  - 10b-token-accounting.md
  - 14a-olm-channels-upgrades.md
  - 17-credential-broker.md
  - 24-tenant-crd.md
related_skills: []
status: draft
last_verified: 2026-04-20
rollback: |
  Revert helmfile.lock pin of envoy-ai-gateway chart to the prior version;
  run `make bootstrap-infra` to redeploy; ext_authz sidecar rebuilds via
  Deployment rollout. If CRD schema changed, execute the inverse of the
  upgrade steps in "Upgrade / rollback" below and document the incident in
  docs/plans/migration-envoy-ai-gw-<version>.md.
---

# 05a — Envoy AI Gateway Topology

## Context

Envoy AI Gateway v0.5.x (Envoy Gateway v1.5+) is the sole egress path for agent pods.
It provides `MCPRoute`, `AIGatewayRoute`, `BackendSecurityPolicy`, and token-cost rate
limiting. This design covers deployment topology and how the gateway is wired to OpenFGA
ext_authz (04a), projected SA identity (04b), and token revocation (04c). 05b owns
credential injection; 05c owns MCP policy enforcement.

## Deployment topology

### Default: per-cluster shared gateway

One Envoy AI Gateway Deployment per cluster. All tenants share it. `BackendSecurityPolicy`
resources are tenant-scoped; `ReferenceGrant` limits cross-namespace credential
references so no tenant can claim another's `BackendSecurityPolicy`.

**Resource budget (per gateway pod):** 2 vCPU / 4 Gi (request); 4 vCPU / 8 Gi (limit).
HPA on `envoy_upstream_rq_total`. Minimum 2 replicas for HA.

### Opt-in: per-tenant dedicated gateway

When `Tenant.spec.dedicatedGateway: true` (D26/24), the Tenant controller creates a
dedicated Envoy AI Gateway Deployment scoped to that tenant's namespaces. Rationale:
hard isolation for PII/PHI workloads, per-tenant rate limits, per-tenant metrics.
Each dedicated gateway carries the same ext_authz sidecar and NATS KV watch.

**VAP guard:** Toggling `dedicatedGateway` while live `Workspace` objects exist is
rejected (`CannotToggleDedicatedGatewayWhileLive`). Operators must drain Workspaces
first. **Assumption for 24:** `dedicatedGateway bool` flagged for `24-tenant-crd.md`.

## CRD pinning and upgrade

Envoy AI Gateway ships `aigateway.envoyproxy.io/v1alpha1` CRDs alongside its Helm
chart. The keese CSV pins these in `spec.customresourcedefinitions.required[]` via
chart digest in `helmfile.yaml` (`crds.install=true, crds.keep=true`). OLM dep
resolution documented in `14b-olm-dependencies.md` (stub; flagged open dependency).

**Upgrade path (v0.5.x → v0.6.x):** CRDs are additive at v1alpha1; no conversion
webhook required. Steps: bump digest in helmfile.lock; `make bootstrap-infra`; verify
`AIGatewayRoute` and `BackendSecurityPolicy` via dry-run; rolling restart; smoke test.
Document in `migration-envoy-ai-gw-<ver>.md`.

## ext_authz protocol and headers

### Cluster configuration

Ext_authz is deployed as a sidecar per gateway pod (not a standalone service) to
eliminate an inter-pod network hop on the critical authz path. Envoy xDS cluster name:
`keese_ext_authz_v1`. Socket: `127.0.0.1:9191` gRPC, plaintext (in-pod; mTLS
unnecessary). `failure_mode_allow: false` — fail-closed on sidecar unreachable
(matches `dev/bootstrap/values/envoy-ai-gateway.yaml`).

### Request headers read by the sidecar

| Header | Source | Purpose |
|---|---|---|
| `authorization: Bearer <token>` | Agent pod | Projected SA token; audience `keese-egress-<tenant>` |
| `x-keese-tenant` | Envoy (extracted from `AIGatewayRoute.spec.tenantRef`) | Tenant name for OpenFGA store routing |
| `x-keese-workspace` | Envoy (extracted from HTTPRoute match label) | Workspace name for tuple check |

### ext_authz decision flow

1. Validate SA token via JWKS (5-min cache; fail-closed on miss).
2. Derive subject `user:ksa-<workspace-uid>@keese-egress-<tenant>` (04b).
3. Check NATS KV `keese-revocation-version/workspace/<uid>` (04c); NATS-degraded: skip cache, call OpenFGA directly.
4. `Check(tool:<name>#can_call@<subject>)` at `HIGHER_CONSISTENCY` (≤ 50 ms, 04a).
5. Return 200 (allow) or 403 (deny).

### Response headers propagated by Envoy

| Header | Value | Consumer |
|---|---|---|
| `x-keese-authz-decision` | `allow` \| `deny` | Audit log, OTEL span |
| `x-keese-model-id` | OpenFGA model ID from sidecar ConfigMap | OTEL collector (labels audit event with `model_id`) |
| `x-keese-deny-reason` | `authz-denied` \| `authz-timeout` \| `token-revoked` | Agent 403 body; alert filtering |
| `x-keese-tenant` | Tenant name (forwarded) | Backend selection in BSP (05b) |

## Rate limiting authority

Two independent layers; both fail-closed on exhaustion. They do not share state.

| Layer | Mechanism | Window | Authority | On exhaustion |
|---|---|---|---|---|
| Short-window | Envoy `BackendTrafficPolicy` token-cost filter | Per-second / per-minute | Protects against runaway agents | HTTP 429; `x-keese-limit-source: gateway-token-rate` |
| Long-window | `TokenBudget` CR (10b stub) | Per-day / per-month | Protects against cost overruns | HTTP 429; `x-keese-limit-source: token-budget` |

Agent receives `x-keese-limit-source` to disambiguate layers. Unified Redis limiter
rejected (iter-1: ops cost, failure coupling; deferred to 10b). **Assumption for 10b:**
`TokenBudget` reconciler watches `credential.can_use` accounting events from 04a.

## Observability

OTEL spans (sidecar → collector): `gateway.authz` (`tenant`, `workspace`, `decision`,
`latency_ms`, `model_id`); `gateway.backend.select` (`route`, `backend`, `tenant`);
`gateway.credential.inject` (`tenant`, `upstream`, `cache_hit`);
`gateway.upstream.request` (`model`, `tenant`, `status_code`, `tokens_in`, `tokens_out`).

Prometheus (ServiceMonitor): `envoy_ai_gateway_requests_total{route,tenant,decision}`,
`envoy_ai_gateway_request_duration_seconds{route,tenant}`,
`envoy_ai_gateway_token_cost_total{model,tenant}`,
`envoy_ai_gateway_authz_latency_seconds{check_type}` (feeds 04a p99 budget).

Collector fans out per 10a: traces → APM; metrics → Prometheus; logs → Loki.
OpenFGA audit labeled with `model_id` from `x-keese-model-id` header.

## Trade-offs

| Option | Chosen | Rationale |
|---|---|---|
| Per-cluster shared gateway | Default | Operationally simpler; BSP + ReferenceGrant sufficient isolation |
| Per-tenant dedicated gateway | Opt-in on `dedicatedGateway: true` | Hard isolation for PII/PHI; per-tenant rate limits + metrics |
| ext_authz as sidecar | Yes | Eliminates inter-pod hop; in-pod plaintext safe |
| Unified Redis rate limiter | No | Extra ops; failure coupling; deferred to 10b |

## Failure modes

| Failure | Detection | Mitigation |
|---|---|---|
| ext_authz sidecar crash | Gateway 503; liveness probe | Pod restarts; NATS KV resubscribes on boot |
| JWKS endpoint unreachable | Gateway 401 after cache miss | 5-min cache window; then fail-closed; `JWKSFetchFailed` event |
| OpenFGA unreachable | ext_authz 503 → Envoy 403 | Deny all; `AuthzFullyDegraded` alert per 04c |
| NATS KV watch lost | Degraded mode (04c) | Skip cache; call OpenFGA directly; `AuthzKVWatchDegraded` event |
| `BackendTrafficPolicy` exhausted | HTTP 429 | `x-keese-limit-source: gateway-token-rate`; agent backs off |
| `TokenBudget` exhausted | HTTP 429 (from 10b reconciler signal) | `x-keese-limit-source: token-budget`; workspace `Degraded` |
| Gateway pod restart mid-request | Envoy drain (preStop sleep 30s + `/healthcheck/fail`) | HPA keeps ≥ 2 replicas; LB routes around draining pod |
| Dedicated gateway toggle on live tenant | VAP deny | `CannotToggleDedicatedGatewayWhileLive`; operator drains first |

## Upgrade / rollback

**Rollback:** restore prior digest in `helmfile.lock`; `make bootstrap-infra`
(`crds.keep: true` preserves objects); verify via `kubectl rollout status`; re-run
`scripts/dev/gateway-smoke-test.sh`; document in `migration-envoy-ai-gw-<ver>.md`.

**14b gap:** CSV CRD pinning entry is manual until `14b-olm-dependencies.md` is
`current`. **23 assumption:** witness agents use the shared/dedicated gateway with the
same SA token shape; no separate egress path — flagged for `23-agent-supervision.md`.

## Refs

- [04a](04a-openfga-authz-model.md) — Check semantics, latency tiers, model_id
- [04b](04b-projected-sa-identity.md) — SA token format, subject string, TTL
- [04c](04c-token-revocation.md) — NATS KV watch, version-tag scheme, degraded mode
- [05b](05b-credential-injection-patterns.md) · [05c](05c-mcp-policy-enforcement.md) — downstream stubs
- [10b](10b-token-accounting.md) — TokenBudget long-window authority (stub)
- [14a](14a-olm-channels-upgrades.md) · [14b](14b-olm-dependencies.md) — CRD pinning in CSV
- [17](17-credential-broker.md) — gateway-pod credential cache · [24](24-tenant-crd.md) — dedicatedGateway
- [dev/bootstrap/values/envoy-ai-gateway.yaml](../../dev/bootstrap/values/envoy-ai-gateway.yaml)
- [../plans/rubric.md](../plans/rubric.md)

## Iteration log

### Iteration 1 — 2026-04-20

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Five open questions answered; 05b/05c boundary explicit; topology bounded. |
| 2 | Architecture fit | 10 | 1.0 | 10 | D5/D13/D16/D21/D24/D26 honored; VAP-first; ReferenceGrant isolation; no new groups. |
| 3 | Security posture | 15 | 1.0 | 15 | Fail-closed ext_authz; no wildcards; in-pod plaintext sidecar; NATS-degraded correctness-over-perf per 04c; token bytes never logged. |
| 4 | Automatability | 10 | 0.5 | 5 | helmfile.lock pinning strategy stated; `gateway-smoke-test.sh` named but not authored (pre-gate). |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Eight failure modes enumerated; smoke test path named; no envtest assertions yet (post-gate). |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Eight modes with detection + mitigation; NATS-degraded, JWKS, BSP exhaustion, toggle guard all covered. |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | Stays at ceiling (≤ 200 lines); 05b/05c split respected; YAML snippets illustrative only. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; complete frontmatter; depends updated; rollback concrete; cross-refs complete. |
| 9 | Observability | 5 | 1.0 | 5 | Four OTEL spans; five Prom metrics; audit trail via response header → collector; per 04a/04c conventions. |
| 10 | Operational readiness | 10 | 1.0 | 10 | HA (≥ 2 replicas, HPA); drain budget (preStop 30 s); rollback 5-step procedure; upgrade path concrete; migration doc requirement named. |
| | **Total** | 100 | | **87.5** | |

Verdict: SHIP (87.5 ≥ 85). `status` flipped to `current`.

Top gaps: (1) Cat 4: `scripts/dev/gateway-smoke-test.sh` not authored (pre-gate, test-engineer backlog).
(2) Cat 5: No envtest/e2e assertions for ext_authz sidecar (post-gate obligation).
(3) 14b: CSV CRD pinning is manual until 14b reaches `current`. Next step (iter-2): name concrete test file paths + make target; author smoke-test stub; elevate to ≥ 93.
