<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: spec
category: contract
depends:
  - ../designs/04a-openfga-authz-model.md
  - ../designs/04a-ii-testplan.md
  - ../designs/04b-projected-sa-identity.md
  - ../designs/04b-ii-oidc-trust.md
  - ../designs/04c-token-revocation.md
  - ../designs/05a-envoy-ai-gateway-topology.md
  - ../designs/05b-credential-injection-patterns.md
  - ../designs/05c-mcp-policy-enforcement.md
  - ../designs/25-cross-tenant-agreement.md
related_skills: [controller-authoring, test-engineer]
status: current
last_verified: 2026-06-11
regression_lock: false
tests:
  unit:
    - internal/extauthz/check_test.go
    - internal/extauthz/subject_test.go
    - internal/extauthz/token_test.go
  envtest:
    - test/envtest/extauthz/suite_test.go
    - test/envtest/audit/no_token_test.go
    - test/envtest/admission/forcerevoke_allow_test.go
    - test/envtest/admission/forcerevoke_deny_test.go
  kuttl:
    - test/e2e/extauthz_openfga_down_test.go
    - test/e2e/extauthz_cross_tenant_test.go
metrics:
  - keese_rebac_check_duration_seconds{check_type,consistency,result}
  - keese_rebac_check_errors_total{check_type}
  - keese_extauthz_budget_429_total{tenant,workspace,budget_key}
  - keese_extauthz_degraded_seconds_total
  - keese_extauthz_timeout_total{workspace,tenant}
events:
  - AuthzCheckFailed
  - AuthzCheckTimeout
  - AuthzCircuitOpen
  - AuthzKVWatchDegraded
  - AuthzKVWatchRecovered
  - AuthzFullyDegraded
---

# egress-authz-protocol — spec

Cross-cutting contract between Envoy AI Gateway's `ext_authz` filter and the `keese-authz` standalone Deployment (`keese-system:9001`). Consumed by 05a (gateway), 09 (a2a transport), 03c (Workflow cross-tenant admission). Iteration log: [egress-authz-protocol-iter-log.md](egress-authz-protocol-iter-log.md).

## 1. Wire protocol

**gRPC:** `envoy.service.auth.v3.Authorization/Check` at
`keese-authz.keese-system.svc.cluster.local:9001`.
`failure_mode_allow: false` — Envoy denies on gRPC error or timeout.

### CheckRequest fields consumed

| Field | Path |
|---|---|
| Bearer token | `request.headers.authorization` + dynamic metadata `keese.sa_token` (post-JWT-Authn) |
| Target path | `request.http.path` |
| HTTP method | `request.http.method` |
| JWT payload | `metadata_context.filter_metadata["keese.sa_token"]` |
| A2A scope (discriminator) | `request.headers["x-keese-a2a-scope"]` — value `cross-tenant` routes to the `messageable_from` Check (§4) instead of `can_call`; stamped by the 09 a2a sidecar |
| A2A peer workspace | `request.headers["x-keese-a2a-peer-workspace"]` — the destination (`W_to`) workspace UID; the FGA *object* of the `messageable_from` Check |

### CheckResponse fields set

**OkResponse** — headers `x-keese-tenant` (tenant name → 05b BSP), `x-keese-workspace`
(workspace name → 05c CEL), `x-keese-audience` (`keese-egress-<tenant>` → cloud IAM);
dynamic metadata mirrors the same three values under `keese.*` namespace for downstream
filters and the credential broker (17).

**DeniedResponse**: HTTP 403 or 503 with `x-keese-deny-reason` ∈
`{openfga-unavailable, authz-denied, authz-timeout, revoked, budget-exceeded, no-backend-credential}`.

## 2. Token validation

JWT validated by Envoy JWT Authn filter against K8s OIDC JWKS
(`https://<apiserver>/openid/v1/jwks`). Cache: 300 s default, 30–600 s range
(04b-ii). Cache expiry + failed fetch → fail-closed 401.

`aud` claim matched against one OIDCProvider audience template (04b iter-3):

| Template | Pattern | Validated by |
|---|---|---|
| `egress` | `keese-egress-<tenant>` | ext_authz + cloud IAM |
| `workflowRun` | `keese-wf-<run-uid>` | 09 NATS bridge only (never cloud IAM) |
| `supervisor` | `keese-supervisor-<ws-uid>` | 08b ACP bridge only (never cloud IAM) |

Only the `egress` audience reaches ext_authz for LLM/MCP calls.

## 3. Subject derivation

`OIDCProvider.spec.subjectTemplate` Go template evaluated at token-mint time:

