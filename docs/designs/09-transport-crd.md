<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: transport
depends:
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
  update. Controller code not yet authored; rollback at design level = revert this
  doc commit and remove transportRef from Workspace/Workflow CRs. At v1beta1
  promotion a docs/plans/migration-transport.md is required before rollback.
---

# 09 — Transport CRD

**Decision:** A single namespace-scoped `Transport` kind at
`transport.operator.keese.ai/v1alpha1` provides pluggable messaging via a
`spec.type` discriminator (`nats|a2a|mcp|stdio`). `spec.type` is immutable after
creation (VAP CEL). Each type activates exactly one typed sub-struct (discriminated
one-of enforced by VAP CEL). Consumers reference Transport by name+namespace via
`transportRef`; cross-namespace access requires a `ReferenceGrant`.

## Context

Keese agent workloads need pluggable messaging with different delivery guarantees,
topologies, and TLS models: NATS JetStream (durable, at-least-once, workflow
fanout), A2A gRPC (agent-to-agent, mutual-TLS), MCP over Envoy AI Gateway
(tool-call JSON-RPC, per-05c policy enforcement), and stdio (in-pod ACP bridge,
per-08b interactive sessions). A single `Transport` CRD unifies these under one
lifecycle and one reference model without collapsing their semantics.

## Discriminated one-of schema

VAP CEL (immutability): `oldSelf.spec.type == self.spec.type` — rejects with
`TransportTypeImmutable`. VAP CEL (exclusivity):
`size([self.spec.stdio,self.spec.nats,self.spec.mcp,self.spec.a2a].filter(x,x!=null))==1`.

### Field table — `spec.stdio` (required iff `type: stdio`)

| Field | Required | Default | VAP constraint |
|---|---|---|---|
| `bridgeImage` | yes | — | non-empty string |
| `inboundQueueDepth` | no | 100 | [10, 10000] |
| `outboundQueueDepth` | no | 1000 | [100, 100000] |
| `reconnectBufferBytes` | no | 4194304 | [1048576, 67108864] |
| `reconnectRetries` | no | 3 | [1, 10] |
| `reconnectBackoff.initial` | no | 1s | duration string |
| `reconnectBackoff.multiplier` | no | 2.0 | [1.0, 10.0] |
| `reconnectBackoff.max` | no | 30s | duration string |

Fields locked from 08b iter-2: `inboundQueueDepth`, `outboundQueueDepth`,
`reconnectBufferBytes` (08b §Backpressure). `bridgeImage` default is embedded in
operator release; `AgentRuntime.spec.sidecars.acpBridge.image` overrides per-runtime.
No TLS for stdio — in-pod Unix socket IPC only.

### Field table — `spec.nats` (required iff `type: nats`)

| Field | Required | Default | VAP constraint |
|---|---|---|---|
| `clusterRef.name` | yes | — | non-empty |
| `clusterRef.namespace` | yes | — | non-empty |
| `streamName` | yes | — | non-empty; max 64 chars |
| `consumerName` | yes | — | non-empty; max 64 chars |
| `ackPolicy` | no | `explicit` | `explicit|none|all` |
| `maxDeliver` | no | 3 | [1, 100] |
| `ackWait` | no | 30s | duration string |
| `replicas` | no | 3 | [1, 5] |
| `retention` | no | 7d | duration string [1d, 30d] |
| `tls.certificateRef.name` | yes | — | non-empty |
| `tls.certificateRef.namespace` | yes | — | non-empty |

### Field table — `spec.mcp` (required iff `type: mcp`)

| Field | Required | Default | VAP constraint |
|---|---|---|---|
| `mcpRouteRef.name` | yes | — | non-empty |
| `mcpRouteRef.namespace` | yes | — | non-empty |
| `protocolVersion` | no | `2024-11-05` | semver or date-string |
| `toolTimeout` | no | 30s | duration string [1s, 300s] |

`mcpRouteRef` points to the Envoy AI GW `MCPRoute` CR managed by the
guardrail-controller projector (05c). Transport controller validates MCPRoute
exists at admission; emits `MCPRouteNotFound` if absent.

### Field table — `spec.a2a` (required iff `type: a2a`)

