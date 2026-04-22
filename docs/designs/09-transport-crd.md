<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: transport
depends:
  - 04a-openfga-authz-model.md          # tenant.allows_messaging + workspace.messageable_from (iter-5 landed)
  - 04b-projected-sa-identity.md        # audienceTemplates.workflowRun (iter-3 in flight)
  - 05a-envoy-ai-gateway-topology.md
  - 05c-mcp-policy-enforcement.md
  - 08b-goose-acp-stdio-k8s.md
  - 12-network-isolation.md
  - 20a-api-group-layout.md
  - 22-workflow-composition-examples.md
  - 25-cross-tenant-agreement.md        # CrossTenantAgreement CRD (D29; stub)
related_skills: [doc-authoring, crd-authoring, controller-authoring]
status: current
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

**NATS as primary intra-tenant transport.** Topic naming pattern:
`keese.tenant.<tenant-uid>.wf.<workflow-run-uid>.*` — provisioned by the Workflow
controller (03 iter-3) at WorkflowRun creation. Topic existence within this prefix
is itself the authorization signal for intra-tenant participants; no additional
OpenFGA check is performed (saves one RTT per message). SA tokens for NATS
publish/subscribe carry audience `keese-wf-<workflow-run-uid>` (04b iter-3
`workflowRun` audience template); NATS server validates JWT before accepting
publish or subscribe. The topic prefix enables tenant-scoped log/audit filtering:
`grep keese.tenant.<tenant-uid>.*` in Elastic. Cross-tenant NATS messaging uses a
separate prefix per CrossTenantAgreement (`keese.cta.<cta-uid>.*`) — flagged for
design 25 to detail.

**Default (pre-existing):** admission queries NATS JetStream; rejects
`NATSStreamNotFound` if stream absent. Stream lifecycle owned externally by NACK
`Stream` CRD (preferred, multi-tenant) or direct NATS operator action (dev/test).
`streamConfig` is ignored and emits `NATSStreamConfigIgnored` if set without opt-in
annotation.

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

## `spec.a2a` — workspace-as-security-boundary (iter-3)

Workspace is the security boundary. Two peer-auth modes only:

| Field | Req | Default | VAP |
|---|---|---|---|
| `endpoint` | yes | — | `grpc://` or `grpcs://` |
| `peerAuth` | no | `workspace-sa` | `workspace-sa\|mutual-tls` |
| `scope` | no | `intra-tenant` | `intra-tenant\|cross-tenant` |
| `workspaceSA.audience` | if ws-sa | — | non-empty |
| `mutualTLS.certificateRef.{name,namespace}` | if mtls | — | non-empty |
| `mutualTLS.clientCaBundle` | no | — | PEM or empty |

**`workspace-sa` (default):** caller presents projected SA token with audience
`keese-wf-<workflow-run-uid>` (04b iter-3 `workflowRun` template). Receiving
Transport validates issuer = `kubernetes.default.svc.cluster.local`.

- **`scope: intra-tenant`** (default): no OpenFGA check. Topic existence in
  `keese.tenant.<tenant-uid>.wf.<workflow-run-uid>.*` is sufficient; Workflow
  controller is the sole provisioner.
- **`scope: cross-tenant`**: OpenFGA `Check(workspace:<peer>#messageable_from@workspace:<caller>)`
  at subscribe + first publish. Caller must have a `CrossTenantAgreement` in
  `Approved` phase covering the workspace pair (D29 controller writes the
  `workspace.messageable_from` tuple as runtime evidence). VAP rejects
  `scope: cross-tenant` when no matching CrossTenantAgreement exists:
  `CrossTenantAgreementMissing`. Uses 04a iter-5 `tenant.allows_messaging` +
  `workspace.messageable_from` relations.

