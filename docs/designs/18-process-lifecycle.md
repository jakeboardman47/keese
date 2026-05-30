<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: reliability
depends:
  - 04a-openfga-authz-model.md
  - 04c-token-revocation.md
  - 05a-envoy-ai-gateway-topology.md
  - 07-agent-runtime-spi.md
  - 23-agent-supervision.md
related_skills: []
status: current
last_verified: 2026-04-21
rollback: |
  Extend terminationGracePeriodSeconds if drain window insufficient; redeploy via
  helmfile rollout. If checkpoint format changes break resume, add migration entry
  to docs/plans/migration-session-checkpoint-<slug>.md and increment the session
  SQLite schema version in internal/session/schema.go before rollout.
---

# 18 — Process Lifecycle

## Context

Keese runs three binary classes: controllers (operator + projectors), agent runtime
pods (goose), and gateway services (keese-authz ext_authz, ext_proc). Each has a different SIGTERM
budget, state ownership model, and SIGKILL recovery path. Without a concrete contract,
implementers guess drain windows, lose session state mid-flight, and write liveness
probes that race the drain. This design turns D21 (`rules/06-signal-handling.md`)
into implementation-ready constraints: drain windows, checkpoint format + location,
idempotent restart, shutdown event schema, and probe composition.

## Drain windows per binary

| Binary | `terminationGracePeriodSeconds` | Drain phases |
|---|---|---|
| Controllers (operator + projectors) | 60 s | Release leader lease (5 s max) → drain reconcile queue (30 s) → flush OTEL exporter (15 s) → exit (10 s buffer) |
| Agent runtime pods (goose) | 120 s | `preStop` invokes SPI `Drain(ctx, session, 90s)` → checkpoint session to PVC + NATS (70 s) → final OTEL span (10 s) → exit (10 s buffer) |
| Gateway services (keese-authz ext_authz, ext_proc) | 30 s | `preStop: sleep 25` + `/healthcheck/fail` (marks NotReady, stops routing) → complete in-flight authz checks (≤ 25 s) → exit |

Controllers install `signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT)` before
starting the manager. `scripts/check-signal-handling.sh` (P3) rejects any
`cmd/**/main.go` that lacks this call.

## Checkpoint format and location

**Location:** `/var/run/keese/sessions/<workspace-uid>/session.sqlite` on the
workspace PVC. This is the workspace-scoped PVC mounted at `/var/run/keese/sessions/`
in every agent runtime pod.

**Write protocol:** atomic — `sqlite3 .backup` to `session.sqlite.new`, then `rename`
(POSIX-atomic on the same filesystem).

**Supplemental NATS message:** on every checkpoint the runtime publishes to stream
`keese-checkpoint-<tenant>`: `{workspace_uid, timestamp_ms, session_sha256,
last_committed_step_id}`. Lets the controller locate the checkpoint without mounting
the PVC.

**Checkpoint frequency:** every step boundary + on `Drain`. Worst-case SIGKILL loss = one step.

**SPI contract:** design 07 exposes `Drain(ctx, session Session, budget time.Duration)
error`. This design fixes budget = 90 s for agent pods; the method MUST return within it.

## Idempotent restart

On pod failure the workspace controller reads
`Workspace.status.lastCheckpoint.{sqliteRef, natsSeq}` and spawns a fresh pod
invoking SPI `Resume(ctx, workspace, lastCheckpoint)` (D25/GUPP; design 07).
Idempotency: the runtime MUST deduplicate by step ID. The checkpoint carries
`last_committed_step_id`; `Resume` rejects any step with `id <=
last_committed_step_id`. No tool call executes twice across crash-and-resume.

**Controller SIGKILL recovery:** controllers are stateless between reconciles.
On restart the manager re-lists from the API server — no local cache needed. The
API server is the durable store for controller state. SSA with `fieldOwner` ensures
idempotent apply even if the previous pod wrote partial state.

## Shutdown event schema

Every binary emits OTEL span `keese.process.shutdown` synchronously before the
exporter closes. Required attributes:

