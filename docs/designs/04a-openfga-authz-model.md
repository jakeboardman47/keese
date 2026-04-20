<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: authz
depends:
  - 01-tenancy-capsule.md
  - 02-workspace-model.md
  - 04b-projected-sa-identity.md
  - 04c-token-revocation.md
  - 10a-otel-topology.md
  - 23-agent-supervision.md
  - 24-tenant-crd.md
related_skills: []
status: draft
last_verified: 2026-04-20
rollback: |
  Bump OPENFGA_AUTHORIZATION_MODEL_ID in keese-rebac-config ConfigMap to the prior
  model ID (stored in dev/bootstrap/openfga/seed.yaml). Controllers re-read on restart.
  Tuple shapes are additive; prior-model tuples are ignored for new relations.
---

# 04a — OpenFGA Authorization Model

> **Status: current.** Iter-2 scored 100/100 (SHIP). Iteration log:
> [04a-ii-iteration-log.md](04a-ii-iteration-log.md).

## Context

keese authorizes every agent tool call at the Envoy AI Gateway via OpenFGA `ext_authz`.
A compromised agent pod carries only a projected ServiceAccount token — no upstream keys.
The gateway terminates that token, resolves the caller's identity to an OpenFGA subject,
and issues a single `Check` call against the ReBAC model defined here. This design
specifies the full tuple shape, relation semantics, consistency strategy, and
failure-closed contract. Token minting is 04b; revocation is 04c.

## Types and relations

### OpenFGA entity map

| OpenFGA type | Keese entity | CRD / primitive |
|---|---|---|
| `tenant` | Keese Tenant (D26) | `tenancy.operator.keese.ai/v1alpha1/Tenant`; identity key `Tenant.metadata.uid` |
| `workspace` | Workspace CR | `workspace.operator.keese.ai/v1alpha1/Workspace` |
| `tool` | Callable function exposed to agents | ConfigMap-backed `ToolAllowList` via GuardrailBinding |
| `extension` | RuntimeExtension provider | `runtime.operator.keese.ai/v1alpha1/RuntimeExtension` |
| `credential` | BackendSecurityPolicy-referenced secret | OpenBao/KMS via ExternalSecrets |
| `memory` | Memory/SharedMemory backend | `memory.operator.keese.ai/v1alpha1/{Memory,SharedMemory}` |
| `service_account` | Per-workspace projected SA | K8s `ServiceAccount`; subject `user:ksa-<ws-uid>@keese-egress-<tenant>` |
| `user` | Human operator or CI identity | OIDC identity (GitHub, SSO) |
| `witness` | Supervision agent (D23) | Provisioned by agent-supervision controller |

Source of truth for the model DSL: `dev/bootstrap/openfga/model.fga`.

### `tenant_member` computed relation

`workspace.tenant_member = member from owner` chains through `owner: [tenant]`.
Any `tenant.member` automatically propagates to every workspace the tenant owns.
Tools use this via `can_call: tenant_member from allowed_in` — a single
`Check(tool:X#can_call@service_account:SA)` walks
`tool → allowed_in → workspace → owner → tenant → member` in one RTT.

The name `tenant_member` appears only on `workspace` and `tool` (via `from` chain).
No other type uses this name (verified in `model.fga`).

### `can_revoke` relation

`workspace.can_revoke: [witness, service_account]` — automation-only.

Written by:
- Operator install Job: `workspace:*#can_revoke@service_account:keese-supervisor`
  (one tuple per workspace, Mode A and Mode B).
- Supervision controller (D23): `workspace:<name>#can_revoke@witness:<id>` when
  dispatching a witness. Revoked when supervision ends.

`user:*` subjects do **not** receive `can_revoke`. Human-initiated graceful suspension
uses `Workspace.spec.suspended = true` via `keese-workspace-editor` RBAC. Force-revoke
is automation-only by design.

## Tuple shapes

| Tuple | Written by | When |
|---|---|---|
| `tenant:T#admin@user:U` | Operator bootstrap Job | Tenant owner provisioned |
| `tenant:T#member@service_account:SA` | Workspace controller | Workspace created, SA projected |
| `workspace:W#owner@tenant:T` | Workspace controller | Workspace created |
| `workspace:W#editor@user:U` | Workspace controller | User granted editor role |
| `workspace:W#viewer@user:U` | Workspace controller | User granted viewer role |
| `workspace:W#supervised_by@witness:WIT` | Supervision controller (D23) | Witness dispatched |
| `workspace:W#can_revoke@service_account:keese-supervisor` | Operator install Job | Per workspace |
| `workspace:W#can_revoke@witness:WIT` | Supervision controller (D23) | Witness assigned |
| `tool:X#allowed_in@workspace:W` | GuardrailBinding controller | ToolAllowList entry reconciled |
| `memory:M#reader@service_account:SA` | Memory controller | SharedMemory read grant |
| `memory:M#writer@service_account:SA` | Memory controller | SharedMemory write grant |
| `credential:C#bound_to@workspace:W` | Credential broker reconciler | BackendSecurityPolicy bound |

