<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: feature
category: feature
depends:
  - docs/designs/02-workspace-model.md
  - docs/designs/08b-ii-session-crd-spec.md
  - docs/designs/12-network-isolation.md
implements_specs:
  - docs/specs/keese.ai-v1alpha1-workspace.md
  - docs/specs/keese.ai-v1alpha1-workspace-ii-session.md
  - docs/specs/keese.ai-v1alpha1-workspace-ii-share.md
implements_plans:
  - docs/plans/demo/D1-controller-wiring.md
source_refs:
  - api/keese/v1alpha1/workspace_types.go:1-217
  - api/keese/v1alpha1/workspacesession_types.go:1-196
  - api/keese/v1alpha1/workspaceshare_types.go:1-97
  - internal/controller/keese/workspace_controller.go:1-634
  - internal/controller/keese/workspacesession_controller.go:1-80
  - internal/controller/keese/workspaceshare_controller.go
  - internal/controller/keese/workspace_events.go:1-60
related_skills: [controller-authoring]
status: implemented
implemented_in_phase: demo-D1
last_verified: 2026-05-29
---

# Workspaces and Sessions

## Summary

`Workspace` is the unit of agent work in keese. Creating one causes the controller
to provision a dedicated ServiceAccount, a fail-closed default-deny `NetworkPolicy`
(plus an egress allowlist to the in-cluster Envoy AI Gateway and NATS), and a
session PVC that persists SQLite checkpoint state across pod restarts. Agent pods
run under the workspace identity and may only egress through the gateway.
`WorkspaceSession` models an attach event against a running (or on-demand-started)
workspace pod, with per-attach, per-user, or shared pod-reuse semantics and idle
eviction. `WorkspaceShare` grants read or write access from another namespace via a
projected Gateway API `ReferenceGrant` and OpenFGA cross-tenant tuples.

## Behavior

- **Provisioning**: on creation the `WorkspaceReconciler` applies (via SSA with
  field owner `keese-workspace-controller`) a ServiceAccount named
  `ksa-<ws-uid>`, a default-deny `NetworkPolicy`, an egress `NetworkPolicy`, and
  a `PersistentVolumeClaim` for session storage (default 10 Gi, overridable via
  `spec.sessionStorage`). See workspace_controller.go:113-185.
- **Phase FSM**: `Pending → Provisioning → Running` once the PVC is Bound.
  `Idle` and `Evicted` are reachable via inactivity. `Terminating` fires on
  deletion. See workspace_types.go:12-23 and workspace_controller.go:208-218.
- **Session modes** (`spec.sessionMode`): `Always` keeps a pod alive at all times;
  `OnDemand` (default) starts a pod when the first `WorkspaceSession` attaches.
  See workspace_types.go:25-33.
- **Attach policy** (`spec.attachPolicy`): `Reuse` (default) routes new sessions
  to the running pod; `New` starts a fresh pod per session. See workspace_types.go:35-42.
- **Session lifecycle**: `WorkspaceSession` moves through
  `Pending → Attaching → Active → Draining → Completed | Evicted → Terminating`.
  Idle eviction fires after `spec.attachGraceSeconds` (0–86400 s) of inactivity.
  See workspacesession_types.go:10-35.
- **Interactive guard**: `spec.interactive` is immutable (CRD CEL `XValidation`);
  non-interactive workspaces reject `WorkspaceSession` creation controller-side
  (`SessionAttachRejectedNonInteractive` event; no separate VAP). See
  workspace_types.go:45, workspace_events.go:41.
- **ReBAC tuples**: the controller syncs `workspace.owner`, `workspace.editor`,
  `workspace.viewer`, and per-tool `tool.allowed_in` tuples for every `spec.egress.allowedTools`
  entry. Count is reflected in `status.rebacTupleCount`. See workspace_controller.go:564-626.
- **WorkspaceShare**: creates a Gateway API `ReferenceGrant` in the target namespace
  and writes `workspace.shared_with` / `workspace.cross_ns_viewer` tuples. Grants
  viewer (default) or editor access to up to 64 grantees. See workspaceshare_types.go:11-35.
- **Deletion**: the finalizer `finalizers.workspace.keese.ai/cleanup` ensures
  ReBAC tuples, NetworkPolicies, PVC, and ServiceAccount are pruned before the
  object disappears. See workspace_controller.go:253-306.

## Configuration surface

Key `Workspace.spec` fields — see workspace_types.go:44-125 and
docs/specs/keese.ai-v1alpha1-workspace.md for the full contract:

