<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: spec
category: contract
depends:
  - ../designs/17-credential-broker.md
  - ../designs/05b-credential-injection-patterns.md
  - ../designs/05b-ii-bsp-examples.md
  - ../designs/04c-token-revocation.md
  - ../designs/04b-projected-sa-identity.md
  - ../designs/04b-ii-oidc-trust.md
related_skills: []
status: current
last_verified: 2026-04-21
regression_lock: false
tests:
  unit: []
  envtest:
    - internal/broker/cache_test.go
    - internal/broker/revocation_test.go
  kuttl:
    - test/e2e/credential-broker/
metrics:
  - keese_broker_vault_errors_total
  - keese_broker_revocation_flush_total
  - keese_broker_pool_exhausted_total
  - keese_broker_credential_expired_seconds_remaining
  - keese_broker_l2_eviction_total
  - keese_broker_refresh_goroutine_high_water
  - keese_bsp_exchange_total
events:
  - VaultUnreachable
  - SecretStale
  - STSTimeout
  - STSAuthFailed
  - STSServerError
  - WIFExchangeFailed
  - EntraExchangeFailed
  - CredentialRevoked
  - CredentialExpiringFailClosed
  - PoolMemberCooling
  - PoolExhausted
---

# credential-broker-protocol — spec

**Goal:** define the contract between `BackendSecurityPolicy`, cloud STS providers
(AWS / GCP / Azure), and OpenBao for credential swap inside the `keese-authz`
standalone Deployment (`keese-system`, gRPC `:9001`). Consumed by designs 05a, 05b, and 17.

## 1. Credential source types

Three types correspond to the three BSP stanza families (05b):

| Type | BSP stanza | Trigger path |
|---|---|---|
| **Static API key** | `spec.apiKey.secretRef` | ESO → K8s Secret → broker L2 |
| **OIDC-STS exchange** | `spec.oidc` / `spec.gcpOidc` / `spec.azureOidc` | Agent `egress` SA token → cloud STS |
| **Dynamic vault credential** | vault-agent sidecar on gateway pod | OpenBao role JWT → file-based plugin |

Static keys (Anthropic, OpenAI, non-OIDC upstreams): stored in OpenBao at
`kv/keese/tenants/<tenant>/credentials/<provider>/<key-name>`; ESO bridges to K8s Secret
`keese-cred-<tenant>-<provider>` in `keese-credentials` namespace (05b).
No credential reaches the agent pod (rules 05.1, 05.2).

## 2. Per-source request shape

**AWS STS AssumeRoleWithWebIdentity:** inputs `(role-arn=BSP.spec.oidc.roleArn,
web-identity-token=/var/run/keese/tokens/egress, session-name=keese-<tenant>-<workspace-uid>)`;
output STS credentials `(AccessKeyId, SecretAccessKey, SessionToken, Expiration)`.
Trust policy constrains `aud=keese-egress-<tenant>` AND `sub` (04b-ii).

**GCP WIF:** inputs `(workload-identity-pool-provider=BSP.spec.gcpOidc.*, sts-token=egress)`;
output GCP access token via `sts.googleapis.com/v1/token` + SA impersonation.

**Azure Entra FIC:** inputs `(client-id=BSP.spec.azureOidc.clientId,
audience=api://AzureADTokenExchange, federated-token=egress)`;
output Entra access token. FIC provisioned by `deploy/opentofu/azure/identity.tf` (04b-ii).

**OpenBao dynamic:** inputs `(role=keese.ai/vault-role annotation, jwt=egress token)`;
output credential file at `/var/run/keese/upstream-creds/<name>` via `file_based_plugin`.
Vault-agent sidecar on gateway pod only — never on agent pod (rules 05.1, 05.7).

## 3. Cache key and TTL

**Cache key:** `(tenant-audience, upstream-role)` — matches the federated credential
scope and BSP role ARN / GCP SA / Entra client-id. Per-gateway-pod; NOT cross-pod.

**TTL schedule (design 17):**

