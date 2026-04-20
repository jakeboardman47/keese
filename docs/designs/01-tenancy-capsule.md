<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: tenancy
depends: [docs/plans/scaffolding-plan.md]
related_skills: [doc-authoring]
status: current
last_verified: 2026-04-20
rollback: |
  Mode A: revert keese operator image via OLM `replaces` chain; no Capsule change needed.
  Mode B: pin Capsule Helm chart to the previously validated version via helmfile.lock;
  revert keese operator image via OLM `replaces` chain; run `make bootstrap-infra`
  against the pinned lock to restore. No keese CRD schema migration required — keese
  owns no Tenant CRD; Capsule's own upgrade contract applies.
---

# 01 — Tenancy via Capsule

## Context

A **Tenant** is an organizational/identity concept — it does NOT map 1:1 to a
namespace. A tenant owns one or more namespaces (no prescribed naming); a
**Workspace** is a CR living inside any tenant-owned namespace; multiple Workspaces
may share a namespace. Tenant membership is expressed through RBAC. Keese owns no
`Tenant` CRD (D23) and consumes `capsule.clastix.io/v1beta2/Tenant` when present
(D3). Two deployment modes: **Mode A** (single-namespace, no Capsule) and
**Mode B** (multi-namespace, Capsule present). v1 runtime isolation uses standard
Kubernetes primitives only.

## Decisions

**D-01.1 — Namespace layout and tenant membership.**
Keese does not prescribe namespace naming. A tenant owns any set of namespaces;
in Mode B each carries label `capsule.clastix.io/tenant: <tenant>` so Capsule's
Tenant CR (label-selector) claims it. Multiple Workspaces may coexist in one
namespace; Workspace names are not baked into namespace names. Tenant membership is
expressed purely through RBAC: users with `keese-tenant-editor` ClusterRole binding
in a namespace may create Workspaces there. The Workspace controller creates only
Workspace-scoped resources (SA, PVC, HTTPRoute, OpenFGA tuples, NetworkPolicy);
namespace creation is an operator or platform-team action.

**D-01.2 — Capsule is opt-in; two deployment modes.**
Flag `--capsule-integration=auto|on|off` (default `auto`) detects Capsule CRDs at
startup via API discovery. Mode A (no Capsule): keese uses namespace-local quota via
the Workspace mutating webhook; no cross-namespace aggregation. Mode B (Capsule
present): the Workspace controller labels namespaces to enter the Capsule Tenant
tree; Capsule reconciles tenant-level quota, LimitRange, and RBAC projections. P7
helmfile installs Capsule by default; production single-namespace tenants do not
need it.

**D-01.3 — Kyverno ClusterPolicy for PSS + keese-specific pod admission.**
Every workspace namespace receives `pod-security.kubernetes.io/enforce: restricted`.
Keese also ships a `ClusterPolicy` (Kyverno, Enforce mode) in
`config/overlays/base/kyverno-policies/` adding defense-in-depth: deny
`hostNetwork/PID/IPC`, `privileged`, `allowPrivilegeEscalation`; require
`readOnlyRootFilesystem: true` for agent pods (rule 05.11); require `runAsNonRoot:
true`; require label `keese.ai/workspace=<name>` for admission in keese-managed
namespaces. Audit mode reserved for break-glass only. Goose complies with
`readOnlyRootFilesystem: true` — all writes target the workspace PVC mount (SQLite
session store) or emptyDir for tmp; both are writable even with a read-only root
filesystem. No workarounds needed.

**D-01.4 — Quota / LimitRange division.**
In Mode B, Capsule's `Tenant.spec.resourceQuota` and `.limitRanges` carry
tenant-level ceilings set by the platform team. In both modes, the Workspace
mutating webhook injects workspace-level defaults: one `ResourceQuota` sized from
`Workspace.spec.resources` (or the tenant default if absent) and one `LimitRange`
enforcing `requests == limits` for agent containers. Workspace quota is always a
subset of tenant quota; the VAP on Workspace creation validates this via CEL before
persisting.

