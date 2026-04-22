<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: spec
category: contract
depends:
  - ../designs/09-transport-crd.md
  - ../designs/09-ii-iter-log.md
  - ../designs/04a-openfga-authz-model.md
  - ../designs/04b-projected-sa-identity.md
  - ../designs/03c-workflow-messaging-plane.md
  - ../designs/25-cross-tenant-agreement.md
related_skills: [crd-authoring, controller-authoring]
status: current
last_verified: 2026-04-21
regression_lock: false
tests:
  unit: []
  envtest:
    - transport_type_immutability_test.go
    - transport_cross_tenant_cra_admission_test.go
    - transport_nats_hybrid_ownership_test.go
    - transport_mcp_route_not_found_test.go
    - transport_stdio_buffer_overflow_test.go
  kuttl: []
metrics:
  - keese_transport_messages_total{type,direction,tenant}
  - keese_transport_errors_total{type,reason,tenant}
  - keese_transport_dep_resolve_duration_seconds{type}
  - keese_transport_a2a_auth_duration_seconds{peer_auth_mode,scope}
events:
  - TransportProvisioned
  - TransportUnreachable
  - CertificateNotFound
  - MCPRouteNotFound
  - ReferenceGrantMissing
  - NATSStreamOwned
  - NATSStreamDeleteFailed
  - NATSStreamNotFound
  - NATSStreamConfigIgnored
  - NATSStreamMigrationRequired
  - A2APeerAuthzDenied
  - CrossTenantAgreementMissing
---

# transport.operator.keese.ai v1alpha1 — spec

**Kind:** `Transport` · **Group:** `transport.operator.keese.ai` ·
**Version:** `v1alpha1` · **Scope:** Namespace

Owning designs: [09](../designs/09-transport-crd.md) (iter-3) ·
[04a](../designs/04a-openfga-authz-model.md) (iter-5) ·
[04b](../designs/04b-projected-sa-identity.md) (iter-3) ·
[03c](../designs/03c-workflow-messaging-plane.md) (iter-3) ·
[25](../designs/25-cross-tenant-agreement.md) (iter-3) ·
Rubric tables: [transport-ii-iter-log.md](transport-ii-iter-log.md)

## 1. CRD schema

`spec.type`: `nats | a2a | mcp | stdio` — **immutable after create** (VAP CEL
`oldSelf.spec.type == self.spec.type`, reason `TransportTypeImmutable`).

**spec.stdio** — field table: [09 §spec.stdio](../designs/09-transport-crd.md).
Key fields: `bridgeImage` (req), `outboundQueueDepth` (def 1000), `inboundQueueDepth` (def 100),
`reconnectBufferBytes` (def 4194304), `reconnectRetries` (def 3), `reconnectBackoff` (def 1s/2.0/30s).

**spec.nats** — field table: [09 §spec.nats](../designs/09-transport-crd.md).
Default (external): admission rejects `NATSStreamNotFound` if stream absent;
`streamConfig` ignored without opt-in annotation → `NATSStreamConfigIgnored`.
Opt-in (`keese.ai/auto-create-stream: "true"`): controller owns JetStream lifecycle;
finalizer `finalizers.transport.operator.keese.ai/cleanup` deletes stream on deletion.

NATS audience: `keese-wf-<workflow-run-uid>` (04b iter-3 `workflowRun` template),
`/var/run/keese/tokens/workflowRun`; per-run, not per-peer.

Topic naming:

| Scope | Pattern | Provisioner |
|---|---|---|
| Intra-tenant | `keese.tenant.<tenant-uid>.wf.<workflow-run-uid>.*` | Workflow controller (03c) at WorkflowRun creation |
| Cross-tenant | `keese.cta.<cra-uid>.*` | Workflow controller (03c) at first cross-tenant transportRef use |

Intra-tenant topic existence IS authz (no per-message ReBAC check).

**spec.mcp** — field table: [09 §spec.mcp](../designs/09-transport-crd.md): `mcpRouteRef.{name,namespace}` (req),
`protocolVersion` (def `2024-11-05`), `toolTimeout` (def 30s). Admission validates MCPRoute → `MCPRouteNotFound`.

**spec.a2a** — field table: [09 §spec.a2a](../designs/09-transport-crd.md).
Two peer-auth modes: `workspace-sa | mutual-tls` (dropped: `user-oidc`, `none`).

| Field | Req | Default |
|---|---|---|
| `endpoint` | yes | `grpc://` or `grpcs://` |
| `peerAuth` | no | `workspace-sa` |
| `scope` | no | `intra-tenant` |
| `workspaceSA.audience` | if ws-sa | non-empty |
| `workspaceSA.authzTupleCheck` | no | `true` (cross-tenant only) |
| `mutualTLS.certificateRef.{name,namespace}` | if mtls | non-empty |
| `mutualTLS.clientCaBundle` | no | PEM or empty |

ReBAC markers (rule 04.14, `scripts/check-rebac-markers.sh`):

```go
// +keese:rebac-tuple=workspace.messageable_from  (spec.a2a.scope)
// +keese:rebac-tuple=tenant.allows_messaging      (spec.a2a.workspaceSA.authzTupleCheck)
```

`scope: intra-tenant` (default): no OpenFGA check.
`scope: cross-tenant`: `Check(workspace:<peer>#messageable_from@workspace:<caller>)`
at subscribe + first publish (04a iter-5); requires Approved CRA (design 25).
`mutual-tls`: ACL in `mutualTLS.aclRef`; no OpenFGA lookup.