| Threshold | Action |
|---|---|
| 0% – 70% TTL | Serve from L2; no STS call |
| 70% TTL | Background goroutine fires; refresh credential atomically |
| 70% – 95% TTL (backoff window) | Exponential backoff: 100 ms / 500 ms / 2 s / 10 s |
| 95% TTL | Fail-closed: L2 entry marked `fail-closed`; next request → HTTP 401 |

**LRU eviction:** L2 capped at 10,000 entries per pod
(`Tenant.spec.credentialBroker.maxEntriesPerPod`); LRU eviction past ceiling.
Goroutine limit: 1,000/pod; warn at 800 (`keese_broker_refresh_goroutine_high_water`);
above 1,000: skip proactive refresh, refresh on next request.

## 4. Three-tier cache (design 17)

| Tier | Scope | Key | Purpose |
|---|---|---|---|
| L1 per-request | In-process request context | `(tenant, workspace, upstream)` | Deduplicates STS calls within a single logical request |
| L2 per-pod | In-process map in `keese-ext-authz` | `(tenant-audience, upstream-role)` | Primary; avoids per-request STS RTT |
| L3 distributed | NATS KV `keese-credential-cache` | `(tenant, upstream-role, version)` | Pool `least-used` counter coordination; opt-in via `Tenant.spec.credentialBroker.sharedCache: true` |

Cold path (L1/L2 miss): call cloud STS or OpenBao → write L2 → spawn refresh goroutine.
Pool selection (`round-robin`, `least-used`, `spillover`) in L3 NATS KV when opt-in;
independent L2 per pod when not.

## 5. Revocation hook (design 04c)

On OpenFGA tuple revoke: controller bumps NATS KV `keese-revocation-version/workspace/<uid>`
(atomic CAS) → `keese-authz` watch fires on all pods (< 1 s) → each pod atomically removes
matching L2 entries → goroutine aborts write if revocation flag set (entry never written) →
next request: OpenFGA `Check` → deny → 403. **SLO:** p95 ≤ 60 s (04c). L2 entries carry
`version uint64`; stale entries (version < current) are denied immediately.

## 6. Failure modes

Sourced from design 17 twelve-row table; organized by tier:

| Tier | Failure | Broker action | Signal |
|---|---|---|---|
| L1 | — | No L1-specific failures | — |
| L2 | AWS STS timeout | Retry 3× jittered (100 ms / 500 ms / 2 s); fail-closed on final | `STSTimeout`; `sts.endpoint` OTEL attr |
| L2 | AWS STS 4xx (expired SA) | Fetch fresh projected SA from kubelet API; retry once | `STSAuthFailed` |
| L2 | AWS STS 5xx | Transient retry with backoff; mark `Degraded` after 3 consecutive | `STSServerError` |
| L2 | GCP WIF exchange failure | Same retry as STS 5xx | `WIFExchangeFailed` |
| L2 | Azure Entra exchange failure | Same retry as STS 5xx | `EntraExchangeFailed` |
| L2 | OpenBao unreachable at refresh | Serve L2 past 70%; exponential backoff; fail-closed past 95% | `VaultUnreachable`; `keese_broker_vault_errors_total` |
| L2 | K8s Secret stale (ESO lag) | Serve current L2; `SecretStale` after 5 min of stale | `keese_broker_secret_staleness_seconds{tenant}` |
| L2 | API key rotated (ESO event) | Drop L2 entry on Secret informer event; debounced refresh | `keese_broker_l2_eviction_total{reason=rotation}` |
| L2 | 04c revocation NATS KV push | Atomic flush of matching L2 entries | `CredentialRevoked`; `keese_broker_revocation_flush_total` |
| L2 | Revocation mid-refresh | Goroutine aborts write if revocation flag set | Logged `warn`; entry never written |
| L3 | Pool member 429 | Mark member `cooling`; select next (`spillover` per 05b) | `PoolMemberCooling`; `keese_broker_pool_cooling_count` |
| L3 | All pool members cooling | Fail-closed 429 to agent | `PoolExhausted`; `keese_broker_pool_exhausted_total` |

## 7. Audit log shape