**D-01.5 — TenantResource propagation and keese NetworkPolicy / BackendSecurityPolicy.**
Keese does not use `TenantResource` for NetworkPolicy or `BackendSecurityPolicy`.
The Workspace controller applies NetworkPolicy via SSA (`fieldOwner:
keese-workspace-controller`) directly into the workspace namespace.
`BackendSecurityPolicy` references live in a designated namespace and are projected
via `ReferenceGrant`. Capsule `TenantResource` is reserved for platform use; double-
propagating NetworkPolicy to workspace namespaces risks SSA field ownership
conflicts.

**D-01.6 — Capsule version upgrade contract (Mode B only).**
Keese pins Capsule via `helmfile.lock`. Before upgrading: (1) run
`scripts/check-capsule-api.sh` which diffs `capsule.clastix.io/v1beta2/Tenant` CRD
schema between versions using `kubectl diff`; (2) if any keese-authored field
mapping changes, an architect-signed commit adding
`docs/plans/migration-capsule-<version>.md` is required before the helmfile pin
bumps; (3) CI runs the Capsule upgrade matrix in `e2e.yaml` against the new version
before merge.

## ClusterRole scaffold

Nine ClusterRoles created by the Workspace controller; in Mode B bound per-namespace
via Capsule `additionalRoleBindings`; in Mode A via direct `RoleBinding`:

| ClusterRole | Verbs | Resources |
|---|---|---|
| `keese-workspace-viewer` | get, list, watch | workspaces, workspaceshares, agentruntimes, workflows, workflowruns (incl. status) |
| `keese-workspace-editor` | viewer + create, update, patch, delete | workspaces, workspaceshares |
| `keese-runtime-invoker` | create on workflowruns; get/list/watch workflows, workspaces, agentruntimes | (as listed) |
| `keese-runtime-admin` | full CRUD | agentruntimes, runtimeextensions |
| `keese-guardrail-author` | full CRUD | guardrailbindings |
| `keese-memory-admin` | full CRUD | memories, sharedmemories |
| `keese-recipe-publisher` | full CRUD | recipes, recipesources |
| `keese-observability-viewer` | get, list, watch | tokenbudgets, events |
| `keese-transport-admin` | full CRUD | transports |

Aggregates: `keese-tenant-admin` (editor + invoker + guardrail-author + memory-admin
+ recipe-publisher + observability-viewer + transport-admin); `keese-tenant-viewer`
(all viewers).

Revisit trigger: revisit after 02-workspace-model.md is authored — specifically
when WorkspaceShare semantics and per-workspace RBAC are pinned down.

## Trade-offs

| Option | Chosen | Rationale |
|---|---|---|
| Own a keese `Tenant` CRD | No | Duplicates Capsule; maintenance burden; D23 forbids it. |
| Mandate Capsule for all deployments | No | Single-namespace installs need no Capsule; opt-in reduces ops surface. |
| Capsule `TenantResource` for NP propagation | No | SSA field ownership ambiguity; propagation ordering races. |
| vcluster for hard isolation at v1 | No | Out of scope for v1; adds operator complexity before primitives are validated. |
| PSS label only, no policy engine | No | Kyverno adds defense-in-depth for agent-specific invariants PSS cannot enforce. |
| Capsule `additionalRoleBindings` for keese RBAC | Yes (Mode B) | Single reconciliation loop; avoids per-namespace ClusterRoleBinding proliferation. |

## Failure modes

| Failure | Detection | Mitigation |
|---|---|---|
| Capsule controller unavailable (Mode B) | Workspace `Phase=Pending`; OTEL span `capsule.tenant.ready=false` | Workspace reconciler exponential backoff; alert on `WorkspaceStuck` event after 5m |
| Kyverno policy enforcement gap | Pod rejected with `ClusterPolicy` reason; event `KyvernoPolicyRejected` | Kyverno must be healthy before keese operator starts; readiness probe gates start |
| Capsule API version breaks (v1beta3) | `scripts/check-capsule-api.sh` fails in CI | Pin blocks; migration doc required before upgrade |
| Workspace quota exceeds tenant quota | VAP rejects Workspace before persist | Clear CEL error message naming the violated dimension |
| SSA field conflict (Capsule overwrites keese NP) | Controller observes unexpected NP change; conflict counter OTEL metric | Platform team must not configure `TenantResource` targeting workspace namespaces |
| Mode A / Mode B detection wrong at startup | Operator logs `capsule-integration` discovery result at INFO level | `--capsule-integration=on|off` override flag for explicit control |

