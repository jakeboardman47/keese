<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: runtime
depends:
  - 02-workspace-model.md
  - 04a-openfga-authz-model.md
  - 04b-projected-sa-identity.md
  - 07-agent-runtime-spi.md
  - 08a-goose-headless-modes.md
  - 09-transport-crd.md
  - 10a-otel-topology.md
  - 13-cli-tunnel-wireguard.md
  - 18-process-lifecycle.md
  - 23-agent-supervision.md
  - 24-tenant-crd.md
related_skills: [doc-authoring, controller-authoring]
status: current
last_verified: 2026-04-21
rollback: |
  Disable interactive attach: recreate Workspace with spec.interactive: false +
  spec.resumeFrom (InteractivityImmutable blocks in-place). Revert bridge image:
  update AgentRuntime.spec.sidecars.acpBridge.image to prior digest; controller
  redeploys pods rolling. Breaking protocol: bump bridge minor; old clients receive
  UNSUPPORTED_VERSION and must upgrade. WorkspaceSession: no v1beta1 migration
  until conversion webhook exists.
---

# 08b — Goose ACP stdio over Kubernetes

## Context

Agent pods have no open ports (zero-trust, rule 05). Interactive users and CI clients
attach to a running goose `serve` session (08a) without breaking the fail-closed
NetworkPolicy. This design specifies: the ACP bridge sidecar (conditional on
`Workspace.spec.interactive`), the `WorkspaceSession` CRD (D27), named sessions +
multi-session-per-user + lazy-spawn, the `kubectl-keese attach` plugin, three-layer
auth, backpressure, session-drop cleanup, pod-failure cleanup, and compose with 13
(WireGuard). Design 09 owns `Transport.spec.type: stdio`; 18 owns drain budgets;
08b-ii owns the full CRD YAML and server-side attach sequence diagram.

## Bridge sidecar — conditional on `Workspace.spec.interactive`

Per 07 iter-2: the bridge is a sidecar container alongside goose, **only** when
`Workspace.spec.interactive: true`. Non-interactive Workspaces (WorkflowRun-driven)
run a single `goose` container; no bridge, no `emptyDir` volume.

**Bridge image:** `ghcr.io/keese-ai/keese-acp-bridge:<semver>` — independently
versioned; built from `cmd/keese-acp-bridge/`. Default digest embedded in operator
release. `AgentRuntime.spec.sidecars.acpBridge.image` allows per-runtime override.

**IPC contract:** shared `emptyDir` volume `acp-ipc` at `/var/run/keese/acp`. Goose
exposes ACP on Unix socket `/var/run/keese/acp/goose.sock`; bridge connects, multiplexes
turns to WebSocket frames. `kubectl-keese attach` runs:
`kubectl exec -it <pod> -c bridge -- /usr/local/bin/bridge` — exec WebSocket carries
ACP frames; no host-port. Non-interactive: single `goose` container, no `emptyDir`.
Reference 07 iter-2 for full topology split.

## WorkspaceSession CRD (D27)

Namespaced; `keese.ai/v1alpha1`; lives in Workspace namespace.
Full YAML + VAP rules + finalizer chain in companion `08b-ii-session-crd-spec.md`.
Name pattern: `<workspace>-<subject-hash-16>-<session-name>`. Required fields:
`workspaceRef`, `attachSubject` (OpenFGA subject), `sessionName`. Mutable:
`attachGraceSeconds` (inherits `Workspace.spec.attachGrace`), `preserveOnPodFailure`.
Status: `phase` (`Starting|Running|Idle|Draining|Terminating`), `podRef`,
`attachedClientCount`, `lastActivityAt`, `conditions[Ready]`.

## Session modes, reuse, and lazy spawn (from 02 iter-2)

`Workspace.spec.sessionMode` controls CR uniqueness:

| Mode | Uniqueness key | Notes |
|---|---|---|
| `shared` | `workspaceRef` | one pod; all clients join; sessionName cluster-default |
| `per-user` | `(workspaceRef, attachSubject, sessionName)` | second terminal on same key reuses pod; increments `attachedClientCount` |
| `per-attach` | `(workspaceRef, operator-generated-name)` | caller-provided sessionName rejected (`AttachSessionNameForbidden`) |

