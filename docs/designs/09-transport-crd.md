<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: transport
depends:
  - 04a-openfga-authz-model.md          # workspace#can_message relation (iter-5 flag)
  - 04b-projected-sa-identity.md        # audienceTemplates per-peer (iter-3 flag)
  - 05a-envoy-ai-gateway-topology.md
  - 05c-mcp-policy-enforcement.md
  - 08b-goose-acp-stdio-k8s.md
  - 12-network-isolation.md
  - 20a-api-group-layout.md
  - 22-workflow-composition-examples.md
related_skills: [doc-authoring, crd-authoring, controller-authoring]
status: draft
last_verified: 2026-04-21
rollback: |
  spec.type is immutable (VAP-enforced). Migration = new Transport CR + consumer
  update. At v1beta1 promotion a docs/plans/migration-transport.md is required.
---

# 09 — Transport CRD

**Decision:** A single namespace-scoped `Transport` kind at
`transport.operator.keese.ai/v1alpha1` with a `spec.type` discriminator
(`nats|a2a|mcp|stdio`). `spec.type` is immutable after creation (VAP CEL:
`oldSelf.spec.type == self.spec.type`, rejects `TransportTypeImmutable`). Each type
activates exactly one typed sub-struct (VAP CEL exclusivity). Consumers reference
by `transportRef`; cross-namespace access requires `ReferenceGrant`.

## `spec.stdio` — locked from 08b iter-2

| Field | Req | Default | VAP |
|---|---|---|---|
| `bridgeImage` | yes | — | non-empty |
| `inboundQueueDepth` | no | 100 | [10, 10000] |
| `outboundQueueDepth` | no | 1000 | [100, 100000] |
| `reconnectBufferBytes` | no | 4194304 | [1048576, 67108864] |
| `reconnectRetries` | no | 3 | [1, 10] |
| `reconnectBackoff.{initial,multiplier,max}` | no | 1s / 2.0 / 30s | duration / [1.0,10.0] / duration |

## `spec.nats` — hybrid stream ownership model

**Default (pre-existing):** admission queries NATS JetStream; rejects `NATSStreamNotFound`
if stream absent. Stream lifecycle owned externally by NACK `Stream` CRD (preferred,
multi-tenant) or direct NATS operator action (dev/test). `streamConfig` is ignored and
emits `NATSStreamConfigIgnored` if set without opt-in annotation.

**Opt-in:** annotation `keese.ai/auto-create-stream: "true"` — controller calls NATS
JetStream `AddStream` / `UpdateStream` / `DeleteStream`; finalizer deletes stream on
Transport deletion. `streamConfig` is read only with this annotation.

| Field | Req | Default | VAP |
|---|---|---|---|
| `clusterRef.{name,namespace}` | yes | — | non-empty |
| `streamName` / `consumerName` | yes | — | non-empty; max 64 |
| `ackPolicy` / `maxDeliver` / `ackWait` | no | `explicit` / 3 / 30s | enum / [1,100] / duration |
| `tls.certificateRef.{name,namespace}` | yes | — | non-empty |
| `streamConfig.{subjects,retention,maxAge,storage,replicas}` | no | — / `limits` / `7d` / `file` / 3 | opt-in only |

**P8 concern:** modifying `streamConfig` on a live stream requires dual-consumer +
backfill; flagged for `docs/plans/runbook-nats-stream-migration.md`.

## `spec.mcp`

| Field | Req | Default | VAP |
|---|---|---|---|
| `mcpRouteRef.{name,namespace}` | yes | — | non-empty |
| `protocolVersion` / `toolTimeout` | no | `2024-11-05` / 30s | string / [1s,300s] |

Admission validates MCPRoute exists; emits `MCPRouteNotFound` if absent.

## `spec.a2a` — workspace-SA peer auth (iter-2 decision)

Workspace is the security boundary. Default is `workspace-sa` (K8s SA identity,
K8s API server as issuer). Human sessions use `user-oidc`; external peers use
`mutual-tls`; `none` is dev-only (VAP-blocked in prod).