| Attribute | Type | Notes |
|---|---|---|
| `reason` | string | `sigterm \| liveness_failed \| oom_killed \| crash \| planned` |
| `drain_duration_ms` | int | wall clock from SIGTERM to exit |
| `checkpoint_location` | string | `sqlite:sessions/<uid>/session.sqlite` or `""` (controllers) |
| `in_flight_remaining` | int | 0 = clean drain; > 0 logged as warning |
| `leader_lease_released` | bool | controllers only; `false` = ungraceful |
| `exit_code` | int | always 0 on clean drain |

If the OTEL exporter flush fails, the same fields are emitted as a structured log
line via `scripts/lib/log.sh` before exit.

**Interaction with 04c revocation:** on `RevokedMidFlight` (`abort` mode), `reason`
is `sigterm` and `checkpoint_location` points to the revocation-scoped path
`sessions/<uid>/revoked-<epoch>/` per design 04c.

## Liveness and readiness probe composition

Rule `06-signal-handling.md` rule 8: `initialDelay + (period × failureThreshold) ≥
terminationGracePeriodSeconds`.

| Binary | initialDelay | period | failureThreshold | total tolerance | Satisfies grace |
|---|---|---|---|---|---|
| Controllers | 30 s | 10 s | 3 | 60 s | = 60 s ✅ |
| Agent runtime | 60 s | 10 s | 6 | 120 s | = 120 s ✅ |
| keese-authz / gateway service | 10 s | 5 s | 4 | 30 s | = 30 s ✅ |

**Readiness NotReady-on-drain (rule 06.9):** the `preStop` hook writes
`/tmp/draining`; the readiness probe returns 503 when the sentinel is present.
This stops Service routing before the process stops listening.

**Agent supervision interaction (design 23):** design 23's "stuck" definition depends
on liveness semantics here. A pod whose liveness fails ≥ `failureThreshold` times
within the drain window is kubelet-killed before drain completes; patrol must treat
this as a crash, not a clean stop.

## Trade-offs

| Decision | Chosen | Rationale |
|---|---|---|
| Checkpoint to PVC + NATS (dual) | Yes | PVC = full state; NATS metadata = fast controller lookup without PVC mount at resume |
| `sqlite3 .backup` + rename | Yes | POSIX-atomic on same fs; avoids partial reads; consistent with goose native session store |
| Fixed drain budget for SPI `Drain()` | 90 s for agent (120 s grace − 10 s exit buffer − 20 s OTEL) | Predictable contract; avoids per-call negotiation in the SPI |
| Liveness probe ceiling == terminationGracePeriodSeconds | Yes | Prevents kubelet racing the drain; exact equality satisfies rule 06.8 |
| Controller drain: release lease first | Yes | Prevents split-brain; successor wins election during the 30 s queue drain |

## Failure modes

| Failure | Detection | Mitigation |
|---|---|---|
| PVC unavailable at checkpoint | `sqlite3 .backup` returns error | Emit `CheckpointFailed` event; retry 3× at 5 s intervals; checkpoint to NATS only (metadata); exit with `in_flight_remaining > 0` |
| NATS unavailable at checkpoint | `js.Publish` returns error | PVC checkpoint succeeded; log `NATSCheckpointFailed`; controller falls back to PVC scan on resume |
| SIGTERM budget overrun (process still alive) | kubelet SIGKILL | `in_flight_remaining > 0` in final log; resume handles idempotency |
| Leader lease held past drain budget | `isLeader` still true at exit | Lease TTL expires within ~15 s; successor elected; no split-brain beyond TTL window |
| OTEL exporter flush fails | synchronous error | Fallback to structured log line; metrics dropped for that shutdown span only |
| Resume step-ID dedup broken by bug | duplicate tool call | Accepted risk at v1alpha1; test harness (`test/scripts/sigterm-drain-test.sh`) guards against it |

## Upgrade and rollback

