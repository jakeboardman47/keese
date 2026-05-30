<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: feature
category: feature
depends:
  - ../designs/09-transport-crd.md
  - ../designs/03c-workflow-messaging-plane.md
implements_specs: [../specs/keese.ai-v1alpha1-transport.md]
implements_plans: [../plans/demo/tech-debt.md]
source_refs:
  - api/keese/v1alpha1/transport_types.go:1-369
  - internal/controller/keese/transport_controller.go:1-614
  - internal/controller/keese/transport_nats_nack.go:1-179
  - internal/controller/keese/transport_certmanager.go:1-228
  - internal/controller/keese/transport_events.go:1-77
related_skills: [controller-authoring]
status: implemented
implemented_in_phase: demo-D1
last_verified: 2026-05-29
---

# Transport (messaging plane)

## Summary

Transport provisions messaging endpoints for workspace agents: NATS JetStream
streams (via NACK CRDs), agent-to-agent gRPC (A2A), MCPRoute-backed tool
channels, and stdio bridges. The controller validates external dependencies,
optionally creates and owns a NATS JetStream stream, SSA-projects cert-manager
Certificates for mTLS, gates cross-tenant A2A connections through an
Approved CrossTenantAgreement, and writes OpenFGA ReBAC tuples on every
reconcile. `spec.type` is immutable after creation (CRD `XValidation` CEL rule).

## Behavior

- **Type selection**: `spec.type` (enum `nats|a2a|mcp|stdio`) is set at
  creation and enforced immutable by a CRD `XValidation` CEL rule at
  `transport_types.go:292-296`.
- **Label gate**: only objects carrying label `keese.ai/managed=true` are
  reconciled (`transport_controller.go:605`).
- **NATS stream lifecycle (opt-in)**: when annotation
  `keese.ai/auto-create-stream=true` is set, the controller SSA-applies a
  `jetstream.nats.io/v1beta2 Stream` CRD via `ClientNatsStreamer` with field
  owner `keese-transport-controller`, and attaches finalizer
  `finalizers.transport.keese.ai/cleanup` to delete the stream on removal.
  Without the annotation the stream must pre-exist; its absence drives phase
  `Degraded` with event `NATSStreamNotFound`.
- **Default NATS subject pattern**: `keese.transport.<streamName>.>` (design
  03c naming); overridden by `spec.nats.streamConfig.subjects`.
- **cert-manager Certificate projection (opt-in)**: when annotation
  `keese.ai/auto-manage-cert=true` is set, the controller SSA-applies a
  `cert-manager.io/v1 Certificate` named
  `keese-transport-<name>-tls` via `ClientCertificateProjector`. SANs are
  derived from the NATS cluster ref DNS names or the A2A endpoint hostname;
  issuer is ClusterIssuer `keese-<namespace>`. Duration 90 days, renewal 30
  days before expiry. Resulting Secret mounts as projected volume on session
  pods (rule 05.7).
- **Cross-tenant A2A gate**: `spec.a2a.scope=cross-tenant` blocks until an
  Approved `CrossTenantAgreement` covering the workspace/endpoint pair is
  resolved; missing agreement sets phase `Degraded` with event
  `CrossTenantAgreementMissing`.
- **MCPRoute validation**: the controller does an unstructured `Get` against
  `aigateway.envoyproxy.io/v1alpha1 MCPRoute`; absent route → event
  `MCPRouteNotFound`.
- **Stdio**: no external dependency validation; VAP enforces `bridgeImage`
  non-empty at admission.
- **ReBAC tuples**: every reconcile calls `Rebac.Sync` to write
  `transport.owner` tuples; count reflected in
  `status.rebacTupleCount`. Failure → phase `Degraded`, event implied by
  reason `RebacSyncFailed`.
- **Phase lifecycle**: `Pending → Provisioning → Ready`; any dep failure →
  `Degraded`; deletion → `Terminating` (finalizer path).
- **Reconcile convergence**: ≤ 3 reconciles to reach `Ready` from a clean
  spec; idempotent SSA writes (rule 04.7).

## Configuration surface

Fields are defined in `api/keese/v1alpha1/transport_types.go`.

