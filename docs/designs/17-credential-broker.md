<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: egress
depends:
  - 04b-projected-sa-identity.md
  - 04b-ii-oidc-trust.md
  - 04c-token-revocation.md
  - 05a-envoy-ai-gateway-topology.md
  - 05b-credential-injection-patterns.md
  - 10a-otel-topology.md
  - 11-secrets-pluggable-vault.md
related_skills: []
status: current
last_verified: 2026-04-21
rollback: |
  If credential caching causes egress regression: annotate the affected gateway pod
  with `keese.ai/flush-credentials=<epoch-ms>` to force L2 cache eviction.
  If refresh goroutine leak: restart the `keese-ext-authz` Deployment; L2 rebuilds
  from live STS/vault on first request per entry. Document in
  docs/plans/migration-broker-<incident>.md before any production rollback.
---

# 17 — Credential Broker

## Context

Agent pods carry only a projected SA token (04b); upstream API keys never reach
them (rule 05.2). The `keese-ext-authz` sidecar inside each Envoy AI Gateway pod
is the credential trust boundary: it verifies the SA token, checks OpenFGA (04a),
selects a `BackendSecurityPolicy`, and injects the upstream credential. This design
defines the per-gateway-pod credential cache — its tiers, 70% TTL refresh goroutine,
95% TTL fail-closed threshold, and the concrete failure table for every named
upstream failure. Pool state machine and `least-used` persistence (05b) are resolved
here. 04c revocation flush interaction is fully specified.

## Decision

Three-tier cache; background goroutine refresh at 70% TTL; fail-closed past 95%
TTL; synchronous L2 flush on 04c revocation signal; pool cooling on 429.

## Caching tiers

| Tier | Scope | Key | Purpose | Lifetime |
|---|---|---|---|---|
| L1 per-request | In-process request context | `(tenant, workspace, upstream)` | Avoids duplicate STS calls within one logical request | Request lifetime |
| L2 per-pod | In-process map in `keese-ext-authz` sidecar | `(tenant-audience, upstream-role)` | Primary cache; avoids per-request STS RTT | 70% TTL → refresh; 95% TTL → fail-closed |
| L3 distributed | NATS KV bucket `keese-credential-cache` | `(tenant, upstream-role, version)` | Pool state coordination across pods (05b `least-used`) | Up to 5 min; opt-in |

L3 is enabled per tenant via `Tenant.spec.credentialBroker.sharedCache: true`.
Required for `least-used` pool selection (05b) where pods must coordinate counters.
Without L3, each pod maintains independent L2; `round-robin` and `spillover` need
no cross-pod state.

## 70% TTL refresh goroutine

One background goroutine per unique `(tenant-audience, upstream-role)` L2 entry:

1. **Spawn:** when a new L2 entry is written after a successful STS/vault exchange.
2. **Sleep:** until `0.70 × credential_TTL` has elapsed.
3. **Refresh:** call cloud STS / OpenBao; write fresh credential to L2 atomically.
4. **Failure:** exponential backoff (100 ms / 500 ms / 2 s / 10 s) until 95% TTL.
   Past 95%, mark entry `fail-closed`; next request receives HTTP 401 with header
   `X-Keese-Cred-Expired: true` + `X-Keese-Cred-Audience: keese-egress-<tenant>`.
5. **Eviction:** entry removal cancels the goroutine via context cancel.
6. **Limit:** 1000/pod. Above 800: warn + metric `keese_broker_refresh_goroutine_high_water`.
   Above 1000: skip proactive refresh; credential refreshes on next request.
7. **Rotation formula (D13/05b):** `oldest_cred_usable_until = max(remaining_old_TTL, 0.70 × new_TTL)`.
   Old credential NOT revoked before window elapses; in-flight streams finish cleanly.

## 95% TTL fail-closed signals

- **Event:** `CredentialExpiringFailClosed` on the `BackendSecurityPolicy` CR.
- **Metric:** `keese_broker_credential_expired_seconds_remaining{tenant,upstream}` gauge;
  reaches 0 at fail-closed. Alert fires when any entry < 5% TTL remaining (severity
  `critical` for prod namespaces).
- **Response header:** `X-Keese-Cred-Expired: true` + `X-Keese-Cred-Audience:
  keese-egress-<tenant>`. Runtime (07) observes this and emits
  `CredentialRotationExpired` event; operator controller drains the workspace pod
  and provisions a replacement.

## Failure table

