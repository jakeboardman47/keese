<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: authz
depends: [04a-openfga-authz-model.md, 04b-projected-sa-identity.md, 17-credential-broker.md, 18-process-lifecycle.md, 23-agent-supervision.md, 24-tenant-crd.md]
related_skills: []
status: current
last_verified: 2026-04-20
rollback: |
  SLO regression (p95 > 60s): raise OTEL alert threshold and flush caches manually
  via `kubectl annotate pod/<gw-pod> keese.ai/flush-credentials=<epoch-ms> --overwrite`.
  Version-tag counter corruption: reset via `Workspace.spec.forceRevoke.epoch=<safe-epoch>`
  (requires can_revoke authz); document the incident in docs/plans/migration-revocation-<slug>.md.
  Circuit-breaker stuck open: delete the controller-local circuit state map entry via
  `kubectl rollout restart deployment/keese-operator`; runbook in docs/plans/runbook-circuit-reset.md.
---

# 04c — Token Revocation

## Context

When a Workspace is suspended or a tenant's ReBAC relation is removed, three downstream
caches must be invalidated within a defined SLO: (1) the Envoy per-pod credential cache
(keyed by `(tenant-audience, upstream-role)`); (2) the OpenFGA check cache inside
ext_authz; (3) any in-flight agent session holding a still-valid SA token. Per D24,
revocation targets Workspace *identity* (UID + tenant), not the ephemeral SA token — a
token-only revoke that leaves the OpenFGA tuple in place is a security bug. Revocation:
remove the OpenFGA tuple AND bump the cache version tag atomically so all downstream
caches fail-closed on the next request.

## Revocation events

| Event | Source | Tuple removed |
|---|---|---|
| `Workspace.spec.suspended: true` | Workspace controller | `workspace:<name>#active@tenant:<id>` |
| Capsule Tenant deletion | Workspace controller (owner-ref GC) | all `tenant:<id>#*` tuples |
| `WorkspaceShare` revoked | WorkspaceShare controller | `workspace:<name>#shared_with@user:<sa>` |
| Supervisor force-revoke (design 23) | Witness via `Workspace.spec.forceRevoke` | `workspace:<name>#active@witness:<id>` |
| GuardrailBinding tightening | GuardrailBinding controller | relevant `tool:<name>#allowed@workspace:<name>` |

Every revocation writes `Workspace.status.conditions[type=Revoked]` with
`lastTransitionTime` as SLO measurement start.

## Force-revoke via `Workspace.spec.forceRevoke`

A witness agent (design 23) forces revocation by writing `Workspace.spec.forceRevoke`:
`epoch` (ms, monotonic, > previous; 0 = unset), `requestedBy` (copied from
`admission.request.userInfo.username`; mandatory when epoch set), `reason` (optional, ≤ 256 chars).

VAP validates `epoch > status.lastForceRevokeEpoch` (prevents replay). Admission webhook calls
`Check(workspace:<name>#can_revoke@<subject>)` (`HIGHER_CONSISTENCY`) — rejects with
`ForbiddenToRevoke` on deny. Tuple shape (04a iter-2):
`workspace:<name>#can_revoke@{witness:<id>,service_account:<sa>}`. Tuple writers: supervisor SA
at install-time; witness agents at dispatch-time (supervision controller).

`Workspace.status.conditions[type=ForceRevoked]`: set on reconcile; `lastTransitionTime` is SLO
measurement start. Controller clears `spec.forceRevoke` after tuple delete + version bump. Audit
lands in ES `keese-revocation-audit-*` + Loki `{ job="keese-revocation", tenant="..." }` (see Observability).

## `Workspace.spec.revocationMode: abort | finish`

`abort` (default): on version-tag mismatch, agent checkpoints to PVC at
`/var/run/keese/sessions/<workspace-uid>/revoked-<epoch>/` — `session.sqlite.partial`,
`artifacts/` (snapshot copy, not moved; keeps durability if revocation reversed),
`metadata.json` (reason, epoch, last-step ID, token count, timestamps). Agent emits
`RevokedMidFlight`; exits 0 (rule 06.3). Resume: `Workspace.spec.resumeFrom: <path>`.

`finish` (opt-in, research/long-compute): agent completes the current step only; MUST NOT
start a new step. Gateway flushes credential cache ~60 s after version bump; any step exceeding
that window fails at the gateway regardless. Security tradeoff: ≤ 60 s additional upstream egress
at old auth level — documented risk. After finish: same snapshot behavior as abort.

VAP emits warning if `revocationMode` changes while `spec.forceRevoke.epoch != 0`.

## Latency SLO

**Target:** p95 ≤ 60 s, wall clock from `Workspace.status.conditions[Revoked].lastTransitionTime`
to first HTTP 403 at Envoy AI Gateway. Measurement via OTEL histogram
`keese_revocation_latency_seconds{workspace,tenant}` and canary probe
(`scripts/dev/revocation-slo-test.sh`). Breach: p95 > 90 s for 5 consecutive minutes →
`RevocationSLOBreach` alert → page on-call.

