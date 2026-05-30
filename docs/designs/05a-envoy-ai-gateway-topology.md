<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: egress
depends:
  - 01-tenancy-capsule.md
  - 04a-openfga-authz-model.md
  - 04b-projected-sa-identity.md
  - 04b-ii-oidc-trust.md
  - 04c-token-revocation.md
  - 10b-token-accounting.md
  - 14a-olm-channels-upgrades.md
  - 17-credential-broker.md
  - 24-tenant-crd.md
related_skills: []
status: current
last_verified: 2026-04-20
rollback: |
  Revert helmfile.lock pin of envoy-ai-gateway chart to the prior version;
  run `make bootstrap-infra` to redeploy; keese-ext-authz Deployment rebuilds via
  rolling rollout. If CRD schema changed, execute the inverse of the upgrade steps
  and document the incident in docs/plans/migration-envoy-ai-gw-<version>.md.
---

# 05a — Envoy AI Gateway Topology

## Context

Envoy AI Gateway v0.5.x (Envoy Gateway v1.5+) is the sole egress path for agent pods.
It provides `MCPRoute`, `AIGatewayRoute`, `BackendSecurityPolicy`, and token-cost rate
limiting. This design covers: deployment topology, ext_authz Deployment architecture,
JWT tenant extraction, NATS KV budget signaling, and witness audience. 05b owns
credential injection; 05c owns MCP policy enforcement.
Iteration log: [05a-ii-iter-log.md](05a-ii-iter-log.md).
## Deployment topology

### Default: per-cluster shared gateway

One Envoy AI Gateway Deployment per cluster. All tenants share it. `BackendSecurityPolicy`
resources are tenant-scoped; `ReferenceGrant` limits cross-namespace credential references.

**Resource budget (per gateway pod):** 2 vCPU / 4 Gi request; 4 vCPU / 8 Gi limit.
HPA on `envoy_upstream_rq_total`. Minimum 2 replicas for HA.

### Opt-in: per-tenant dedicated gateway

When `Tenant.spec.dedicatedGateway: true` (D26/24), the Tenant controller provisions a
dedicated Envoy AI Gateway Deployment in the tenant namespace. Same chart values, separate
HPA, separate failure domain. Rationale: hard isolation for PII/PHI, per-tenant rate limits,
per-tenant metrics. Operator auto-provisions when `dedicatedGateway` flips to `true` and
`status.phase` is `Pending` or `Provisioning`.

## ext_authz — Deployment architecture

### Why not a sidecar

Iter-1 specified ext_authz as a container sidecar per Envoy pod (127.0.0.1:9191). Reviewer
rejected on scale grounds: 300+ containers at 100 dedicated tenants; N×M log streams;
upgrade coupling. Latency delta vs sidecar: +1–3 ms per authz decision. The 04a consistency
tiers (≤15/25/50 ms p99) absorb this budget. Fail-closed semantics are identical.

### Shared mode (default)

One `keese-ext-authz` Deployment in `keese-system`, 3 replicas, PDB `minAvailable: 2`,
HPA on CPU target 60%. Service: `keese-authz.keese-system.svc.cluster.local:9001`
(ClusterIP). All shared-gateway Envoy pods reference this Service via `envoy_grpc` cluster
`keese_ext_authz_v1`. Image: `ghcr.io/keese-ai/keese-ext-authz:<semver>` — keese-owned
adapter built from `cmd/ext-authz/`. Dependencies: `github.com/openfga/go-sdk`,
`github.com/nats-io/nats.go`, `go.opentelemetry.io/otel` — all pinned in `go.mod`.
Build: `Dockerfile.ext-authz`; signed via cosign keyless OIDC through P5 `image.yaml`.
Failure domain: shared-mode failure = cluster-wide egress blocked. HA (PDB + 3 replicas)
mitigates; rule 05 fail-closed is the correct behavior.

### Dedicated mode

Per-tenant `keese-ext-authz-<tenant>` Deployment in the tenant namespace, 2 replicas
minimum. Same image, separate Service, separate NATS KV watch, separate PDB.
Failure domain: per-tenant blast radius only.