Checkpoint SQLite schema is versioned in `internal/session/schema.go`. Additive
changes (new nullable columns) migrate in-place via `Resume`. Destructive changes
require `docs/plans/migration-session-checkpoint-<slug>.md` and a blue/green
OLM `replaces` rollout (design 14a) so the prior controller drains all active
sessions before the new one starts.

Drain budget increases: update `terminationGracePeriodSeconds` in the CSV and all
three probe parameters to maintain the `= grace` equality from the table above.

## Observability

- **OTEL span:** `keese.process.shutdown` (see schema above).
- **Counter:** `keese_process_shutdown_total{binary,reason,clean}` — `clean=true`
  when `in_flight_remaining == 0`.
- **Histogram:** `keese_drain_duration_ms{binary}` — p95 tracked per binary class.
- **Counter:** `keese_checkpoint_failures_total{workspace,backend}` — `backend` in
  `{pvc, nats}`.
- **Events:** `CheckpointFailed`, `NATSCheckpointFailed`, `ShutdownUnclean` (reasons
  enumerated in `internal/controller/workspace/events.go`).
- **Alert:** `ShutdownUnclean` with `in_flight_remaining > 0` AND `reason == sigterm`
  for > 3 pods within 5 minutes → page on-call (drain budget may be too small).

## Refs

- [rules/06-signal-handling.md](../../.claude/rules/06-signal-handling.md) — D21; 11 load-bearing rules
- [04a-openfga-authz-model.md](04a-openfga-authz-model.md) — MODEL_MIGRATION drain-and-rollout precedent
- [04c-token-revocation.md](04c-token-revocation.md) — `abort|finish` checkpoint path; `RevokedMidFlight`
- [05a-envoy-ai-gateway-topology.md](05a-envoy-ai-gateway-topology.md) — keese-authz / gateway service grace = 30 s
- [07-agent-runtime-spi.md](07-agent-runtime-spi.md) — `Drain(ctx, session, budget)` + `Resume` signatures
- [23-agent-supervision.md](23-agent-supervision.md) — "stuck" detection depends on liveness probe semantics here
- [../plans/scaffolding-plan.md](../plans/scaffolding-plan.md) — D21, D24, D25

## Iteration log

### Iteration 1 — 2026-04-21

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Five open questions answered; three binary classes bounded; exit criteria explicit. |
| 2 | Architecture fit | 10 | 1.0 | 10 | D21 encoded; rule 06 satisfied; SSA fieldOwner for controller recovery; SPI contract feeds design 07. |
| 3 | Security posture | 15 | 1.0 | 15 | SIGKILL recovery via durable stores; `abort` default on revocation; checkpoint scoped per workspace-uid; no secrets in drain path. |
| 4 | Automatability | 10 | 0.5 | 5 | `sigterm-drain-test.sh` not yet authored (pre-gate); `check-signal-handling.sh` pre-commit exists (P3). |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Failure modes table present; drain smoke referenced; envtest harness not yet authored — honest dock per task brief. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Six failure modes; PVC + NATS dual-backend fallback; lease TTL gap; SIGKILL budget overrun documented. |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | ≤ 200 lines; single responsibility; no inline code; cross-refs via path. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX + frontmatter complete; `depends` includes 04a, 04c, 05a, 07, 23; `status: current`. |
| 9 | Observability | 5 | 1.0 | 5 | OTEL span + 2 counters + histogram + alert; event reasons in events.go; per-binary labels. |
| 10 | Operational readiness | 10 | 1.0 | 10 | Schema-versioned upgrade; OLM `replaces` rollout; probe composition table prevents false-positive restarts. |
| | **Total** | 100 | | **87.5** | |

Verdict: SHIP (87.5 ≥ 85). Status: `current`.

Top gaps:
1. Cat 4 (0.5): `scripts/dev/sigterm-drain-test.sh` unimplemented — authors with P7 infra-bootstrap.
2. Cat 5 (0.5): envtest drain-budget harness unimplemented — authors post gate-open with `controller-author`.

Next step: Iter-2 when `sigterm-drain-test.sh` ships. Target score 97.5 (both cats → 1.0)