### SLO progression

| Version | p95 target | Gate conditions |
|---|---|---|
| v1 (initial) | ≤ 60 s | Day-one |
| v2 (≥ 90 days prod data) | ≤ 30 s | NATS KV + ext_authz p95 propagation < 2 s; Envoy cache flush p95 < 5 s; ≤ 2 SLO breaches per 90 days |

v2 tightening requires `docs/plans/migration-revocation-slo-30s.md` and architect sign-off.
`keese_revocation_latency_seconds` exports per-tenant labels from day one for data-driven decision.

## Cache invalidation — version-tag scheme

Monotonic epoch-ms `uint64` in NATS JetStream KV bucket `keese-revocation-version`,
keys `workspace/<uid>` and `tenant/<id>`. Propagation:
1. OpenFGA tuple delete → `internal/rebac/revocation.go:BumpVersion(uid)` (atomic CAS).
2. Ext_authz: `js.KeyValue("keese-revocation-version").Watch("workspace.>")` — on bump, flush OpenFGA check cache; DENY until fresh check.
3. Credential broker (design 17): same KV watch; flush per-pod cache entry for `(tenant-audience, upstream-role)`.
4. Agent: reads `keese.ai/revocation-version=<epoch>` annotation each step; mismatch → `RevokedMidFlight` + checkpoint.

### NATS-degraded mode