## JWT Authn filter — tenant extraction

Envoy AI Gateway v0.5.x has no `AIGatewayRoute.spec.tenantRef` field (verified against
`aigateway.envoyproxy.io` CRD spec). Tenant identity is extracted from the agent's projected
SA token `aud` claim via Envoy's native JWT Authn filter, projected to dynamic metadata
and header `x-keese-tenant`. Zero keese-side Envoy build required.

```yaml
http_filters:
  - name: envoy.filters.http.jwt_authn
    config:
      providers:
        keese_sa:
          issuer: "https://kubernetes.default.svc.cluster.local"
          audiences: ["keese-egress-*"]
          payload_in_metadata: "keese.sa_token"
  - name: envoy.filters.http.ext_authz
    config:
      grpc_service: { envoy_grpc: { cluster_name: keese_ext_authz_v1 } }
      failure_mode_allow: false
      metadata_context_namespaces: ["keese.sa_token"]
  - name: envoy.filters.http.router
```

Ext_authz reads dynamic metadata `keese.sa_token.aud` → parses `keese-egress-<tenant>`
→ stamps header `x-keese-tenant` for downstream `BackendSecurityPolicy` selection (05b).

## JWKS cache fail-open window

Default: 300 s. Floor: 30 s (below this, kube-apiserver load dominates). Ceiling: 600 s.
Configurable via `Tenant.spec.jwksCacheFailOpenSeconds` — **flagged residual: 24 iter-2
must carry this field**. For `dedicatedGateway: true` tenants, the default drops to 60 s
automatically.

## TokenBudget 429 signaling via NATS KV

**NATS KV carries signals, not counters.** Consumption counters (tokens used per window)
live in Prometheus as OTEL metrics (10b). The `TokenBudget` controller scrapes Prometheus
at reconcile interval; when consumed ≥ limit, it writes a boolean flag to NATS JetStream
KV bucket `keese-budget-exceeded` under keys `tenant/<name>` or `workspace/<uid>`.

`keese-ext-authz` Deployment watches the same NATS KV bucket — reusing the infrastructure
already required for 04c revocation (same NATS server, same Go client). On match,
ext_authz sets response header `x-keese-budget-exceeded: true`. Envoy's `local_reply_config`
matches on that header → returns HTTP 429 with `Retry-After: <seconds-to-budget-reset>`.
Metric: `keese_extauthz_budget_429_total{tenant, workspace, budget_key}`.
This is the definitive answer; 10b iter-1 inherits.

## Witness gateway audience

Witness agents (design 23) carry a separate SA token audience `keese-egress-supervisor-<tenant>`.
Separate IAM trust policy per cloud: witness can invoke diagnostic tools but cannot
impersonate workspace upstream-model calls. Cross-ref: 23 iter-1 must honor this audience;
04b iter-2 may add a supervisor SA projection row.

## ext_authz decision flow

1. JWT Authn filter validates SA token via JWKS; projects `aud` to dynamic metadata.
2. Ext_authz reads `keese.sa_token.aud` → stamps `x-keese-tenant`.
3. Derives subject `user:ksa-<workspace-uid>` for OpenFGA checks (04b; per-cluster OpenFGA means bare SA name is globally unique within the store). The JWT `aud` claim remains `keese-egress-<tenant>` for cloud STS trust policies — audience and OpenFGA subject are deliberately separate concerns.
4. Checks NATS KV `keese-revocation-version/workspace/<uid>` (04c); NATS-degraded: skip cache.
5. Checks NATS KV `keese-budget-exceeded/workspace/<uid>` → sets `x-keese-budget-exceeded`.
6. `Check(tool:<name>#can_call@<subject>)` at `HIGHER_CONSISTENCY` (≤ 50 ms, 04a).
7. Returns 200 (allow) or 403 (deny); 429 via Envoy `local_reply_config` on budget flag.

## VAP gate for `dedicatedGateway` toggle