| Field | Notes |
|---|---|
| `spec.type` | `nats\|a2a\|mcp\|stdio`; immutable (line 292) |
| `spec.nats.clusterRef` | Required for `nats`; names the NATS cluster object |
| `spec.nats.streamName` | Max 64 chars; must pre-exist or pair with annotation |
| `spec.nats.consumerName` | Max 64 chars |
| `spec.nats.ackPolicy` | `explicit\|none\|all`; default `explicit` |
| `spec.nats.maxDeliver` | Range 1–100; default 3 |
| `spec.nats.tls.certificateRef` | cert-manager Certificate ref for mTLS |
| `spec.nats.streamConfig` | JetStream tuning; honored only with annotation |
| `spec.a2a.endpoint` | `grpc://` or `grpcs://` URI |
| `spec.a2a.scope` | `intra-tenant\|cross-tenant`; default `intra-tenant` |
| `spec.a2a.peerAuth` | `workspace-sa\|mutual-tls`; default `workspace-sa` |
| `spec.a2a.workspaceSA.audience` | SA token audience (line 229) |
| `spec.a2a.mutualTLS.certificateRef` | Required when `peerAuth=mutual-tls` |
| `spec.mcp.mcpRouteRef` | `aigateway.envoyproxy.io/v1alpha1 MCPRoute` ref |
| `spec.mcp.protocolVersion` | Default `2024-11-05` |
| `spec.mcp.toolTimeout` | Per-call timeout; range 1s–300s; default 30s |
| `spec.stdio.bridgeImage` | Sidecar image; required (CRD `XValidation`-validated) |
| `spec.stdio.inboundQueueDepth` | Default 100; range 10–10 000 |
| `spec.stdio.outboundQueueDepth` | Default 1 000; drop-oldest at ceiling |
| `spec.stdio.reconnectRetries` | Default 3; range 1–10 |
| Annotation `keese.ai/auto-create-stream` | `"true"` enables stream ownership |
| Annotation `keese.ai/auto-manage-cert` | `"true"` enables cert projection |

## Observability

**Status** (`api/keese/v1alpha1/transport_types.go:322-340`):
- `status.phase`: `Pending | Provisioning | Ready | Degraded | Terminating`
- `status.observedGeneration`: generation at last successful sync
- `status.rebacTupleCount`: OpenFGA tuples written on last sync
- `status.conditions`: `Ready` (True/False) and `Progressing` (True/False),
  plus `CertificateProjected` when cert annotation active

**Printer columns**: `Age`, `Ready`, `Phase`, `Type`
(`transport_types.go:344-347`)

**Events** — all reasons from `transport_events.go:9-76`:

| Reason | Type | Trigger |
|---|---|---|
| `TransportProvisioned` | Normal | Phase → Ready |
| `NATSStreamOwned` | Normal | Controller creates a stream |
| `CertificateProjected` | Normal | SSA cert success |
| `NATSStreamNotFound` | Warning | Pre-existing stream absent |
| `NATSStreamConfigIgnored` | Warning | `streamConfig` set without annotation |
| `MCPRouteNotFound` | Warning | MCPRoute ref unresolvable |
| `CertificateNotFound` | Warning | TLS `certificateRef` absent |
| `CrossTenantAgreementMissing` | Warning | A2A cross-tenant with no CTA |
| `A2APeerAuthzDenied` | Warning | OpenFGA ext_authz denial |
| `StreamLagged` | Warning | stdio outbound queue ceiling hit |
| `CertificateProjectionFailed` | Warning | SSA cert failure |
| `NATSStreamDeleteFailed` | Warning | Finalizer cleanup failure |

## Known limitations

- **NATSSubscription trigger and output-sink projections are no-op.** The
  controller does not reconcile any `NATSSubscription` trigger or output-sink
  resources; these projections are deferred.
- **A2A and MCP support is partial.** Dependency validation (MCPRoute lookup,
  CrossTenantAgreement check) is implemented, but no A2A gRPC endpoint or
  MCPRoute binding is actively wired in the reconcile loop beyond the check.
- **stdio bridge sidecar injection is not yet implemented.** `spec.stdio` is
  validated and stored but the controller does not inject a bridge sidecar
  into any pod.
- **Fakes are used for all external integrations at startup.** If no
  production `NatsStreamer`, `CertManagerReader`, `CertProjector`, or
  `CTAResolver` is provided at setup time, the controller falls back to
  no-op fakes (`transport_controller.go:586-603`).

## Change history

- `demo-D1` — initial Transport CRD, reconciler, NACK stream projection,
  cert-manager projection, ReBAC tuple sync, and CTA cross-tenant gate landed.

## References

- Design: `docs/designs/09-transport-crd.md`,
  `docs/designs/03c-workflow-messaging-plane.md`
- Spec: `docs/specs/keese.ai-v1alpha1-transport.md`
- Plan: `docs/plans/demo/tech-debt.md`
- Source: `api/keese/v1alpha1/transport_types.go`,
  `internal/controller/keese/transport_controller.go`,
  `internal/controller/keese/transport_nats_nack.go`,
  `internal/controller/keese/transport_certmanager.go`,
  `internal/controller/keese/transport_events.go`