| Failure | Broker action | Observable signal |
|---|---|---|
| OpenBao unreachable at refresh | Serve L2 past 70%; retry with backoff; fail-closed past 95% | Event `VaultUnreachable`; `keese_broker_vault_errors_total` |
| K8s Secret stale (ESO lag) | Serve current L2; `SecretStale` event after 5 min of stale | `keese_broker_secret_staleness_seconds{tenant}` |
| AWS STS timeout | Retry 3× jittered (100 ms / 500 ms / 2 s); fail-closed on final | `STSTimeout` event with `sts.endpoint` OTEL attribute |
| AWS STS 4xx (expired SA token) | Fetch fresh projected SA token from kubelet API; retry once | `STSAuthFailed` event |
| AWS STS 5xx | Transient; retry with backoff; mark broker `Degraded` after 3 consecutive | `STSServerError` event |
| GCP WIF exchange failure | Same retry policy as STS 5xx | `WIFExchangeFailed` event |
| Azure Entra exchange failure | Same retry policy as STS 5xx | `EntraExchangeFailed` event |
| Upstream API key rotated (ESO) | Drop L2 entry for `(tenant, upstream)` on Secret informer event | Debounced refresh; `keese_broker_l2_eviction_total{reason=rotation}` |
| 04c revocation NATS KV push | Atomic flush of matching L2 entries; deny if revoked (04a) | `CredentialRevoked` event; `keese_broker_revocation_flush_total` |
| Revocation arrives mid-refresh | Goroutine checks revocation flag before writing; aborts if set | Logged at `warn` level; L2 entry never written |
| Pool member 429 | Mark member `cooling`; select next member (`spillover` mode per 05b) | `PoolMemberCooling` event; `keese_broker_pool_cooling_count` |
| All pool members cooling | Fail-closed 429 to agent; emit `PoolExhausted` | `keese_broker_pool_exhausted_total` |
| OpenFGA down (ext_authz side) | Deny all; not broker concern — handled by 04c NATS-degraded mode | `AuthzFullyDegraded` event (04c) |

## 04c revocation flush — exact sequence

1. Controller writes NATS KV `keese-revocation-version/workspace/<uid>` (04c).
2. `keese-ext-authz` NATS watch fires on all gateway pods within < 1 s (04c SLO).
3. Sidecar atomically removes all L2 entries whose `(tenant, workspace, upstream)`
   matches the revoked workspace's tenant + workspace UID.
4. Next request: ext_authz `Check` → OpenFGA → deny (tuple removed); broker
   not consulted; 403 returned. Contributes to revocation p95 ≤ 60 s SLO.
5. Full pool flush: admin annotates
   `keese.ai/flush-all-credentials=<epoch>` on the BSP CR; broker watches
   annotation and flushes all L2 entries for that BSP.

## Pool state machine (05b resolution)

States: `active | cooling | exhausted`. `active → cooling` on 429
(`min(retry-after-header, 60 s)`, default 30 s); `cooling → active` after
cooling elapses. `active → exhausted` on 3 consecutive 5xx; reset via
`kubectl annotate bsp/<name> keese.ai/reset-pool-member=<member-index>`.
`least-used` counters in L3 NATS KV (opt-in); reset on pod restart if
L3 disabled — acceptable for `round-robin`.

## Observability

OTEL span `keese.broker.exchange` on every L2 miss with fields:
`tenant`, `upstream_role`, `exchange_type` (`sts|gcp_wif|azure_entra|static|openbao`),
`result` (`ok|timeout|auth_failed|server_error`), `cache_tier` (`l1|l2|miss`).
Flagged for 10a iter-2: add `broker.cache_tier` to `keese.rebac.check` span.

Metrics: `keese_broker_vault_errors_total{tenant,upstream}`,
`keese_broker_revocation_flush_total{tenant}`,
`keese_broker_pool_exhausted_total{tenant,bsp}`,
`keese_broker_credential_expired_seconds_remaining{tenant,upstream}` (gauge),
`keese_broker_l2_eviction_total{reason}`,
`keese_broker_refresh_goroutine_high_water` (gauge, per-pod).

Events (enumerated in `internal/controller/egress/events.go`):
`VaultUnreachable`, `SecretStale`, `STSTimeout`, `STSAuthFailed`, `STSServerError`,
`WIFExchangeFailed`, `EntraExchangeFailed`, `CredentialRevoked`,
`CredentialExpiringFailClosed`, `PoolMemberCooling`, `PoolExhausted`.

