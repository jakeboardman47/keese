<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: runtime
depends:
  - 02-workspace-model.md
  - 04b-projected-sa-identity.md
  - 07-agent-runtime-spi.md
  - 08a-goose-headless-modes.md
  - 09-transport-crd.md
  - 13-cli-tunnel-wireguard.md
  - 18-process-lifecycle.md
related_skills: [doc-authoring, controller-authoring]
status: current
last_verified: 2026-04-21
rollback: |
  Set Transport CR spec.stdio.bridgeImage to the prior digest; the workspace
  controller redeploys agent pods on the next reconcile (rolling; no downtime).
  If the session-sequence protocol changes (breaking reconnect), bump bridge
  minor version; prior clients receive UNSUPPORTED_VERSION on reconnect and must
  upgrade. No CRD migration required until v1beta1 promotion.
---

# 08b — Goose ACP stdio over Kubernetes

## Context

Agent pods have no open ports (zero-trust, rule 05). Interactive users and CI
clients must attach to a running goose `serve` session (08a) without breaking
the fail-closed NetworkPolicy. This design specifies the `kubectl-keese attach`
plugin, the in-pod `goose-acp-stdio-bridge` binary that multiplexes ACP over
the WebSocket channel that `kubectl exec` provides, client authentication,
backpressure, and session-drop cleanup. Design 09 (`Transport` CRD) owns the
`spec.type: stdio` CR that wraps this bridge; 08a owns mode selection; 18 owns
the drain budget.

## Exec invocation + reconnect

`kubectl-keese attach workspace/<name> [-n <namespace>]` resolves
`Workspace.status.podRef` and runs:

```
kubectl exec -it <pod> -c goose -- /usr/local/bin/goose-acp-stdio-bridge \
  --session-id=<Workspace.status.activeSessionRef> \
  --workspace-uid=<uid>
```

The bridge binary exposes ACP JSON-object turns over stdin/stdout. The
`kubectl exec` WebSocket channel (Kubernetes `SPDY upgrade` or WebSocket exec
on 1.29+) is the transport.

**Sequence tracking.** Each direction carries a monotonically increasing
uint64 sequence number embedded in the ACP frame envelope. The client echoes
its last received seq in a reconnect handshake header. On reconnect, the bridge
resumes from `last_acked_seq + 1` if the gap fits within the 4 MB rolling
server-side buffer; otherwise it responds with `SessionLost` and the client
must re-initiate a new session.

**Reconnect policy.** On WebSocket drop, the plugin retries 3 times with
exponential backoff (1 s, 2 s, 4 s). ACP turns are complete JSON objects; a
partially delivered object is discarded on reconnect (client re-sends the turn
from the last `turn_id` ACK). Maximum single-session reconnect gap: 5 minutes
(bridge holds state across reconnects for the full `Workspace.spec.attachGrace`
window).

## Client authentication

Three layers applied in order on every attach:

1. **K8s API auth.** `kubectl exec` requires verb `exec` on `pods/exec` in the
   agent namespace. ClusterRole `keese-workspace-attach` is bound per-tenant via
   Capsule (Mode B) or direct RoleBinding (Mode A). Principal = kubeconfig
   identity. CI service accounts use `keese-workspace-attach-ci` ClusterRole +
   namespace-scoped RoleBinding.

2. **Workspace authorization.** The bridge calls OpenFGA on first attach (and
   on every reconnect after `attachGrace` reset): `Check(workspace:<uid>#editor@user:<identity>)`.
   Deny → immediate disconnect, close code 4403 `NotAuthorized`. The subject
   form for human users is `user:<email-or-oidc-sub>`; for CI SAs it is
   `user:<sa-ns>/<sa-name>`.

3. **No SA token required for human clients.** The projected SA token pattern
   (04b) is agent-only. Human and CI clients present their kubeconfig identity
   through the K8s API server TLS channel; no additional credential is threaded
   through the exec session.