**`mutual-tls`:** external gRPC peers with no K8s identity; ACL in
`mutualTLS.aclRef`; no OpenFGA lookup.

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
carries no credentials (rule 05.7). cert-manager is a soft dep — bootstrap must
install it (helmfile or OLM dep via 14b).

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
| a2a cross-tenant authz deny | `A2APeerAuthzDenied` event | Verify D29 controller wrote `messageable_from` tuple |
| cross-tenant with no CRA | VAP `CrossTenantAgreementMissing` | Create CrossTenantAgreement (design 25) |
| Stdio buffer overflow | `StreamLagged`; drop oldest | Increase `outboundQueueDepth` |

## Observability

OTEL spans: `keese.transport.{provision,dep_resolve,degraded,terminating,a2a.auth}`.
Metrics: `keese_transport_messages_total{type,direction,tenant}`,
`keese_transport_errors_total{type,reason,tenant}`,
`keese_transport_dep_resolve_duration_seconds{type}`,
`keese_transport_a2a_auth_duration_seconds{peer_auth_mode,scope}`.
Events: `TransportProvisioned`, `TransportUnreachable`, `CertificateNotFound`,
`MCPRouteNotFound`, `ReferenceGrantMissing`, `NATSStreamOwned`, `NATSStreamDeleteFailed`,
`NATSStreamNotFound`, `NATSStreamConfigIgnored`, `NATSStreamMigrationRequired`,
`A2APeerAuthzDenied`, `CrossTenantAgreementMissing`.
Printer columns: `Age`, `Ready`, `Phase`, `Type`.

## Cross-dep flags

- **04a iter-5 (LANDED 2026-04-21):** `workspace#can_message` dropped; `tenant.allows_messaging: [tenant]` and `workspace.messageable_from: [workspace]` added. D29 controller writes both relations post bilateral approval.
- **04b iter-3 (LANDED 2026-04-21):** `audienceTemplates.workflowRun` (`keese-wf-<workflow-run-uid>`) — required by `spec.a2a` workspace-sa and `spec.nats` JWT validation.
- **03 iter-3 (LANDED 2026-04-21):** Workflow controller provisions NATS topics + mints WorkflowRun-scoped SA tokens + validates CrossTenantAgreement on WorkflowRun admission for cross-tenant peers — peers derived implicitly from `transportRef`s with `scope: cross-tenant` (Q2(b) 2026-04-21; no new WorkflowRun spec field).
- **D29 / design 25 (stub):** CrossTenantAgreement CRD; required for `scope: cross-tenant`; also details `keese.cta.<cta-uid>.*` NATS prefix.
- **12 iter-1 (required):** workspace NP must allow egress to NATS:4222 and Envoy AI GW:443.

## Refs

[04a](04a-openfga-authz-model.md) · [04b](04b-projected-sa-identity.md) ·
[05a](05a-envoy-ai-gateway-topology.md) · [05c](05c-mcp-policy-enforcement.md) ·
[08b](08b-goose-acp-stdio-k8s.md) · [12](12-network-isolation.md) ·
[20a](20a-api-group-layout.md) · [22](22-workflow-composition-examples.md) ·
[25](25-cross-tenant-agreement.md) · [rubric](../plans/rubric.md) ·
[iter-log](09-ii-iter-log.md)

## Iteration log

Iter-1 (2026-04-21): **92.5 SHIP held at draft** — stdio locked; type immutability VAP;
delivery guarantees; cert-manager reference model; cross-namespace ReferenceGrant;
lifecycle + failure modes; observability. NATS stream ownership and a2a peer-auth
left ambiguous.

Iter-2 (2026-04-21): **95 SHIP** — hybrid NATS stream ownership; 4-mode a2a peer-auth
(`workspace-sa | user-oidc | mutual-tls | none`); `workspace#can_message` relation
cross-dep; per-peer `keese-a2a-<uid>` audience flagged. Status held at draft (mismatch
between score and new reframe; iter-3 lands final model).

Iter-3 (2026-04-21): see [09-ii-iter-log.md](09-ii-iter-log.md).