## Operational readiness

- Goroutine limit (1000/pod) prevents runaway on large tenants; alert at 800.
- L2 map protected by `sync.RWMutex`; reads do not block concurrent refreshes.
- L3 NATS KV TTL capped at 5 min; stale entries expire automatically on pod restart.
- Pod restart: L2 rebuilds from live STS/vault on first request; no warm-up. L3
  (pool `least-used` counters) preserved across restarts when opt-in.
- HA: independent L2 per pod; 04c NATS KV push reaches all pods within < 1 s.
- Resource ceiling: L2 bounded at 10,000 entries per pod (configurable via
  `Tenant.spec.credentialBroker.maxEntriesPerPod`); LRU eviction past limit.

## Trade-offs

| Option | Chosen | Rationale |
|---|---|---|
| External controller loop for refresh | No | Per-entry goroutines keep refresh local; no round-trip cost |
| Envoy credential refresh callback | No | Envoy has no proactive credential hook; 401 signal used instead |
| Redis for L2 | No | Network hop per request negates caching benefit; NATS KV for L3 only |
| Single global lock on L2 | No | Per-entry mutex (or shard map) avoids write bottleneck |
| Poll ESO secret revisions | No | Secret informer event is push-based; eliminates 5-min polling lag |

## Refs

- [04b](04b-projected-sa-identity.md) · [04b-ii](04b-ii-oidc-trust.md) — SA token TTL; OIDC trust per cloud
- [04c](04c-token-revocation.md) — NATS KV version watch; revocation flush; SLO
- [05a](05a-envoy-ai-gateway-topology.md) — ext_authz topology; decision step ordering
- [05b](05b-credential-injection-patterns.md) — pool state machine; rotation formula; BSP patterns
- [10a](10a-otel-topology.md) — OTEL pipeline; flagged: `broker.cache_tier` span field
- [11](11-secrets-pluggable-vault.md) — OpenBao path structure; ESO bridge (stub)
- [../plans/rubric.md](../plans/rubric.md)

## Iteration log

### Iteration 1 — 2026-04-21

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Goal in one sentence; five open questions answered; bounded inputs/outputs. |
| 2 | Architecture fit | 10 | 1.0 | 10 | Three-table D13 preserved; no rule violations; goroutine-per-entry fits controller-runtime lifecycle. |
| 3 | Security posture | 15 | 1.0 | 15 | Fail-closed past 95% TTL; revocation race covered; no cred in agent pod; pool member exhaustion deny. |
| 4 | Automatability | 10 | 0.5 | 5 | Flush annotation + LRU ceiling operable; `goroutine-drain-test.sh` not yet authored (pre-gate). |
| 5 | Verifiability | 15 | 1.0 | 15 | Twelve failure rows with concrete actions; retry counts; jitter values; pool state transitions named. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Mid-refresh revocation race; NATS-degraded deferred to 04c; all STS variants covered. |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | 200 lines (at limit); single responsibility; no inline YAML; skill pointers via refs. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX header; full frontmatter; depends lists 04b/04b-ii/04c/05a/05b/10a/11. |
| 9 | Observability | 5 | 1.0 | 5 | OTEL span + 6 metrics + 11 event reasons declared. |
| 10 | Operational readiness | 10 | 1.0 | 10 | Goroutine limit + LRU ceiling + HA fan-out + resource ceiling stated; rollback via flush annotation. |
| | **Total** | 100 | | **95** | |

Verdict: SHIP (95 ≥ 90 iter-1 target). Status set to `current`.

Top gaps:
1. `scripts/dev/goroutine-drain-test.sh` unimplemented — blocks gate open (pre-gate acceptable).
2. `keese_broker_refresh_goroutine_high_water` per-pod gauge requires Prometheus metric registration — implementation detail, not design gap.
3. 10a iter-2 flagged: add `broker.cache_tier` field to `keese.rebac.check` OTEL span.

Cross-deps settled: 05b pool state machine (`active|cooling|exhausted`), `least-used` L3 NATS KV, rotation formula `max(remaining_old_TTL, 0.70 × new_TTL)`. 04c flush sequence fully specified (5-step). 07 `CredentialRotationExpired` trigger confirmed via 95% header.

Cross-deps flagged: 11 (stub) — OpenBao path structure; must reach `current` before spec. L3 NATS KV 5-min TTL cap cross-references 04c JetStream bucket config — confirm in 04c iter-3.