## Check semantics

### Computed-relation single Check (default)

```
Check(tool:<name>#can_call@<subject>)
```

OpenFGA resolves the full chain in one API call. Replaces the iter-1 dual-Check pattern.

### Feature flag: `--openfga-check-mode`

```
--openfga-check-mode=computed   # default: single Check via computed relation
--openfga-check-mode=dual       # incident mitigation only — not a supported config
```

The `dual` mode restores two sequential Checks if a bug in the computed relation is
discovered. It must never be enabled in normal operation.

### Consistency tiers

| Check type | Consistency | Latency budget |
|---|---|---|
| Tool call (`tool#can_call`) | `HIGHER_CONSISTENCY` | ≤ 50 ms p99 |
| Memory read/write | `HIGHER_CONSISTENCY` | ≤ 50 ms p99 |
| Workspace provisioning | eventual | ≤ 500 ms p99 |
| Audit-only reads | eventual | ≤ 1 s p99 |

## Force-revoke authz flow

Admission webhook sequence for `PATCH Workspace.spec.forceRevoke.{epoch,requestedBy}`:
1. Webhook reads `request.userInfo.username` → subject.
2. Webhook calls `Check(workspace:<name>#can_revoke@<subject>)` with `HIGHER_CONSISTENCY`.
3. `allowed=true` → persist; `allowed=false` → admission rejected, reason `ForbiddenToRevoke`.
4. Event `ForceRevokeAttempt` records `(subject, authz-decision, workspace, epoch)`.
5. Persisted PATCH triggers 04c revocation flow.

Bypass prevention: webhook is fail-closed (deny on error); agents never hold a kubeconfig
(rule 05.1), so they cannot impersonate a privileged subject.

## Failure modes

| Failure | Behavior | Recovery |
|---|---|---|
| OpenFGA unreachable | `ext_authz` returns 503; Envoy denies call | Pod restart; no stale cache served |
| Check error | Treated as deny; event `AuthzCheckFailed` | Alert on rate > 1% |
| Stale tuple | `HIGHER_CONSISTENCY` mitigates; reconciler retries | ≤ 3 reconciles per rule 04.16 |
| Model migration mid-flight | Old model ID in ConfigMap; operator rolls atomically | Bump ConfigMap; restart controllers |
| `tenant_member` computed bug | `--openfga-check-mode=dual` restores sequential Checks | File OpenFGA bug; revert model |
| Force-revoke bypass | Impossible — fail-closed webhook + no kubeconfig in agents | Periodic audit Check |

## Audit and observability

### Elasticsearch (primary)

Index `keese-openfga-audit-*` — hot path, real-time queries, 30-day ILM. Every `ext_authz`
decision emits `{tuple, sa, host, decision, upstream_status, latency_ms, model_id}`. No
tokens or request/response bodies (rule 05.10).

### Loki (secondary — long-term)

Stream `{ job="keese-ext-authz", tenant="<T>" }`, ≥ 1-year retention via object storage
(S3/GCS/Azure Blob per D21 OpenTofu modules). Queries via LogQL from Grafana. OTEL
collector fans out to both ES and Loki — no keese-side dual-write. Same redaction rules
as ES. Depends on: design 10a iter-1.

### Metrics and traces

- `keese_rebac_check_duration_seconds{check_type, consistency, result}` histogram.
- `keese_rebac_check_errors_total{check_type}` counter.
- `keese_rebac_tuple_writes_total{type, relation}` counter.
- Every `ext_authz` call: OTEL span `keese.rebac.check` with `(tuple, model_id, result)`.

## Model versioning and rollback

`OPENFGA_AUTHORIZATION_MODEL_ID` in ConfigMap `keese-rebac-config`. On model update:
operator writes new model → updates ConfigMap → controllers restart to pick up new ID.
Version-tagged cache in `internal/rebac/cache.go` bumped in coordination with 04c.

Rollback: record prior model ID, restore ConfigMap, restart controllers. New tuples for
rolled-back relations are harmless (not checked) and pruned via cleanup Job if permanent.

## Refs

- [model.fga](../../dev/bootstrap/openfga/model.fga)
- [04a-ii-iteration-log.md](04a-ii-iteration-log.md)
- [04b-projected-sa-identity.md](04b-projected-sa-identity.md)
- [04c-token-revocation.md](04c-token-revocation.md)
- [05a-envoy-ai-gateway-topology.md](05a-envoy-ai-gateway-topology.md)
- [10a-otel-topology.md](10a-otel-topology.md)
- [23-agent-supervision.md](23-agent-supervision.md)
- [24-tenant-crd.md](24-tenant-crd.md)
- [../specs/egress-authz-protocol.md](../specs/egress-authz-protocol.md)
- [../plans/rubric.md](../plans/rubric.md)