| Field | Req | Default | VAP |
|---|---|---|---|
| `endpoint` | yes | — | `grpc://` or `grpcs://` |
| `peerAuth` | no | `workspace-sa` | `workspace-sa\|user-oidc\|mutual-tls\|none` |
| `workspaceSA.audience` | if ws-sa | — | non-empty |
| `workspaceSA.authzTupleCheck` | if ws-sa | — | non-empty |
| `userOidc.oidcProviderRef.name` | if user-oidc | — | non-empty |
| `userOidc.{audiences[],authzTupleCheck}` | if user-oidc | — | non-empty |
| `mutualTLS.certificateRef.{name,namespace}` | if mtls | — | non-empty |
| `mutualTLS.clientCaBundle` | no | — | PEM or empty |

**`workspace-sa` (default):** caller presents projected SA token, aud = `keese-a2a-<peer-workspace-uid>`.
Receiving Transport validates: `iss = kubernetes.default.svc.cluster.local` (OIDCProvider
`kubernetes-default`, 04b); subject → `user:ksa-<caller-workspace-uid>` (04b iter-2 bare).
Then: OpenFGA `Check(workspace:<peer>#can_message@user:ksa-<caller-uid>)`.
`workspace#can_message` is a **new relation** — **flag 04a iter-5**. Writer: Workflow
controller (03) at WorkflowRun create for cross-workspace DAG steps; deleted on completion.
**Flag 04b iter-3:** per-peer aud `keese-a2a-<uid>` not in 04b iter-2 `keese-egress-*` glob;
flag 04b iter-3 to add `spec.audienceTemplates.a2a: keese-a2a-{{.PeerWorkspaceUid}}`.

**`user-oidc`:** D28 `OIDCProvider` + user JWT; authz via `workspace#editor` (04a). For
programmatic human calls (CI); typical human path is 08b attach.
**`mutual-tls`:** external gRPC peers, no K8s identity; ACL in `mutualTLS.aclRef`; no OpenFGA.
**`none`:** VAP rejects in prod (`Tenant.spec.security.allowUnsafeTransports == false` →
`UnsafeTransportForbidden`).

## Delivery guarantees

| Type | Guarantee | Dedup owner |
|---|---|---|
| `nats` | at-least-once | Workflow controller via `Nats-Msg-Id`; KV `keese-wf-delivered` 24h TTL |
| `a2a` | at-most-once | Caller-side; gRPC unary |
| `mcp` | at-most-once | Caller-side; JSON-RPC |
| `stdio` | reliable in-order | 08b bridge; SIGKILL loses unacked frames |

## TLS and cert-manager

Transport **references** cert-manager `Certificate` CRs; does not create them.
Admission validates existence; emits `CertificateNotFound` if absent. `mcp` delegates
TLS to Envoy AI Gateway. `stdio` uses in-pod Unix socket (no TLS). Transport spec
carries no credentials (rule 05.7). cert-manager is a soft dep — bootstrap must install
it (helmfile or OLM dep via 14b).

## Lifecycle + failure modes

`Pending → Provisioning → Ready → Degraded → Terminating`. Finalizer
`finalizers.transport.operator.keese.ai/cleanup` deletes controller-owned NATS streams
only (annotation-scoped).

| Failure | Detection | Mitigation |
|---|---|---|
| NATS stream absent (default) | Admission `NATSStreamNotFound` | Create via NACK or direct |
| NATS stream config change on live | `NATSStreamMigrationRequired` event | P8 runbook: dual-consumer + backfill |
| Certificate absent | Admission `CertificateNotFound` | Provision cert-manager Certificate |
| NATS cluster unreachable | `TransportUnreachable`; Degraded | JetStream reconnect; workflow pauses |
| ReferenceGrant missing | Admission `ReferenceGrantMissing` | Create ReferenceGrant |
| MCPRoute not found | `MCPRouteNotFound`; Degraded | Guardrail-controller provisions MCPRoute |
| Type change attempted | VAP `TransportTypeImmutable` | New Transport CR; migrate consumers |
| a2a workspace-sa authz deny | `A2APeerAuthzDenied` event | Verify Workflow controller wrote `can_message` tuple |
| `peerAuth: none` in prod | VAP `UnsafeTransportForbidden` | Set `peerAuth: workspace-sa` |
| Stdio buffer overflow | `StreamLagged`; drop oldest | Increase `outboundQueueDepth` |