| Identity type | Template | OpenFGA subject |
|---|---|---|
| Workspace SA | `user:ksa-{{.WorkspaceUid}}` | `user:ksa-<workspace-uid>` |
| Human OIDC | `user:{{.Email}}` | `user:<email>` |
| CI SA | `service_account:{{.Subject}}` | `service_account:<sub>` |

Missing required template variables → `AudienceTemplateEvalError`; deny 403.

## 4. OpenFGA Check calls

Single `Check` per request; `HIGHER_CONSISTENCY` always.

| Traffic type | Check tuple | p99 budget |
|---|---|---|
| LLM / MCP egress | `tool:<name>#can_call@<subject>` | ≤ 50 ms (4–5 hop) |
| Credential use | `credential:<C>#can_use@<subject>` | ≤ 25 ms (2-hop) |
| Force-revoke admission | `workspace:<name>#can_revoke@<subject>` | ≤ 15 ms (1-hop) |
| NATS intra-tenant subscribe | No Check — topic existence IS authz | — |
| NATS cross-tenant subscribe | `workspace:<W_to>#messageable_from@workspace:<W_from>` | ≤ 25 ms |
| A2A direct call (E2) | `workspace:<W_to>#a2a_callable_by@workspace:<W_from>` | ≤ 25 ms |

Cross-tenant NATS check enforced by 09 NATS bridge at subscribe + first-publish.
Tuple written by CRA controller (25) only after bilateral approval.

### A2A direct call (`a2a_callable_by`) — E2

Synchronous A2A HTTP/SSE endpoint calls (E1b a2a-bridge) are gated by
`a2a_callable_by` — distinct from NATS `messageable_from`. Discriminator
`x-keese-a2a-call: true` routes to `authorizeA2AEndpoint`
(`internal/authz/extauth/crosstenant.go`). Direction (rule 05.9): caller W_from (projected
SA token via `subject.go`) is the FGA **user**; peer header `x-keese-a2a-peer-workspace`
(W_to) is the FGA **object** — `Check(workspace:W_from, a2a_callable_by, workspace:W_to)`.

Tuples are written by the Workspace controller (`workspace_a2a.go`), keyed on
`spec.a2a.enabled` (the `// +keese:rebac-tuple=a2a_callable_by` marker): **intra-tenant** →
self tuple `workspace:W#a2a_callable_by@workspace:W` (admits same-tenant peers);
**cross-tenant** → one tuple `workspace:W_to#a2a_callable_by@workspace:W_from` per peer,
written **only** after an Approved, non-expired CrossTenantAgreement (D25/D29) pairs the
peer with this callee in `status.workspaceSnapshot` — absent a CTA, no tuple, Check fails
closed (rule 05.4). Audit reuses `from_workspace`/`to_workspace` (§6). Relation added
additively at 04a iter-7.

## 5. Decision metadata

Dynamic metadata set on allow: `keese.tenant` (tenant name → BSP lookup 05b),
`keese.workspace` (workspace name → 05c CEL), `keese.audience` (audience string →
credential broker 17). BSP resolution: workspace → tenant → cluster-default (05b §BSP precedence).

## 6. Audit log shape

Per rule 05.10 — never tokens, never request/response bodies. Fields:
`job="keese-ext-authz"`, `tenant`, `tuple`, `sa`, `host`, `decision` (allow|deny),
`upstream_status`, `latency_ms`, `model_id`. Cross-tenant a2a decisions additionally
carry `from_workspace`/`to_workspace` (the `messageable_from` Check pair). No other fields.

Destinations: ES `keese-openfga-audit-*` (30-day ILM) + Loki
`{job="keese-ext-authz", tenant="<T>"}` (≥ 1-year). Fan-out via OTEL (10a).

## 7. Failure modes

All fail-closed. Sourced from 04a iter-5 and 04c.

| Failure | Behavior | Recovery |
|---|---|---|
| OpenFGA unreachable | 503; Envoy denies | Pod restart; NATS KV resubscribes |
| Check error | Deny + `AuthzCheckFailed` event | Alert at rate > 1% |
| Check timeout | 403 `authz-timeout` + `AuthzCheckTimeout`; circuit break > 50% / 2 min | Break-glass: namespace `keese.ai/break-glass=true` |
| Stale tuple | `HIGHER_CONSISTENCY` mitigates | ≤ 3 reconciles to converge |
| Partial model rollout | Operator blocks migration exit until 100% convergence | MODEL_MIGRATION drain (04a) |
| NATS KV watch lost | Skip cache; direct Check every request; `AuthzKVWatchDegraded` | Recovers on NATS reconnect |
| OpenFGA + NATS both down | Deny all; `AuthzFullyDegraded`; immediate page | HA (PDB + 3 replicas) |
| Budget flag set | 429 via Envoy `local_reply_config`; `x-keese-limit-source: token-budget` | Wait for budget reset |