**Lazy spawn:** pod created only when first `WorkspaceSession` CR appears. Cold boot ~15–30 s.
**Per-user isolation:** use `--session=term-1` / `--session=term-2` for distinct pods.

## Attach CLI API

| Command | Behavior |
|---|---|
| `kubectl-keese attach <workspace>` | Session `default` for caller. Create if absent. |
| `kubectl-keese attach <workspace> --session=<name>` | Named session; create if absent. |
| `kubectl-keese attach <workspace> --session=<name> --if-not-exists=fail` | Reject if absent. |
| `kubectl-keese sessions list <workspace>` | List caller's sessions (`attachSubject == caller`). |
| `kubectl-keese sessions delete <workspace> --session=<name>` | Delete CR; triggers finalizer. |

Disconnect with `?mode=terminate` query param: delete CR immediately on drop; no grace window.

Full server-side sequence (attach webhook → OpenFGA → WorkspaceSession CR → pod → exec) is
in `08b-ii-session-crd-spec.md`.

## Three-layer auth

ALL three must pass; deny at any layer → `403 NotAuthorized` with reason code.

1. **K8s RBAC:** verb `exec` on `pods/exec`; ClusterRole `keese-workspace-attach` bound
   per-tenant via Capsule (Mode B) or direct RoleBinding. CI SAs use `-ci` variant.
2. **OIDC (D28):** JWT validated via `OIDCProvider` CR; subject → `user:<email-or-sub@domain>`
   via `subjectTemplate` (04b iter-2). Validated by attach webhook. `Tenant.spec.oidc.
   allowedProviders[]` pending 24 iter-3; cluster-wide until then.
3. **OpenFGA:** `Check(workspace:<name>#editor@<subject>)` MUST allow. Plus
   `attachPolicy` constraints: `allowedSubjects[]`, `requiredClaims`,
   `maxConcurrentSessionsPerUser`, `maxConcurrentAttaches` (VAP-counted on active CRs).

Re-checked on reconnect after `attachGrace` reset.

## Reconnect + sequence tracking

Frames carry uint64 sequence numbers. On reconnect client presents `last_acked_seq`;
bridge resumes from `last_acked_seq + 1` if within the 4 MB rolling buffer. Overflow
→ `SessionLost`; client starts new ACP session. Policy: 3 retries, 1/2/4 s backoff.

## Backpressure + buffering

| Direction | Queue depth | Full policy |
|---|---|---|
| Client → goose | 100 msgs (override: `Transport.spec.stdio.inboundQueueDepth`) | EAGAIN — write blocks; no silent drop |
| Goose → client | 1 000 msgs (override: `Transport.spec.stdio.outboundQueueDepth`) | Drop oldest + `StreamLagged` frame |

Metrics: `keese_acp_inbound_queue_depth` (gauge), `keese_acp_stream_lagged_total{direction}`.
Flag to 09 iter-1: `Transport.spec.stdio.{inboundQueueDepth, outboundQueueDepth,
reconnectBufferBytes}` authoring deferred to 09.

## Session drop cleanup + pod-failure

On WebSocket drop: (1) bridge calls `Drain(ctx, session, 15s)` — flush to PVC (18);
(2) **pause by default** — pod stays up; supervisor (23) exempt during grace; agent may
continue D25 GUPP work; (3) **reconnect during `attachGrace`** — client re-proves authz,
bridge resumes from checkpoint seq, `attachedClientCount` tracks; (4) **grace expiry** —
delete `WorkspaceSession` CR → finalizer → `Drain(ctx, session, 90s)` → pod delete →
Workspace `Idle → Ready` (scale-to-zero, 02 iter-2); (5) `?mode=terminate` — CR deleted
immediately, no grace.

**Pod failure** (`Failed` phase, crash or OOMKill): controller waits 30 s for reconnect.
`preserveOnPodFailure: false` (default): auto-delete CR. `preserveOnPodFailure: true`:
CR stays `Failed`; `SessionStuckAfterPodFailure` event; manual `kubectl delete` required.

## Out-of-cluster access via 13 (WireGuard)