| Field | Default | Effect |
|---|---|---|
| `runtimeRef` | required | `AgentRuntime` that bootstraps the pod spec |
| `sessionMode` | `OnDemand` | Pod start policy |
| `attachPolicy` | `Reuse` | Pod sharing vs. new-per-session |
| `attachGrace` | `30s` | Idle timeout before eviction |
| `interactive` | `false` | Immutable; gates `WorkspaceSession` creation |
| `sessionStorage` | `10Gi` | PVC size for SQLite checkpoint state |
| `egress.allowedTools` | `[]` | Per-workspace OpenFGA tool allowlist |
| `editors` / `viewers` | `[]` | OpenFGA identity allowlists |

`WorkspaceSession.spec` key fields — see workspacesession_types.go:76-119:
`workspaceRef`, `attachSubject`, `sessionName` (default `default`), `mode`
(`shared` / `per-user` / `per-attach`), `attachGraceSeconds`, `preserveOnPodFailure`.

`WorkspaceShare.spec` key fields — see workspaceshare_types.go:11-35:
`workspaceRef`, `targetNamespace`, `grantees` (max 64), `readOnly` (default `true`).

## Observability

Events fired from workspace_events.go (all via `recorder.Eventf`):

- `WorkspaceProvisioned`, `WorkspaceReady`, `WorkspaceIdle`, `WorkspaceEvicted`,
  `WorkspaceTerminating` — workspace lifecycle.
- `ServiceAccountEnsured`, `PVCEnsured`, `NetworkPolicyEnsured` — sub-resource
  provisioning.
- `RebacTupleWritten`, `RebacTupleDeleteFailed` — OpenFGA sync.
- `RuntimeBootstrapFailed` — agent runtime SPI errors.
- `ShareReferenceGrantProjected`, `ShareReferenceGrantPruned`,
  `ShareReferenceGrantEnsured`, `ShareRebacTupleWritten`,
  `ShareRebacTupleDeleteFailed` — WorkspaceShare lifecycle.
- `SessionAttaching`, `SessionActive`, `SessionDraining`, `SessionEvicted`,
  `SessionCompleted`, `SessionDrained`, `SessionResumed` — session lifecycle.
- `SessionAttachRejectedNonInteractive`, `SessionDuplicate` — controller-side
  admission gate events (not emitted by a VAP).
- `TokenBudgetExceeded` — token budget gating (TD-P2-14).

Status conditions on `Workspace`: `Ready`, `Progressing`, `NetworkIsolated`,
`SessionStorageReady`. See workspace_types.go:177-186.

Status conditions on `WorkspaceSession`: `Ready`, `Progressing`, `Attached`,
`TokenBudgetWithinLimit`, `TokenBudgetExceeded`. See
workspacesession_controller.go:59-72.

## Known limitations

- **Egress NetworkPolicy does not pin a destination port.** The egress rule to the
  Envoy AI Gateway pods omits a port number. Kubernetes `NetworkPolicy` port matching
  applies to the destination pod's container port after DNAT, not the Service port the
  client dials; the upstream Envoy Gateway chart selects the listener port (10443 in
  v1.4.x) independently of the `443` constant declared in the controller. The security
  boundary is therefore namespace + pod-selector only until a service-port-aware CNI
  (Cilium `EnableServiceTopology`, Calico named ports) is configured. Tracked as TD-P2-X.
  See workspace_controller.go:478-508.
- `spec.interactive` is immutable after creation; changing session model requires
  deleting and re-creating the Workspace.
- `WorkspaceSession` fields `workspaceRef`, `attachSubject`, `sessionName`, and
  `mode` are immutable (XValidation enforced); a new session object is required to
  change any of them.
- `WorkspaceShare` grantee list is capped at 64 entries by CRD validation.

## Change history

- demo-D1: initial implementation — Workspace, WorkspaceSession, WorkspaceShare
  controllers wired with SSA, NetworkPolicy isolation, ReBAC tuple sync, and session
  lifecycle FSM (docs/plans/demo/D1-controller-wiring.md).

## References

- Design: docs/designs/02-workspace-model.md
- Design: docs/designs/08b-ii-session-crd-spec.md
- Design: docs/designs/12-network-isolation.md
- Spec: docs/specs/keese.ai-v1alpha1-workspace.md
- Spec: docs/specs/keese.ai-v1alpha1-workspace-ii-session.md
- Spec: docs/specs/keese.ai-v1alpha1-workspace-ii-share.md
- Plan: docs/plans/demo/D1-controller-wiring.md
- Source: internal/controller/keese/workspace_controller.go
- Source: internal/controller/keese/workspacesession_controller.go
- Source: internal/controller/keese/workspaceshare_controller.go
- Source: internal/controller/keese/workspace_events.go