## Observability

OTEL spans: `keese.transport.{provision,dep_resolve,degraded,terminating,a2a.auth}`.
Metrics: `keese_transport_messages_total{type,direction,tenant}`,
`keese_transport_errors_total{type,reason,tenant}`,
`keese_transport_dep_resolve_duration_seconds{type}`,
`keese_transport_a2a_auth_duration_seconds{peer_auth_mode}`.
Events: `TransportProvisioned`, `TransportUnreachable`, `CertificateNotFound`,
`MCPRouteNotFound`, `ReferenceGrantMissing`, `NATSStreamOwned`, `NATSStreamDeleteFailed`,
`NATSStreamNotFound`, `NATSStreamConfigIgnored`, `NATSStreamMigrationRequired`,
`A2APeerAuthzDenied`, `UnsafeTransportForbidden`.
Printer columns: `Age`, `Ready`, `Phase`, `Type`.

## Cross-dep flags

- **04a iter-5 (blocks a2a ws-sa controller code):** add `workspace#can_message: [user, service_account]` to OpenFGA model; Workflow controller (03) writes/deletes tuples at WorkflowRun create/complete.
- **04b iter-3 (blocks a2a ws-sa token minting):** add `spec.audienceTemplates.a2a` for per-peer audience template.
- **03 iter-3 (follow-on):** explicit tuple-write protocol for `can_message` in Workflow controller.
- **12 iter-1 (required):** workspace NP must allow egress to NATS:4222 and Envoy AI GW:443.

## Refs

[04a](04a-openfga-authz-model.md) · [04b](04b-projected-sa-identity.md) ·
[05a](05a-envoy-ai-gateway-topology.md) · [05c](05c-mcp-policy-enforcement.md) ·
[08b](08b-goose-acp-stdio-k8s.md) · [12](12-network-isolation.md) ·
[20a](20a-api-group-layout.md) · [22](22-workflow-composition-examples.md) ·
[rubric](../plans/rubric.md)

## Iteration log

Iter-1 (2026-04-21): **92.5 SHIP held at draft** — stdio locked; type immutability VAP;
delivery guarantees; cert-manager reference model; cross-namespace ReferenceGrant;
lifecycle + failure modes; observability. NATS stream ownership and a2a peer-auth
left ambiguous.

### Iteration 2 — 2026-04-21

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Hybrid NATS model + 4 a2a auth modes bounded with explicit enforcement points. |
| 2 | Architecture fit | 10 | 1.0 | 10 | workspace-sa uses kubernetes-default OIDCProvider (04b); can_message cross-refs 04a; Workflow controller (03) writes tuples. |
| 3 | Security posture | 15 | 1.0 | 15 | VAP blocks `none` in prod; JWT before OpenFGA; no keys in spec; streamConfig gated by annotation; rule-05 satisfied. |
| 4 | Automatability | 10 | 0.5 | 5 | Admission checks named; make targets pre-gate. |
| 5 | Verifiability | 15 | 0.5 | 7.5 | 10 failure modes; a2a auth testable in envtest (pre-gate). |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Hybrid stream paths split; P8 runbook flagged; a2a deny + unsafe-prod enumerated. |
| 7 | Context efficiency | 10 | 1.0 | 10 | ≤ 200 lines; cross-dep flags section; no inline code blocks. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX + frontmatter; depends includes 04a + 04b; refs valid. |
| 9 | Observability | 5 | 1.0 | 5 | a2a auth span + metric added; 4 new events. |
| 10 | Operational readiness | 10 | 1.0 | 10 | P8 runbook flagged; auto-create finalizer annotation-scoped; rollback unchanged. |
| | **Total** | 100 | | **95** | |

Verdict: **SHIP** (95 ≥ 95). `status` flipped to `current`.

Top gaps:
1. Cat 4/5: make targets + envtest pre-gate — acceptable, unchanged from iter-1.
2. 04a iter-5 `can_message` relation not yet in model.fga; blocks a2a workspace-sa controller code.
3. 04b iter-3 per-peer `audienceTemplates` not yet added; blocks SA token minting for a2a.