## Future extension: hard isolation

Vcluster is planned as a bolt-on for `Workspace.spec.isolation: hard` once v1
namespace isolation is validated. No vcluster API, finalizer, or failure mode is
designed here.

## Upgrade / rollback

Rollback path is in frontmatter. For in-place Capsule patch upgrades (no CRD schema
delta): update helmfile.lock, run `helmfile sync`, no keese restart required. For
minor/major Capsule upgrades: follow migration doc process; run `make smoke` after.

## Observability

- OTEL span per Workspace reconcile: `capsule.tenant.ready` (Mode B),
  `workspace.quota.applied`, `kyverno.policy.applied`.
- Event reasons (enumerated in `internal/controller/workspace/events.go`):
  `TenantNotFound`, `QuotaApplied`, `NetworkPolicyApplied`, `WorkspaceStuck`,
  `KyvernoPolicyRejected`, `CapsuleIntegrationMode`.
- Metrics: `keese_workspace_reconcile_duration_seconds{phase}`,
  `keese_workspace_capsule_conflict_total`, `keese_workspace_kyverno_reject_total`.

## Refs

- [Capsule v1beta2 Tenant API](https://capsule.clastix.io/docs/general/references/tenant-crd)
- [Kyverno ClusterPolicy](https://kyverno.io/docs/kyverno-policies/)
- [20-api-group-layout.md](20-api-group-layout.md)
- [02-workspace-model.md](02-workspace-model.md)
- [12-network-isolation.md](12-network-isolation.md)
- [05b-credential-injection-patterns.md](05b-credential-injection-patterns.md)
- [../plans/rubric.md](../plans/rubric.md)

## Iteration log

### Iteration 1 — 2026-04-20 — score 87.5 (SHIP)

Gaps: (1) `check-capsule-api.sh` not yet authored; (2) envtest/kuttl test names
awaiting spec phase; (3) vcluster pin strategy unspecified. Human reviewer rewrote
D-01.1, D-01.3, and added Capsule opt-in / Kyverno requirements → iter-2.

### Iteration 2 — 2026-04-20

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Two deployment modes (A/B) explicit; tenant vs. namespace distinction clear; Workspace / namespace relationship corrected. |
| 2 | Architecture fit | 10 | 1.0 | 10 | D3/D23 honored; opt-in Capsule consistent with compose-over-replicate; vcluster deferred cleanly. |
| 3 | Security posture | 15 | 1.0 | 15 | PSS restricted + Kyverno defense-in-depth; goose PVC/emptyDir write path confirmed compliant; no wildcard RBAC; SSA fieldOwner; NP fail-closed. |
| 4 | Automatability | 10 | 0.5 | 5 | `check-capsule-api.sh` still TBD; Kyverno ClusterPolicy path (`config/overlays/base/kyverno-policies/`) stated but not scaffolded — acceptable pre-gate. |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Kyverno reject event + metric named; mode-detection failure case enumerated; envtest/kuttl test names still awaiting spec phase. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Six failure modes including Kyverno unavailability and mode-detection override; detection + mitigation for each. |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | Under 200 lines; split avoided; single responsibility preserved. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; frontmatter updated (depends, rollback covers both modes); no broken links. |
| 9 | Observability | 5 | 1.0 | 5 | OTEL spans, events, metrics updated to reflect Kyverno + mode-detection signals. |
| 10 | Operational readiness | 10 | 1.0 | 10 | Two-mode rollback documented in frontmatter; upgrade contract scoped to Mode B; Capsule early implementation called out in next steps. |
| | **Total** | 100 | | **92.5** | |

Verdict: SHIP (92.5 ≥ 92 target)

Top gaps: (1) `check-capsule-api.sh` not yet authored — pre-gate acceptable;
(2) envtest/kuttl test names awaiting spec phase; (3) Kyverno ClusterPolicy
manifests deferred to post-gate controller phase. Build Capsule integration early
in the controller implementation phase so Mode A vs. Mode B can be tested before
specs freeze; Kyverno manifests land with the first Workspace reconciler.