No tokens in audit logs (rule 05.10). Every authz decision emits:
`{tenant_audience, upstream_role, decision, ttl_remaining_seconds, exchange_type, cache_tier}`
where `exchange_type ∈ {sts, gcp_wif, azure_entra, static, openbao}` and
`cache_tier ∈ {l1, l2, miss}`. Token bytes and request/response bodies are NEVER logged.
OTEL span `keese.broker.exchange` on every L2 miss with the same fields.

## 8. Acceptance tests (`test/e2e/credential-broker/`)

`STS_happypath_aws` — L2 miss → STS call → cached; second request is L2 hit.
`STS_happypath_gcp` — GCP WIF exchange cached; `exchange_type=gcp_wif` in span.
`STS_happypath_azure` — Azure Entra FIC cached; `exchange_type=azure_entra` in span.
`cache_hit` — same `(audience, role)` second request: L2 hit, no STS call.
`cache_miss_cold` — fresh pod: L3 cold path STS call.
`refresh_at_70pct` — goroutine fires at 70% TTL; no request dropped.
`fail_closed_at_95pct` — no refresh by 95% → HTTP 401 `X-Keese-Cred-Expired: true`.
`revocation_propagation` — NATS KV bump → L2 flush within SLO; next request → 403.
`openbao_role_rotation` — ESO event evicts L2; new OpenBao credential fetched.
`revocation_mid_refresh` — revocation flag set during goroutine refresh; entry never written.

## Iteration log

### Iteration 1 — 2026-04-21 (correctness & security) — Score 95 — SHIP
Gaps: Cat 4 (−5) pre-gate scripts; Cat 10 (−5) HA not explicit. Cross-deps settled: cache key `(tenant-audience, upstream-role)`; audit shape (7 fields); STS session-name `keese-<tenant>-<workspace-uid>`; version-tag deny-before-evict semantics.

### Iteration 2 — 2026-04-21 (performance & quality) — Score 95 — SHIP
Added `openbao_role_rotation` and `revocation_mid_refresh` tests (10 total); verified L1/L2/L3 matches design 17 verbatim; vault-agent gateway-pod placement confirmed against rule 05.7. Flagged: L3 NATS KV TTL cap vs. 04c JetStream config — confirm in 04c iter-3.

### Iteration 3 — 2026-04-21 (operational readiness)

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Three source types, contract consumers, bounded inputs/outputs. |
| 2 | Architecture fit | 10 | 1.0 | 10 | L1/L2/L3 verbatim from 17; BSP families from 05b; egress audience from 04b. |
| 3 | Security posture | 15 | 1.0 | 15 | No cred in agent pod; no tokens in audit; fail-closed at 95%; revocation race. |
| 4 | Automatability | 10 | 0.5 | 5 | Pre-gate scripts outstanding; no change possible before gate open. |
| 5 | Verifiability | 15 | 1.0 | 15 | 10 tests: all source types, all cache tiers, revocation, rotation. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | 13-row tier-annotated table; mid-refresh race; all STS clouds. |
| 7 | Context efficiency | 10 | 1.0 | 10 | ≤200 lines; single responsibility; skill pointers via depends. |
| 8 | Docs quality | 5 | 1.0 | 5 | `regression_lock: false` (lifts to true at `status: implemented`); frontmatter fully populated. |
| 9 | Observability | 5 | 1.0 | 5 | OTEL span + 7 metrics + 11 events; 6-field audit shape. |
| 10 | Operational readiness | 10 | 1.0 | 10 | HA: per-pod L2; NATS KV fan-out < 1 s; cold rebuild on restart; L3 pool counters opt-in; LRU configurable; goroutine alert at 800. |
| | **Total** | 100 | | **95** | Cat 4 (−5) pre-gate structural. |

Verdict: SHIP (95 ≥ 90). Status: `current`.

Top residuals (not blocking):
1. Pre-gate scripts (`goroutine-drain-test.sh`, `revocation-slo-test.sh`) — gate dependency.
2. Design 11 (OpenBao) still stub — cross-ref only; does not block this spec.
3. L3 NATS KV TTL cap vs. 04c JetStream config — confirm in 04c iter-3.