## 8. Throughput budget and upgrade

**Latency / throughput (shared-mode, 3 replicas):**

| Signal | Target | Alert |
|---|---|---|
| p99 latency (LLM/MCP, 4–5 hop) | ≤ 50 ms | > 50 ms / 5 min → P2 |
| p99 latency (1-hop direct) | ≤ 15 ms | > 15 ms / 5 min → P3 |
| Throughput | ≤ 5,000 rps (HPA at 60% CPU) | HPA at max replicas → P2 |
| Error rate | < 0.1% non-revocation denies | > 1% → `AuthzCheckFailed` alert |

**CI:** `scripts/check-rebac-markers.sh` + `scripts/check-openfga-assertions.sh`
pre-commit; `make test-model-migration` e2e drain.

**Upgrade/rollback:** image pinned in CSV (14a/14b); OLM channel upgrade (rule 05.14).
Rolling restart: `kubectl rollout restart deployment/keese-ext-authz`; NATS KV
resubscribes on startup; no state lost. Rollback: OLM prior channel head.

## 9. Acceptance tests

| Test name | File | Assertion |
|---|---|---|
| `TestFailClosed_OpenFGADown` | `test/e2e/extauthz_openfga_down_test.go` | OpenFGA killed → 503; `AuthzCheckFailed` rate > 1% fires |
| `TestComputedRelation_CanCall` | `tests/openfga/can_call.yaml` | 4–5 hop; allowed / denied correctly |
| `TestAuditEvent_NoTokenBytes` | `test/envtest/audit/no_token_test.go` | ES + Loki records contain no token bytes or bodies |
| `TestForceRevoke_AdmissionAllow` | `test/envtest/admission/forcerevoke_allow_test.go` | `keese-supervisor` patch allowed; event decision=allowed |
| `TestForceRevoke_AdmissionDeny` | `test/envtest/admission/forcerevoke_deny_test.go` | `user:bob` → `ForbiddenToRevoke` |
| `TestCrossTenant_MessageableFrom_Allow` | `test/e2e/extauthz_cross_tenant_test.go` | CRA Approved; NATS cross-tenant subscribe allowed |
| `TestCrossTenant_MessageableFrom_Deny` | `test/e2e/extauthz_cross_tenant_test.go` | No CRA; NATS cross-tenant subscribe 403 |
| `TestSubjectDerivation_KsaUid` | `internal/extauthz/subject_test.go` | SA sub → `user:ksa-<uid>` |
| `TestTokenValidation_WrongAudience` | `internal/extauthz/token_test.go` | `aud=keese-wf-*` at LLM endpoint → 401 |
| `TestCheckResponse_MetadataFields` | `internal/extauthz/check_test.go` | OK response sets all 3 dynamic metadata fields |
| `TestNATSDegraded_DirectCheck` | `test/envtest/extauthz/suite_test.go` | NATS watch lost → direct Check; correct decision |
| `TestCircuitBreaker_Open` | `test/envtest/extauthz/suite_test.go` | > 50% timeout / 2 min → `AuthzCircuitOpen`; deny all |
| `TestModelMigration_Drain` | `test/e2e/model_migration_drain_test.go` | (from 04a-ii) gate holds until all pods report new model ID |

## Refs

- [04a-openfga-authz-model.md](../designs/04a-openfga-authz-model.md)
- [04a-ii-testplan.md](../designs/04a-ii-testplan.md)
- [04b-projected-sa-identity.md](../designs/04b-projected-sa-identity.md)
- [04b-ii-oidc-trust.md](../designs/04b-ii-oidc-trust.md)
- [04c-token-revocation.md](../designs/04c-token-revocation.md)
- [05a-envoy-ai-gateway-topology.md](../designs/05a-envoy-ai-gateway-topology.md)
- [05b-credential-injection-patterns.md](../designs/05b-credential-injection-patterns.md)
- [05c-mcp-policy-enforcement.md](../designs/05c-mcp-policy-enforcement.md)
- [25-cross-tenant-agreement.md](../designs/25-cross-tenant-agreement.md)
- [egress-authz-protocol-iter-log.md](egress-authz-protocol-iter-log.md)
- [../plans/rubric.md](../plans/rubric.md)