13 provides WireGuard VPN for clients outside the cluster network (cloud, corporate firewall,
air-gapped). Through the tunnel, K8s API is reachable → `kubectl-keese attach` works
normally → connects to 08b's bridge. 08b is identical in both paths. 13 is OPTIONAL.

## Transport CRD integration (flag for 09 iter-1)

`Transport.spec.type: stdio` is the CRD surface. Workspace declares `spec.transportRef`.
Fields `Transport.spec.stdio.{bridgeImage, inboundQueueDepth, outboundQueueDepth,
reconnectBufferBytes}` expected in 09 spec; 09 owns field validation. 08b references but
does not author the Transport CRD.

## Failure modes

| Failure | Mitigation |
|---|---|
| Bridge OOMKilled | Resume from PVC checkpoint (18/D25); `BridgeOOMKilled` |
| OpenFGA unavailable | Fail-closed deny; `OpenFGAUnavailable`; client retries |
| Reconnect seq gap > buffer | `SessionLost`; client starts new ACP session; goose state preserved |
| PVC drain failure | Retry 3×; fallback NATS metadata; `CheckpointFailed`; Workspace stays `Running` |
| SIGTERM during grace | Grace cancelled; Drain immediately; `WorkspaceIdle` |
| Inbound queue full | EAGAIN; client backs off; alert if `keese_acp_inbound_dropped_total > 0` |
| preserveOnPodFailure=true + pod Failed | `SessionStuckAfterPodFailure`; manual delete |
| OIDCProvider degraded | `403 OIDCProviderDegraded`; fail-closed; operator fixes CR |

## Upgrade / rollback

Bridge versioned by `AgentRuntime.spec.sidecars.acpBridge.image` digest; controller
redeploys pods rolling. Mid-session clients reconnect within grace; PVC checkpoint
preserves state. Breaking protocol: bump `X-Keese-Bridge-Version`; old clients receive
`UNSUPPORTED_VERSION`. See frontmatter rollback for disable/revert paths.

## Observability

OTEL spans: `keese.acp.attach`, `keese.acp.reconnect`, `keese.acp.drain_on_drop`,
`keese.acp.session.create`, `keese.acp.pod_failure_cleanup`. Metrics:
`keese_acp_attach_total{result,workspace,mode}`, `keese_acp_reconnect_total`,
`keese_acp_inbound_queue_depth` (gauge), `keese_acp_stream_lagged_total{direction}`,
`keese_acp_session_drop_total{reason}`, `keese_workspace_session_active{tenant,mode}`.
Events (`events.go`): `ACPAttached`, `ACPDetached`, `ACPSessionLost`, `ACPGraceExpired`,
`BridgeOOMKilled`, `OpenFGAUnavailable`, `SessionStuckAfterPodFailure`.
`WorkspaceSession` printer columns: `Age`, `Ready`, `Phase`, `Subject`, `SessionName`.
Alerts: `keese_acp_stream_lagged_total > 0 for 5m` → P3; `keese_acp_inbound_dropped_total > 0` → P3.

## Refs

[02](02-workspace-model.md) · [04a](04a-openfga-authz-model.md) · [04b](04b-projected-sa-identity.md) · [07](07-agent-runtime-spi.md) · [08a](08a-goose-headless-modes.md) · [08b-ii](08b-ii-session-crd-spec.md) · [08b-iii](08b-iii-iter-log.md) · [09](09-transport-crd.md) · [10a](10a-otel-topology.md) · [13](13-cli-tunnel-wireguard.md) · [18](18-process-lifecycle.md) · [23](23-agent-supervision.md) · [24](24-tenant-crd.md) · [rubric](../plans/rubric.md) · [plan D27/D28](../plans/scaffolding-plan.md)

## Iteration log

Full rubric tables in [08b-iii](08b-iii-iter-log.md).
Iter-1 (2026-04-21): 92.5 SHIP, held draft — D27 not authored; sidecar conditional not formalized.
Iter-2 (2026-04-21): **97.5 SHIP** — 11 mandates absorbed (bridge sidecar, D27 in 08b-ii, named sessions, multi-session reuse, lazy spawn, session modes, pod-failure cleanup, 13 compose, 24 flag, Transport fields, deps expanded). Cat 4/5 gaps remain pre-gate.
