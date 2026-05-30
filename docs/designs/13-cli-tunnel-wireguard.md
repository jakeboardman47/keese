<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: developer-experience
depends: [04b-projected-sa-identity.md, 12-network-isolation.md]
related_skills: []
status: current
last_verified: 2026-04-21
rollback: Delete the TunnelSession CRD and wg-gateway Deployment; remove the keese-wg-gateway NetworkPolicy exception; delete OpenFGA tuples keyed on tunnel peer pubkeys. No migration needed — design gate not yet open.
---

# 13 — CLI Tunnel (WireGuard)

## Decision

`keesectl tunnel open` establishes a WireGuard VPN from the developer's
workstation to a shared in-cluster **wg-gateway** Deployment, authenticated
by an OIDC-issued ephemeral peer key (TTL ≤ 24h). The tunnel reaches only
NATS JetStream and Envoy AI Gateway endpoints authorised for the developer's
identity — the Kubernetes API is never exposed through the tunnel.

## Context

Developers debugging agent runs need live access to NATS topics and the ACP
attach endpoint without running multiple `kubectl port-forward` processes.
WireGuard with per-developer peer scoping satisfies this without widening the
cluster's public attack surface.

## WireGuard topology

```
Developer workstation
  keesectl tunnel open
      |
      | UDP :51820 → cloud LB → wg-gateway Service (LoadBalancer)
      |
  wg-gateway Deployment (2 replicas, PDB minAvailable=1)
      |
      | ClusterIP routes (allowedServices only)
      ├── NATS JetStream :4222
      └── Envoy AI Gateway :443
```

**wg-gateway pod:** runs a minimal WireGuard image pinned by digest. Exposes
UDP 51820 via `Service type: LoadBalancer`. The OpenTofu `networking` module
provisions the cloud LB and outputs the endpoint; `helmfile` injects it into
the wg-gateway `ConfigMap`. In `kind` (dev), runs as a single replica — kindnet
does not support UDP ECMP (see `13b-cli-tunnel-ha-ops.md`).

**IP allocation:** the TunnelSession controller (leader-elected) allocates a
`/32` from CIDR `10.224.0.0/16` via a `ResourceLock` ConfigMap. The allocated
IP is written to `TunnelSession.status.peerIP` and is stable across reconnects.

**NetworkPolicy exception:** the workspace controller writes a targeted
allow-rule for `podSelector: {keese.ai/role: wg-gateway}` to reach only the
NATS and Envoy AI Gateway ClusterIP services. All other egress is denied.

## Auth (OIDC ephemeral peer keys)

1. `keesectl tunnel open` presents the developer's OIDC `id_token` (audience
   `keese-tunnel-<tenant>`; TTL ≤ 24h; obtained via `keesectl login`) to the
   operator's TunnelSession admission webhook.
2. The webhook verifies the token against the OIDCProvider JWKS and evaluates an
   OpenFGA `tunnel:open` check. On allow, it creates a `TunnelSession` CR.
3. The TunnelSession controller generates a WireGuard keypair server-side, writes
   the peer public key into the wg-gateway `ConfigMap` (triggers `wg syncconf`),
   and returns the server public key + allocated peer IP via `status`.
4. `keesectl` configures the local WireGuard interface. The private key never
   leaves the developer's machine.

Reuses the 04b audience pattern: `keese-tunnel-<tenant>` is a named
`audienceTemplate` in the `OIDCProvider` CR with `expirationSeconds: 86400`.

**Revocation:** `keesectl tunnel close` or TTL expiry deletes the `TunnelSession`
CR. The controller removes the peer from the wg-gateway `ConfigMap` and deletes
the OpenFGA tuple `developer:<sub>` → `tunnel_peer:<session-uid>`.

## Per-developer scoping

Each `TunnelSession` records `allowedServices` derived from the caller's
OpenFGA tuples at admission time:

```yaml
status:
  peerIP: 10.224.0.42/32
  allowedServices:
    - nats.nats.svc.cluster.local:4222
    - envoy-ai-gateway.keese-system.svc.cluster.local:443
  expiresAt: "2026-04-22T14:00:00Z"
```

The wg-gateway pod enforces per-peer `PostUp` iptables rules generated from
`TunnelSession.status.allowedServices`. Peers cannot reach each other or the
Kubernetes API. Service CIDR is routed only to the specific ClusterIPs listed.

## kubeconfig avoidance (rule 05.1)

The tunnel routes TCP only to named service ClusterIPs — port 6443 is not
routed. Developers access `kubectl` via their normal kubeconfig; the tunnel is a
separate path for service-level debugging only.

## Cleanup

- `keesectl tunnel close` — explicit; deletes `TunnelSession` CR immediately.
- TTL expiry — controller reconcile loop checks `spec.expiresAt`; expired sessions
  deleted and peers removed.
- Reconnect — `keesectl tunnel open` with an existing active session returns the
  current session; if expired, creates a new one. Idempotent by `(sub, workspace)`.
- Pod restart — wg-gateway startup re-applies all active `TunnelSession` peers
  from the `ConfigMap` on becoming Ready.

## Failure modes

| Failure | Detection | Behavior | Recovery |
|---|---|---|---|
| OIDC token expired at admission | Webhook 401 | `keesectl` exits non-zero; prints `keesectl login` hint | Re-login; retry |
| OpenFGA `tunnel:open` deny | Webhook 403 | No CR created; `keesectl` prints denial | Request workspace access |
| wg-gateway pod restart | Ready probe fails | Existing peers lose connectivity; controller re-applies all active sessions on pod Ready | Auto-heals ≤30s |
| Cloud LB IP change on re-provision | Service IP drift | `keesectl tunnel open` fetches current endpoint from `status.serverEndpoint` | Re-run `keesectl tunnel open` |
| TunnelSession TTL races reconnect | Peer removed while client re-authenticates | New CR created; peer entry overwritten (same pubkey key) | Transparent |
| wg-gateway ConfigMap update lag | `wg syncconf` not yet applied | Peer traffic dropped ≤5s | WireGuard handshake retries automatically |

## Observability

Metrics (OTEL → ECK):
- `keese_tunnel_sessions_active{tenant}` — gauge
- `keese_tunnel_session_duration_seconds{tenant}` — histogram; on session close
- `keese_tunnel_auth_total{tenant, result}` — counter; `result ∈ {ok, deny, error}`

Events (`events.go` const table):
- `TunnelSessionOpened` — peer IP, developer sub
- `TunnelSessionClosed` — reason `(explicit, ttl_expired)`
- `TunnelSessionAuthDenied` — checked OpenFGA tuple
- `TunnelPeerSyncFailed` — ConfigMap update or `wg syncconf` error

OTEL trace span: `tunnel.session_open{tenant, sub, workspace}` on admission;
child span `tunnel.peer_sync{session_uid}` on ConfigMap apply.

## Refs

- [04b-projected-sa-identity.md](04b-projected-sa-identity.md) — OIDC audience pattern
- [12-network-isolation.md](12-network-isolation.md) — NetworkPolicy context
- [13b-cli-tunnel-ha-ops.md](13b-cli-tunnel-ha-ops.md) — HA, ECMP, iteration log
- [19-ide-and-debugging.md](19-ide-and-debugging.md) — consumes this design
- [../plans/rubric.md](../plans/rubric.md)