## 2. VAP exclusivity

One sub-field set per `spec.type`; others absent. Violation: `TransportSubfieldMismatch`.
CEL pattern and full expressions: [09 §Decision](../designs/09-transport-crd.md).

## 3. Cross-tenant scope admission

`scope: cross-tenant` → validating webhook (rule 04.12; VAP cannot do cross-resource
dereference). Webhook verifies an Approved `CrossTenantAgreement` covering:
`(Transport namespace → Workspace → Tenant, endpoint target → Workspace → Tenant)`,
selectors matching, `status.phase = Approved`, `status.expiresAt > now`.

Rejection: `CrossTenantAgreementMissing` with `(from-tenant, to-tenant)` pair.
Runtime enforcement: `Check(workspace:<peer>#messageable_from@workspace:<caller>)` at
transport layer (04a iter-5). Admission is fast-fail UX; bypass impossible.

## 4. NATS audience

`workspace-sa` mode: audience `keese-wf-<workflow-run-uid>` (04b iter-3 `workflowRun`
template). Per-run; single token per WorkflowRun regardless of Transport count. Minted
by Workflow controller via `OIDCProvider.spec.audienceTemplates[name=workflowRun]`.
NATS server validates JWT issuer before publish/subscribe.

## 5. NATS topic naming

Intra-tenant stream: `keese-tenant-<tenant-uid>-wf-<run-uid>`, subjects
`keese.tenant.<t>.wf.<r>.>`, `retention=workqueue`, `storage=file`, `replicas=3`,
`maxAge=WorkflowRun.spec.timeout`. Owner-ref: Argo Workflow → GC on deletion;
7 d post-mortem retention after completion.

Cross-tenant stream: `keese-cta-<cra-uid>`, subjects `keese.cta.<cra-uid>.>`.
CRA deletion finalizer cleans stream.

## 6. RBAC, finalizer, SSA fieldOwner

RBAC markers: `transports` + status + finalizers (get;list;watch;create;update;patch;delete);
`jetstream.nats.io/streams;consumers` (get;list;watch;create;update;delete);
`referencegrants`, `mcproutes`, `certificates` (get;list;watch).
Finalizer `finalizers.transport.operator.keese.ai/cleanup` — annotation-gated
(`keese.ai/auto-create-stream: "true"`). SSA fieldOwner: `keese-transport-controller`.

## 7. Status conditions and event reasons

Lifecycle: `Pending → Provisioning → Ready → Degraded → Terminating`.
`observedGeneration` on every status write; status never inputs into next reconcile (rule 04.4).
`Ready=False` reasons: `DepsNotReady`, `NATSStreamNotFound`, `CertificateNotFound`,
`MCPRouteNotFound`, `ReferenceGrantMissing`, `CrossTenantAgreementMissing`, `TransportUnreachable`.
`Ready=True`: `TransportProvisioned`. Event const table: `internal/controller/transport/events.go`.
Full event list in frontmatter. Printer columns (rule 04.5): `Age`, `Ready`, `Phase`, `Type`.
Markers: `// +kubebuilder:subresource:status` + four `// +kubebuilder:printcolumn`.

## 8. Acceptance tests

Five named envtest files in `internal/controller/transport/` (pre-gate):

| Test | Assertion |
|---|---|
| `transport_type_immutability_test.go` | VAP rejects `spec.type` mutation; 3 idempotent reconciles |
| `transport_cross_tenant_cra_admission_test.go` | Webhook rejects `scope: cross-tenant` without Approved CRA; accepts with valid CRA |
| `transport_nats_hybrid_ownership_test.go` | Default: rejects absent stream; opt-in: creates stream, sets finalizer, deletes on removal |
| `transport_mcp_route_not_found_test.go` | Admission rejects absent MCPRoute; `Ready=False` `MCPRouteNotFound` condition |
| `transport_stdio_buffer_overflow_test.go` | `StreamLagged` event + oldest-frame drop at `outboundQueueDepth` ceiling |

Samples (rule 04.15): `config/samples/transport_v1alpha1_transport_nats_minimal.yaml`,
`config/samples/transport_v1alpha1_transport_a2a_full.yaml` — pass `kubectl apply --dry-run=server`.

## 9. Delivery guarantees and failure modes

Delivery: `nats` at-least-once (`Nats-Msg-Id` dedup, KV `keese-wf-delivered` 24h TTL);
`a2a` + `mcp` at-most-once (caller-side); `stdio` reliable in-order (08b bridge;
SIGKILL loses unacked frames).

Full failure modes (10 rows): [09 §Lifecycle](../designs/09-transport-crd.md).
Key: `CrossTenantAgreementMissing` — create CRA (design 25);
`NATSStreamMigrationRequired` — P8 runbook dual-consumer + backfill;
`TransportTypeImmutable` — new Transport CR + migrate consumers.

Rollback: `spec.type` immutable. `v1beta1` promotion requires
`docs/plans/migration-transport.md` scored ≥ 90 (rule 04.2).

## Iteration log

Full rubric tables: [transport-ii-iter-log.md](transport-ii-iter-log.md).
Iter-1 (2026-04-21): **92.5 SHIP held** — correctness + security; Cat 4/5 pre-gate; `authzTupleCheck` marker clarified.
Iter-2 (2026-04-21): **95 SHIP held** — performance + quality; see companion.
Iter-3 (2026-04-21): **97.5 SHIP** — operational readiness; `status: current`; see companion.
