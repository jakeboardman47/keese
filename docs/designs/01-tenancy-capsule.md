<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: tenancy
depends: []
related_skills: [doc-authoring]
status: current
last_verified: 2026-04-20
rollback: Pin Capsule Helm chart to the previously validated version via helmfile.lock;
  revert keese operator image to the prior tag via OLM `replaces` chain; run
  `make bootstrap-infra` against the pinned lock to restore. No keese CRD schema
  migration is required because keese owns no Tenant CRD — Capsule's own upgrade
  contract applies.
---

# 01 — Tenancy via Capsule

## Context

Keese serves multiple tenants on a shared cluster. Tenants own a namespace tree;
workspaces run inside those namespaces. Rather than maintaining a keese `Tenant` CRD
(D23 — compose over replicate), keese consumes `capsule.clastix.io/v1beta2/Tenant`
directly (D3). Capsule reconciles namespace ownership, `ResourceQuota`, `LimitRange`,
`NetworkPolicy`, RBAC, and scheduling constraints. Keese's Workspace controller adds
workspace-scoped resources on top (SA, PVC, HTTPRoute, OpenFGA tuples) and labels
namespaces to opt them into Capsule's tenant tree. Hard isolation uses vcluster as a
separate virtual cluster, opted in via `Workspace.spec.isolation: hard`.

## Decisions

**D-01.1 — Namespace layout per tenant.**
Every tenant gets a root namespace `keese-<tenant>` and zero or more workspace
namespaces `keese-<tenant>-<workspace>`. The root namespace holds tenant-level
`BackendSecurityPolicy` references and shared `ConfigMap` resources. Workspace
namespaces are annotated `capsule.clastix.io/tenant: <tenant>` so Capsule claims
them. Keese's Workspace controller creates workspace namespaces; Capsule reconciles
quota and policy templates into them automatically.

**D-01.2 — Capsule additionalRoleBindings and keese RBAC.**
Keese uses Capsule's `Tenant.spec.additionalRoleBindings` to bind two keese roles
into every tenant namespace: `keese-workspace-viewer` (get/list/watch Workspace,
WorkspaceShare) and `keese-runtime-invoker` (create WorkflowRun). Tenant admins
may add further bindings; keese's own service account (`keese-operator`) holds
`cluster-admin`-equivalent only inside tenant namespaces via a ClusterRole scoped
by `ResourceNames`, not a wildcard. RBAC markers on every reconciler enumerate
exact verbs and resources; no `resources: ["*"]` or `verbs: ["*"]` (rule 04.9).

**D-01.3 — vcluster lifecycle ownership for `isolation: hard`.**
The Workspace controller owns the vcluster lifecycle end-to-end. When
`Workspace.spec.isolation: hard`, the Workspace reconciler provisions a vcluster
`VirtualCluster` CR (loft-labs/vcluster operator) in the tenant namespace, waits
for `Ready`, then projects workspace resources into the virtual cluster via a
dedicated kubeconfig Secret mounted to the workspace sidecar only — never to the
agent pod (rule 05.1). On workspace deletion the finalizer `finalizers.workspace.
operator.keese.ai/vcluster` blocks until the VirtualCluster CR is deleted and the
kubeconfig Secret is wiped. Lifecycle ownership does not extend to the vcluster
operator itself; that is a Helmfile-managed dependency (P7).

**D-01.4 — Quota / LimitRange / PSS division.**
Capsule's `Tenant.spec.resourceQuota` and `.limitRanges` carry tenant-level
ceilings set by the platform team. Keese injects workspace-level defaults via a
mutating webhook on `Workspace` creation: one `ResourceQuota` sized from
`Workspace.spec.resources` (or the tenant default if absent) and one `LimitRange`
enforcing `requests == limits` for agent containers. PSS label `pod-security.
kubernetes.io/enforce: restricted` is set on every workspace namespace by the
Workspace controller. Workspace quota is always a subset of tenant quota; the VAP
on Workspace creation validates this before persisting.

**D-01.5 — TenantResource propagation and keese NetworkPolicy / BackendSecurityPolicy.**
Keese does not use `TenantResource` propagation for NetworkPolicy or
`BackendSecurityPolicy`. Instead: (a) the Workspace controller applies workspace
NetworkPolicy via SSA with `fieldOwner: keese-workspace-controller` directly into
the workspace namespace; (b) `BackendSecurityPolicy` references live in the root
tenant namespace and are projected into workspace namespaces via `ReferenceGrant`.
This avoids propagation ordering races and keeps field ownership unambiguous.
Capsule `TenantResource` is reserved for platform team use (e.g. injecting a
shared monitoring `NetworkPolicy`) — keese docs warn against double-propagating
NetworkPolicy to workspace namespaces since Capsule would overwrite SSA fields.

**D-01.6 — Capsule version upgrade contract.**
Keese pins Capsule via `helmfile.lock`. Before upgrading: (1) run
`scripts/check-capsule-api.sh` which diffs `capsule.clastix.io/v1beta2/Tenant`
CRD schema between old and new Capsule versions using `kubectl diff`; (2) if any
keese-authored field mapping changes, an architect-signed commit adding a
`docs/plans/migration-capsule-<version>.md` is required before the helmfile pin
bumps; (3) CI runs the Capsule upgrade matrix in `e2e.yaml` against the new
version before merge. Because keese owns no Tenant CRD, Capsule CRD upgrades
are transparent as long as the fields keese reads or sets remain stable.

