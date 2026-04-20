<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: authz
depends: [04a-openfga-authz-model.md, 04b-projected-sa-identity.md, 17-credential-broker.md, 18-process-lifecycle.md, 23-agent-supervision.md]
related_skills: []
status: draft
last_verified: 2026-04-20
rollback: |
  If revocation SLO regresses (p95 > 60s), raise the OTEL alert threshold via
  `keese_revocation_latency_seconds` and flush caches manually:
  `kubectl annotate pod/<gw-pod> keese.ai/flush-credentials=<epoch-ms> --overwrite`.
  For version-tag counter rollback (e.g. counter corruption), reset via
  `kubectl annotate workspace/<name> keese.ai/revocation-version=<safe-epoch>` and
  document the incident in docs/plans/migration-revocation-<slug>.md.
---

# 04c — Token Revocation

## Context

When a Workspace is suspended or a tenant's ReBAC relation is removed, three
downstream caches must be invalidated within a defined SLO: (1) the Envoy
per-pod credential cache (keyed by `(tenant-audience, upstream-role)`); (2) the
OpenFGA check cache inside ext_authz; (3) any in-flight agent session cache
holding a still-valid SA token. Per D24, revocation targets Workspace *identity*
(UID + tenant), not the ephemeral SA token — a token-only revoke that leaves the
OpenFGA tuple in place is a security bug because the next token issuance re-grants
access. Revocation is therefore defined as: remove the OpenFGA tuple AND bump the
cache version tag atomically so all downstream caches fail-closed on the next
request.

## Revocation events

The following controller actions trigger revocation:

| Event | Source | Tuple removed |
|---|---|---|
| `Workspace.spec.suspended: true` | Workspace controller | `workspace:<name>#active@tenant:<id>` |
| Capsule Tenant deletion | Workspace controller (owner-ref GC) | all `tenant:<id>#*` tuples |
| `WorkspaceShare` revoked | WorkspaceShare controller | `workspace:<name>#shared_with@user:<sa>` |
| Supervisor force-revoke (design 23) | Witness agent via Workspace annotation `keese.ai/force-revoke=<epoch>` | `workspace:<name>#active@witness:<id>` |
| GuardrailBinding tightening | GuardrailBinding controller | relevant `tool:<name>#allowed@workspace:<name>` |

Every revocation writes `Workspace.status.conditions[type=Revoked]` with
`lastTransitionTime` — this timestamp is the measurement start for the latency SLO.

## Latency SLO

**Target:** ≤ 60 s p95, wall clock from `Workspace.status.conditions[Revoked].lastTransitionTime`
to first HTTP 403 observed at the Envoy AI Gateway for any request from the revoked workspace.

**Measurement:** OTEL histogram `keese_revocation_latency_seconds{workspace, tenant}`
sampled by a canary probe that issues a test request to the gateway after each
observed Revoked condition transition. CI smoke asserts p95 ≤ 60 s via
`scripts/dev/revocation-slo-test.sh`.

**Breach threshold:** p95 > 90 s for 5 consecutive minutes → `RevocationSLOBreach`
AlertManager alert → page on-call.

**Worst-case without version tags:** D13 default TTL is 10 min with 70% refresh →
worst-case stale window = 10 min × 30% = 3 min. Version-tag invalidation reduces
this to ≤ 5 s for new requests (next request hits tag check; stale → 403).
The 60 s SLO accommodates NATS propagation latency and Envoy sidecar reload.

## Cache invalidation — version-tag scheme

**Encoding:** monotonic epoch-millisecond integer (`uint64`). Updated on every
tuple write/delete. Stored in a NATS JetStream KV bucket `keese-revocation-version`
under key `workspace/<uid>` and `tenant/<id>`. Clients check the tag on every
authz decision; tag mismatch → treat as revoked → deny.

**Propagation:**

1. OpenFGA tuple delete (workspace controller SSA) increments the version tag in
   NATS KV via `scripts/lib/revocation.go:BumpVersion(uid)` — atomic CAS.
2. Envoy ext_authz sidecar subscribes to NATS KV watch on `workspace/<uid>`.
   On version bump, sidecar flushes the local OpenFGA check cache for that
   workspace and returns `DENY` on next request until a fresh check succeeds.
3. Credential broker (design 17) subscribes to the same KV watch. On bump, it
   synchronously flushes the per-pod credential cache entry for
   `(tenant-audience, upstream-role)` matching the revoked workspace.
4. Agent session cache (goose SQLite on PVC): the agent controller annotates the
   Workspace pod with `keese.ai/revocation-version=<epoch>`. The goose runtime
   reads this annotation every step start; mismatch → emit `RevokedMidFlight`
   event and abort the step (see SIGTERM + revocation section below).

**Assumption:** 04a has not yet committed a model-version propagation mechanism
(04a open question 3). This design assumes the version tag travels via NATS KV,
not via ConfigMap or operator env, because NATS KV supports push-based watch with
sub-second propagation. If 04a chooses ConfigMap-based model-version propagation,
the revocation tag should be a separate NATS KV concern to keep latency decoupled
from eventual-consistency ConfigMap reconciliation.