CEL: toggle allowed only when `status.phase in ['Pending', 'Terminating']` — blocks while
`Ready`, `Degraded`, or `Provisioning`. Drain procedure: set all tenant Workspaces to
`spec.suspended: true`; wait for `status.phase → Pending`; toggle `dedicatedGateway`;
resume workspaces. Runbook: `docs/plans/runbook-dedicated-gateway-toggle.md` — **flagged
residual gap**.

## Rate limiting authority

| Layer | Mechanism | Window | On exhaustion |
|---|---|---|---|
| Short-window | Envoy `BackendTrafficPolicy` token-cost filter | Per-sec / per-min | HTTP 429; `x-keese-limit-source: gateway-token-rate` |
| Long-window | `TokenBudget` CR (D10b) via NATS KV | Per-day / per-month | HTTP 429; `x-keese-limit-source: token-budget` |

## CRD pinning and upgrade

Keese CSV pins `aigateway.envoyproxy.io/v1alpha1` CRDs via digest in `helmfile.yaml`
(`crds.install=true, crds.keep=true`). **14b gap:** CSV pinning manual until 14b `current`.

## Observability

OTEL spans: `gateway.authz` (`tenant`, `workspace`, `decision`, `latency_ms`, `model_id`);
`gateway.backend.select` (`route`, `backend`, `tenant`); `gateway.upstream.request`
(`model`, `tenant`, `status_code`, `tokens_in`, `tokens_out`).

Prometheus: `envoy_ai_gateway_requests_total{route,tenant,decision}`,
`envoy_ai_gateway_token_cost_total{model,tenant}`, `keese_extauthz_budget_429_total{tenant,workspace,budget_key}`,
`keese_extauthz_degraded_seconds_total`.

## Failure modes

| Failure | Detection | Mitigation |
|---|---|---|
| keese-ext-authz pod crash | Gateway 503; PDB | Pod restarts; NATS KV resubscribes on boot |
| JWKS endpoint unreachable | 401 after cache miss | Fail-open window (30–600 s); then fail-closed |
| OpenFGA unreachable | ext_authz 503 → 403 | Deny all; `AuthzFullyDegraded` alert (04c) |
| NATS KV watch lost | Degraded mode (04c) | Skip cache; direct OpenFGA call |
| `BackendTrafficPolicy` exhausted | HTTP 429 | `x-keese-limit-source: gateway-token-rate` |
| `TokenBudget` exhausted | NATS KV → 429 | `x-keese-limit-source: token-budget` |
| Gateway pod restart | Envoy drain (preStop 30 s) | HPA ≥ 2 replicas; LB routes around draining pod |
| Shared ext_authz down | Cluster-wide egress blocked | PDB + HPA; alert fires; HA mitigates |
| `dedicatedGateway` toggle on Ready | VAP deny | Drain procedure; runbook |

## Refs

- [04a](04a-openfga-authz-model.md) — Check semantics, latency tiers, model_id
- [04b](04b-projected-sa-identity.md) · [04b-ii](04b-ii-oidc-trust.md) — SA token, subject, OIDC trust
- [04c](04c-token-revocation.md) — NATS KV watch, version-tag, NATS-degraded mode
- [05b](05b-credential-injection-patterns.md) · [05c](05c-mcp-policy-enforcement.md) — downstream stubs
- [05a-ii-iter-log.md](05a-ii-iter-log.md) — iteration log (split for line ceiling)
- [10b](10b-token-accounting.md) — TokenBudget NATS KV signaling (inherits this decision)
- [14a](14a-olm-channels-upgrades.md) · [14b](14b-olm-dependencies.md) — CRD pinning in CSV
- [17](17-credential-broker.md) · [24](24-tenant-crd.md) — credential cache · dedicatedGateway
- [dev/bootstrap/values/envoy-ai-gateway.yaml](../../dev/bootstrap/values/envoy-ai-gateway.yaml)
- [../plans/rubric.md](../plans/rubric.md)
- [../plans/runbook-dedicated-gateway-toggle.md](../plans/runbook-dedicated-gateway-toggle.md) (to author)