## Trade-offs

| Option | Chosen | Rationale |
|---|---|---|
| Own a keese `Tenant` CRD | No | Duplicates Capsule; maintenance burden; D23 forbids it. |
| Capsule `TenantResource` for NP propagation | No | SSA race; field ownership ambiguity. |
| vcluster lifecycle in a separate controller | No | Adds a controller binary; the Workspace FSM already owns isolation. |
| PSS via PodSecurityAdmission namespace label | Yes | GA in K8s 1.25+; no policy engine required for baseline isolation. |
| Capsule `additionalRoleBindings` for keese RBAC | Yes | Single reconciliation loop; avoids a separate ClusterRoleBinding per namespace. |

## Failure modes

| Failure | Detection | Mitigation |
|---|---|---|
| Capsule controller unavailable | Workspace `Phase=Pending`; OTEL span `capsule.tenant.ready=false` | Workspace reconciler exponential backoff; alert on `WorkspaceStuck` event after 5m |
| vcluster VirtualCluster not ready within 5m | Workspace `Phase=Degraded`; event `VirtualClusterTimeout` | Finalizer blocks delete; operator retries; page on stuck workspace after 10m |
| Capsule API version breaks (v1beta3) | `scripts/check-capsule-api.sh` fails in CI | Pin blocks; migration doc required before upgrade |
| Workspace quota exceeds tenant quota | VAP rejects Workspace before persist | Clear CEL error message naming the violated dimension |
| SSA field conflict (Capsule overwrites keese NP) | Controller observes unexpected NP change; conflict counter OTEL metric | Platform team must not configure `TenantResource` targeting workspace namespaces |

## Upgrade / rollback

Rollback path is in frontmatter. For in-place Capsule patch upgrades (no CRD
schema delta): update helmfile.lock, run `helmfile sync`, no keese restart
required. For minor/major Capsule upgrades: follow migration doc process; run
`make smoke` after; if smoke fails, revert helmfile.lock and re-sync.

## Observability

- OTEL span per Workspace reconcile: `capsule.tenant.ready`, `vcluster.ready`
  (if hard isolation), `workspace.quota.applied`.
- Event reasons (enumerated in `internal/controller/workspace/events.go`):
  `TenantNotFound`, `VirtualClusterTimeout`, `QuotaApplied`, `NetworkPolicyApplied`,
  `WorkspaceStuck`.
- Metrics: `keese_workspace_reconcile_duration_seconds{phase}`,
  `keese_workspace_capsule_conflict_total` (SSA conflict counter).

## Refs

- [Capsule v1beta2 Tenant API](https://capsule.clastix.io/docs/general/references/tenant-crd)
- [vcluster operator CRDs](https://www.vcluster.com/docs/using-vcluster/access/operator)
- [20-api-group-layout.md](20-api-group-layout.md)
- [02-workspace-model.md](02-workspace-model.md)
- [12-network-isolation.md](12-network-isolation.md)
- [05b-credential-injection-patterns.md](05b-credential-injection-patterns.md)
- [../plans/rubric.md](../plans/rubric.md)

## Iteration log

### Iteration 1 — 2026-04-20

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Goal, inputs, decisions, exit criteria explicit. |
| 2 | Architecture fit | 10 | 1.0 | 10 | D3/D23 honored; no keese Tenant CRD; compose over replicate. |
| 3 | Security posture | 15 | 1.0 | 15 | PSS restricted; no wildcard RBAC; SSA fieldOwner; vcluster kubeconfig never on agent pod; NetworkPolicy fail-closed. |
| 4 | Automatability | 10 | 0.5 | 5 | `check-capsule-api.sh` referenced but not yet written; migration script TBD. |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Failure modes named; acceptance tests for Capsule upgrade matrix stated; no unit/envtest test names yet (awaits gate open). |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Five failure modes with detection + mitigation. |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | Under 200 lines; single responsibility; skill pointers via refs. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; frontmatter complete; rollback filled; no broken links. |
| 9 | Observability | 5 | 1.0 | 5 | OTEL spans, event reasons, metrics declared. |
| 10 | Operational readiness | 10 | 1.0 | 10 | Rollback path concrete; upgrade contract in D-01.6; helmfile.lock strategy. |
| | **Total** | 100 | | **87.5** | |

Verdict: SHIP (87.5 ≥ 85)

Top gaps:
1. `scripts/check-capsule-api.sh` is referenced but not yet authored — blocked by design-gate, acceptable.
2. Envtest + kuttl test names for Workspace-Capsule interaction not enumerated — awaits spec authoring phase.
3. vcluster operator version pin strategy not specified (helmfile.lock covers Capsule, vcluster pin should follow same pattern).

Next step: Human reviewer to confirm D-01.3 (vcluster ownership in Workspace reconciler vs. separate controller) and D-01.5 (no TenantResource for NetworkPolicy). Iteration 2 should add test names and lock vcluster pin strategy once reviewer approves.