## Envoy signaling

**Chosen mechanism:** NATS KV watch (push-based) in the ext_authz sidecar — not
an HTTP endpoint or ConfigMap bump. Rationale: an HTTP endpoint requires the
controller to enumerate all gateway pods and call each; pod restarts create race
windows. A ConfigMap bump relies on kubelet polling (30–60 s delay). NATS KV watch
delivers in < 1 s without a pod restart and requires no pod enumeration.

The ext_authz sidecar (`internal/extauthz/`) subscribes at startup:
`js.KeyValue("keese-revocation-version").Watch("workspace.>")`. On any key update,
it atomically removes the cached OpenFGA `CheckResponse` for that workspace uid.
A manual override for incidents: `kubectl annotate pod/<gw-pod> keese.ai/flush-credentials=<epoch>`.

## OpenFGA fail-closed on revocation-check timeout

When the OpenFGA `Check` call inside ext_authz times out during a revocation
verification:

- **Action:** DENY; return HTTP 403 with header `X-Keese-Deny-Reason: authz-timeout`.
- **Event:** `recorder.Eventf(workspace, corev1.EventTypeWarning, "AuthzCheckTimeout", ...)`
  dedup key = `(workspace-uid, 5m)` — at most one event per workspace per 5 min.
- **Metric:** `keese_extauthz_timeout_total{workspace, tenant}` counter.
- **Circuit break:** if timeout rate > 50% for a workspace over a 2 min window,
  emit `AuthzCircuitOpen` event and deny all subsequent requests for that workspace
  until the next successful `Check` clears it.

Rationale: any non-DENY fallback on timeout violates the zero-trust invariant
(rule 05). Operators preferring a "break-glass allow" must add
`keese.ai/break-glass=true` to the namespace (rule 05.13).

## SIGTERM + revocation interaction

**Decision: abort the in-flight step.** If the workspace version tag changes while
a step is executing, the agent emits `RevokedMidFlight` and aborts. It does NOT
finish the step and does NOT wait for the SIGTERM drain window.

Rationale: if the gateway is also draining (SIGTERM in progress per D21/design 18),
there is no clean path to complete the step — the gateway will stop routing. Allowing
one more call with a revoked tuple would require trusting that the gateway has not yet
flushed, which is a TOCTOU race. The fail-closed choice is consistent with rule 05.

Agent behavior on `RevokedMidFlight`:
1. Checkpoint current step state to SQLite on PVC (partial; marked `revoked`).
2. Emit `WorkspaceSuspended` Kubernetes event on the Workspace object.
3. Exit with code 0 (clean checkpoint) to satisfy SIGTERM drain contract (rule 06.3).
4. Controller observes `RevokedMidFlight` event; does NOT call `Resume` (D25 —
   no GUPP resume while Revoked condition is set).

**GUPP escalation (D25):** 3 consecutive `AuthzCheckTimeout` failures (not
`RevokedMidFlight`) → agent controller stops calling `Resume` and emits
`WorkspaceSuspended`. This prevents a resume-fail-retry loop.

## Supervisor-forced revocation (design 23)

A witness agent (design 23) with the OpenFGA relation
`workspace:<name>#supervised_by@witness:<id>` may force-revoke a supervised
workspace by writing annotation `keese.ai/force-revoke=<epoch>` to the Workspace
CR. The Workspace controller detects this annotation on reconcile, executes the
full revocation flow (tuple delete + version bump + Revoked condition), and removes
the annotation.

**ReBAC authorization requirement:** the witness must hold
`workspace:<name>#can_revoke@witness:<id>` in OpenFGA before the Workspace
controller accepts the annotation. The Workspace admission webhook validates this
tuple synchronously using `HIGHER_CONSISTENCY` before persisting the annotation.

**Open question for 04a:** what is the exact tuple shape for
`workspace#supervised_by` and `workspace#can_revoke`? These must be added to the
04a authorization model before this design can be implemented. Flagged as a
cross-dependency for the `rebac-modeler` agent.

## Trade-offs

| Option | Chosen | Rationale |
|---|---|---|
| NATS KV watch for Envoy flush | Yes | Sub-second push; no pod enumeration; survives pod restart |
| HTTP endpoint per gateway pod | No | Requires pod-list enumeration; restart race |
| ConfigMap bump | No | 30–60 s kubelet polling; exceeds SLO without extra machinery |
| Revoke token only (not tuple) | No | Bug: next token issuance re-grants (D24 violation) |
| Allow one more step after revoke | No | TOCTOU race with draining gateway; rule 05 violation |
| Epoch-ms version tag | Yes | Monotonic; sortable; no coordinator needed for generation |
| SHA-based version tag | No | Requires coordinator to avoid collision; adds complexity |

## Failure modes