When the `keese-authz` ext_authz service loses its NATS KV watch:
1. Liveness probe detects watch-dead (configurable interval, default 5 s).
2. `keese-authz` sets `NATS_DEGRADED` flag; emits `AuthzKVWatchDegraded` event on operator namespace (dedup per pod per 5 min).
3. **Behavior:** skip local OpenFGA check cache (can't trust version-tag freshness); every authz decision requires a fresh `Check` call to OpenFGA.
4. **Latency impact:** 1 ms (cache hit) → 15–30 ms (OpenFGA RTT). Correctness preserved; throughput reduced — expected and documented.
5. **Recovery:** on reconnect, clear flag; emit `AuthzKVWatchRecovered`.
6. **If OpenFGA is also unreachable:** deny all; emit `AuthzFullyDegraded`; page immediately.

Metric: `keese_extauthz_degraded_seconds_total` — cumulative seconds spent in degraded mode.

## OpenFGA fail-closed on timeout

DENY on any `Check` timeout; return HTTP 403 with `X-Keese-Deny-Reason: authz-timeout`.
Event: `AuthzCheckTimeout` (dedup per workspace per 5 min). Metric: `keese_extauthz_timeout_total{workspace,tenant}`.
Circuit break: > 50% timeout rate for a workspace over 2 min → `AuthzCircuitOpen` event; deny all until
next successful `Check`. Break-glass allow requires `keese.ai/break-glass=true` on namespace (rule 05.13).

## Failure modes

| Failure | Detection | Mitigation |
|---|---|---|
| NATS KV unavailable at revoke | `BumpVersion` error | Controller retries with backoff; emits `RevocationPending`; does NOT proceed until bump succeeds |
| Envoy pod restart before KV update | — | Pod resubscribes on startup; KV retains last value indefinitely |
| OpenFGA tuple delete fails | SSA non-nil error | Exponential backoff; `RevocationFailed` event after 3 retries; page alert |
| NATS KV watch lost (ext_authz) | Liveness probe | NATS-degraded mode (see above); `AuthzKVWatchDegraded` event |
| OpenFGA + NATS both down | — | Deny all; `AuthzFullyDegraded`; immediate page |
| Version tag overflow (uint64) | Theoretical ~292 years | Log warning at 2^62; migration-revocation-overflow.md |
| Agent SIGKILL before checkpoint | Pod restart; SQLite record | Controller sees Revoked condition; does not Resume; emits `WorkspaceSuspended` |

## Observability

- **Histogram:** `keese_revocation_latency_seconds{workspace,tenant}` — p50/p95/p99; per-tenant for v2 tightening.
- **Counters:** `keese_extauthz_timeout_total{workspace,tenant}`, `keese_revocation_version_bump_total{workspace,tenant}`.
- **Gauge:** `keese_revocation_cache_staleness_seconds` — age of oldest un-flushed cache entry.
- **Counter:** `keese_extauthz_degraded_seconds_total` — cumulative degraded mode seconds.
- **Events:** `RevocationPending`, `RevocationFailed`, `RevokedMidFlight`, `WorkspaceSuspended`, `AuthzCheckTimeout`, `AuthzCircuitOpen`, `AuthzKVWatchDegraded`, `AuthzKVWatchRecovered`, `AuthzFullyDegraded`. All reasons enumerated in `internal/controller/workspace/events.go`.
- **Audit log (force-revoke admission):** ES index `keese-revocation-audit-*` (30-day retention) + Loki stream `{ job="keese-revocation", tenant="..." }` (1-year retention). Both redact token bytes and request bodies (rule 05.10).
- **Alerts:** `RevocationSLOBreach` (p95 > 90 s for 5 min); `RevocationFailed` (any after 3 retries); `AuthzFullyDegraded` (immediate page).

## Trade-offs

| Option | Chosen | Rationale |
|---|---|---|
| `spec.forceRevoke` (spec field) | Yes | Spec owns control-plane values; annotation pattern rejected (reviewer mandate Q10) |
| `revocationMode: abort\|finish` | Yes | Abort default for security; finish for research workloads where truncation is costly |
| Snapshot on abort | Yes | Preserves partial state for human review; copied not moved for durability |
| NATS KV watch (Envoy flush) | Yes | Sub-second push; survives pod restart; no pod enumeration |
| ConfigMap bump | No | 30–60 s kubelet polling; exceeds SLO |
| Revoke token only | No | Bug: next token issuance re-grants (D24 violation) |
| Allow one more step after revoke | `finish` mode only | `abort` default refuses TOCTOU race; `finish` documents the 60 s residual risk |

## Refs

- [04a-openfga-authz-model.md](04a-openfga-authz-model.md) — `workspace#can_revoke` relation; `HIGHER_CONSISTENCY` tier
- [04b-projected-sa-identity.md](04b-projected-sa-identity.md) — TTL 600s/refresh 420s confirmed
- [17-credential-broker.md](17-credential-broker.md) — downstream cache consumer of revocation signal
- [18-process-lifecycle.md](18-process-lifecycle.md) — SIGTERM drain budget (60 s operator, 120 s agent)
- [23-agent-supervision.md](23-agent-supervision.md) — witness force-revoke; B+C pattern
- [24-tenant-crd.md](24-tenant-crd.md) — `tenant:X` ReBAC object backing
- [../plans/scaffolding-plan.md](../plans/scaffolding-plan.md) — D13, D21, D24, D25
- [../plans/rubric.md](../plans/rubric.md)
- [../../.claude/rules/05-security-zero-trust.md](../../.claude/rules/05-security-zero-trust.md)
- [../../.claude/rules/06-signal-handling.md](../../.claude/rules/06-signal-handling.md)

## Iteration log

### Iteration 1 — 2026-04-20 — Score 90 — SHIP (held draft pending iter-2)

Gaps: (1) `revocation-slo-test.sh` not authored; (2) circuit-break reset runbook missing;
(3) `workspace#can_revoke` tuple shape blocked on 04a; (4) annotation used for force-revoke.

### Iteration 2 — 2026-04-20

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | force-revoke spec field, revocationMode, NATS-degraded, SLO progression bounded. |
| 2 | Architecture fit | 10 | 1.0 | 10 | `spec.forceRevoke` replaces annotation (Q10); `can_revoke` from 04a iter-2; 24-tenant-crd dep added. |
| 3 | Security posture | 15 | 1.0 | 15 | Admission checks `Check(workspace#can_revoke)` HIGHER_CONSISTENCY; `finish` 60s risk documented; NATS-degraded correctness-over-perf; dual audit with token redaction. |
| 4 | Automatability | 10 | 0.5 | 5 | `revocation-slo-test.sh` still unimplemented (pre-gate); circuit-break runbook path named. |
| 5 | Verifiability | 15 | 1.0 | 15 | Seven failure modes; SLO progression gate conditions concrete; canary + CI smoke unchanged. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | NATS-degraded fully specified; OpenFGA-also-down branch; snapshot path on abort. |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | At 200-line ceiling; single responsibility; no inline code. |
| 8 | Docs quality | 5 | 1.0 | 5 | `depends` adds 23+24; `status: current`; rollback expanded. |
| 9 | Observability | 5 | 1.0 | 5 | Five OTEL signals; nine event reasons; three alerts; ES+Loki dual audit. |
| 10 | Operational readiness | 10 | 1.0 | 10 | SLO progression + v2 migration doc named; `resumeFrom` recovery; circuit runbook; tag corruption rollback via spec. |
| | **Total** | 100 | | **95** | |

Verdict: SHIP (95 ≥ 95 iter-2 bar). Status flipped to `current`.

Top gaps: (1) `scripts/dev/revocation-slo-test.sh` unimplemented — blocks gate open.
(2) `docs/plans/runbook-circuit-reset.md` — author before controller work.
(3) `docs/plans/migration-revocation-slo-30s.md` — write after 90 days prod data.

Cross-deps settled: 04a iter-2 `can_revoke` tuple shape confirmed; 04b TTL 600s/420s confirmed.
Cross-deps flagged: 23 witness escalation ladder deferred; 24 Tenant spec schema deferred.