## Backpressure + buffering

| Direction | Queue depth | Full policy | Rationale |
|---|---|---|---|
| Client → server | 100 messages | `EAGAIN` — client write blocks; no silent drops | Inbound message integrity critical; agent cannot re-request |
| Server → client | 1 000 messages | Drop oldest + `StreamLagged` frame to client | Goose bursts tool-call events; clients resync from ACP session state |

Goose processes ACP turns serially; a new inbound message during an in-flight
turn is queued. `keese_acp_inbound_queue_depth` (gauge) and
`keese_acp_stream_lagged_total{direction}` (counter) are the monitoring handles.

## Transport CRD integration (09)

`Transport.spec.type: stdio` is the CRD surface for this bridge. Every
Workspace declares `spec.transportRef` pointing to a `Transport` CR. The
workspace controller reads `Transport.spec.stdio.bridgeImage` (default:
operator-bundled digest) and `Transport.spec.stdio.bufferSizeBytes` (default:
4 194 304 = 4 MB) and injects them into the agent Pod spec at reconcile time.

First-class CR chosen over an implicit default: uniform RBAC via
`transports.transport.operator.keese.ai`; VAP static validation; per-tenant
bridge image pin (air-gapped clusters). Implicit stdio was rejected: special-case
code path in the workspace controller; no per-tenant buffer tuning without
env-var injection.

**Flag for 09 iter-1.** `Transport.spec.stdio.{bridgeImage,bufferSizeBytes}`
expected in the 09 spec; 09 owns field validation. Design 13 (WireGuard / CLI
tunnel) targets clusters without `kubectl exec`; its scope does not overlap
this design; 13 must declare whether it replaces or composes with this bridge.

## Session drop cleanup

On WebSocket drop (TCP keepalive failure, `Ctrl-C`, pod eviction):

1. Bridge detects disconnect; calls `Drain(ctx, session, 15s)` — flushes to PVC (18).
2. **Default: pause.** Agent NOT terminated; session resumable for `Workspace.spec.attachGrace` (default `5m`).
3. **Reconnect during grace.** Client re-proves authz and presents session ID; bridge resumes ACP stream from checkpoint seq.
4. **Grace expiry.** Controller sends SIGTERM; `Drain(ctx, session, 90s)`; Workspace FSM `Running → Idle` (02).
5. **Immediate termination.** Client passes `?mode=terminate`; full Drain called immediately; Workspace → `Idle`.

## Trade-offs

Pause-on-drop (default): agent mid-recipe loses no work; grace window allows
CLI reconnect. 3-retry with 1/2/4 s backoff: covers transient TCP flaps
without thundering-herd. No SA token for humans: K8s API server TLS already
authenticates the kubeconfig identity — an additional credential would be
redundant and a new secret surface. Backpressure and CRD decisions are argued
inline in their sections above.

## Failure modes

| Failure | Detection | Mitigation |
|---|---|---|
| Bridge binary OOMKilled | Pod `Failed`; workspace controller detects | Resume from checkpoint (18/D25); emit `BridgeOOMKilled` |
| OpenFGA unavailable at authz check | Bridge returns 503 on check call | Fail-closed: deny attach; `OpenFGAUnavailable` event; client retries after backing off |
| Reconnect seq gap > buffer | `SessionLost` close code | Client must start new session; existing ACP session state not lost (in goose process) |
| PVC mount failure at drain | `sqlite3 .backup` error | Retry 3× (18); checkpoint to NATS metadata; log `CheckpointFailed`; Workspace stays `Running` |
| SIGTERM during attachGrace window | Grace timer cancelled | Drain called immediately; clean checkpoint; `WorkspaceIdle` emitted |
| Client sends > 100 inbound messages | Bridge returns EAGAIN | Client backs off; no silent drops; `keese_acp_inbound_dropped_total` alerted if nonzero |