| Field | Required | Default | VAP constraint |
|---|---|---|---|
| `endpoint` | yes | — | valid gRPC URI (grpc:// or grpcs://) |
| `peerAuth` | no | `mutual-tls` | `mutual-tls|jwt|none` |
| `certificateRef.name` | yes if `peerAuth=mutual-tls` | — | non-empty |
| `certificateRef.namespace` | yes if `peerAuth=mutual-tls` | — | non-empty |

## Delivery guarantees

| Type | Guarantee | Dedup owner | Notes |
|---|---|---|---|
| `nats` | at-least-once | Workflow controller via `Nats-Msg-Id` header; NATS KV `keese-wf-delivered` 24h TTL (22 iter-2) | Exactly-once via JetStream preview feature; future option only |
| `a2a` | at-most-once | Caller-side; no broker | gRPC unary; retries at call site |
| `mcp` | at-most-once | Caller-side | JSON-RPC over HTTP; not idempotent by default |
| `stdio` | reliable in-order | Bridge (08b reconnect buffer + seq tracking) | Session-scoped; not durable; SIGKILL loses unacked frames |

## TLS — cert-manager integration

cert-manager `Certificate` CRs are **referenced by name, not created by the
Transport controller.** Tenant provisions `Certificate` CRs (cluster-wide
`ClusterIssuer` for prod; self-signed for dev); Transport controller validates at
admission that the referenced `Certificate` exists and emits `CertificateNotFound`
if absent. Rotation is native cert-manager renewal; consumers reload via sidecar
or SIGHUP. Applies to `nats` and `a2a` types. `mcp` delegates TLS to the Envoy AI
Gateway. `stdio` has no TLS (in-pod Unix socket).

Transport spec carries no credentials (rule 05.7) — `certificateRef` fields point
at cert-manager `Certificate` status; the controller reads status only, no Secret
mount. Agent pods reach NATS via SA token exchanged at Envoy AI Gateway (05a/05b).

**Flag:** cert-manager is a soft dependency — not installed by the Transport
controller itself. Bootstrap must install it (dev helmfile or OLM dependency via
14b). Pre-gate residual for the 12 / infra-bootstrap owners.

## Lifecycle

`Pending → Provisioning → Ready → Degraded → Terminating`. Provisioning resolves
deps (NATS cluster reachable, Certificate ready, MCPRoute exists, ReferenceGrant
valid). Degraded when a dep breaks post-provision; consumers should pause, not crash.
Finalizer `finalizers.transport.operator.keese.ai/cleanup` disconnects consumers and
removes owned NATS streams (annotated `keese.ai/nats-stream-owned: true`). All fields
except `spec.type` are mutable; changes reconcile in-place.

## Cross-namespace reference model

Consumers declare `transportRef: {name, namespace}` (empty namespace = same namespace).
Cross-namespace requires `gateway.networking.k8s.io/v1beta1/ReferenceGrant` in the
Transport namespace granting the consumer namespace access. Admission rejects if absent
(`ReferenceGrantMissing`). No cluster-scoped Transports at v1alpha1 (20a §Scope).

## Failure modes

| Failure | Detection | Mitigation |
|---|---|---|
| Certificate absent at admission | Admission reject `CertificateNotFound` | Provision cert-manager Certificate first |
| NATS cluster unreachable | `TransportUnreachable` event; phase=Degraded | JetStream reconnect; alert; workflow pauses |
| ReferenceGrant missing | Admission reject `ReferenceGrantMissing` | Create ReferenceGrant in Transport namespace |
| MCPRoute not found | `MCPRouteNotFound` event; phase=Degraded | Guardrail-controller must provision MCPRoute first |
| Type change attempted | VAP reject `TransportTypeImmutable` | Create new Transport CR; migrate consumers |
| Stdio buffer overflow | `StreamLagged` frame; drop oldest (08b) | Scale down concurrent clients; increase `outboundQueueDepth` |
| NATS stream deletion during Terminating | Finalizer blocks; retries | Alert; manual kubectl delete if stuck > 5 min |

**Flag (12 — NetworkPolicy):** Transport pods do not exist (Transport is a config
CRD, not a Pod). But the NetworkPolicy for workspace namespaces (12, stub) must
allow egress to NATS on port 4222 and to the Envoy AI Gateway on 443 for `mcp`
type. 12 iter-1 must incorporate these rules. This is a cross-dep flag.

## Observability

OTEL spans: `keese.transport.provision`, `keese.transport.dep_resolve`,
`keese.transport.degraded`, `keese.transport.terminating`. Metrics:
`keese_transport_messages_total{type,direction,tenant}`,
`keese_transport_errors_total{type,reason,tenant}`,
`keese_transport_dep_resolve_duration_seconds{type}`. Events (`events.go`):
`TransportProvisioned`, `TransportUnreachable`, `CertificateNotFound`,
`MCPRouteNotFound`, `ReferenceGrantMissing`, `NATSStreamOwned`,
`NATSStreamDeleteFailed`. Printer columns: `Age`, `Ready`, `Phase`, `Type`.

## Refs

[05a](05a-envoy-ai-gateway-topology.md) · [05c](05c-mcp-policy-enforcement.md) ·
[08b](08b-goose-acp-stdio-k8s.md) · [12](12-network-isolation.md) ·
[20a](20a-api-group-layout.md) · [22](22-workflow-composition-examples.md) ·
[rubric](../plans/rubric.md)

## Iteration log

Iter-1 (2026-04-21): **92.5 SHIP** — 5 open questions answered; stdio fields locked from
08b; type immutability VAP; delivery guarantees table; cert-manager reference model;
cross-namespace ReferenceGrant model; lifecycle + failure modes; observability.
Cat 4/5 pre-gate gaps (no controller code or envtest yet). Full rubric table in
[09-ii-iter-log.md](09-ii-iter-log.md).