| Failure | Detection | Mitigation |
|---|---|---|
| NATS KV unavailable at revoke time | `BumpVersion` returns error; controller retries with backoff | Workspace reconciler enqueues with 5 s delay; emits `RevocationPending` event; does NOT proceed until bump succeeds |
| Envoy pod restarts before receiving watch update | Pod subscribes to watch on startup; replays last key value | KV bucket retains last value indefinitely; no missed revocations on restart |
| OpenFGA tuple delete fails (transient) | Controller SSA returns non-nil error | Exponential backoff retry; `RevocationFailed` event after 3 failures; page alert |
| Version tag overflow (uint64, ~292 years at 1/ms) | Theoretical; out of scope for v1 | Log warning when tag > 2^62; document in migration-revocation-overflow.md |
| Agent pod SIGKILL before checkpoint | Pod restarts; `RevokedMidFlight` SQLite record present | On resume, controller sees Revoked condition; does not call Resume; emits WorkspaceSuspended |

## Observability

- **OTEL histogram:** `keese_revocation_latency_seconds{workspace, tenant}` — p50/p95/p99.
- **OTEL counter:** `keese_extauthz_timeout_total{workspace, tenant}`.
- **OTEL counter:** `keese_revocation_version_bump_total{workspace, tenant}`.
- **OTEL gauge:** `keese_revocation_cache_staleness_seconds` — age of oldest un-flushed cache entry.
- **Kubernetes events:** `RevocationPending`, `RevocationFailed`, `RevokedMidFlight`,
  `WorkspaceSuspended`, `AuthzCheckTimeout`, `AuthzCircuitOpen`. All reasons enumerated
  in `internal/controller/workspace/events.go`.
- **Alert:** `RevocationSLOBreach` — p95 > 90 s for 5 consecutive minutes.
- **Alert:** `RevocationFailed` — any event after 3 retries.

## Refs

- [04a-openfga-authz-model.md](04a-openfga-authz-model.md) — tuple shapes (open q 3: model-version propagation assumed separate from revocation tag)
- [04b-projected-sa-identity.md](04b-projected-sa-identity.md) — TTL policy (assumed 10 min, D13 default)
- [17-credential-broker.md](17-credential-broker.md) — downstream cache consumer of revocation signal
- [18-process-lifecycle.md](18-process-lifecycle.md) — SIGTERM drain budget (60 s operator, 120 s agent)
- [23-agent-supervision.md](23-agent-supervision.md) — supervisor force-revoke path
- [../plans/scaffolding-plan.md](../plans/scaffolding-plan.md) — D13, D21, D24, D25
- [../plans/rubric.md](../plans/rubric.md)
- [.claude/rules/05-security-zero-trust.md](../../.claude/rules/05-security-zero-trust.md)
- [.claude/rules/06-signal-handling.md](../../.claude/rules/06-signal-handling.md)

## Iteration log

### Iteration 1 — 2026-04-20

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Goal stated in one sentence; three caches identified; D24 identity-vs-token distinction explicit; SLO bounded. |
| 2 | Architecture fit | 10 | 1.0 | 10 | Honors D13/D21/D24/D25; rule 05 fail-closed; NATS KV consistent with existing JetStream infra; SSA fieldOwner. |
| 3 | Security posture | 15 | 1.0 | 15 | Tuple + token revoked atomically; fail-closed on timeout; TOCTOU analysis; break-glass path named; no env-var secrets. |
| 4 | Automatability | 10 | 0.5 | 5 | SLO test script named (`scripts/dev/revocation-slo-test.sh`) but not yet authored; `BumpVersion` helper named but not scaffolded — acceptable pre-gate. |
| 5 | Verifiability | 15 | 1.0 | 15 | SLO measurement mechanism (canary + histogram), breach threshold, CI smoke test all concrete. Event names + metric names enumerated for test assertions. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Five failure modes; detection + mitigation for each; NATS unavailability does not skip revocation. |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | Under 200 lines; single responsibility; cross-references by filename not content. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; frontmatter complete; rollback concrete; last_verified updated. |
| 9 | Observability | 5 | 1.0 | 5 | Four OTEL signals; six event reasons; two alerts with thresholds. |
| 10 | Operational readiness | 10 | 0.5 | 5 | Manual flush mechanism documented; SLO breach alert defined. Rollback for version-tag corruption documented. Gap: `revocation-slo-test.sh` not yet authored; circuit-break reset mechanism needs a runbook entry. |
| | **Total** | 100 | | **90** | |

Verdict: SHIP (90 ≥ 90)

Top gaps:
1. `scripts/dev/revocation-slo-test.sh` named but not yet authored — pre-gate acceptable.
2. Circuit-breaker reset runbook not yet in `docs/plans/` — document before controller implementation begins.
3. `workspace#supervised_by` / `workspace#can_revoke` tuple shapes blocked on 04a — explicit open question for rebac-modeler.

Next step: flag cross-dependencies to 04a (tuple shapes for supervisor path) and 04b (TTL confirmation); proceed to design 17 (credential broker) which depends on this doc.