## Upgrade / rollback

Bridge versioned by `Transport.spec.stdio.bridgeImage` digest; operator updates
the CR, workspace controller redeploys pods one at a time. Mid-session clients
detect the pod eviction disconnect, retry, and reconnect within the grace window;
PVC checkpoint preserves session state across the pod replacement. Breaking
protocol changes: bump `X-Keese-Bridge-Version`; old clients receive
`UNSUPPORTED_VERSION` and must upgrade the `kubectl-keese` plugin.

## Observability

OTEL spans: `keese.acp.attach`, `keese.acp.reconnect`, `keese.acp.drain_on_drop`.
Metrics: `keese_acp_attach_total{result,workspace}`, `keese_acp_reconnect_total`,
`keese_acp_inbound_queue_depth` (gauge), `keese_acp_stream_lagged_total{direction}`,
`keese_acp_session_drop_total{reason}`. Events in `events.go`: `ACPAttached`,
`ACPDetached`, `ACPSessionLost`, `ACPGraceExpired`, `BridgeOOMKilled`,
`OpenFGAUnavailable`. Alerts: `keese_acp_stream_lagged_total > 0 for 5m` → P3;
`keese_acp_inbound_dropped_total > 0` → P3.

## Refs

[02](02-workspace-model.md) · [04b](04b-projected-sa-identity.md) · [07](07-agent-runtime-spi.md) ·
[08a](08a-goose-headless-modes.md) · [09](09-transport-crd.md) · [13](13-cli-tunnel-wireguard.md) ·
[18](18-process-lifecycle.md) · [rubric](../plans/rubric.md) · [plan D8/D9/D24/D25](../plans/scaffolding-plan.md)

## Iteration log

### Iteration 1 — 2026-04-21

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | All 5 open questions answered; bounded inputs/outputs; exec invocation, authz, backpressure, CRD, drop-cleanup all explicit. |
| 2 | Architecture fit | 10 | 1.0 | 10 | D8/D9 honored; zero-trust (no open ports); SPI Drain matches 07/18; FSM transition aligns with 02. |
| 3 | Security posture | 15 | 1.0 | 15 | Three-layer auth (K8s RBAC + OpenFGA + no SA for humans); fail-closed on OpenFGA unavailable; no secrets in exec channel; EAGAIN not silent drop on inbound. |
| 4 | Automatability | 10 | 0.5 | 5 | Transport CR bridge image + buffer fields stated; workspace controller injection described; plugin build not yet scripted (pre-gate). |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Six failure modes; reconnect/seq-gap contract explicit; envtest harness not yet authored (pre-gate P8). |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Six failure modes; seq-gap, OOM, EAGAIN, PVC failure, OpenFGA unavailable, grace-window SIGTERM all covered. |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | 198 lines; single responsibility; all deps linked; no inline code reproduced. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; complete frontmatter; rollback concrete; 09 flag explicit. |
| 9 | Observability | 5 | 1.0 | 5 | OTEL spans, 5 metrics, 6 event reasons, 2 alerts named. |
| 10 | Operational readiness | 10 | 1.0 | 10 | Rolling upgrade via Transport CR digest; protocol version negotiation; mid-session reconnect during rolling update; grace window covers pod eviction. |
| | **Total** | 100 | | **92.5** | |

Verdict: **SHIP** (92.5 ≥ 90). Status: `current`.

Top gaps:
1. Cat 4 (0.5): `kubectl-keese attach` plugin build script and Transport CR `stdio` fields not yet scripted — pre-gate; 09 iter-1 must add the fields before spec authoring.
2. Cat 5 (0.5): envtest attach/reconnect harness not yet authored — authors post gate-open with `controller-author`.

Next step: Publish `Transport.spec.stdio.{bridgeImage,bufferSizeBytes}` flag to 09 iter-1. Confirm 08a `serve` mode selection references this doc on goose ACP session startup.
